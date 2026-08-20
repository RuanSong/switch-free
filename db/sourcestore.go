package db

import "database/sql"

// UpsertSource 按 (name, user_agent) 去重插入，返回 id
func (d *DB) UpsertSource(name, userAgent string) (int64, error) {
	res, err := d.conn.Exec(
		`INSERT INTO sources (name, user_agent) VALUES (?, ?)
		 ON CONFLICT(name, user_agent) DO UPDATE SET name=excluded.name`,
		name, userAgent,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// QuerySource 按名称查询 source（用于日志查询时 JOIN）
func (d *DB) QuerySource(name string) (int64, error) {
	var id int64
	err := d.conn.QueryRow(`SELECT id FROM sources WHERE name = ? LIMIT 1`, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// SourceInfo sources 表的一行（用于前端 UA 辅助配置）
type SourceInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	UserAgent string `json:"userAgent"`
	Count     int    `json:"count"`
}

// ListSources 列出所有 source 及其请求次数（按次数降序）。
// 次数来自 usage_stats 聚合表，清空 logs 不影响。
func (d *DB) ListSources() ([]SourceInfo, error) {
	rows, err := d.conn.Query(`
		SELECT s.id, s.name, s.user_agent, COALESCE(SUM(u.reqs), 0) AS cnt
		FROM sources s
		LEFT JOIN usage_stats u ON u.source_id = s.id
		GROUP BY s.id
		ORDER BY cnt DESC, s.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SourceInfo
	for rows.Next() {
		var si SourceInfo
		if err := rows.Scan(&si.ID, &si.Name, &si.UserAgent, &si.Count); err != nil {
			continue
		}
		out = append(out, si)
	}
	return out, nil
}

// SourceModelStat 某个 source 历史请求过的模型及次数
type SourceModelStat struct {
	Model    string `json:"model"`
	Count    int    `json:"count"`
	LastSeen string `json:"lastSeen"`
}

// QueryModelsBySource 查询某个 source name 历史请求过的模型（按次数降序）。
// 读 usage_stats 聚合表（model_id 已存实际生效模型），lastSeen 用小时桶近似；清空 logs 不影响。
func (d *DB) QueryModelsBySource(sourceName string) ([]SourceModelStat, error) {
	rows, err := d.conn.Query(`
		SELECT COALESCE(m.model_id, '') AS model,
		       SUM(u.reqs)          AS cnt,
		       MAX(u.hour)          AS last_seen
		FROM usage_stats u
		JOIN sources s ON u.source_id = s.id
		LEFT JOIN models m ON u.model_id = m.id
		WHERE s.name = ?
		  AND m.model_id != ''
		GROUP BY m.model_id
		ORDER BY cnt DESC
	`, sourceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SourceModelStat
	for rows.Next() {
		var st SourceModelStat
		if err := rows.Scan(&st.Model, &st.Count, &st.LastSeen); err != nil {
			continue
		}
		out = append(out, st)
	}
	return out, nil
}
