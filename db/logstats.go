package db

import (
	"database/sql"
	"sort"

	"switchdev/proxy"
)

// ProviderUsage 供应商维度统计
type ProviderUsage struct {
	Provider      string  `json:"provider"`
	ProviderLabel string  `json:"providerLabel"`
	Tokens        int64   `json:"tokens"`
	Input         int64   `json:"input"`
	Output        int64   `json:"output"`
	Cost          float64 `json:"cost"`
	Requests      int64   `json:"requests"`
	SuccessReqs   int64   `json:"successReqs"`
}

// ModelUsage 模型维度统计
type ModelUsage struct {
	Model      string  `json:"model"`
	ModelLabel string  `json:"modelLabel"`
	Tokens     int64   `json:"tokens"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	Cost       float64 `json:"cost"`
	Requests   int64   `json:"requests"`
	Percent    float64 `json:"percent"`
}

// UsageStats 使用统计结果
type UsageStats struct {
	StartDate   string          `json:"startDate"`
	EndDate     string          `json:"endDate"`
	TotalTokens int64           `json:"totalTokens"`
	TotalInput  int64           `json:"totalInput"`
	TotalOutput int64           `json:"totalOutput"`
	TotalCost   float64         `json:"totalCost"`
	TotalReqs   int64           `json:"totalReqs"`
	SuccessReqs int64           `json:"successReqs"`
	ByProvider  []ProviderUsage `json:"byProvider"`
	ByModel     []ModelUsage    `json:"byModel"`
}

// ComputeUsageStats 按日期范围统计使用情况
func (d *DB) ComputeUsageStats(startDate, endDate string) (*UsageStats, error) {
	stats := &UsageStats{StartDate: startDate, EndDate: endDate}

	// 供应商维度（读 usage_stats 聚合表，清空 logs 不影响统计）
	provRows, err := d.conn.Query(`
		SELECT
			COALESCE(u.name, 'unknown') AS provider,
			COALESCE(u.label, u.name, 'unknown') AS label,
			SUM(s.reqs) AS reqs,
			SUM(s.input_tokens) AS input,
			SUM(s.output_tokens) AS output,
			SUM(s.cost) AS cost
		FROM usage_stats s
		LEFT JOIN upstreams u ON s.upstream_id = u.id
		WHERE s.date BETWEEN ? AND ?
		GROUP BY s.upstream_id
		ORDER BY SUM(s.input_tokens + s.output_tokens) DESC
	`, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer provRows.Close()

	for provRows.Next() {
		var p ProviderUsage
		var input, output sql.NullInt64
		var cost sql.NullFloat64
		if err := provRows.Scan(&p.Provider, &p.ProviderLabel, &p.Requests, &input, &output, &cost); err != nil {
			continue
		}
		p.Input = input.Int64
		p.Output = output.Int64
		p.Cost = cost.Float64
		p.Tokens = p.Input + p.Output
		p.SuccessReqs = p.Requests
		stats.ByProvider = append(stats.ByProvider, p)
		stats.TotalTokens += p.Tokens
		stats.TotalInput += p.Input
		stats.TotalOutput += p.Output
		stats.TotalCost += p.Cost
		stats.TotalReqs += p.Requests
		stats.SuccessReqs += p.SuccessReqs
	}

	// 模型维度：usage_stats.model_id 已存「实际生效模型」（写入时 used_model 优先）。
	// 按规范名聚合（provider/<pid>/<mid>、wb/<mid> 等剥前缀），使同一模型合并为一行。
	modelRows, err := d.conn.Query(`
		SELECT
			COALESCE(m.model_id, '') AS model,
			SUM(s.reqs) AS reqs,
			SUM(s.input_tokens) AS input,
			SUM(s.output_tokens) AS output,
			SUM(s.cost) AS cost
		FROM usage_stats s
		LEFT JOIN models m ON s.model_id = m.id
		WHERE s.date BETWEEN ? AND ?
		GROUP BY COALESCE(m.model_id, '')
	`, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer modelRows.Close()

	modelMap := map[string]*ModelUsage{}
	for modelRows.Next() {
		var rawModel string
		var reqs, input, output sql.NullInt64
		var cost sql.NullFloat64
		if err := modelRows.Scan(&rawModel, &reqs, &input, &output, &cost); err != nil {
			continue
		}
		display := proxy.ActualUpstreamModel(rawModel)
		if display == "" {
			display = "未知"
		}
		m := modelMap[display]
		if m == nil {
			m = &ModelUsage{Model: display, ModelLabel: display}
			modelMap[display] = m
		}
		m.Requests += reqs.Int64
		m.Input += input.Int64
		m.Output += output.Int64
		m.Cost += cost.Float64
		m.Tokens = m.Input + m.Output
	}
	for _, m := range modelMap {
		if stats.TotalTokens > 0 {
			m.Percent = float64(m.Tokens) / float64(stats.TotalTokens) * 100
		}
		stats.ByModel = append(stats.ByModel, *m)
	}
	sort.Slice(stats.ByModel, func(i, j int) bool {
		return stats.ByModel[i].Tokens > stats.ByModel[j].Tokens
	})

	return stats, nil
}
