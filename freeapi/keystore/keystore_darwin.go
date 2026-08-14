//go:build darwin

package keystore

import (
	"errors"
	"os/exec"
	"strings"
)

type darwinBackend struct{}

var platformBackend backend = darwinBackend{}

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
	return nil
}

func (darwinBackend) get(service, account string) (string, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-a", account,
		"-s", service,
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		// 不存在时 security 返回非 0，视为空
		return "", nil
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (darwinBackend) delete(service, account string) error {
	return deleteByAccount(service, account)
}

func deleteByAccount(service, account string) error {
	cmd := exec.Command("security", "delete-generic-password",
		"-a", account,
		"-s", service,
	)
	_ = cmd.Run() // 删除失败（如不存在）忽略
	return nil
}
