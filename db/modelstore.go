package db

import "time"

// ModelMeta 模型元数据（用于 upsert）
type ModelMeta struct {
	Label    string
	Context  int
	Output   int
	Stream   bool
	Vision   bool
	ToolCall bool
	Free     bool
}

// BenchResult 测评结果（写入 models 表的 bench_* 字段）
type BenchResult struct {
	TPS      float64
	Duration int64
	Error    string
	Success  bool
}

// ModelEntry 批量同步模型时的条目
type ModelEntry struct {
	UpstreamName string
	ModelID      string
	Label        string
	Context      int
	Output       int
	Stream       bool
	Vision       bool
	ToolCall     bool
	Free         bool
}

// UpsertModel 按 (upstream_id, model_id) 去重插入/更新，返回 model id。
// meta 中非零值才覆盖已有记录。
func (d *DB) UpsertModel(upstreamID int64, modelID, label string, meta ModelMeta) (int64, error) {
	if upstreamID == 0 {
		return 0, nil
	}

	// 先尝试 INSERT OR IGNORE
	res, err := d.conn.Exec(
		`INSERT OR IGNORE INTO models (upstream_id, model_id, label, context, output, stream, vision, tool_call, free)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		upstreamID, modelID, label,
		meta.Context, meta.Output,
		boolToInt(meta.Stream), boolToInt(meta.Vision), boolToInt(meta.ToolCall), boolToInt(meta.Free),
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id > 0 {
		return id, nil // 新插入
	}
	// 已存在：查 id，并用非零值更新
	var existingID int64
	err = d.conn.QueryRow(
		`SELECT id FROM models WHERE upstream_id = ? AND model_id = ?`,
		upstreamID, modelID,
	).Scan(&existingID)
	if err != nil {
		return 0, err
	}
	// 更新 label 和元数据（COALESCE 逻辑：只在新值非零时覆盖）
	_, err = d.conn.Exec(
		`UPDATE models SET
			label    = CASE WHEN ? != '' THEN ? ELSE label END,
			context  = CASE WHEN ? > 0 THEN ? ELSE context END,
			output   = CASE WHEN ? > 0 THEN ? ELSE output END,
			stream   = CASE WHEN ? = 1 THEN 1 ELSE stream END,
			vision   = CASE WHEN ? = 1 THEN 1 ELSE vision END,
			tool_call= CASE WHEN ? = 1 THEN 1 ELSE tool_call END,
			free     = CASE WHEN ? = 1 THEN 1 ELSE free END
		 WHERE id = ?`,
		label, label,
		meta.Context, meta.Context,
		meta.Output, meta.Output,
		boolToInt(meta.Stream), boolToInt(meta.Vision), boolToInt(meta.ToolCall), boolToInt(meta.Free),
		existingID,
	)
	return existingID, err
}

// UpsertModelByUpstreamName 按 upstream name + model_id upsert（便捷方法）
func (d *DB) UpsertModelByUpstreamName(upstreamName, modelID, label string, meta ModelMeta) (int64, error) {
	upID := d.QueryUpstreamID(upstreamName)
	if upID == 0 {
		// upstream 不存在，先创建（默认 builtin 类型）
		var err error
		upID, err = d.UpsertUpstream(upstreamName, "builtin", upstreamName, "")
		if err != nil {
			return 0, err
		}
	}
	return d.UpsertModel(upID, modelID, label, meta)
}

// BenchRecord 持久化的测评结果（models 表中的 bench_* 列）
type BenchRecord struct {
	Upstream string
	Model    string
	TPS      float64
	Duration int64  // 毫秒
	BenchAt  string // 测评时间文本
	Error    string
	Success  bool
}

// QueryBenchResults 返回所有测评过的模型结果（bench_at 非空），按时间倒序。
func (d *DB) QueryBenchResults() ([]BenchRecord, error) {
	rows, err := d.conn.Query(`
		SELECT u.name, m.model_id, m.bench_tps, m.bench_duration, m.bench_at, m.bench_error, m.verified
		FROM models m
		JOIN upstreams u ON m.upstream_id = u.id
		WHERE m.bench_at != ''
		ORDER BY m.bench_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BenchRecord
	for rows.Next() {
		var r BenchRecord
		var verified int
		if err := rows.Scan(&r.Upstream, &r.Model, &r.TPS, &r.Duration, &r.BenchAt, &r.Error, &verified); err != nil {
			continue
		}
		// 有 TPS 且无错误记为成功（verified 列可能被其他路径置位，不作为唯一依据）
		r.Success = r.TPS > 0 && r.Error == ""
		out = append(out, r)
	}
	return out, nil
}

// UpsertModelBench 更新模型测评结果
func (d *DB) UpsertModelBench(upstreamName, modelID string, bench BenchResult) error {
	upID := d.QueryUpstreamID(upstreamName)
	if upID == 0 {
		var err error
		upID, err = d.UpsertUpstream(upstreamName, "builtin", upstreamName, "")
		if err != nil {
			return err
		}
	}
	// 确保 model 行存在
	modelDBID, err := d.UpsertModel(upID, modelID, modelID, ModelMeta{})
	if err != nil {
		return err
	}

	verified := 0
	healthy := 0
	errMsg := bench.Error
	if bench.Success {
		verified = 1
		healthy = 1
		errMsg = ""
	}

	_, err = d.conn.Exec(
		`UPDATE models SET
			verified = ?, healthy = ?,
			bench_tps = ?, bench_duration = ?,
			bench_at = ?, bench_error = ?
		 WHERE id = ?`,
		verified, healthy,
		bench.TPS, bench.Duration,
		time.Now().Format("2006-01-02 15:04:05"),
		errMsg,
		modelDBID,
	)
	return err
}

// UpsertModelVerified 标记模型为已验证
func (d *DB) UpsertModelVerified(upstreamName, modelID, label string) error {
	upID := d.QueryUpstreamID(upstreamName)
	if upID == 0 {
		var err error
		upID, err = d.UpsertUpstream(upstreamName, "provider", upstreamName, upstreamName)
		if err != nil {
			return err
		}
	}
	id, err := d.UpsertModel(upID, modelID, label, ModelMeta{})
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(`UPDATE models SET verified = 1 WHERE id = ?`, id)
	return err
}

// BatchUpsertModels 批量同步模型元数据
func (d *DB) BatchUpsertModels(entries []ModelEntry) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO models (upstream_id, model_id, label, context, output, stream, vision, tool_call, free)
		 SELECT id, ?, ?, ?, ?, ?, ?, ?, ? FROM upstreams WHERE name = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		_, err := stmt.Exec(
			e.ModelID, e.Label,
			e.Context, e.Output,
			boolToInt(e.Stream), boolToInt(e.Vision), boolToInt(e.ToolCall), boolToInt(e.Free),
			e.UpstreamName,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// QueryModelID 按 upstream_id + model_id 查询 model 主键
func (d *DB) QueryModelID(upstreamID int64, modelID string) int64 {
	if upstreamID == 0 || modelID == "" {
		return 0
	}
	var id int64
	err := d.conn.QueryRow(
		`SELECT id FROM models WHERE upstream_id = ? AND model_id = ?`,
		upstreamID, modelID,
	).Scan(&id)
	if err != nil {
		return 0
	}
	return id
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
