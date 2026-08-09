package config

import (
	"strings"

	"switchfree/proxy"
)

// Resolve 解析请求 model -> 有序尝试列表（含降级链 + 兜底）
// 返回的列表按顺序尝试，第一个成功即返回
func (c *Config) Resolve(requestedModel string) []proxy.ModelRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if requestedModel == "" || strings.ToLower(requestedModel) == "auto" {
		return c.expandAutoChain()
	}
	return c.expandManual(requestedModel)
}

// expandAutoChain 展开 auto 优先级链（agent 分组 -> 扁平列表）
func (c *Config) expandAutoChain() []proxy.ModelRef {
	var result []proxy.ModelRef
	for _, ag := range c.AutoChain {
		for _, model := range ag.Models {
			result = append(result, proxy.ModelRef{Upstream: ag.Upstream, Model: model})
		}
	}
	// 追加上全局兜底（去重，避免重复尝试同一个模型）
	if c.GlobalFallback.Upstream != "" && c.GlobalFallback.Model != "" && !c.isInChain(result, c.GlobalFallback) {
		result = append(result, c.GlobalFallback)
	}
	return result
}

// expandManual 展开手动模式：指定模型 + 它的降级链 + 兜底
func (c *Config) expandManual(requestedModel string) []proxy.ModelRef {
	// 解析模型所属 upstream
	resolvedID := proxy.ResolveModel(requestedModel)
	upstream := proxy.ResolveUpstream(requestedModel)

	result := []proxy.ModelRef{
		{Upstream: upstream, Model: resolvedID},
	}

	// 该模型配的降级链
	if fallbacks, ok := c.ManualFallbacks[resolvedID]; ok {
		result = append(result, fallbacks...)
	}
	// 也尝试用原始请求模型名查找
	if strings.ToLower(requestedModel) != strings.ToLower(resolvedID) {
		if fallbacks, ok := c.ManualFallbacks[requestedModel]; ok {
			result = append(result, fallbacks...)
		}
	}

	// 全局兜底
	if !c.isInChain(result, c.GlobalFallback) {
		result = append(result, c.GlobalFallback)
	}
	return result
}

// isInChain 检查模型引用是否已在链中
func (c *Config) isInChain(chain []proxy.ModelRef, ref proxy.ModelRef) bool {
	for _, item := range chain {
		if item.Upstream == ref.Upstream && item.Model == ref.Model {
			return true
		}
	}
	return false
}
