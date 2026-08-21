//go:build darwin || linux

package main

import (
	"strconv"
	"syscall"
	"time"
)

// waitForProcessExit 轮询目标 PID 是否仍存活，进程退出或超时后返回。
// 自动更新重启时新进程用它等待旧进程完全退出（释放单实例锁/端口）后再继续初始化。
func waitForProcessExit(pid string, timeout time.Duration) {
	pidInt, err := strconv.Atoi(pid)
	if err != nil || pidInt <= 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		// kill -0：不发信号，仅探测进程是否存在。
		// 不存在返回 ESRCH；存在（即便无权限）返回 nil 或 EPERM，都视为还活着。
		if err := syscall.Kill(pidInt, 0); err == syscall.ESRCH {
			return
		}
		<-ticker.C
	}
}
