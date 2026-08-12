package service

import "switchfree/proxy"

// ModelService 模型管理服务（暴露给前端）
type ModelService struct {
	core *Core
}

func NewModelService(core *Core) *ModelService {
	return &ModelService{core: core}
}

// ModelDetail 模型详情（前端展示用）
type ModelDetail struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Upstream string `json:"upstream"`
	Stream   bool   `json:"stream"`
	Context  int    `json:"context"`
	Output   int    `json:"output"`
	Vision   bool   `json:"vision"`
	ToolCall bool   `json:"toolCall"`
	Free     bool   `json:"free,omitempty"` // 限时免费标识
}

// GetModels 获取全部可用模型
func (s *ModelService) GetModels() []*ModelDetail {
	var result []*ModelDetail

	// auto 虚拟模型
	result = append(result, &ModelDetail{
		ID: "auto", Label: "Auto（DevEco GLM-5.1，失败降级 JoyCode）",
		Upstream: "deveco", Stream: true, ToolCall: true,
	})

	// OpenCode Zen 模型
	for _, m := range proxy.OpenCodeModels {
		result = append(result, &ModelDetail{
			ID: m.ID, Label: m.Label, Upstream: "opencode",
			Stream: true, Context: m.Context, Output: m.Output, ToolCall: true, Free: m.Free,
		})
	}

	// DevEco 模型
	for _, m := range proxy.DevEcoModels {
		result = append(result, &ModelDetail{
			ID: m.ID, Label: m.Label, Upstream: "deveco",
			Stream: true, Context: m.Context, Output: m.Output, ToolCall: true, Free: m.Free,
		})
	}

	// JoyCode 模型
	for _, m := range proxy.JoyCodeModels {
		result = append(result, &ModelDetail{
			ID: m.ID, Label: m.Label, Upstream: "joycode",
			Stream: m.Stream, Output: m.OutputMaxTokens, ToolCall: true, Free: m.Free,
		})
	}

	// WorkBuddy 模型
	for _, m := range proxy.WorkBuddyModels {
		result = append(result, &ModelDetail{
			ID: m.ID, Label: m.Label, Upstream: "workbuddy",
			Stream: true, Context: m.Context, Output: m.Output,
			Vision: m.Vision, ToolCall: m.ToolCall, Free: m.Free,
		})
	}

	// 免费 API 模型（动态注册的 verified 模型）
	for _, m := range proxy.FreeModels {
		result = append(result, &ModelDetail{
			ID: m.InternalID, Label: m.Label, Upstream: m.ProviderID,
			Stream: true, Context: m.Context, Free: true,
		})
	}

	return result
}

// AutoStrategy auto 模式策略说明
type AutoStrategy struct {
	Primary       string `json:"primary"`       // 主力上游模型
	Fallback      string `json:"fallback"`      // 降级模型
	PrimaryUpstream   string `json:"primaryUpstream"`
	FallbackUpstream  string `json:"fallbackUpstream"`
}

// GetAutoStrategy 获取 auto 模式策略
func (s *ModelService) GetAutoStrategy() *AutoStrategy {
	return &AutoStrategy{
		Primary:          proxy.AutoModel,
		Fallback:         proxy.AutoModelJoyCodeFallback,
		PrimaryUpstream:  proxy.ResolveUpstream(proxy.AutoModel),
		FallbackUpstream: "joycode",
	}
}