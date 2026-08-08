package service

import (
	"fmt"
	"time"
)

// TrendPoint 趋势图一个数据点
type TrendPoint struct {
	Label          string `json:"label"`          // 展示标签（"00:00"、"08-01"、"07-01"）
	Tokens         int64  `json:"tokens"`         // token 用量
	Reqs           int64  `json:"reqs"`           // 请求数
	CacheHitTokens int64  `json:"cacheHitTokens"` // 命中缓存的输入 token
	CacheHitReqs   int64  `json:"cacheHitReqs"`   // 命中缓存的请求数
}

// UsageTrend 使用趋势结果
type UsageTrend struct {
	StartDate string       `json:"startDate"`
	EndDate   string       `json:"endDate"`
	Granularity string     `json:"granularity"` // "hour" | "day"
	Points    []TrendPoint `json:"points"`
}

// ComputeUsageTrend 按粒度统计使用趋势
// granularity: "hour"（按天看时，每小时一个点）| "day"（按周/月看时，每天一个点）
func ComputeUsageTrend(startDate, endDate string, granularity string) *UsageTrend {
	if startDate == "" || endDate == "" {
		endDate = time.Now().Format("2006-01-02")
		startDate = time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	}
	if granularity != "hour" && granularity != "day" {
		granularity = "day"
	}

	logs := getLogsByRange(startDate, endDate, 0)

	// 生成桶
	var buckets []TrendPoint
	if granularity == "hour" {
		// startDate 到 endDate 的每一天 0-23 小时
		d, _ := time.Parse("2006-01-02", startDate)
		end, _ := time.Parse("2006-01-02", endDate)
		if end.IsZero() {
			end = time.Now()
		}
		for !d.After(end) {
			for h := 0; h < 24; h++ {
				buckets = append(buckets, TrendPoint{
					Label: d.Format("01-02 ") + pad2(h) + ":00",
				})
			}
			d = d.AddDate(0, 0, 1)
		}
	} else {
		// 每天一个点
		d, _ := time.Parse("2006-01-02", startDate)
		end, _ := time.Parse("2006-01-02", endDate)
		if end.IsZero() {
			end = time.Now()
		}
		for !d.After(end) {
			buckets = append(buckets, TrendPoint{Label: d.Format("01-02")})
			d = d.AddDate(0, 0, 1)
		}
	}

	// 日志填入桶
	// 需要把日志的 dateTime 解析出小时/日期
	bucketIndex := map[string]int{}
	for i, b := range buckets {
		bucketIndex[b.Label] = i
	}

	for _, log := range logs {
		if log.Status != "success" {
			continue
		}
		var key string
		var tokens int64
		tokens = int64(log.InputTokens + log.OutputTokens)

		if granularity == "hour" {
			// dateTime 格式 "2026-08-08 13:14:52"
			if len(log.DateTime) >= 14 {
				key = log.DateTime[5:10] + " " + log.DateTime[11:13] + ":00" // "08-08 13:00"
			}
		} else {
			if len(log.DateTime) >= 10 {
				key = log.DateTime[5:10] // "08-08"
			}
		}

		if idx, ok := bucketIndex[key]; ok {
			buckets[idx].Tokens += tokens
			buckets[idx].Reqs++
			if log.CacheHitTokens > 0 {
				buckets[idx].CacheHitTokens += int64(log.CacheHitTokens)
				buckets[idx].CacheHitReqs++
			}
		}
	}

	return &UsageTrend{
		StartDate:   startDate,
		EndDate:     endDate,
		Granularity: granularity,
		Points:      buckets,
	}
}

// GetUsageTrend 暴露给前端的趋势方法
func (s *LogService) GetUsageTrend(startDate, endDate string, granularity string) *UsageTrend {
	return ComputeUsageTrend(startDate, endDate, granularity)
}

func pad2(n int) string {
	return fmt.Sprintf("%02d", n)
}
