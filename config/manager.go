package config

import (
	"fmt"
	"strings"
	"sync"

	"switchfree/proxy"
)

// Manager 配置管理器，线程安全，支持热重载
type Manager struct {
	mu     sync.RWMutex
	config *Config
	path   string
}

// NewManager 创建配置管理器
func NewManager(path string) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		// 即使出错也返回可用配置（已降级为默认）
		fmt.Printf("[switch-free] 配置加载: %v\n", err)
	}
	return &Manager{
		config: cfg,
		path:   cfg.path,
	}, nil
}

// Get 获取当前配置（线程安全，返回克隆避免外部修改）
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Clone()
}

// Resolve 解析请求模型（线程安全，直接用当前配置的 Resolve）
func (m *Manager) Resolve(requestedModel string) []proxy.ModelRef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Resolve(requestedModel)
}

// GetMode 获取当前模式
func (m *Manager) GetMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.GetMode()
}

// GetAPIKey 获取当前 apiKey（供代理鉴权使用）
func (m *Manager) GetAPIKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.GetAPIKey()
}

// SaveConfig 保存并热加载新配置
// 调用方提供新 *Config 对象（通常来自前端），由 Manager 校验 + 替换 + 写盘
func (m *Manager) SaveConfig(newCfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 校验
	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("配置校验失败: %w", err)
	}

	// 保留路径
	newCfg.path = m.config.path

	// 原子替换
	m.config = newCfg

	// 写盘
	if err := newCfg.Save(); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	return nil
}

// ResetConfig 重置为默认配置（保留现有 apiKey，避免客户端接入中断）
func (m *Manager) ResetConfig() error {
	m.mu.RLock()
	oldKey := m.config.APIKey
	m.mu.RUnlock()
	d := Defaults()
	if oldKey != "" {
		d.APIKey = oldKey
	} else {
		d.APIKey = generateAPIKey()
	}
	return m.SaveConfig(d)
}

// GetConfig 获取当前配置（供前端 GetConfig 使用，返回指针）
func (m *Manager) GetConfig() *Config {
	return m.Get()
}

// ====== 运行模式方案（Preset）======
//
// 方案是快照语义：保存时冻结当前运行模式配置，切换时覆盖回当前配置。
// 切换后继续编辑不会回写方案，需再次 SavePreset 同名覆盖。
//
// 以下方法一律走 Get() 取克隆 -> 改 -> SaveConfig 的模式。
// 不能自己持 m.mu 再调 SaveConfig —— 后者会 m.mu.Lock()，会死锁。

// findPreset 返回方案在切片中的下标，不存在返回 -1
func findPreset(presets []Preset, name string) int {
	for i, p := range presets {
		if p.Name == name {
			return i
		}
	}
	return -1
}

// SavePreset 把当前运行模式配置存为方案快照；同名则覆盖
func (m *Manager) SavePreset(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("方案名不能为空")
	}

	cfg := m.Get()
	snap := Preset{
		Name:            name,
		Mode:            cfg.Mode,
		AutoChain:       copyChain(cfg.AutoChain),
		ManualFallbacks: copyFallbacks(cfg.ManualFallbacks),
		GlobalFallback:  cfg.GlobalFallback,
	}

	if idx := findPreset(cfg.Presets, name); idx >= 0 {
		cfg.Presets[idx] = snap
	} else {
		cfg.Presets = append(cfg.Presets, snap)
	}
	cfg.ActivePreset = name

	return m.SaveConfig(cfg)
}

// ApplyPreset 应用方案到当前配置（覆盖并立即生效）
func (m *Manager) ApplyPreset(name string) error {
	cfg := m.Get()
	idx := findPreset(cfg.Presets, name)
	if idx < 0 {
		return fmt.Errorf("方案不存在: %s", name)
	}

	p := cfg.Presets[idx]
	cfg.Mode = p.Mode
	cfg.AutoChain = copyChain(p.AutoChain)
	cfg.ManualFallbacks = copyFallbacks(p.ManualFallbacks)
	cfg.GlobalFallback = p.GlobalFallback
	cfg.ActivePreset = name

	return m.SaveConfig(cfg)
}

// DeletePreset 删除方案；删的是当前激活方案时清空激活标记
func (m *Manager) DeletePreset(name string) error {
	cfg := m.Get()
	idx := findPreset(cfg.Presets, name)
	if idx < 0 {
		return fmt.Errorf("方案不存在: %s", name)
	}

	cfg.Presets = append(cfg.Presets[:idx], cfg.Presets[idx+1:]...)
	if cfg.ActivePreset == name {
		cfg.ActivePreset = ""
	}

	return m.SaveConfig(cfg)
}

// RenamePreset 重命名方案（当前配置内容不变）
func (m *Manager) RenamePreset(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("方案名不能为空")
	}

	cfg := m.Get()
	idx := findPreset(cfg.Presets, oldName)
	if idx < 0 {
		return fmt.Errorf("方案不存在: %s", oldName)
	}
	if newName != oldName && findPreset(cfg.Presets, newName) >= 0 {
		return fmt.Errorf("方案名已存在: %s", newName)
	}

	cfg.Presets[idx].Name = newName
	if cfg.ActivePreset == oldName {
		cfg.ActivePreset = newName
	}

	return m.SaveConfig(cfg)
}