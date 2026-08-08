package config

import (
	"fmt"
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

// ResetConfig 重置为默认配置
func (m *Manager) ResetConfig() error {
	return m.SaveConfig(Defaults())
}

// GetConfig 获取当前配置（供前端 GetConfig 使用，返回指针）
func (m *Manager) GetConfig() *Config {
	return m.Get()
}