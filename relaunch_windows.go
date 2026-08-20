//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// startNewInstance 启动新实例并与当前进程分离，
// 这样旧进程退出时不会带走新进程（自动更新后真正实现自动重启）。
//
// DETACHED_PROCESS：不继承控制台；
// CREATE_NEW_PROCESS_GROUP：新进程组，不受父进程控制台事件影响。
// （不加 CREATE_BREAKAWAY_FROM_JOB：若当前进程在不允许 breakaway 的 Job 中，
// 该标志会让 CreateProcess 直接失败；普通桌面应用通常不在 Job 里，上述两个标志已足够。）
func startNewInstance(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	const (
		detachedProcess       = 0x00000008
		createNewProcessGroup = 0x00000200
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
	return cmd.Start()
}
