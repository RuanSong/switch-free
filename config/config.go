package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"switchfree/paths"
	"switchfree/proxy"
)

// AgentModels agent 分组内的一组模型
type AgentModels struct {
	Upstream string   `json:"upstream"`
	Models   []string `json:"models"`
}

// ModelRef 复用 proxy.ModelRef，避免类型不一致
// （config 包已 import proxy，无需重复定义）

// UpdateConfig 自动升级配置
type UpdateConfig struct {
	Enabled   bool        `json:"enabled"`             // 是否启用自动升级检查
	Provider  string      `json:"provider"`            // "github" | "custom"（默认 github）
	GitHub    GitHubConfig `json:"github"`             // GitHub Releases 配置
	UpdateURL string      `json:"updateUrl"`           // 自定义检查地址（优先于 github）
	Channel   string      `json:"channel"`             // "stable" | "beta"（默认 stable）
}

// GitHubConfig GitHub Releases 配置
type GitHubConfig struct {
	Owner string `json:"owner"` // GitHub 用户名/组织
	Repo  string `json:"repo"`  // 仓库名
	Token string `json:"token"` // 私有仓库 PAT（公开仓库可空）
}

// Config 代理运行配置
type Config struct {
	Mode            string                       `json:"mode"`            // "auto" | "manual"
	AutoChain       []AgentModels                `json:"autoChain"`       // auto 模式优先级链
	ManualFallbacks map[string][]proxy.ModelRef  `json:"manualFallbacks"` // 手动模式下模型的降级链
	GlobalFallback  proxy.ModelRef               `json:"globalFallback"`  // 全局兜底
	Port            int                          `json:"port"`            // 代理监听端口
	APIKey          string                       `json:"apiKey"`          // 客户端接入密钥（严格校验）
	AutoUpdate      UpdateConfig                 `json:"update"`          // 自动升级配置

	mu   sync.RWMutex `json:"-"`
	path string        `json:"-"`
}

// DefaultConfigPath 默认配置文件路径
func DefaultConfigPath() string {
	return filepath.Join(paths.AppConfigDir(), "config.json")
}

// DefaultPort 默认代理端口
const DefaultPort = 8787

// generateAPIKey 生成随机 apiKey，格式 rs-<uuid>
// 首次启动或老配置无此字段时调用
func generateAPIKey() string {
	return "rs-" + uuid.NewString()
}

// Defaults 返回默认配置（等价于当前硬编码行为）
func Defaults() *Config {
	return &Config{
		Mode: "auto",
		AutoChain: []AgentModels{
			{Upstream: "deveco", Models: []string{proxy.AutoModel}},
			{Upstream: "joycode", Models: []string{proxy.AutoModelJoyCodeFallback}},
		},
		ManualFallbacks: map[string][]proxy.ModelRef{},
		GlobalFallback:  proxy.ModelRef{Upstream: "joycode", Model: proxy.AutoModelJoyCodeFallback},
		Port:            DefaultPort,
		AutoUpdate: UpdateConfig{
			Enabled:  true,
			Provider: "github",
			GitHub: GitHubConfig{
				Owner: "RuanSong",
				Repo:  "switch-free",
			},
			Channel: "stable",
		},
	}
}

// Load 从文件加载配置；不存在则返回默认配置并自动写盘
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	c := Defaults()
	c.path = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// 文件不存在：生成 apiKey 后写默认配置
		c.APIKey = generateAPIKey()
		if err := c.Save(); err != nil {
			return c, fmt.Errorf("保存默认配置失败: %w", err)
		}
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := json.Unmarshal(data, c); err != nil {
		// 损坏的 JSON 用默认并覆盖
		*c = *Defaults()
		c.path = path
		c.APIKey = generateAPIKey()
		c.Save()
		return c, fmt.Errorf("配置 JSON 解析失败，已重置为默认: %w", err)
	}

	// 老配置无 apiKey 字段：先补全，避免被 Validate 当作非法配置重置
	keyGenerated := false
	if c.APIKey == "" {
		c.APIKey = generateAPIKey()
		keyGenerated = true
	}

	if err := c.Validate(); err != nil {
		*c = *Defaults()
		c.path = path
		c.APIKey = generateAPIKey()
		c.Save()
		return c, fmt.Errorf("配置校验失败，已重置为默认: %w", err)
	}

	// 补全的 apiKey 需持久化（升级用户首次启动）
	if keyGenerated {
		c.Save()
	}

	return c, nil
}

