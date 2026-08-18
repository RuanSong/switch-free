package config

import (
	"strings"

	"switchdev/proxy"
)

// Resolve 解析请求 model + User-Agent -> 有序尝试列表（含降级链 + 兜底）
// UA 路由为叠加层：匹配成功时把目标模型插入链首，正常降级链追加在后做兜底。
// 返回的列表按顺序尝试，第一个成功即返回
func (c *Config) Resolve(requestedModel string, userAgent string) []proxy.ModelRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// ua 模式：路由完全由 UA 规则驱动，不叠加 auto/manual 链
	if c.Mode == "ua" {
		return c.expandUALocked(requestedModel, userAgent)
	}

	if c.UARoutingEnabled {
		if ref, ok := c.matchUARule(requestedModel, userAgent); ok {
			chain := []proxy.ModelRef{ref}
			var normal []proxy.ModelRef
			if requestedModel == "" || strings.EqualFold(requestedModel, "auto") {
				normal = c.expandAutoChainLocked()
			} else {
				normal = c.expandManualLocked(requestedModel)
			}
			for _, r := range normal {
				if !c.isInChain(chain, r) {
					chain = append(chain, r)
				}
			}
			return chain
		}
	}

	if requestedModel == "" || strings.EqualFold(requestedModel, "auto") {
		return c.expandAutoChainLocked()
	}
	return c.expandManualLocked(requestedModel)
}

// expandUALocked ua 模式链展开：UA 规则命中的目标模型 + UA 全局兜底
func (c *Config) expandUALocked(requestedModel, userAgent string) []proxy.ModelRef {
	var chain []proxy.ModelRef
	if ref, ok := c.matchUARule(requestedModel, userAgent); ok {
		chain = append(chain, ref)
	}
	if c.UAGlobalFallback.Upstream != "" && c.UAGlobalFallback.Model != "" && !c.isInChain(chain, c.UAGlobalFallback) {
		chain = append(chain, c.UAGlobalFallback)
	}
	return chain
}

// matchUARule 遍历 UA 规则，返回第一个匹配的目标 ModelRef
func (c *Config) matchUARule(requestedModel, userAgent string) (proxy.ModelRef, bool) {
	if userAgent == "" {
		return proxy.ModelRef{}, false
	}
	uaLow := strings.ToLower(userAgent)
	for _, rule := range c.UARules {
		if !rule.Enabled || rule.Pattern == "" {
			continue
		}
		if !strings.Contains(uaLow, strings.ToLower(rule.Pattern)) {
			continue
		}
		for _, m := range rule.Mappings {
			if strings.EqualFold(m.RequestedModel, requestedModel) &&
				m.Target.Upstream != "" && m.Target.Model != "" {
				return m.Target, true
			}
		}
	}
	return proxy.ModelRef{}, false
}

// expandAutoChain 展开 auto 优先级链（agent 分组 -> 扁平列表）
func (c *Config) expandAutoChain() []proxy.ModelRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.expandAutoChainLocked()
}

func (c *Config) expandAutoChainLocked() []proxy.ModelRef {
	var result []proxy.ModelRef
	for _, ag := range c.AutoChain {
		for _, model := range ag.Models {
			result = append(result, proxy.ModelRef{Upstream: ag.Upstream, Model: model})
		}
	}
	if c.GlobalFallback.Upstream != "" && c.GlobalFallback.Model != "" && !c.isInChain(result, c.GlobalFallback) {
		result = append(result, c.GlobalFallback)
	}
	return result
}

// expandManual 展开手动模式：指定模型 + 它的降级链 + 兜底
func (c *Config) expandManual(requestedModel string) []proxy.ModelRef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.expandManualLocked(requestedModel)
}

func (c *Config) expandManualLocked(requestedModel string) []proxy.ModelRef {
	resolvedID := proxy.ResolveModel(requestedModel)
	upstream := proxy.ResolveUpstream(requestedModel)

	result := []proxy.ModelRef{
		{Upstream: upstream, Model: resolvedID},
	}

	if fallbacks, ok := c.ManualFallbacks[resolvedID]; ok {
		result = append(result, fallbacks...)
	}
	if !strings.EqualFold(requestedModel, resolvedID) {
		if fallbacks, ok := c.ManualFallbacks[requestedModel]; ok {
			result = append(result, fallbacks...)
		}
	}

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
