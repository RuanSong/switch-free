//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

const desktopTmpl = `[Desktop Entry]
Type=Application
Name={{.DisplayName}}
Exec={{.ExecLine}}
{{- if .Icon}}
Icon={{.Icon}}
{{- end}}
Terminal=false
X-GNOME-Autostart-enabled=true
`

type desktopData struct {
	*App
	ExecLine string
}

func (a *App) desktopPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", a.Name+".desktop"), nil
}

// IsEnabled .desktop 是否存在。
func (a *App) IsEnabled() bool {
	p, err := a.desktopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Enable 写入 autostart .desktop。
func (a *App) Enable() error {
	if len(a.Exec) == 0 {
		return fmt.Errorf("autostart: Exec 不能为空")
	}
	p, err := a.desktopPath()
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
	t := template.Must(template.New("desktop").Parse(desktopTmpl))
	return t.Execute(f, desktopData{App: a, ExecLine: quoteCmd(a.Exec)})
}

// Disable 删除 .desktop（不存在视为成功）。
func (a *App) Disable() error {
	p, err := a.desktopPath()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func quoteCmd(exec []string) string {
	var b strings.Builder
	b.WriteString(exec[0])
	for _, a := range exec[1:] {
		b.WriteByte(' ')
		if strings.ContainsAny(a, " \t\"") {
			b.WriteString(`"` + a + `"`)
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}
