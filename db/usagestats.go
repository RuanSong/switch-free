package db

import (
	"fmt"

	"switchdev/proxy"
)

// usage_stats 表：按小时桶聚合的用量统计，与 logs 表解耦。
// 写日志时同步累积（UpsertUsageStat），启动时把历史 logs 回填一次（BackfillUsageStats）。
// 只累计 status='success' 的请求，与现有统计口径一致；清空 logs 不影响本表。

// usageHour 把 "2006-01-02 15:04:05" 截成小时桶 "2006-01-02 15"
func usageHour(dateTime string) string {
	if len(dateTime) >= 13 {
		return dateTime[:13]
	}
	return dateTime
}

// UpsertUsageStat 把一条日志累计进 usage_stats（仅 success）。
// 在事务外独立执行（调用方通常在 InsertLog 之后调用），失败不影响主流程。
func (d *DB) UpsertUsageStat(entry *proxy.LogEntry) error {
	if entry == nil || entry.Status != "success" {
		return nil
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 归一化 source / upstream / model id（与 InsertLog 同一套逻辑）
	sourceName := entry.Source
	if sourceName == "" {
		sourceName = "未知"
	}
	var sourceID int64
	if err := tx.QueryRow(
		`INSERT INTO sources (name, user_agent) VALUES (?, ?)
		 ON CONFLICT(name, user_agent) DO UPDATE SET name=excluded.name
		 RETURNING id`,
		sourceName, entry.UserAgent,
	).Scan(&sourceID); err != nil {
		return fmt.Errorf("upsert source: %w", err)
	}

	upstreamName := entry.Upstream
	if upstreamName == "" {
		upstreamName = "unknown"
	}
	var upstreamID int64
	err = tx.QueryRow(
		`INSERT INTO upstreams (name, type, label) VALUES (?, 'builtin', ?)
		 ON CONFLICT(name) DO NOTHING
		 RETURNING id`,
		upstreamName, upstreamLabel(upstreamName),
	).Scan(&upstreamID)
	if err != nil {
		if err2 := tx.QueryRow(`SELECT id FROM upstreams WHERE name = ?`, upstreamName).Scan(&upstreamID); err2 != nil {
			return fmt.Errorf("upsert upstream: %w", err)
		}
	}

	// 实际生效模型：used_model 优先，回退 model（与统计口径一致）
	effModel := entry.UsedModel
	if effModel == "" {
		effModel = entry.Model
	}
	modelID := d.upsertModelInTx(tx, upstreamID, effModel)

	// 速率统计口径：output_tokens>0 且 duration>=50ms 的请求才计入 output_reqs
	outputReq := 0
	if entry.OutputTokens > 0 && entry.Duration >= 50 {
		outputReq = 1
	}
	cacheReq := 0
	if entry.CacheHitTokens > 0 {
		cacheReq = 1
	}

	dateTime := entry.DateTime
	if dateTime == "" {
		dateTime = entry.Timestamp
	}
	_, err = tx.Exec(
		`INSERT INTO usage_stats (
			hour, date, upstream_id, model_id, source_id,
			reqs, input_tokens, output_tokens, cache_hit_tokens, cache_hit_reqs,
			cost, duration_ms, output_reqs
		) VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hour, upstream_id, model_id, source_id) DO UPDATE SET
			reqs             = reqs + 1,
			input_tokens     = input_tokens + excluded.input_tokens,
			output_tokens    = output_tokens + excluded.output_tokens,
			cache_hit_tokens = cache_hit_tokens + excluded.cache_hit_tokens,
			cache_hit_reqs   = cache_hit_reqs + excluded.cache_hit_reqs,
			cost             = cost + excluded.cost,
			duration_ms      = duration_ms + excluded.duration_ms,
			output_reqs      = output_reqs + excluded.output_reqs`,
		usageHour(dateTime), entry.Date, upstreamID, modelID, sourceID,
		entry.InputTokens, entry.OutputTokens, entry.CacheHitTokens, cacheReq,
		entry.Cost, entry.Duration, outputReq,
	)
	if err != nil {
		return fmt.Errorf("upsert usage_stats: %w", err)
	}

	return tx.Commit()
}

// BackfillUsageStats 把 logs 表的历史成功日志一次性聚合进 usage_stats。
// 幂等：usage_stats 已有数据则跳过（避免重复累计）。用于建表后回填历史。
func (d *DB) BackfillUsageStats() error {
	var n int64
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM usage_stats`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil // 已回填过
	}

	// model 取 used_model 优先、回退 model；source/upstream/model id 直接复用 logs 外键。
	_, err := d.conn.Exec(`
		INSERT INTO usage_stats (
			hour, date, upstream_id, model_id, source_id,
			reqs, input_tokens, output_tokens, cache_hit_tokens, cache_hit_reqs,
			cost, duration_ms, output_reqs
		)
		SELECT
			substr(l.date_time, 1, 13) AS hour,
			l.date,
			l.upstream_id,
			CASE WHEN l.used_model_id != 0 THEN l.used_model_id ELSE l.model_id END AS model_id,
			l.source_id,
			COUNT(*),
			SUM(l.input_tokens),
			SUM(l.output_tokens),
			SUM(l.cache_hit_tokens),
			SUM(CASE WHEN l.cache_hit_tokens > 0 THEN 1 ELSE 0 END),
			SUM(l.cost),
			SUM(l.duration),
			SUM(CASE WHEN l.output_tokens > 0 AND l.duration >= 50 THEN 1 ELSE 0 END)
		FROM logs l
		WHERE l.status = 'success'
		GROUP BY hour, l.upstream_id, model_id, l.source_id
	`)
	return err
}
