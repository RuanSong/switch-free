package db

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"switchdev/paths"
	"switchdev/proxy"
)

// runJSONLMigration 将旧 JSONL 日志文件迁移到 SQLite（后台异步执行，幂等）
func (d *DB) runJSONLMigration() {
	logDir := filepath.Join(paths.AppConfigDir(), "logs")
	marker := filepath.Join(logDir, ".migrated")

	// 标记文件存在则跳过
	if _, err := os.Stat(marker); err == nil {
		return
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return // 目录不存在，无需迁移
	}

	var total int
	start := time.Now()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(logDir, e.Name())
		count := d.migrateJSONLFile(path)
		total += count
	}
	// 写标记文件
	markerContent := fmt.Sprintf("migrated %d entries at %s\n", total, time.Now().Format(time.RFC3339))
	os.WriteFile(marker, []byte(markerContent), 0600)

	if total > 0 {
		log.Printf("[db] JSONL 迁移完成: %d 条日志，耗时 %v", total, time.Since(start))
	}
}

// migrateJSONLFile 读取单个 JSONL 文件并写入 db，返回成功条数
func (d *DB) migrateJSONLFile(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry proxy.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		// 旧日志没有 UserAgent，source 已有解析后的名称，UA 留空
		if err := d.InsertLog(&entry); err != nil {
			continue
		}
		count++
	}
	return count
}
