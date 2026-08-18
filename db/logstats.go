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

	// 供应商维度
	provRows, err := d.conn.Query(`
		SELECT
			COALESCE(u.name, 'unknown') AS provider,
			COALESCE(u.label, u.name, 'unknown') AS label,
			COUNT(*) AS reqs,
			SUM(l.input_tokens) AS input,
			SUM(l.output_tokens) AS output,
			SUM(l.cost) AS cost
		FROM logs l
		LEFT JOIN upstreams u ON l.upstream_id = u.id
		WHERE l.date BETWEEN ? AND ? AND l.status = 'success'
		GROUP BY l.upstream_id
		ORDER BY SUM(l.input_tokens + l.output_tokens) DESC
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

	// 模型维度：按实际发往上游的 model 名聚合（provider/<pid>/<mid>、wb/<mid>、DevEco 本地 id 等
	// 都剥成规范名），使同一模型在不同供应商/前缀下合并为一行。
	// 优先用 used_model（实际命中的模型），请求模型是 "auto"/空 时回退到它。
	modelRows, err := d.conn.Query(`
		SELECT
			COALESCE(NULLIF(mu.model_id, ''), m.model_id, '') AS model,
			COUNT(*) AS reqs,
			SUM(l.input_tokens) AS input,
			SUM(l.output_tokens) AS output,
			SUM(l.cost) AS cost
		FROM logs l
		LEFT JOIN models m  ON l.model_id = m.id
		LEFT JOIN models mu ON l.used_model_id = mu.id
		WHERE l.date BETWEEN ? AND ? AND l.status = 'success'
		GROUP BY COALESCE(NULLIF(mu.model_id, ''), m.model_id, '')
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
