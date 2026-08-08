package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"switchfree/proxy"
)

// 日志文件保留天数
const logRetentionDays = 30

// logDir 日志目录（按天 JSONL 文件）
func logDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "switch-free", "logs")
}

// logFilePath 某天的日志文件路径
func logFilePath(date string) string {
	return filepath.Join(logDir(), date+".jsonl")
}

// appendLogToDisk 追加一条日志到当天的 JSONL 文件（追加模式）
func appendLogToDisk(entry *proxy.LogEntry) {
	date := entry.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	path := logFilePath(date)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	// 以追加模式打开（不存在则创建），写入一行 JSON
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f.Write(append(line, '\n'))
}

// readLogsFromDisk 读取某天的日志文件，返回最新在前的条目
func readLogsFromDisk(date string) []*proxy.LogEntry {
	path := logFilePath(date)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []*proxy.LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 512*1024) // 允许大行
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e proxy.LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, &e)
	}
	// 最新在前
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].DateTime > entries[j].DateTime
	})
	return entries
}

// listLogDates 列出所有日志文件的日期（降序）
func listLogDates() []string {
	dir := logDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dates []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") {
			dates = append(dates, strings.TrimSuffix(name, ".jsonl"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates
}

// getLogsByRange 按日期范围查询日志（含 startDate 到 endDate）
// 返回所有匹配条目的合并（按时间倒序）
func getLogsByRange(startDate, endDate string, limit int) []*proxy.LogEntry {
	// 生成日期列表（闭区间）
	var dates []string
	d, _ := time.Parse("2006-01-02", startDate)
	end, _ := time.Parse("2006-01-02", endDate)
	if end.IsZero() {
		end = time.Now()
	}
	for !d.After(end) {
		dates = append(dates, d.Format("2006-01-02"))
		d = d.AddDate(0, 0, 1)
	}

	// 读每个日期的文件，合并
	var all []*proxy.LogEntry
	for _, date := range dates {
		all = append(all, readLogsFromDisk(date)...)
	}
	// 已按单文件倒序，合并后再按时间倒序
	sort.Slice(all, func(i, j int) bool {
		return all[i].DateTime > all[j].DateTime
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// cleanupOldLogs 清理超过保留天数的日志文件
func cleanupOldLogs() {
	dates := listLogDates()
	if len(dates) == 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -logRetentionDays).Format("2006-01-02")
	for _, date := range dates {
		if date < cutoff {
			os.Remove(logFilePath(date))
		}
	}
}

var _ = fmt.Sprintf
