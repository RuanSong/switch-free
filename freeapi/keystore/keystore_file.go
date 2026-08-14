//go:build !darwin

package keystore

import (
	"os"
	"path/filepath"
	"strings"
)

// 非 macOS 平台暂时用 0600 文件兜底（后续 Windows 可接 DPAPI、Linux 接 libsecret）。
// 文件存放在配置目录下，注意它本身受文件系统权限保护，但安全性低于系统钥匙串。
type fileBackend struct{}

var platformBackend backend = fileBackend{}

func secretDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "switch-dev", "secrets")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "switch-dev", "secrets")
}

func secretPath(account string) string {
	return filepath.Join(secretDir(), sanitize(account)+".secret")
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, string(filepath.Separator), "_")
	return strings.ReplaceAll(s, "..", "_")
}

func (fileBackend) set(_, account, value string) error {
	if err := os.MkdirAll(secretDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(secretPath(account), []byte(value), 0o600)
}

func (fileBackend) get(_, account string) (string, error) {
	b, err := os.ReadFile(secretPath(account))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

func (fileBackend) delete(_, account string) error {
	if err := os.Remove(secretPath(account)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
