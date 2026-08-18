package service

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"switchdev/pricing"
)

// PricingService 费率管理服务（暴露给前端）
type PricingService struct {
	mgr *pricing.Manager
}

func NewPricingService(mgr *pricing.Manager) *PricingService {
	return &PricingService{mgr: mgr}
}

// PricingItem 费率项（前端展示用）
type PricingItem struct {
	ModelID          string  `json:"modelId"`
	DisplayName      string  `json:"displayName"`
	InputPerMillion  float64 `json:"inputPerMillion"`
	OutputPerMillion float64 `json:"outputPerMillion"`
	CacheRead        float64 `json:"cacheRead"`
	CacheCreation    float64 `json:"cacheCreation"`
}

// ListPrices 列出全部费率
func (s *PricingService) ListPrices() []*PricingItem {
	list := s.mgr.ListPrices()
	result := make([]*PricingItem, 0, len(list))
	for _, p := range list {
		result = append(result, &PricingItem{
			ModelID:          p.ModelID,
			DisplayName:      p.DisplayName,
			InputPerMillion:  p.InputPerMillion,
			OutputPerMillion: p.OutputPerMillion,
			CacheRead:        p.CacheRead,
			CacheCreation:    p.CacheCreation,
		})
	}
	return result
}

// SetPrice 新增/更新费率
func (s *PricingService) SetPrice(item *PricingItem) error {
	if item.ModelID == "" {
		return nil
	}
	if item.DisplayName == "" {
		item.DisplayName = item.ModelID
	}
	return s.mgr.SetPrice(&pricing.Price{
		ModelID:          item.ModelID,
		DisplayName:      item.DisplayName,
		InputPerMillion:  item.InputPerMillion,
		OutputPerMillion: item.OutputPerMillion,
		CacheRead:        item.CacheRead,
		CacheCreation:    item.CacheCreation,
	})
}

// DeletePrice 删除费率
func (s *PricingService) DeletePrice(modelID string) error {
	return s.mgr.DeletePrice(modelID)
}

// Count 费率条数
func (s *PricingService) Count() int {
	return s.mgr.Count()
}

// SyncFromGitHub 从 GitHub 拉取最新 rates_default.go 并覆盖本地费率
func (s *PricingService) SyncFromGitHub() (int, error) {
	url := "https://raw.githubusercontent.com/rosanruan/switch-dev/main/pricing/rates_default.go"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("拉取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("拉取失败: HTTP %d", resp.StatusCode)
	}
	src, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %w", err)
	}
	prices, err := pricing.ParseRatesGoFile(src)
	if err != nil {
		return 0, err
	}
	if err := s.mgr.ReplaceAll(prices); err != nil {
		return 0, err
	}
	return len(prices), nil
}
