//go:build darwin

package keystore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type darwinBackend struct{}

var platformBackend backend = darwinBackend{}

// macOS 主存储是 Login Keychain；同时在配置目录写一份 0600 文件作为冗余兜底。
// 钥匙串项可能因 dev 模式反复重启/多实例竞争而被覆盖或丢失，导致自动加密的随机主密码
// 永久失踪（进而无法解密 apiKey）。文件副本受用户目录权限保护（同 Linux 文件后端），
// 安全性低于钥匙串，但能在钥匙串项缺失时让应用自动恢复，避免凭据永久不可用。
func fallbackSecretDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "switch-dev", "secrets")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "switch-dev", "secrets")
}

func fallbackSecretPath(account string) string {
	s := strings.ReplaceAll(account, string(filepath.Separator), "_")
	s = strings.ReplaceAll(s, "..", "_")
	return filepath.Join(fallbackSecretDir(), s+".secret")
}

func (darwinBackend) set(service, account, value string) error {
	// 先删旧项，再 add-generic-password（add 不覆盖会报重复）
	_ = deleteByAccount(service, account)
	cmd := exec.Command("security", "add-generic-password",
		"-a", account,
		"-s", service,
		"-w", value,
		"-U", // update if exists
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("keychain set failed: " + strings.TrimSpace(string(out)))
	}
	// 钥匙串写成功后，再写文件冗余（失败不影响主路径）
	if err := os.MkdirAll(fallbackSecretDir(), 0o700); err == nil {
		_ = os.WriteFile(fallbackSecretPath(account), []byte(value), 0o600)
	}
	return nil
}

func (darwinBackend) get(service, account string) (string, error) {
	// 1. 优先从钥匙串读取
	if v, ok := readKeychain(service, account); ok && v != "" {
		return v, nil
	}
	// 2. 钥匙串缺失：从文件冗余恢复，并回填钥匙串（自愈）
	if b, err := os.ReadFile(fallbackSecretPath(account)); err == nil {
		v := strings.TrimRight(string(b), "\n")
		if v != "" {
			_ = writeKeychainOnly(service, account, v) // 回填失败不影响读取
		}
		return v, nil
	}
	return "", nil
}

func (darwinBackend) delete(service, account string) error {
	_ = deleteByAccount(service, account)
	// 同步删除文件冗余
	if err := os.Remove(fallbackSecretPath(account)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// recover 依次尝试钥匙串和文件冗余副本，返回第一个通过 valid 校验的密码。
// 两个方向都做自愈：
//   - 钥匙串正确、文件缺失/不一致 -> 补写文件（让老用户升级后即建立冗余）；
//   - 文件正确、钥匙串缺失/过期   -> 回填钥匙串。
func (darwinBackend) recover(service, account string, valid func(string) bool) string {
	kc, kcOK := readKeychain(service, account)
	fileVal, fileErr := os.ReadFile(fallbackSecretPath(account))
	fileStr := ""
	if fileErr == nil {
		fileStr = strings.TrimRight(string(fileVal), "\n")
	}

	// 1. 钥匙串密码正确：补写文件副本（若缺失或不一致），返回
	if kcOK && kc != "" && valid(kc) {
		if kc != fileStr {
			if err := os.MkdirAll(fallbackSecretDir(), 0o700); err == nil {
				_ = os.WriteFile(fallbackSecretPath(account), []byte(kc), 0o600)
			}
		}
		return kc
	}
	// 2. 钥匙串不可用或密码过期：试文件冗余副本
	if fileStr != "" && valid(fileStr) {
		_ = writeKeychainOnly(service, account, fileStr) // 回填钥匙串
		return fileStr
	}
	return ""
}

// readKeychain 从钥匙串读取一项；找不到或出错返回 ("", false)。
func readKeychain(service, account string) (string, bool) {
	cmd := exec.Command("security", "find-generic-password",
		"-a", account,
		"-s", service,
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

// writeKeychainOnly 只写钥匙串（用于从文件冗余回填，不碰文件副本）。
func writeKeychainOnly(service, account, value string) error {
	_ = deleteByAccount(service, account)
	cmd := exec.Command("security", "add-generic-password",
		"-a", account,
		"-s", service,
		"-w", value,
		"-U",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("keychain set failed: " + strings.TrimSpace(string(out)))
	}
	return nil
}

func deleteByAccount(service, account string) error {
	cmd := exec.Command("security", "delete-generic-password",
		"-a", account,
		"-s", service,
	)
	_ = cmd.Run() // 删除失败（如不存在）忽略
	return nil
}
