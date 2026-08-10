package creds

import (
	"os"
	"path/filepath"
	"strings"

	"switchfree/paths"
)

// AgentType agent 工具类型，决定前端引导方式
const (
	AgentTypeGUI = "gui" // GUI 应用（如 JoyCode），引导方式是下载安装包
	AgentTypeCLI = "cli" // CLI 工具（如 DevEco/OpenCode），引导方式是一行安装命令
)

// AgentInfo agent 工具元数据（可配置注册表的一条）
// 加新 agent 工具只需往 AgentRegistry 追加一条，前端自动渲染对应引导卡片
type AgentInfo struct {
	Name        string   // 显示名，如 "JoyCode"
	Upstream    string   // 上游标识，如 "joycode"，与 CredStatusInfo 对应
	Type        string   // AgentTypeGUI | AgentTypeCLI
	Desc        string   // 一句话描述
	DownloadURL string   // 官网/下载页
	InstallCmd  string   // CLI 安装命令（GUI 类型为空）
	LoginCmd    string   // 登录命令（GUI 类型为"打开客户端扫码"之类提示）
	LoginURL    string   // 浏览器登录页 URL
	ProbePaths  []string // 凭据文件探测路径（判断 Installed），支持 ~ 展开
}

// AgentRegistry 已支持的 agent 工具注册表
// 后续要加更多 agent 工具，往这里追加一条 AgentInfo 即可，无需改其他逻辑
var AgentRegistry = []AgentInfo{
	{
		Name:        "JoyCode",
		Upstream:    "joycode",
		Type:        AgentTypeGUI,
		Desc:        "京东云 AI 编程工具（VS Code 套壳），pt_key 登录态存于 state.vscdb",
		DownloadURL: "https://joycode.jd.com",
		InstallCmd:  "", // GUI 应用，无 CLI 安装命令
		LoginCmd:    "打开 JoyCode 客户端扫码登录",
		LoginURL:    "https://joycode.jd.com/portal/login",
		ProbePaths: []string{
			"~/Library/Application Support/JoyCode/User/globalStorage/state.vscdb",
		},
	},
	{
		Name:        "DevEco Code",
		Upstream:    "deveco",
		Type:        AgentTypeCLI,
		Desc:        "华为 DevEco Code（基于 OpenCode），OAuth 三层加密存本地",
		DownloadURL: "https://cn.devecostudio.huawei.com",
		InstallCmd:  "npm i -g @deveco/deveco-code",
		LoginCmd:    "deveco auth login",
		LoginURL:    "https://cn.devecostudio.huawei.com",
		ProbePaths: []string{
			"~/.local/share/deveco/auth.json",
		},
	},
	{
		Name:        "OpenCode Zen",
		Upstream:    "opencode",
		Type:        AgentTypeCLI,
		Desc:        "OpenCode 开源 CLI 的免费模型通道，静态 apiKey 明文存本地",
		DownloadURL: "https://opencode.ai",
		InstallCmd:  "brew install opencode-ai/tap/opencode",
		LoginCmd:    "opencode auth login",
		LoginURL:    "https://opencode.ai",
		ProbePaths: []string{
			"~/.local/share/opencode/auth.json",
		},
	},
	{
		Name:        "WorkBuddy",
		Upstream:    "workbuddy",
		Type:        AgentTypeGUI,
		Desc:        "腾讯 CodeBuddy 桌面版，OAuth token 明文存本地，免费模型通道",
		DownloadURL: "https://workbuddy.app",
		InstallCmd:  "", // GUI 应用，无 CLI 安装命令
		LoginCmd:    "打开 WorkBuddy 客户端登录",
		LoginURL:    "https://copilot.tencent.com/login?platform=workbuddy",
		ProbePaths: []string{
			"~/Library/Application Support/CodeBuddyExtension/Data/Public/auth/workbuddy-desktop.info",
		},
	},
}

// FindAgent 按 upstream 名查 agent 元数据，找不到返回 nil
func FindAgent(upstream string) *AgentInfo {
	for i := range AgentRegistry {
		if AgentRegistry[i].Upstream == upstream {
			return &AgentRegistry[i]
		}
	}
	return nil
}

// IsAgentInstalled 探测 agent 的凭据文件是否存在（任一路径存在即视为已装）
// 优先用 paths 包的跨平台候选路径（与凭据加载同源，保证探测与加载一致）；
// ProbePaths 作为补充（含 ~ 展开），兼容历史硬编码路径
func IsAgentInstalled(agent *AgentInfo) bool {
	if agent == nil {
		return false
	}
	// 1. 跨平台候选路径（paths 包，与 EnsureCreds 加载用的是同一套）
	for _, p := range probeCandidates(agent.Upstream) {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	// 2. ProbePaths 补充（含 ~ 展开）
	for _, p := range agent.ProbePaths {
		expanded := expandPath(p)
		if _, err := os.Stat(expanded); err == nil {
			return true
		}
	}
	return false
}

// probeCandidates 按 upstream 返回跨平台凭据候选路径（委托 paths 包，单一真相）
func probeCandidates(upstream string) []string {
	switch upstream {
	case "joycode":
		return paths.JoyCodeVscdbCandidates()
	case "workbuddy":
		return paths.WorkBuddyInfoCandidates()
	case "deveco":
		return paths.DevEcoAuthCandidates()
	case "opencode":
		return paths.OpenCodeAuthCandidates()
	}
	return nil
}

// expandPath 展开路径开头的 ~ 为用户 HOME
func expandPath(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// FillAgentMeta 把 AgentRegistry 里对应 upstream 的安装/登录元数据注入 CredStatusInfo
// 三态判定：Installed 由 IsAgentInstalled 探测；元数据始终注入（前端按状态渲染）
func FillAgentMeta(info *CredStatusInfo, upstream string) {
	agent := FindAgent(upstream)
	if agent == nil {
		return
	}
	info.Installed = IsAgentInstalled(agent)
	info.AgentType = agent.Type
	info.InstallCmd = agent.InstallCmd
	info.LoginCmd = agent.LoginCmd
	info.DownloadURL = agent.DownloadURL
	info.LoginURL = agent.LoginURL
}