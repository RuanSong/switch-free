package db

import (
	"fmt"
	"time"
)

// TrendPoint 趋势图一个数据点
type TrendPoint struct {
	Label          string `json:"label"`
	Tokens         int64  `json:"tokens"`
	InputTokens    int64  `json:"inputTokens"`
	OutputTokens   int64  `json:"outputTokens"`
	Reqs           int64  `json:"reqs"`
	CacheHitTokens int64  `json:"cacheHitTokens"`
	CacheHitReqs   int64  `json:"cacheHitReqs"`
}

// UsageTrend 使用趋势结果
type UsageTrend struct {
	StartDate   string       `json:"startDate"`
	EndDate     string       `json:"endDate"`
	Granularity string       `json:"granularity"`
	Points      []TrendPoint `json:"points"`
}

// ComputeUsageTrend 按粒度统计使用趋势
func (d *DB) ComputeUsageTrend(startDate, endDate, granularity string) (*UsageTrend, error) {
	if granularity != "hour" && granularity != "day" {
		granularity = "day"
	}

	trend := &UsageTrend{StartDate: startDate, EndDate: endDate, Granularity: granularity}

	// 生成桶
	start, _ := time.Parse("2006-01-02", startDate)
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil || end.IsZero() {
		end = time.Now()
	}

	type bucket struct {
		label string
		minDT string // date_time 范围下界
		maxDT string // date_time 范围上界
	}
	var buckets []bucket
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if granularity == "hour" {
			for h := 0; h < 24; h++ {
				label := fmt.Sprintf("%s %02d:00", d.Format("01-02"), h)
				buckets = append(buckets, bucket{
					label: label,
					minDT: fmt.Sprintf("%s %02d:00:00", d.Format("2006-01-02"), h),
					maxDT: fmt.Sprintf("%s %02d:59:59", d.Format("2006-01-02"), h),
				})
			}
		} else {
			buckets = append(buckets, bucket{
				label: d.Format("01-02"),
				minDT: d.Format("2006-01-02") + " 00:00:00",
				maxDT: d.Format("2006-01-02") + " 23:59:59",
			})
		}
	}

	// 从 usage_stats 聚合表按小时桶汇总（清空 logs 不影响趋势）
	rows, err := d.conn.Query(`
		SELECT hour, SUM(input_tokens), SUM(output_tokens), SUM(cache_hit_tokens), SUM(cache_hit_reqs), SUM(reqs)
		FROM usage_stats
		WHERE date BETWEEN ? AND ?
		GROUP BY hour
	`, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 建立 label -> index 映射
	pointMap := make(map[string]int, len(buckets))
	for i, b := range buckets {
		pointMap[b.label] = i
		trend.Points = append(trend.Points, TrendPoint{Label: b.label})
	}

	for rows.Next() {
		var hour string
		var input, output, cache, cacheReqs, reqs int64
		if err := rows.Scan(&hour, &input, &output, &cache, &cacheReqs, &reqs); err != nil {
			continue
		}
		if len(hour) < 13 {
			continue
		}
		// hour format "2006-01-02 15" -> 与桶 label 对齐
		var label string
		if granularity == "hour" {
			// -> "01-02 15:00"
			label = hour[5:10] + " " + hour[11:13] + ":00"
		} else {
			// -> "01-02"
			label = hour[5:10]
		}
		idx, ok := pointMap[label]
		if !ok {
			continue
		}
		trend.Points[idx].Tokens += input + output
		trend.Points[idx].InputTokens += input
		trend.Points[idx].OutputTokens += output
		trend.Points[idx].Reqs += reqs
		trend.Points[idx].CacheHitTokens += cache
		trend.Points[idx].CacheHitReqs += cacheReqs
	}

	return trend, nil
}
