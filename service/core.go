package service

import (
	"context"
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
	joycode   *upstream.JoyCodeUpstream
	deveco    *upstream.DevEcoUpstream
	opencode  *upstream.OpenCodeUpstream
	workbuddy *upstream.WorkBuddyUpstream

	// 凭据管理器（用于刷新操作）
	joycodeMgr   *creds.JoyCodeCredManager
	devecoMgr    *creds.DevEcoCredManager
	opencodeMgr  *creds.OpenCodeCredManager
	workbuddyMgr *creds.WorkBuddyCredManager

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
	wbMgr *creds.WorkBuddyCredManager,
	jy *upstream.JoyCodeUpstream,
	de *upstream.DevEcoUpstream,
	oc *upstream.OpenCodeUpstream,
	wb *upstream.WorkBuddyUpstream,
	server *proxy.Server,
) {
	c.joycodeMgr = jyMgr
	c.devecoMgr = deMgr
	c.opencodeMgr = ocMgr
	c.workbuddyMgr = wbMgr
	c.joycode = jy
	c.deveco = de
	c.opencode = oc
	c.workbuddy = wb
	c.server = server
	// 把 Core 注册为代理的事件日志器
	server.Logger = c
}

// Server 暴露代理服务
func (c *Core) Server() *proxy.Server { return c.server }

// Upstreams 暴露四上游适配器（供 ConfigService 拉取模型列表）
func (c *Core) Upstreams() (upstream.Upstream, upstream.Upstream, upstream.Upstream, upstream.Upstream) {
	return c.joycode, c.deveco, c.opencode, c.workbuddy
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

// AllCredStatus 四上游 + 免费 API 凭据状态
type AllCredStatus struct {
	JoyCode  *creds.CredStatusInfo          `json:"joycode"`
	DevEco   *creds.CredStatusInfo          `json:"deveco"`
	OpenCode *creds.CredStatusInfo          `json:"opencode"`
	WorkBuddy *creds.CredStatusInfo         `json:"workbuddy"`
	FreeAPIs map[string]*creds.CredStatusInfo `json:"freeAPIs,omitempty"` // 免费 API 供应商（动态）
}

// GetCredStatus 获取全部凭据状态
func (c *Core) GetCredStatus() *AllCredStatus {
	c.mu.RLock()
	server := c.server
	c.mu.RUnlock()

	status := &AllCredStatus{
		JoyCode:   c.joycode.CredStatus(),
		DevEco:    c.deveco.CredStatus(),
		OpenCode:  c.opencode.CredStatus(),
		WorkBuddy: c.workbuddy.CredStatus(),
	}
	if server != nil {
		freeAPIs := server.GetFreeAPIs()
		if len(freeAPIs) > 0 {
			status.FreeAPIs = map[string]*creds.CredStatusInfo{}
			for pid, up := range freeAPIs {
				status.FreeAPIs[pid] = up.CredStatus()
			}
		}
	}
	return status
}

// FreeUpstreams 返回免费 API 上游集合（key = provider id）
func (c *Core) FreeUpstreams() map[string]upstream.Upstream {
	c.mu.RLock()
	server := c.server
	c.mu.RUnlock()
	if server == nil {
		return nil
	}
	return server.GetFreeAPIs()
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
	case "workbuddy":
		c.workbuddy.InvalidateCreds()
		_, err := c.workbuddyMgr.EnsureCreds()
		return err
	}
	return nil
}

// RefreshAllCreds 刷新全部凭据
func (c *Core) RefreshAllCreds() {
	c.RefreshCreds("joycode")
	c.RefreshCreds("deveco")
	c.RefreshCreds("opencode")
	c.RefreshCreds("workbuddy")
	// 推送状态更新
	c.EmitEvent("cred:change", c.GetCredStatus())
}

// WatchInstalledAgents 后台周期探测各 agent 安装状态。
// 检测到「未安装 -> 已安装」时自动校验该上游凭据并推送状态，
// 让引导界面无需重启即可反映新安装的工具。filesystem/PATH 探测很廉价，
// 每 interval 扫一次即可。应在独立 goroutine 中运行，ctx 取消时退出。
func (c *Core) WatchInstalledAgents(ctx context.Context, interval time.Duration) {
	// 已探测为「已安装」的 upstream 集合，避免重复校验
	installed := map[string]bool{}
	for _, a := range creds.AgentRegistry {
		if creds.IsAgentInstalled(&a) {
			installed[a.Upstream] = true
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := false
			for i := range creds.AgentRegistry {
				a := &creds.AgentRegistry[i]
				now := creds.IsAgentInstalled(a)
				if now && !installed[a.Upstream] {
					// 新安装：触发凭据校验（失败仅记录，等待用户登录）
					_ = c.RefreshCreds(a.Upstream)
					changed = true
				}
				installed[a.Upstream] = now
			}
			if changed {
				c.EmitEvent("cred:change", c.GetCredStatus())
			}
		}
	}
}

// emitCredChange 推送凭据状态变化
func (c *Core) emitCredChange() {
	c.EmitEvent("cred:change", c.GetCredStatus())
}

// EmitFreeHealth 免费模型健康状态变化回调（实现 freeapi.HealthReporter）
func (c *Core) EmitFreeHealth(health map[string]map[string]bool) {
	c.EmitEvent("freeapi:health", health)
	// 同时刷新凭据状态（模型健康变化影响可用性展示）
	c.EmitEvent("cred:change", c.GetCredStatus())
}

// timestampNow 当前时间字符串（避免在 hot path 反复格式化）
func timestampNow() string {
	return time.Now().Format("15:04:05")
}