// Package logfile 把控制台日志（log.Printf / fmt.Print* 等所有写到 os.Stdout/os.Stderr 的内容）
// 同时落到磁盘文件，按天切分：<AppConfigDir>/logs/YYYY-MM-DD.log。
// 后台定期把 7 天前的日志 gzip 压缩、90 天前的压缩包删除，以节省磁盘。
// 可通过 Config.LogFile.Enabled 运行时开关。
package logfile

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	dayFormat       = "2006-01-06"
	compressAge     = 7 * 24 * time.Hour  // 7 天前的 .log 压缩为 .gz
	deleteAge       = 90 * 24 * time.Hour // 90 天前的 .gz 删除
	maintenanceTick = 6 * time.Hour       // 后台维护扫描间隔
)

// Manager 管理文件日志的生命周期
type Manager struct {
	dir string
	dw  *dailyWriter

	origStdout *os.File
	origStderr *os.File

	pipeR *os.File
	pipeW *os.File

	stop    chan struct{}
	wg      sync.WaitGroup
	started atomic.Bool
}

// New 创建管理器；dir 为日志目录（通常是 AppConfigDir/logs）
func New(dir string) *Manager {
	return &Manager{
		dir:  dir,
		dw:   &dailyWriter{dir: dir},
		stop: make(chan struct{}),
	}
}

// Start 初始化目录、接管 stdout/stderr 与标准 log 输出，并启动后台维护。
// enabled 为 false 时不写文件，但控制台输出不受影响。
func (m *Manager) Start(enabled bool) error {
	if m.started.Load() {
		return nil
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	m.dw.enabled.Store(enabled)

	m.origStdout = os.Stdout
	m.origStderr = os.Stderr

	// pipe 接管：所有写 os.Stdout/os.Stderr 的内容走 pipe，
	// 由 goroutine 同时写到当日日志文件 + 原始 stderr（终端）。
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("创建日志 pipe 失败: %w", err)
	}
	m.pipeR, m.pipeW = r, w
	os.Stdout = w
	os.Stderr = w

	// 标准 log 默认输出到 os.Stderr，但它在 init 时可能已捕获旧 fd，显式重定向。
	// 直接 tee 到文件 writer + 原始 stderr，不经过 pipe，避免重复写入。
	log.SetOutput(io.MultiWriter(m.dw, m.origStderr))

	m.wg.Add(2)
	go m.consumePipe()
	go m.maintenanceLoop()

	m.started.Store(true)

	// 启动时先做一次维护（压缩/清理旧日志）
	go m.maintenance()
	return nil
}

// SetEnabled 运行时开关文件日志
func (m *Manager) SetEnabled(enabled bool) {
	m.dw.enabled.Store(enabled)
}

// Enabled 返回当前是否启用
func (m *Manager) Enabled() bool {
	return m.dw.enabled.Load()
}

// Stop 恢复 stdout/stderr，刷新并关闭文件。可安全重复调用。
func (m *Manager) Stop() {
	if !m.started.CompareAndSwap(true, false) {
		return
	}
	close(m.stop)
	// 关闭写端，让 consumePipe 的 io.Copy 收到 EOF 退出
	if m.pipeW != nil {
		_ = m.pipeW.Close()
	}
	m.wg.Wait()

	log.SetOutput(m.origStderr)
	os.Stdout = m.origStdout
	os.Stderr = m.origStderr
	if m.pipeR != nil {
		_ = m.pipeR.Close()
	}
	m.dw.close()
}

// consumePipe 持续读取 pipe，tee 到日志文件和原始 stderr
func (m *Manager) consumePipe() {
	defer m.wg.Done()
	_, _ = io.Copy(io.MultiWriter(m.dw, m.origStderr), m.pipeR)
}

// maintenanceLoop 定期扫描日志目录做压缩/清理
func (m *Manager) maintenanceLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(maintenanceTick)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.maintenance()
		}
	}
}

// maintenance 压缩旧 .log、删除旧 .gz
func (m *Manager) maintenance() {
	now := time.Now()
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		full := filepath.Join(m.dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())

		if strings.HasSuffix(name, ".log") && age >= compressAge {
			m.compressLog(full)
		} else if strings.HasSuffix(name, ".gz") && age >= deleteAge {
			_ = os.Remove(full)
		}
	}
}

// compressLog 把单个 .log 压缩为同名 .gz，成功后删除原文件；
// 若 .gz 已存在则覆盖；正在写入的当日文件不会被选中（modTime 较新）。
func (m *Manager) compressLog(path string) {
	in, err := os.Open(path)
	if err != nil {
		return
	}
	defer in.Close()

	gzPath := path + ".gz"
	tmp := gzPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return
	}
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, gzPath); err != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Remove(path)
}

// dailyWriter 按天写文件的并发安全 io.Writer
type dailyWriter struct {
	dir string

	mu      sync.Mutex
	f       *os.File
	date    string
	enabled atomic.Bool
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	// 始终返回 len(p)，即使被禁用也不能让调用方报错
	if !w.enabled.Load() {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format(dayFormat)
	if w.f == nil || today != w.date {
		if err := w.rotate(today); err != nil {
			return len(p), nil // 写文件失败不影响控制台输出
		}
	}
	_, _ = w.f.Write(p)
	return len(p), nil
}

func (w *dailyWriter) rotate(date string) error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	path := filepath.Join(w.dir, date+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.f = f
	w.date = date
	return nil
}

func (w *dailyWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
}
