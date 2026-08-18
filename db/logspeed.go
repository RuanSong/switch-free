package db

import (
	"database/sql"
	"sort"
	"time"

	"switchdev/proxy"
)

// ModelSpeed 单个模型的输出速率
type ModelSpeed struct {
	Model    string  `json:"model"`
	TPS      float64 `json:"tps"`
	Reqs     int64   `json:"reqs"`
	Output   int64   `json:"output"`
	Duration float64 `json:"duration"`
}

// SpeedStats 今日输出速率统计
type SpeedStats struct {
	OverallTPS    float64      `json:"overallTps"`
	TotalReqs     int64        `json:"totalReqs"`
	TotalOutput   int64        `json:"totalOutput"`
	TotalDuration float64      `json:"totalDuration"`
	ByModel       []ModelSpeed `json:"byModel"`
}

// ComputeTodaySpeed 统计今日模型输出速率
func (d *DB) ComputeTodaySpeed() (*SpeedStats, error) {
	today := time.Now().Format("2006-01-02")

	rows, err := d.conn.Query(`
		SELECT
			COALESCE(NULLIF(mu.model_id, ''), m.model_id, '') AS model,
			COUNT(*) AS reqs,
			SUM(l.output_tokens) AS output,
			SUM(l.duration) / 1000.0 AS duration
		FROM logs l
		LEFT JOIN models m  ON l.model_id = m.id
		LEFT JOIN models mu ON l.used_model_id = mu.id
		WHERE l.date = ?
			AND l.status = 'success'
			AND l.output_tokens > 0
			AND l.duration >= 50
		GROUP BY COALESCE(NULLIF(mu.model_id, ''), m.model_id, '')
	`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := &SpeedStats{}
	modelMap := map[string]*ModelSpeed{}
	for rows.Next() {
		var rawModel string
		var reqs, output sql.NullInt64
		var duration sql.NullFloat64
		if err := rows.Scan(&rawModel, &reqs, &output, &duration); err != nil {
			continue
		}
		display := proxy.ActualUpstreamModel(rawModel)
		if display == "" {
			display = "未知"
		}
		m := modelMap[display]
		if m == nil {
			m = &ModelSpeed{Model: display}
			modelMap[display] = m
		}
		m.Reqs += reqs.Int64
		m.Output += output.Int64
		m.Duration += duration.Float64
	}
	for _, m := range modelMap {
		if m.Duration > 0 {
			m.TPS = float64(m.Output) / m.Duration
		}
		stats.TotalReqs += m.Reqs
		stats.TotalOutput += m.Output
		stats.TotalDuration += m.Duration
		stats.ByModel = append(stats.ByModel, *m)
	}
	if stats.TotalDuration > 0 {
		stats.OverallTPS = float64(stats.TotalOutput) / stats.TotalDuration
	}
	sort.Slice(stats.ByModel, func(i, j int) bool {
		return stats.ByModel[i].TPS > stats.ByModel[j].TPS
	})
	return stats, nil
}
