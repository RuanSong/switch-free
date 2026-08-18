package service

import "switchdev/proxy"

// LogService 请求日志服务（暴露给前端）
type LogService struct {
	core *Core
}

func NewLogService(core *Core) *LogService {
	return &LogService{core: core}
}

// GetRecentLogs 获取最近 N 条日志（内存 buffer，倒序，最新在前）
func (s *LogService) GetRecentLogs(count int) []*proxy.LogEntry {
	return s.core.GetRecentLogs(count)
}

// GetLogsByRange 按日期范围查询日志，从 SQLite 读，倒序
// 日期格式 "YYYY-MM-DD"
func (s *LogService) GetLogsByRange(startDate, endDate string, limit int) []*proxy.LogEntry {
	if s.core.DB() == nil {
		return nil
	}
	logs, err := s.core.DB().QueryLogs(startDate, endDate, limit)
	if err != nil {
		return nil
	}
	return logs
}

// GetLogDates 列出所有有日志的日期（倒序）
func (s *LogService) GetLogDates() []string {
	if s.core.DB() == nil {
		return listLogDates() // 回退到旧 JSONL
	}
	return s.core.DB().QueryLogDates()
}

// ClearLogs 清空内存日志 + 数据库日志
func (s *LogService) ClearLogs() {
	s.core.ClearLogs()
	if s.core.DB() != nil {
		_ = s.core.DB().ClearLogs()
	}
}

// GetLogStats 获取日志统计
func (s *LogService) GetLogStats() *LogStats {
	return s.core.GetLogStats()
}
