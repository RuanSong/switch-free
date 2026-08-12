package freeapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"switchfree/paths"
)

// ProviderModel 单个免费模型（模型级 verified/healthy 状态，支持逐个评测 + 健康监控）
type ProviderModel struct {
	ID         string `json:"id"`         // 原始 model id（如 "llama-3.3-70b"）
	Context    int    `json:"context"`    // 上下文窗口
	Verified   bool   `json:"verified"`   // 是否评测通过（未通过不参与路由）
	Healthy    bool   `json:"healthy"`    // 后台监控状态（每 5 分钟探测；false=权重降级）
	FailCount  int    `json:"failCount"`  // 连续失败次数（监控用）
}

// ProviderConfig 单个免费 API 供应商配置
type ProviderConfig struct {
	ID           string          `json:"id"`           // 唯一 id：目录 slug 或 custom-<随机>
	Name         string          `json:"name"`         // 显示名
	BaseURL      string          `json:"baseURL"`      // OpenAI 兼容地址
	APIKey       string          `json:"apiKey"`       // 密钥（本地存储，不上 GitHub）
	GetAPIKeyURL string          `json:"getAPIKeyURL"` // 申请 key 地址
	MaxContext   int             `json:"maxContext"`   // 最大上下文（可选）
	Custom       bool            `json:"custom"`       // 是否手动自定义添加
	Verified     bool            `json:"verified"`     // provider 是否至少评测通过一个模型
	Models       []ProviderModel `json:"models"`       // 该 provider 的模型（逐个评测通过才加）
}

// InternalID 生成代理内部模型 id（free/<providerID>/<modelID>，前缀隔离避免重名）
func (p *ProviderConfig) InternalID(modelID string) string {
	return "free/" + p.ID + "/" + modelID
}

// Config 独立文件内容（多供应商配置）
type Config struct {
	Providers map[string]*ProviderConfig `json:"providers"` // key = provider id
}

// Manager 免费 API 配置管理器（线程安全）
type Manager struct {
	mu     sync.RWMutex
	config *Config
	path   string
}

// NewManager 创建并加载配置（文件不存在时用空配置）
func NewManager(path string) (*Manager, error) {
	if path == "" {
		path = paths.FreeAPIConfigPath()
	}
	cfg, err := load(path)
	if err != nil {
		// 文件缺失或损坏：用空配置兜底，不覆盖损坏文件
		cfg = &Config{Providers: map[string]*ProviderConfig{}}
	}
	return &Manager{config: cfg, path: path}, nil
}

// load 从文件读配置；文件不存在返回空配置
func load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Providers: map[string]*ProviderConfig{}}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 free_apis.json 失败: %w", err)
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]*ProviderConfig{}
	}
	return &cfg, nil
}

// Save 写盘（0600，密钥文件）
func (m *Manager) Save() error {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0600)
}

// Get 返回配置克隆（前端安全读取）
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfig(m.config)
}

// GetProviders 返回所有供应商（克隆）
func (m *Manager) GetProviders() map[string]*ProviderConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]*ProviderConfig, len(m.config.Providers))
	for k, v := range m.config.Providers {
		out[k] = cloneProvider(v)
	}
	return out
}

// GetProvider 返回单个供应商克隆
func (m *Manager) GetProvider(id string) *ProviderConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.config.Providers[id]
	if !ok {
		return nil
	}
	return cloneProvider(p)
}

// GetVerifiedModels 返回所有已评测通过（verified）的模型集合
// map[providerID][]ProviderModel（只含 verified）
func (m *Manager) GetVerifiedModels() map[string][]ProviderModel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]ProviderModel)
	for pid, p := range m.config.Providers {
		if !p.Verified {
			continue
		}
		for _, mo := range p.Models {
			if mo.Verified {
				out[pid] = append(out[pid], mo)
			}
		}
	}
	return out
}

// GetModel 获取某 provider 下某模型
func (m *Manager) GetModel(providerID, modelID string) *ProviderModel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.config.Providers[providerID]
	if !ok {
		return nil
	}
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			cp := p.Models[i]
			return &cp
		}
	}
	return nil
}

// UpsertProvider 新增或更新供应商（同 id 覆盖）
func (m *Manager) UpsertProvider(p *ProviderConfig) error {
	m.mu.Lock()
	if m.config.Providers == nil {
		m.config.Providers = map[string]*ProviderConfig{}
	}
	m.config.Providers[p.ID] = cloneProvider(p)
	m.mu.Unlock()
	return m.Save()
}

