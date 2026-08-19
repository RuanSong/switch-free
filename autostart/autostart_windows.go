//go:build windows

package autostart

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// IsEnabled 是否已在当前用户登录项注册。
func (a *App) IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(a.Name)
	return err == nil && strings.TrimSpace(v) != ""
}

// Enable 写入登录项（命令用引号包裹可执行路径，兼容含空格的路径）。
func (a *App) Enable() error {
	if len(a.Exec) == 0 {
		return fmt.Errorf("autostart: Exec 不能为空")
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(a.Name, quoteCmd(a.Exec))
}

// Disable 移除登录项（不存在视为成功）。
func (a *App) Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		// 键不存在即无需处理
		return nil
	}
	defer k.Close()
	err = k.DeleteValue(a.Name)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

// quoteCmd 把可执行路径（加引号以兼容空格）和参数拼成一行命令。
func quoteCmd(exec []string) string {
	var b strings.Builder
	b.WriteString(`"` + exec[0] + `"`)
	for _, a := range exec[1:] {
		b.WriteByte(' ')
		// 含空格的参数加引号
		if strings.ContainsAny(a, " \t\"") {
			b.WriteString(`"` + a + `"`)
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}
