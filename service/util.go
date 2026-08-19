package service

import (
	"os/exec"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// openBrowser 用系统默认浏览器打开 URL（跨平台）。
//
// 优先走 Wails 官方 API（底层 github.com/pkg/browser，Windows 用 ShellExecute），
// 与托盘菜单「去 GitHub Star」按钮保持一致，避免旧版 rundll32 url.dll,FileProtocolHandler
// 在部分 Windows 机器上对复杂 URL / 非 Edge 默认浏览器静默失败的问题。
// 无 Wails app 实例时（如纯 server 模式）回退到直接调系统命令。
func openBrowser(url string) {
	if url == "" {
		return
	}
	if app := application.Get(); app != nil {
		_ = app.Browser.OpenURL(url)
		return
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// 回退路径：用 cmd /c start "" <url> 比 rundll32 url.dll 更稳，
		// 空标题 "" 防止 URL 首段被当成窗口标题。
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}
