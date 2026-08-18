package service

import (
	"sort"
	"time"

	"switchdev/db"
	"switchdev/proxy"
)

// SpeedStats 今日输出速率统计（性能评估）
type SpeedStats = db.SpeedStats

// ModelSpeed 单个模型的输出速率
type ModelSpeed = db.ModelSpeed

// ComputeTodaySpeed 统计今日模型输出速率（从文件读取，兼容旧数据）
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
		key = proxy.ActualUpstreamModel(key)
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

// GetTodaySpeed 暴露给前端：今日输出速率统计（优先从 db 查）
func (s *LogService) GetTodaySpeed() *SpeedStats {
	if s.core.DB() != nil {
		speed, err := s.core.DB().ComputeTodaySpeed()
		if err == nil && speed != nil {
			return speed
		}
	}
	return ComputeTodaySpeed()
}
