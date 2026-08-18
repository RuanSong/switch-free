package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB 封装 SQLite 连接 + 字段加密器
type DB struct {
	conn   *sql.DB
	crypto *Crypto
}

// Open 打开（或创建）SQLite 数据库，执行建表和内置 upstream 初始化
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	conn.SetMaxOpenConns(4)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	d := &DB{conn: conn, crypto: NewCrypto()}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	if err := d.syncBuiltins(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("初始化内置上游失败: %w", err)
	}

	go d.runJSONLMigration()
	return d, nil
}

// Close 关闭数据库连接
func (d *DB) Close() error {
	return d.conn.Close()
}

// SetDEK 注入或清除数据加密密钥
func (d *DB) SetDEK(dek []byte) {
	d.crypto.SetDEK(dek)
}

// Raw 返回底层 *sql.DB（迁移等特殊场景使用）
func (d *DB) Raw() *sql.DB {
	return d.conn
}

// migrate 执行建表（IF NOT EXISTS，幂等）
func (d *DB) migrate() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS sources (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			UNIQUE(name, user_agent)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_name ON sources(name)`,

		`CREATE TABLE IF NOT EXISTS providers (
			id             TEXT PRIMARY KEY,
			name           TEXT NOT NULL DEFAULT '',
			base_url       TEXT NOT NULL DEFAULT '',
			protocol       TEXT NOT NULL DEFAULT '',
			created_at     TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS upstreams (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL UNIQUE,
			type        TEXT NOT NULL DEFAULT 'builtin',
			label       TEXT NOT NULL DEFAULT '',
			provider_id TEXT DEFAULT NULL,
			FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE SET NULL
		)`,

		`CREATE TABLE IF NOT EXISTS models (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			upstream_id    INTEGER NOT NULL,
			model_id       TEXT NOT NULL DEFAULT '',
			label          TEXT NOT NULL DEFAULT '',
			context        INTEGER NOT NULL DEFAULT 0,
			output         INTEGER NOT NULL DEFAULT 0,
			stream         INTEGER NOT NULL DEFAULT 0,
			vision         INTEGER NOT NULL DEFAULT 0,
			tool_call      INTEGER NOT NULL DEFAULT 0,
			free           INTEGER NOT NULL DEFAULT 0,
			verified       INTEGER NOT NULL DEFAULT 0,
			healthy        INTEGER NOT NULL DEFAULT 0,
			bench_tps      REAL NOT NULL DEFAULT 0,
			bench_duration INTEGER NOT NULL DEFAULT 0,
			bench_at       TEXT NOT NULL DEFAULT '',
			bench_error    TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (upstream_id) REFERENCES upstreams(id),
			UNIQUE(upstream_id, model_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_models_upstream ON models(upstream_id)`,

		`CREATE TABLE IF NOT EXISTS logs (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			date_time        TEXT NOT NULL,
			date             TEXT NOT NULL,
			model_id         INTEGER NOT NULL DEFAULT 0,
			used_model_id    INTEGER NOT NULL DEFAULT 0,
			source_id        INTEGER NOT NULL DEFAULT 0,
			upstream_id      INTEGER NOT NULL DEFAULT 0,
			status           TEXT NOT NULL DEFAULT '',
			code             INTEGER NOT NULL DEFAULT 0,
			duration         INTEGER NOT NULL DEFAULT 0,
			error_msg        TEXT NOT NULL DEFAULT '',
			method           TEXT NOT NULL DEFAULT '',
			path             TEXT NOT NULL DEFAULT '',
			stream           INTEGER NOT NULL DEFAULT 0,
			request_body     TEXT NOT NULL DEFAULT '',
			response_body    TEXT NOT NULL DEFAULT '',
			input_tokens     INTEGER NOT NULL DEFAULT 0,
			output_tokens    INTEGER NOT NULL DEFAULT 0,
			cache_hit_tokens INTEGER NOT NULL DEFAULT 0,
			cost             REAL NOT NULL DEFAULT 0,
			cost_text        TEXT NOT NULL DEFAULT '',
			first_byte_ms    INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (model_id)      REFERENCES models(id),
			FOREIGN KEY (used_model_id) REFERENCES models(id),
			FOREIGN KEY (source_id)     REFERENCES sources(id),
			FOREIGN KEY (upstream_id)   REFERENCES upstreams(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_date_status   ON logs(date, status)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_date_datetime ON logs(date, date_time DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_upstream  ON logs(upstream_id)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_source    ON logs(source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_model     ON logs(model_id)`,

		`CREATE TABLE IF NOT EXISTS config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, schema := range schemas {
		if _, err := d.conn.Exec(schema); err != nil {
			return fmt.Errorf("执行 schema 失败: %w\nSQL: %s", err, schema)
		}
	}

	// 哨兵行：id=0 的 source/upstream/model，供日志外键引用空值（model="" 或 "auto" 时 upsertModelInTx 返回 0）
	// SQLite AUTOINCREMENT 从 1 开始，手动插入 id=0 不会冲突
	d.conn.Exec(`INSERT OR IGNORE INTO sources (id, name, user_agent) VALUES (0, '', '')`)
	d.conn.Exec(`INSERT OR IGNORE INTO upstreams (id, name, type, label) VALUES (0, 'unknown', 'builtin', '未知')`)
	d.conn.Exec(`INSERT OR IGNORE INTO models (id, upstream_id, model_id, label) VALUES (0, 0, '', '')`)

	return nil
}
