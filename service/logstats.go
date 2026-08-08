package service

import (
	"sort"
	"time"
)

// UsageStats 使用统计结果
type UsageStats struct {
	StartDate   string           `json:"startDate"`
	EndDate     string           `json:"endDate"`
	TotalTokens int64            `json:"totalTokens"`
	TotalInput  int64            `json:"totalInput"`
	TotalOutput int64            `json:"totalOutput"`
	TotalCost   float64          `json:"totalCost"`
	TotalReqs   int64            `json:"totalReqs"`
	SuccessReqs int64            `json:"successReqs"`
	ByAgent     []AgentUsage     `json:"byAgent"`
	ByModel     []ModelUsage     `json:"byModel"`
}

// AgentUsage agent 维度统计
type AgentUsage struct {
	Agent       string  `json:"agent"`       // 上游标识 joycode/deveco/opencode
	AgentLabel  string  `json:"agentLabel"`  // 显示名
	Tokens      int64   `json:"tokens"`
	Input       int64   `json:"input"`
	Output      int64   `json:"output"`
	Cost        float64 `json:"cost"`
	Requests    int64   `json:"requests"`
	SuccessReqs int64   `json:"successReqs"`
}

// ModelUsage 模型维度统计
type ModelUsage struct {
	Model      string  `json:"model"`      // 实际用到的模型（usedModel 或 model）
	Tokens     int64   `json:"tokens"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	Cost       float64 `json:"cost"`
	Requests   int64   `json:"requests"`
	Percent    float64 `json:"percent"`    // token 占比 0-100
}

// agentLabelFromUpstream 上游显示名
func agentLabelFromUpstream(up string) string {
	switch up {
	case "joycode":
		return "京东 JoyCode"
	case "deveco":
		return "华为 DevEco"
	case "opencode":
		return "OpenCode Zen"
	default:
		return up
	}
}

// ComputeUsageStats 按日期范围统计使用情况
func ComputeUsageStats(startDate, endDate string) *UsageStats {
	// 默认最近 7 天
	if startDate == "" || endDate == "" {
		endDate = time.Now().Format("2006-01-02")
		startDate = time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	}

	logs := getLogsByRange(startDate, endDate, 0) // 0 = 不限制数量，统计全部

	stats := &UsageStats{
		StartDate: startDate,
		EndDate:   endDate,
	}

	agentMap := map[string]*AgentUsage{}
	modelMap := map[string]*ModelUsage{}

	for _, log := range logs {
		// 只统计成功请求
		if log.Status != "success" {
			continue
		}
		in := int64(log.InputTokens)
		out := int64(log.OutputTokens)
		// 空 token 且非成功则跳过；成功但 0 token 也计入请求数
		if in == 0 && out == 0 && log.InputTokens == 0 && log.OutputTokens == 0 {
			// 保留请求计数，token 为 0
		}

		// agent 维度
		agentKey := log.Upstream
		if agentKey == "" {
			agentKey = "unknown"
		}
		ag := agentMap[agentKey]
		if ag == nil {
			ag = &AgentUsage{Agent: agentKey, AgentLabel: agentLabelFromUpstream(agentKey)}
			agentMap[agentKey] = ag
		}
		ag.Tokens += in + out
		ag.Input += in
		ag.Output += out
		ag.Cost += log.Cost
		ag.Requests++
		ag.SuccessReqs++

		// 模型维度（用实际用到的模型）
		modelKey := log.UsedModel
		if modelKey == "" {
			modelKey = log.Model
		}
		if modelKey == "" || modelKey == "auto" {
			modelKey = "未知"
		}
		mu := modelMap[modelKey]
		if mu == nil {
			mu = &ModelUsage{Model: modelKey}
			modelMap[modelKey] = mu
		}
		mu.Tokens += in + out
		mu.Input += in
		mu.Output += out
		mu.Cost += log.Cost
		mu.Requests++
	}

	// 汇总
	for _, ag := range agentMap {
		stats.TotalTokens += ag.Tokens
		stats.TotalInput += ag.Input
		stats.TotalOutput += ag.Output
		stats.TotalCost += ag.Cost
		stats.TotalReqs += ag.Requests
		stats.SuccessReqs += ag.SuccessReqs
	}

	// agent 按 token 降序
	for _, ag := range agentMap {
		stats.ByAgent = append(stats.ByAgent, *ag)
	}
	sort.Slice(stats.ByAgent, func(i, j int) bool {
		return stats.ByAgent[i].Tokens > stats.ByAgent[j].Tokens
	})

	// 模型按 token 降序 + 算占比
	for _, mu := range modelMap {
		if stats.TotalTokens > 0 {
			mu.Percent = float64(mu.Tokens) / float64(stats.TotalTokens) * 100
		}
		stats.ByModel = append(stats.ByModel, *mu)
	}
	sort.Slice(stats.ByModel, func(i, j int) bool {
		return stats.ByModel[i].Tokens > stats.ByModel[j].Tokens
	})

	return stats
}

// GetUsageStats 暴露给前端的统计方法（service 层包装）
func (s *LogService) GetUsageStats(startDate, endDate string) *UsageStats {
	return ComputeUsageStats(startDate, endDate)
}

// TodaySummary 今日使用概览（首页展示）
type TodaySummary struct {
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// GetTodaySummary 今日总消耗 token 和费用
func (s *LogService) GetTodaySummary() *TodaySummary {
	today := time.Now().Format("2006-01-02")
	stats := ComputeUsageStats(today, today)
	return &TodaySummary{
		Tokens: stats.TotalTokens,
		Cost:   stats.TotalCost,
	}
}
