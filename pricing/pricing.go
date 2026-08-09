package pricing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"switchfree/paths"
)

// Price 单个模型的费率（每百万 token 的成本，美元）
type Price struct {
	ModelID          string  `json:"model_id"`
	DisplayName      string  `json:"display_name"`
	InputPerMillion  float64 `json:"input_cost_per_million"`
	OutputPerMillion float64 `json:"output_cost_per_million"`
	CacheRead        float64 `json:"cache_read_cost_per_million"`
	CacheCreation    float64 `json:"cache_creation_cost_per_million"`
}

// pricingFile 自有费率库文件
type pricingFile struct {
	Version int      `json:"version"`
	Prices  []*Price `json:"prices"`
}

// Manager 费率管理器（自有库，持久化 JSON，支持增删改查）
type Manager struct {
	mu     sync.RWMutex
	prices map[string]*Price // key = ModelID
	path   string            // 自有库文件路径
}

// DefaultPath 自有费率库默认路径
func DefaultPath() string {
	return filepath.Join(paths.AppConfigDir(), "pricing.json")
}

// NewManager 创建费率管理器
func NewManager() *Manager {
	return &Manager{
		prices: make(map[string]*Price),
		path:   DefaultPath(),
	}
}

// SetPath 自定义费率库路径（测试用）
func (m *Manager) SetPath(p string) { m.path = p }

// Load 加载费率库：
// - 自有库文件存在：直接加载
// - 不存在：从内置硬编码 DefaultRates 还原并保存，之后用自有库
// 不再依赖 cc-switch.db
func (m *Manager) Load() (imported bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 自有库存在则直接加载
	if data, err := os.ReadFile(m.path); err == nil {
		var f pricingFile
		if json.Unmarshal(data, &f) == nil && len(f.Prices) > 0 {
			m.prices = make(map[string]*Price, len(f.Prices))
			for _, p := range f.Prices {
				m.prices[p.ModelID] = p
			}
			return false, nil
		}
		// 文件存在但损坏/空 -> 用硬编码重建
		m.prices = make(map[string]*Price)
	}

	// 2. 从内置硬编码还原
	if len(DefaultRates) > 0 {
		m.prices = make(map[string]*Price, len(DefaultRates))
		for i := range DefaultRates {
			p := DefaultRates[i]
			m.prices[p.ModelID] = &p
		}
		// 保存到自有库
		if saveErr := m.saveLocked(); saveErr != nil {
			return true, fmt.Errorf("还原硬编码费率但保存自有库失败: %w", saveErr)
		}
		return true, nil
	}
	return false, fmt.Errorf("无内置费率且自有库为空")
}

// saveLocked 保存到自有库（需持锁）
func (m *Manager) saveLocked() error {
	if m.path == "" {
		m.path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	list := make([]*Price, 0, len(m.prices))
	for _, p := range m.prices {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ModelID < list[j].ModelID })
	f := pricingFile{Version: 1, Prices: list}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0600)
}

// IsLoaded 费率库是否已加载
func (m *Manager) IsLoaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.prices) > 0
}

// Count 费率条数
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.prices)
}

// ListPrices 列出全部费率（按 model_id 排序）
func (m *Manager) ListPrices() []*Price {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Price, 0, len(m.prices))
	for _, p := range m.prices {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ModelID < list[j].ModelID })
	return list
}

// GetPrice 查单条费率
func (m *Manager) GetPrice(modelID string) *Price {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.prices[modelID]; ok {
		cp := *p
		return &cp
	}
	return nil
}

// SetPrice 新增/更新费率并保存
func (m *Manager) SetPrice(p *Price) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prices[p.ModelID] = p
	return m.saveLocked()
}

// DeletePrice 删除费率并保存
func (m *Manager) DeletePrice(modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.prices, modelID)
	return m.saveLocked()
}

// normalize 归一化模型 id：小写 + 去常见后缀，匹配费率表标准 id
func normalize(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimSuffix(id, "-agent")
	id = strings.TrimSuffix(id, "-free")
	return id
}

// Lookup 查某模型的费率（精确 + 归一化），查不到返回 nil
func (m *Manager) Lookup(modelID string) *Price {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.prices[modelID]; ok {
		cp := *p
		return &cp
	}
	if p, ok := m.prices[normalize(modelID)]; ok {
		cp := *p
		return &cp
	}
	return nil
}

// CalculateCost 计算一次请求的费用（美元）
func (m *Manager) CalculateCost(modelID string, inputTokens, outputTokens int) (float64, *Price) {
	p := m.Lookup(modelID)
	if p == nil {
		return 0, nil
	}
	cost := float64(inputTokens)/1_000_000*p.InputPerMillion +
		float64(outputTokens)/1_000_000*p.OutputPerMillion
	return cost, p
}

// AgentLabel 上游显示名
func AgentLabel(upstream string) string {
	switch upstream {
	case "joycode":
		return "京东 JoyCode"
	case "deveco":
		return "华为 DevEco"
	case "opencode":
		return "OpenCode Zen"
	default:
		return upstream
	}
}

// parseFloat 解析字符串为浮点数，失败返回 0
func parseFloat(s string) float64 {
	var v float64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &v); err != nil {
		return 0
	}
	return v
}