// RemoveProvider 删除供应商（其模型一并移除）
func (m *Manager) RemoveProvider(id string) error {
	m.mu.Lock()
	delete(m.config.Providers, id)
	m.mu.Unlock()
	return m.Save()
}

// AddVerifiedModel 评测通过后把模型加入（逐个加，不是全量）
// 同 id 覆盖；更新 provider 级 Verified 标记
func (m *Manager) AddVerifiedModel(providerID string, model ProviderModel) error {
	m.mu.Lock()
	p, ok := m.config.Providers[providerID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("供应商不存在: %s", providerID)
	}
	model.Verified = true
	model.Healthy = true // 刚评测通过默认健康
	model.FailCount = 0
	found := false
	for i := range p.Models {
		if p.Models[i].ID == model.ID {
			// 保留既有 healthy/failCount，更新能力信息
			prev := p.Models[i]
			model.Healthy = prev.Healthy
			model.FailCount = prev.FailCount
			p.Models[i] = model
			found = true
			break
		}
	}
	if !found {
		p.Models = append(p.Models, model)
	}
	p.Verified = true
	m.mu.Unlock()
	return m.Save()
}

// MarkHealth 更新某模型健康状态（监控用）
func (m *Manager) MarkHealth(providerID, modelID string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.config.Providers[providerID]
	if !ok {
		return
	}
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			p.Models[i].Healthy = healthy
			if healthy {
				p.Models[i].FailCount = 0
			} else {
				p.Models[i].FailCount++
			}
			return
		}
	}
}

// BumpFailCount 连续失败计数 +1；达到阈值后标记不健康（返回是否刚变不健康）
func (m *Manager) BumpFailCount(providerID, modelID string, threshold int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.config.Providers[providerID]
	if !ok {
		return false
	}
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			p.Models[i].FailCount++
			if p.Models[i].FailCount >= threshold && p.Models[i].Healthy {
				p.Models[i].Healthy = false
				return true // 刚变不健康
			}
			return false
		}
	}
	return false
}

// ResetFailCount 重置连续失败计数 + 标记健康
func (m *Manager) ResetFailCount(providerID, modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.config.Providers[providerID]
	if !ok {
		return
	}
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			p.Models[i].FailCount = 0
			p.Models[i].Healthy = true
			return
		}
	}
}

// HasValidCreds 某 provider 是否有可用凭据（verified 且 key 非空）
func (m *Manager) HasValidCreds(providerID string) bool {
	p := m.GetProvider(providerID)
	return p != nil && p.Verified && strings.TrimSpace(p.APIKey) != ""
}

// ====== 凭据管理（对齐 creds/opencode.go 模式） ======

// CredStatusInfo 凭据状态（对齐 creds.CredStatusInfo 的 JSON 形状）
type CredStatusInfo struct {
	Valid      bool   `json:"valid"`
	Installed  bool   `json:"installed"`
	KeyPreview string `json:"keyPreview,omitempty"`
	Source     string `json:"source"`
	LastCheck  string `json:"lastCheck"`
}

// CredStatus 返回某 provider 凭据状态
func (m *Manager) CredStatus(providerID string) *CredStatusInfo {
	p := m.GetProvider(providerID)
	if p == nil {
		return &CredStatusInfo{Valid: false, Installed: false, Source: "未配置"}
	}
	preview := ""
	if len(p.APIKey) > 8 {
		preview = p.APIKey[:8] + "..."
	} else if p.APIKey != "" {
		preview = "***"
	}
	valid := p.Verified && p.APIKey != ""
	return &CredStatusInfo{
		Valid:      valid,
		Installed:  valid,
		KeyPreview: preview,
		Source:     p.Name + " (" + p.BaseURL + ")",
		LastCheck:  time.Now().Format("15:04:05"),
	}
}

// cloneConfig 深拷贝配置
func cloneConfig(c *Config) *Config {
	out := &Config{Providers: map[string]*ProviderConfig{}}
	for k, v := range c.Providers {
		out.Providers[k] = cloneProvider(v)
	}
	return out
}

// cloneProvider 深拷贝供应商（含模型切片）
func cloneProvider(p *ProviderConfig) *ProviderConfig {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Models = make([]ProviderModel, len(p.Models))
	copy(cp.Models, p.Models)
	return &cp
}
