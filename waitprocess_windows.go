//go:build windows

package main

import (
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

// waitForProcessExit 等待目标进程退出（或超时）。自动更新重启时新进程用它
// 等旧进程完全退出（释放命名 Mutex/端口）后再继续初始化。
func waitForProcessExit(pid string, timeout time.Duration) {
	pidInt, err := strconv.Atoi(pid)
	if err != nil || pidInt <= 0 {
		return
	}
	// SYNCHRONIZE 权限即可用于 WaitForSingleObject。
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pidInt))
	if err != nil {
		// 进程已不存在（或无权限），直接继续。
		return
	}
	defer windows.CloseHandle(h)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// 50ms 超时轮询：WAIT_OBJECT_0=0 表示进程已退出；
		// WAIT_TIMEOUT(258) 表示仍在运行，继续等到 deadline。
		s, err := windows.WaitForSingleObject(h, 50)
		if err != nil || s == windows.WAIT_OBJECT_0 {
			return
		}
	}
}
