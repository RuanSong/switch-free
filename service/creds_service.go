package service

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"switchfree/creds"
)

// CredsService 凭据管理服务（暴露给前端）
type CredsService struct {
	core *Core
}

func NewCredsService(core *Core) *CredsService {
	return &CredsService{core: core}
}

// AgentDetail agent 工具详情（前端 SetupGuide 渲染用）
type AgentDetail struct {
	Name        string `json:"name"`
	Upstream    string `json:"upstream"`
	Type        string `json:"type"` // "gui" | "cli"
	Desc        string `json:"desc"`
	DownloadURL string `json:"downloadUrl"`
	InstallCmd  string `json:"installCmd"`
	LoginCmd    string `json:"loginCmd"`
	LoginURL    string `json:"loginUrl"`
	Installed   bool   `json:"installed"`
	Valid       bool   `json:"valid"`
}

// GetAgents 返回全部 agent 工具及其当前状态（供 SetupGuide 渲染）
func (s *CredsService) GetAgents() []*AgentDetail {
	credStatus := s.core.GetCredStatus()
	result := make([]*AgentDetail, 0, len(creds.AgentRegistry))
	for i := range creds.AgentRegistry {
		a := &creds.AgentRegistry[i]
		detail := &AgentDetail{
			Name:        a.Name,
			Upstream:    a.Upstream,
			Type:        a.Type,
			Desc:        a.Desc,
			DownloadURL: a.DownloadURL,
			InstallCmd:  a.InstallCmd,
			LoginCmd:    a.LoginCmd,
			LoginURL:    a.LoginURL,
			Installed:   creds.IsAgentInstalled(a),
		}
		switch a.Upstream {
		case "joycode":
			detail.Valid = credStatus.JoyCode != nil && credStatus.JoyCode.Valid
		case "deveco":
			detail.Valid = credStatus.DevEco != nil && credStatus.DevEco.Valid
		case "opencode":
			detail.Valid = credStatus.OpenCode != nil && credStatus.OpenCode.Valid
		}
		result = append(result, detail)
	}
	return result
}

// GetCredStatus 获取三上游凭据状态
func (s *CredsService) GetCredStatus() *AllCredStatus {
	return s.core.GetCredStatus()
}

// RefreshCreds 强制刷新某上游凭据
func (s *CredsService) RefreshCreds(name string) error {
	err := s.core.RefreshCreds(name)
	s.core.emitCredChange()
	return err
}

// RefreshAllCreds 刷新全部凭据
func (s *CredsService) RefreshAllCreds() error {
	s.core.RefreshAllCreds()
	return nil
}

// GetLoginURL 获取某上游的登录页 URL
func (s *CredsService) GetLoginURL(name string) string {
	switch name {
	case "joycode":
		return "https://joycode.jd.com/portal/login"
	case "deveco":
		return "https://cn.devecostudio.huawei.com"
	case "opencode":
		return "https://opencode.ai"
	}
	return ""
}

// OpenDownloadURL 用系统浏览器打开某上游的下载/官网页
func (s *CredsService) OpenDownloadURL(name string) {
	agent := creds.FindAgent(name)
	if agent == nil || agent.DownloadURL == "" {
		return
	}
	openBrowser(agent.DownloadURL)
}

// OpenLoginURL 用系统浏览器打开登录页
func (s *CredsService) OpenLoginURL(name string) {
	url := s.GetLoginURL(name)
	if url == "" {
		return
	}
	_ = application.Get() // 保持引用，OpenLoginURL 可在无 app 时也尝试打开
	openBrowser(url)
}
