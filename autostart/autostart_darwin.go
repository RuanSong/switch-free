//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

const plistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>{{.Name}}</string>
    <key>ProgramArguments</key>
    <array>
      {{- range .Exec}}
      <string>{{.}}</string>
      {{- end}}
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>ProcessType</key>
    <string>Interactive</string>
  </dict>
</plist>
`

func (a *App) plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	return filepath.Join(dir, a.Name+".plist"), nil
}

// IsEnabled LaunchAgent plist 是否存在。
func (a *App) IsEnabled() bool {
	p, err := a.plistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Enable 写入 LaunchAgent（RunAtLoad 实现登录自启）。
func (a *App) Enable() error {
	if len(a.Exec) == 0 {
		return fmt.Errorf("autostart: Exec 不能为空")
	}
	p, err := a.plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	t := template.Must(template.New("plist").Parse(plistTmpl))
	return t.Execute(f, a)
}

// Disable 删除 LaunchAgent（不存在视为成功）。
func (a *App) Disable() error {
	p, err := a.plistPath()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
