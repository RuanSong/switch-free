package service

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"switchfree/creds"
	"switchfree/proxy"
	"switchfree/upstream"
)

// Core 共享核心：持有代理服务、三上游、日志存储，并实现 proxy.EventLogger
type Core struct {
	mu       sync.RWMutex
	server   *proxy.Server
	joycode  *upstream.JoyCodeUpstream
	deveco   *upstream.DevEcoUpstream
	opencode *upstream.OpenCodeUpstream

	// 凭据管理器（用于刷新操作）
	joycodeMgr  *creds.JoyCodeCredManager
	devecoMgr   *creds.DevEcoCredManager
	opencodeMgr *creds.OpenCodeCredManager

	// 请求日志环形 buffer
	logMu     sync.RWMutex
	logs      []*proxy.LogEntry
	logMaxLen int

	// 统计
	statsMu      sync.RWMutex
	totalReqs    int64
	successReqs  int64
	errorReqs    int64
	authErrReqs  int64
}

func NewCore() *Core {
	return &Core{
		logMaxLen: 500,
	}
}

// Setup 初始化所有组件（在 main 中调用）
func (c *Core) Setup(
	jyMgr *creds.JoyCodeCredManager,
	deMgr *creds.DevEcoCredManager,
	ocMgr *creds.OpenCodeCredManager,
	jy *upstream.JoyCodeUpstream,
	de *upstream.DevEcoUpstream,
	oc *upstream.OpenCodeUpstream,
	server *proxy.Server,
) {
	c.joycodeMgr = jyMgr
	c.devecoMgr = deMgr
	c.opencodeMgr = ocMgr
	c.joycode = jy
	c.deveco = de
	c.opencode = oc
	c.server = server
	// 把 Core 注册为代理的事件日志器
	server.Logger = c
}

// Server 暴露代理服务
func (c *Core) Server() *proxy.Server { return c.server }

// Upstreams 暴露三上游适配器（供 ConfigService 拉取模型列表）
func (c *Core) Upstreams() (upstream.Upstream, upstream.Upstream, upstream.Upstream) {
	return c.joycode, c.deveco, c.opencode
}

// ====== proxy.EventLogger 实现 ======

// RecordLog 记录请求日志（由代理调用）+ 推送到前端
func (c *Core) RecordLog(entry *proxy.LogEntry) {
	c.logMu.Lock()
	c.logs = append(c.logs, entry)
	if len(c.logs) > c.logMaxLen {
		c.logs = c.logs[len(c.logs)-c.logMaxLen:]
	}
	c.logMu.Unlock()

	// 更新统计
	c.statsMu.Lock()
	c.totalReqs++
	switch entry.Status {
	case "success":
		c.successReqs++
	case "error":
		c.errorReqs++
	case "auth_error":
		c.authErrReqs++
	}
	c.statsMu.Unlock()

	// 实时推送到前端
	c.EmitEvent("log:new", entry)

	// 异步持久化到磁盘（按天 JSONL）
	go appendLogToDisk(entry)
}

// EmitEvent 推送事件到前端
func (c *Core) EmitEvent(event string, data interface{}) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(event, data)
}

// ====== 日志查询 ======

// GetRecentLogs 获取最近 N 条日志
func (c *Core) GetRecentLogs(count int) []*proxy.LogEntry {
	c.logMu.RLock()
	defer c.logMu.RUnlock()
	if count <= 0 || count > len(c.logs) {
		count = len(c.logs)
	}
	// 返回倒序（最新在前）
	result := make([]*proxy.LogEntry, count)
	for i := 0; i < count; i++ {
		result[i] = c.logs[len(c.logs)-1-i]
	}
	return result
}

// ClearLogs 清空日志
func (c *Core) ClearLogs() {
	c.logMu.Lock()
	c.logs = nil
	c.logMu.Unlock()
}

// LogStats 日志统计
type LogStats struct {
	Total     int64 `json:"total"`
	Success   int64 `json:"success"`
	Error     int64 `json:"error"`
	AuthError int64 `json:"authError"`
}

// GetLogStats 获取统计
func (c *Core) GetLogStats() *LogStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	return &LogStats{
		Total:     c.totalReqs,
		Success:   c.successReqs,
		Error:     c.errorReqs,
		AuthError: c.authErrReqs,
	}
}

// ====== 凭据状态汇总 ======

// AllCredStatus 三上游凭据状态
type AllCredStatus struct {
	JoyCode  *creds.CredStatusInfo `json:"joycode"`
	DevEco   *creds.CredStatusInfo `json:"deveco"`
	OpenCode *creds.CredStatusInfo `json:"opencode"`
}

// GetCredStatus 获取三上游凭据状态
func (c *Core) GetCredStatus() *AllCredStatus {
	return &AllCredStatus{
		JoyCode:  c.joycode.CredStatus(),
		DevEco:   c.deveco.CredStatus(),
		OpenCode: c.opencode.CredStatus(),
	}
}

// RefreshCreds 强制刷新某上游凭据
func (c *Core) RefreshCreds(name string) error {
	switch name {
	case "joycode":
		c.joycode.InvalidateCreds()
		_, err := c.joycodeMgr.EnsureCreds()
		return err
	case "deveco":
		c.deveco.InvalidateCreds()
		_, err := c.devecoMgr.EnsureCreds()
		return err
	case "opencode":
		c.opencode.InvalidateCreds()
		_, err := c.opencodeMgr.EnsureCreds()
		return err
	}
	return nil
}

// RefreshAllCreds 刷新全部凭据
func (c *Core) RefreshAllCreds() {
	c.RefreshCreds("joycode")
	c.RefreshCreds("deveco")
	c.RefreshCreds("opencode")
	// 推送状态更新
	c.EmitEvent("cred:change", c.GetCredStatus())
}

// emitCredChange 推送凭据状态变化
func (c *Core) emitCredChange() {
	c.EmitEvent("cred:change", c.GetCredStatus())
}

// timestampNow 当前时间字符串（避免在 hot path 反复格式化）
func timestampNow() string {
	return time.Now().Format("15:04:05")
}