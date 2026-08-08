package service

import "switchfree/proxy"

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

// GetLogsByRange 按日期范围查询日志（含 startDate 到 endDate），从磁盘读，倒序
// 日期格式 "YYYY-MM-DD"
func (s *LogService) GetLogsByRange(startDate, endDate string, limit int) []*proxy.LogEntry {
	return getLogsByRange(startDate, endDate, limit)
}

// GetLogDates 列出所有有日志文件的日期（倒序）
func (s *LogService) GetLogDates() []string {
	return listLogDates()
}

// ClearLogs 清空内存日志（磁盘日志保留，可通过日期范围查）
func (s *LogService) ClearLogs() {
	s.core.ClearLogs()
}

// GetLogStats 获取日志统计
func (s *LogService) GetLogStats() *LogStats {
	return s.core.GetLogStats()
}