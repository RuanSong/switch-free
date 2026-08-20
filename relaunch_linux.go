//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// startNewInstance 启动新实例并脱离当前进程组，旧进程退出时不带走新进程。
func startNewInstance(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
