//go:build darwin

package main

import (
	"os/exec"
	"strings"
	"syscall"
)

// startNewInstance 启动一个新的应用实例，并让它脱离当前进程组，
// 这样旧进程 app.Quit() 退出时不会把新进程一起带走（自动更新后真正实现自动重启）。
//
// 若运行在 .app bundle 里，优先用 `open` 启动整个 bundle，确保新实例以正常
// GUI 应用方式唤起（获得窗口、Dock 焦点），而不是后台裸进程。
func startNewInstance(exe string, args []string) error {
	if appPath := bundlePath(exe); appPath != "" {
		// open -n 强制新开实例（即便已有同名应用在跑）；--args 透传参数
		var openArgs []string
		if len(args) > 0 {
			openArgs = append([]string{"-n", appPath, "--args"}, args...)
		} else {
			openArgs = []string{"-n", appPath}
		}
		cmd := exec.Command("open", openArgs...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return nil
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// bundlePath 从可执行路径推断 .app bundle 根目录；不在 bundle 里则返回空。
// 形如 /Applications/Switch Dev.app/Contents/MacOS/switch-dev
func bundlePath(exe string) string {
	idx := strings.Index(exe, ".app/Contents/MacOS/")
	if idx < 0 {
		return ""
	}
	// 找到 .app 的起始位置（往前到上一个路径分隔符）
	dotApp := strings.LastIndex(exe[:idx], "/")
	if dotApp < 0 {
		return ""
	}
	return exe[:dotApp+1] + exe[dotApp+1:idx] + ".app"
}
