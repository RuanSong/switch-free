package service

import (
	"switchfree/pricing"
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