// Save 保存配置到磁盘
func (c *Config) Save() error {
	if c.path == "" {
		c.path = DefaultConfigPath()
	}
	// 确保目录存在
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0600); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// Validate 校验配置合法性
func (c *Config) Validate() error {
	// apiKey
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("apiKey 不能为空")
	}

	// 端口
	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("无效的端口: %d，应在 1024-65535 之间", c.Port)
	}

	// 升级配置
	if c.AutoUpdate.Provider != "" && c.AutoUpdate.Provider != "github" && c.AutoUpdate.Provider != "custom" {
		return fmt.Errorf("无效的更新 provider: %s", c.AutoUpdate.Provider)
	}
	if c.AutoUpdate.Channel != "" && c.AutoUpdate.Channel != "stable" && c.AutoUpdate.Channel != "beta" {
		return fmt.Errorf("无效的更新 channel: %s", c.AutoUpdate.Channel)
	}

	// 模式
	if c.Mode != "auto" && c.Mode != "manual" {
		return fmt.Errorf("无效的模式: %s，应为 auto 或 manual", c.Mode)
	}

	// auto 链
	if len(c.AutoChain) == 0 {
		return fmt.Errorf("autoChain 不能为空")
	}
	for _, ag := range c.AutoChain {
		if !isValidUpstream(ag.Upstream) {
			return fmt.Errorf("无效的 upstream: %s", ag.Upstream)
		}
		for _, m := range ag.Models {
			if !isValidModel(m) {
				// 仅警告，允许不存在的模型名（可能是新增的，不影响正常运行）
			}
		}
	}

	// 手动降级链
	for key, chain := range c.ManualFallbacks {
		if !isValidModel(key) {
			return fmt.Errorf("manualFallbacks 的 key 不是有效模型: %s", key)
		}
		for _, ref := range chain {
			if !isValidUpstream(ref.Upstream) {
				return fmt.Errorf("manualFallbacks[%s] 中无效的 upstream: %s", key, ref.Upstream)
			}
		}
	}

	// 全局兜底
	if !isValidUpstream(c.GlobalFallback.Upstream) {
		return fmt.Errorf("globalFallback 的 upstream 无效: %s", c.GlobalFallback.Upstream)
	}

	return nil
}

// Clone 深拷贝（线程安全）
func (c *Config) Clone() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cp := &Config{
		Mode:            c.Mode,
		AutoChain:       make([]AgentModels, len(c.AutoChain)),
		ManualFallbacks: make(map[string][]proxy.ModelRef, len(c.ManualFallbacks)),
		GlobalFallback:  c.GlobalFallback,
		Port:            c.Port,
		APIKey:          c.APIKey,
		AutoUpdate:      c.AutoUpdate,
		path:            c.path,
	}
	for i, ag := range c.AutoChain {
		cp.AutoChain[i] = AgentModels{
			Upstream: ag.Upstream,
			Models:   append([]string{}, ag.Models...),
		}
	}
	for k, v := range c.ManualFallbacks {
		cp.ManualFallbacks[k] = append([]proxy.ModelRef{}, v...)
	}
	return cp
}

// Update 原子更新配置（线程安全），自动保存
func (c *Config) Update(newCfg *Config) error {
	// 校验
	if err := newCfg.Validate(); err != nil {
		return err
	}
	newCfg.path = c.path

	c.mu.Lock()
	c.Mode = newCfg.Mode
	c.AutoChain = newCfg.AutoChain
	c.ManualFallbacks = newCfg.ManualFallbacks
	c.GlobalFallback = newCfg.GlobalFallback
	c.Port = newCfg.Port
	c.APIKey = newCfg.APIKey
	c.AutoUpdate = newCfg.AutoUpdate
	c.mu.Unlock()

	return c.Save()
}

// GetMode 线程安全获取当前模式
func (c *Config) GetMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Mode
}

// GetAPIKey 线程安全获取当前 apiKey
func (c *Config) GetAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.APIKey
}

// isValidUpstream 检查 upstream 名是否合法
func isValidUpstream(u string) bool {
	switch u {
	case "joycode", "deveco", "opencode", "workbuddy":
		return true
	}
	return false
}

// isValidModel 检查模型 id 是否在已知白名单中（宽松校验，允许自定义）
func isValidModel(m string) bool {
	// 检查所有已知模型
	if proxy.JoyCodeModelIDs[m] || proxy.DevEcoModelIDs[m] || proxy.OpenCodeModelIDs[m] {
		return true
	}
	// 用 label 反查也接受
	low := strings.ToLower(m)
	for _, v := range proxy.JoyCodeLabelToID {
		if strings.ToLower(v) == low {
			return true
		}
	}
	for _, v := range proxy.DevEcoLabelToID {
		if strings.ToLower(v) == low {
			return true
		}
	}
	return true // 宽松模式：允许不认识的模型名（可能是新增的）
}