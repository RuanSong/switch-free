package providerapi

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/shirou/gopsutil/v3/host"
)

// machineWrap 用本机机器码派生 KEK 包裹 DEK 的包裹层。
// 与「记住密码」共享同一信任边界（本机当前用户可访问），但不依赖系统钥匙串/DPAPI，
// 因而不受 macOS 自更新后二进制签名/CDHash 变化导致钥匙串 ACL 失效的影响。
// 仅在「本就允许无密码解锁」时存在：自动加密（masterSet=false）或用户勾选记住密码。
type machineWrap struct {
	Salt       string `json:"salt"`
	WrappedDEK sealed `json:"wrappedDEK"`
}

// machineID 返回稳定的本机机器标识（macOS IOPlatformUUID / Windows MachineGuid / Linux /etc/machine-id）。
// 该值是标识符而非机密；取不到时返回 error，调用方据此跳过机器包裹路径降级到钥匙串。
func machineID() (string, error) {
	id, err := host.HostID()
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("machine id 为空")
	}
	return id, nil
}

// wrapDEKForMachine 用机器码派生 KEK 包裹 DEK，返回可落盘的 machineWrap。
func wrapDEKForMachine(dek []byte, id string) (*machineWrap, error) {
	salt := randomBytes(kdfSaltLen)
	kek := deriveKey(id, salt)
	wrapped, err := aesGCMSeal(kek, dek)
	if err != nil {
		return nil, err
	}
	return &machineWrap{
		Salt:       base64.StdEncoding.EncodeToString(salt),
		WrappedDEK: *wrapped,
	}, nil
}

// openMachineWrap 用机器码解开机器包裹层，返回 DEK。
func openMachineWrap(mw *machineWrap, id string) ([]byte, error) {
	if mw == nil {
		return nil, errors.New("缺少机器包裹元数据")
	}
	salt, err := base64.StdEncoding.DecodeString(mw.Salt)
	if err != nil {
		return nil, err
	}
	kek := deriveKey(id, salt)
	return aesGCMOpen(kek, &mw.WrappedDEK)
}

// buildMachineWrapLocked 在 DEK 已在内存时生成机器包裹（调用方持锁）。
// 取机器码失败时返回 nil（不报错——调用方可继续无机器包裹运行，仅少一条解锁路径）。
func (m *Manager) buildMachineWrapLocked() *machineWrap {
	if m.dek == nil {
		return nil
	}
	id, err := machineID()
	if err != nil || id == "" {
		return nil
	}
	mw, err := wrapDEKForMachine(m.dek, id)
	if err != nil {
		return nil
	}
	return mw
}
