package db

import (
	"path/filepath"
	"testing"

	"switchdev/proxy"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func successEntry(model, usedModel, upstream string, input, output int, durMs int64) *proxy.LogEntry {
	return &proxy.LogEntry{
		DateTime:     "2026-08-20 10:15:00",
		Date:         "2026-08-20",
		Model:        model,
		UsedModel:    usedModel,
		Upstream:     upstream,
		Source:       "test",
		Status:       "success",
		Duration:     durMs,
		InputTokens:  input,
		OutputTokens: output,
	}
}

// TestUpsertUsageStatAccumulates 同一小时桶多次写入应累加
func TestUpsertUsageStatAccumulates(t *testing.T) {
	d := newTestDB(t)

	for i := 0; i < 3; i++ {
		if err := d.UpsertUsageStat(successEntry("auto", "JoyAI-Code-1.5", "joycode", 100, 200, 1000)); err != nil {
			t.Fatalf("UpsertUsageStat: %v", err)
		}
	}
	// 非 success 不应计入
	if err := d.UpsertUsageStat(&proxy.LogEntry{Status: "error", Date: "2026-08-20", DateTime: "2026-08-20 10:20:00"}); err != nil {
		t.Fatalf("UpsertUsageStat(error): %v", err)
	}

	var reqs, input, output, outputReqs int64
	err := d.conn.QueryRow(`
		SELECT reqs, input_tokens, output_tokens, output_reqs
		FROM usage_stats WHERE hour = '2026-08-20 10'`).
		Scan(&reqs, &input, &output, &outputReqs)
	if err != nil {
		t.Fatalf("query usage_stats: %v", err)
	}
	if reqs != 3 || input != 300 || output != 600 || outputReqs != 3 {
		t.Fatalf("累计错误: reqs=%d input=%d output=%d outputReqs=%d", reqs, input, output, outputReqs)
	}
}

// TestStatsSurviveClearLogs 核心需求：清空 logs 后统计数据仍在
func TestStatsSurviveClearLogs(t *testing.T) {
	d := newTestDB(t)

	e := successEntry("auto", "JoyAI-Code-1.5", "joycode", 100, 200, 1000)
	if err := d.InsertLog(e); err != nil {
		t.Fatalf("InsertLog: %v", err)
	}
	if err := d.UpsertUsageStat(e); err != nil {
		t.Fatalf("UpsertUsageStat: %v", err)
	}

	before, err := d.ComputeUsageStats("2026-08-20", "2026-08-20")
	if err != nil {
		t.Fatalf("ComputeUsageStats(before): %v", err)
	}
	if before.TotalOutput != 200 {
		t.Fatalf("清空前 TotalOutput=%d, 期望 200", before.TotalOutput)
	}

	if err := d.ClearLogs(); err != nil {
		t.Fatalf("ClearLogs: %v", err)
	}

	after, err := d.ComputeUsageStats("2026-08-20", "2026-08-20")
	if err != nil {
		t.Fatalf("ComputeUsageStats(after): %v", err)
	}
	if after.TotalOutput != 200 || after.TotalInput != 100 || after.TotalReqs != 1 {
		t.Fatalf("清空 logs 后统计丢失: %+v", after)
	}

	speed, err := d.ComputeTodaySpeed()
	if err != nil {
		t.Fatalf("ComputeTodaySpeed: %v", err)
	}
	// 今天的日期不一定等于 entry.Date（2026-08-20 是固定值），速率表按 time.Now 过滤，
	// 仅当运行测试当天是 2026-08-20 才有数据；这里只验证不报错即可。
	_ = speed
}

// TestBackfillUsageStats 回填历史 logs，且幂等（重复调用不重复累计）
func TestBackfillUsageStats(t *testing.T) {
	d := newTestDB(t)

	// 只写 logs、不写统计表（模拟建表前的历史数据）
	for i := 0; i < 2; i++ {
		if err := d.InsertLog(successEntry("glm-5.1", "", "deveco", 50, 80, 500)); err != nil {
			t.Fatalf("InsertLog: %v", err)
		}
	}

	if err := d.BackfillUsageStats(); err != nil {
		t.Fatalf("BackfillUsageStats: %v", err)
	}

	stats, err := d.ComputeUsageStats("2026-08-20", "2026-08-20")
	if err != nil {
		t.Fatalf("ComputeUsageStats: %v", err)
	}
	if stats.TotalReqs != 2 || stats.TotalOutput != 160 {
		t.Fatalf("回填后统计错误: %+v", stats)
	}

	// 幂等：再回填一次不应翻倍
	if err := d.BackfillUsageStats(); err != nil {
		t.Fatalf("BackfillUsageStats(2nd): %v", err)
	}
	stats2, _ := d.ComputeUsageStats("2026-08-20", "2026-08-20")
	if stats2.TotalReqs != 2 || stats2.TotalOutput != 160 {
		t.Fatalf("回填不幂等，重复累计: %+v", stats2)
	}
}
