package service

import (
	"sort"
	"time"
)

// SpeedStats 今日输出速率统计（性能评估）
type SpeedStats struct {
	OverallTPS    float64      `json:"overallTps"`    // 加权平均 tok/s = 总输出 / 总耗时
	TotalReqs     int64        `json:"totalReqs"`     // 参与统计的成功请求数
	TotalOutput   int64        `json:"totalOutput"`   // 总输出 token
	TotalDuration float64      `json:"totalDuration"` // 总耗时（秒）
	ByModel       []ModelSpeed `json:"byModel"`       // 按 tps 降序
}

// ModelSpeed 单个模型的输出速率
type ModelSpeed struct {
	Model    string  `json:"model"`
	TPS      float64 `json:"tps"`      // 加权平均 tok/s
	Reqs     int64   `json:"reqs"`
	Output   int64   `json:"output"`
	Duration float64 `json:"duration"` // 秒
}

// ComputeTodaySpeed 统计今日模型输出速率
// 口径：仅 success 且 OutputTokens>0 且 Duration>=50ms 的请求；
// 整体与分模型均用加权平均（总输出/总耗时），避免短请求被异常放大
func ComputeTodaySpeed() *SpeedStats {
	today := time.Now().Format("2006-01-02")
	logs := getLogsByRange(today, today, 0)

	stats := &SpeedStats{}
	modelMap := map[string]*ModelSpeed{}

	for _, log := range logs {
		if log.Status != "success" || log.OutputTokens <= 0 || log.Duration < 50 {
			continue
		}
		out := int64(log.OutputTokens)
		durSec := float64(log.Duration) / 1000.0

		stats.TotalReqs++
		stats.TotalOutput += out
		stats.TotalDuration += durSec

		key := log.UsedModel
		if key == "" {
			key = log.Model
		}
		if key == "" || key == "auto" {
			key = "未知"
		}
		m := modelMap[key]
		if m == nil {
			m = &ModelSpeed{Model: key}
			modelMap[key] = m
		}
		m.Reqs++
		m.Output += out
		m.Duration += durSec
	}

	if stats.TotalDuration > 0 {
		stats.OverallTPS = float64(stats.TotalOutput) / stats.TotalDuration
	}

	for _, m := range modelMap {
		if m.Duration > 0 {
			m.TPS = float64(m.Output) / m.Duration
		}
		stats.ByModel = append(stats.ByModel, *m)
	}
	sort.Slice(stats.ByModel, func(i, j int) bool {
		return stats.ByModel[i].TPS > stats.ByModel[j].TPS
	})
	return stats
}

// GetTodaySpeed 暴露给前端：今日输出速率统计
func (s *LogService) GetTodaySpeed() *SpeedStats {
	return ComputeTodaySpeed()
}
