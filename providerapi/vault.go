package providerapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"switchdev/providerapi/keystore"
)

// 磁盘文件版本
const configVersion = 2

// ── 加载 / 保存 ──────────────────────────────────────────────

// loadFromDisk 读取并解密配置。
//   - v2 加密文件：尝试用钥匙串记住的主密码自动解锁；失败则保持锁定（内存为空配置）。
//   - v1 明文文件：直接加载到内存，并在解锁/首次写盘时迁移为 v2。
func (m *Manager) loadFromDisk() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.config = &Config{Providers: map[string]*ProviderConfig{}}
			return nil
		}
		return err
	}

	// 先尝试解析为 v2 结构
	var disk onDiskFile
	if err := json.Unmarshal(data, &disk); err == nil && disk.Version >= 2 {
		return m.loadV2(&disk)
	}

	// 回退：v1 明文
	var old Config
	if err := json.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("解析 credentials.json 失败: %w", err)
	}
	if old.Providers == nil {
		old.Providers = map[string]*ProviderConfig{}
	}
	m.config = &old
	// 标记需要迁移：dek 为空时，Save 会自动初始化加密
	return nil
}

func (m *Manager) loadV2(disk *onDiskFile) error {
	if disk.WrappedDEK == nil {
		return errors.New("加密配置缺少 wrappedDEK")
	}
	// 尝试从钥匙串取记住的主密码自动解锁
	if mp, err := keystore.Get(keystore.Account); err == nil && mp != "" && disk.KDF != nil {
		if dek, err := m.unwrapDEK(mp, disk.KDF, disk.WrappedDEK); err == nil {
			m.mu.Lock()
			m.dek = dek
			m.kdfSalt, _ = base64.StdEncoding.DecodeString(disk.KDF.Salt)
			m.kdfMeta = disk.KDF
			m.wrappedDEK = disk.WrappedDEK
			m.recoveryMeta = disk.Recovery
			m.masterSet = disk.MasterSet || disk.Recovery != nil
			m.config = m.decryptConfig(disk)
			m.uiLocked = false
			m.mu.Unlock()
			return nil
		}
		// 自动解锁失败：钥匙串密码与磁盘不匹配，清除过期条目
		_ = keystore.Delete(keystore.Account)
	}
	// 未能自动解锁：保留磁盘上的加密元数据，但仍加载供应商的明文元数据
	// （id/名称/baseURL/模型列表/verified 等在磁盘上本就是明文，仅 apiKey 加密），
	// 这样锁定状态下也能列出/选择模型，只是没有 apiKey 无法真正发起调用。
	m.mu.Lock()
	m.kdfMeta = disk.KDF
	m.wrappedDEK = disk.WrappedDEK
	m.recoveryMeta = disk.Recovery
	m.masterSet = disk.MasterSet || disk.Recovery != nil
	m.dek = nil
	m.uiLocked = true
	m.config = m.decryptConfig(disk) // dek==nil：元数据照常填充，apiKey 留空
	m.mu.Unlock()
	return nil
}

// unwrapDEK 用密码派生 KEK 并解开 DEK
func (m *Manager) unwrapDEK(password string, kdf *kdfParams, wrapped *sealed) ([]byte, error) {
	if kdf == nil || wrapped == nil {
		return nil, errors.New("缺少 KDF/DEK 元数据")
	}
	salt, err := base64.StdEncoding.DecodeString(kdf.Salt)
	if err != nil {
		return nil, err
	}
	kek := deriveKey(password, salt)
	return aesGCMOpen(kek, wrapped)
}

// IsLocked 是否已解锁（DEK 在内存中）
// IsLocked 是否已锁定（UI 层锁定；DEK 可能仍在内存，代理调用不受影响）
func (m *Manager) IsLocked() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.uiLocked
}

// DEK 返回当前数据加密密钥的副本（供 db 包加密字段使用），未初始化/锁定时返回 nil
func (m *Manager) DEK() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.dek == nil {
		return nil
	}
	cp := make([]byte, len(m.dek))
	copy(cp, m.dek)
	return cp
}

// Lock 锁定 UI（前端显示解锁界面），不清除内存中的 DEK，不影响代理调用。
// 若尚未设置主密码（无 kdfMeta），则不做任何事（自动加密模式下锁定无意义）。
func (m *Manager) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.kdfMeta == nil {
		return
	}
	m.uiLocked = true
}

// TryAutoUnlock 尝试用钥匙串记住的密码自动解锁。
// 返回 nil 表示解锁成功；钥匙串无密码或密码过期返回错误（过期条目会被清除）。
func (m *Manager) TryAutoUnlock() error {
	mp, err := keystore.Get(keystore.Account)
	if err != nil || mp == "" {
		return errors.New("钥匙串没有记住的密码")
	}
	if err := m.Unlock(mp); err != nil {
		// 钥匙串密码过期：清除，避免 remembered 状态误导
		_ = keystore.Delete(keystore.Account)
		return err
	}
	return nil
}

// syncRememberedPassword 解锁成功后防御性同步钥匙串：
// 若钥匙串中记着旧密码（与当前不匹配），用刚验证的正确密码覆盖。
func (m *Manager) syncRememberedPassword(password string) {
	if mp, err := keystore.Get(keystore.Account); err == nil && mp != "" && mp != password {
		_ = keystore.Set(keystore.Account, password)
	}
}

// Unlock 用主密码解锁（派生 KEK -> 解 DEK -> 解 apiKey）。
// 若 DEK 已在内存（UI 锁定场景），只验证密码并解除 UI 锁定；
// 若 DEK 不在内存（应用重启场景），从磁盘解密并加载配置。
func (m *Manager) Unlock(password string) error {
	m.mu.RLock()
	dekInMemory := m.dek != nil
	kdfMeta := m.kdfMeta
	wrappedDEK := m.wrappedDEK
	m.mu.RUnlock()

	if dekInMemory {
		// DEK 已在内存：UI 锁定，只需验证密码正确即可解除
		if kdfMeta == nil || wrappedDEK == nil {
			return errors.New("配置未加密，无需解锁")
		}
		if _, err := m.unwrapDEK(password, kdfMeta, wrappedDEK); err != nil {
			return errors.New("密码错误或配置已损坏")
		}
		m.mu.Lock()
		m.uiLocked = false
		m.mu.Unlock()
		m.syncRememberedPassword(password)
		return nil
	}

	// DEK 不在内存：从磁盘解密
	data, err := os.ReadFile(m.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var disk onDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	if disk.Version < 2 || disk.KDF == nil || disk.WrappedDEK == nil {
		return errors.New("配置未加密，无需解锁")
	}
	salt, _ := base64.StdEncoding.DecodeString(disk.KDF.Salt)
	dek, err := m.unwrapDEK(password, disk.KDF, disk.WrappedDEK)
	if err != nil {
		return errors.New("密码错误或配置已损坏")
	}
	m.mu.Lock()
	m.dek = dek
	m.kdfSalt = salt
	m.kdfMeta = disk.KDF
	m.wrappedDEK = disk.WrappedDEK
	m.recoveryMeta = disk.Recovery
	m.masterSet = disk.MasterSet || disk.Recovery != nil
	m.config = m.decryptConfig(&disk)
	m.uiLocked = false
	m.mu.Unlock()
	m.syncRememberedPassword(password)
	return nil
}

// ResetVault 忘记密码时的兜底：删除加密配置和钥匙串，重置为空配置。
// 警告：所有已保存的供应商和 API Key 会丢失。
func (m *Manager) ResetVault() error {
	m.mu.Lock()
	m.dek = nil
	m.kdfMeta = nil
	m.wrappedDEK = nil
	m.recoveryMeta = nil
	m.masterSet = false
	m.uiLocked = false
	m.config = &Config{Providers: map[string]*ProviderConfig{}}
	m.mu.Unlock()

	// 删除钥匙串里记住的密码
	_ = keystore.Delete(keystore.Account)

	// 删除配置文件（下次启动重新开始）
	if err := os.Remove(m.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// decryptConfig 把磁盘结构（密文 apiKey）转成内存结构（明文）
func (m *Manager) decryptConfig(disk *onDiskFile) *Config {
	cfg := &Config{Providers: map[string]*ProviderConfig{}}
	for id, dp := range disk.Providers {
		if dp == nil {
			continue
		}
		p := &ProviderConfig{
			ID:           dp.ID,
			Name:         dp.Name,
			BaseURL:      dp.BaseURL,
			GetAPIKeyURL: dp.GetAPIKeyURL,
			Protocol:     dp.Protocol,
			MaxContext:   dp.MaxContext,
			Custom:       dp.Custom,
			Imported:     dp.Imported,
			Verified:     dp.Verified,
			Models:       dp.Models,
		}
		if dp.APIKey != nil && m.dek != nil {
			if plain, err := aesGCMOpen(m.dek, dp.APIKey); err == nil {
				p.APIKey = string(plain)
			}
		}
		// 空 key 或解密失败保持空
		cfg.Providers[id] = p
	}
	return cfg
}

// encryptConfig 把内存结构转成磁盘结构（加密 apiKey）
func (m *Manager) encryptConfig(cfg *Config) *onDiskFile {
	disk := &onDiskFile{
		Version:   configVersion,
		Providers: map[string]*onDiskProvider{},
	}
	for id, p := range cfg.Providers {
		dp := &onDiskProvider{
			ID:           p.ID,
			Name:         p.Name,
			BaseURL:      p.BaseURL,
			GetAPIKeyURL: p.GetAPIKeyURL,
			Protocol:     p.Protocol,
			MaxContext:   p.MaxContext,
			Custom:       p.Custom,
			Imported:     p.Imported,
			Verified:     p.Verified,
			Models:       p.Models,
		}
		if strings.TrimSpace(p.APIKey) != "" && m.dek != nil {
			if s, err := aesGCMSeal(m.dek, []byte(p.APIKey)); err == nil {
				dp.APIKey = s
			}
		}
		disk.Providers[id] = dp
	}
	return disk
}

// Save 写盘。若尚未初始化加密（全新空配置或 v1 明文迁移），自动生成 DEK + 随机主密码。
// 已加密但当前处于锁定状态时，返回错误而不是重新初始化，避免用空配置覆盖磁盘数据。
func (m *Manager) Save() error {
	m.mu.Lock()
	cfg := m.config
	needsInit := m.dek == nil
	var initMaster string
	if needsInit {
		// 已存在加密元数据说明是"已加密但未解锁"，禁止写入
		if m.kdfMeta != nil {
			m.mu.Unlock()
			return errors.New("当前已锁定，请先解锁主密码再保存")
		}
		var err error
		initMaster, err = m.initializeEncryptionLocked()
		if err != nil {
			m.mu.Unlock()
			return err
		}
	}
	disk := m.encryptConfig(cfg)
	disk.KDF = m.kdfMeta
	disk.WrappedDEK = m.wrappedDEK
	disk.Recovery = m.recoveryMeta
	m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		m.rollbackInit(needsInit)
		return err
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		m.rollbackInit(needsInit)
		return err
	}

	// 首次初始化加密：必须先把随机密码写入钥匙串并确认成功，再写磁盘。
	// 绝不能先写磁盘——若钥匙串写入失败，随机密码无人知晓，磁盘上的密文将永远无法解开。
	if initMaster != "" {
		if err := keystore.Set(keystore.Account, initMaster); err != nil {
			m.rollbackInit(true)
			return fmt.Errorf("初始化加密失败（钥匙串不可用，已取消写入）: %w", err)
		}
	}

	if err := os.WriteFile(m.path, data, 0o600); err != nil {
		// 磁盘写入失败：钥匙串里刚写入的随机密码已成孤儿，删除并回滚内存
		if initMaster != "" {
			_ = keystore.Delete(keystore.Account)
		}
		m.rollbackInit(true)
		return err
	}
	return nil
}

// rollbackInit 在首次加密初始化失败时，把内存中的加密元数据恢复到未初始化状态。
// 磁盘不受影响（调用方保证失败时不写盘或写盘失败已清理钥匙串）。
func (m *Manager) rollbackInit(wasInit bool) {
	if !wasInit {
		return
	}
	m.mu.Lock()
	m.dek = nil
	m.kdfSalt = nil
	m.kdfMeta = nil
	m.wrappedDEK = nil
	m.mu.Unlock()
}

// initializeEncryptionLocked 生成 DEK + 随机主密码，用主密码包裹 DEK。
// 调用方持锁。用于无感升级（仅钥匙串自动解锁）。
// 返回随机主密码，由调用方在写盘成功后存入钥匙串。
func (m *Manager) initializeEncryptionLocked() (string, error) {
	dek := randomBytes(32)
	master := randomRecoveryCode() // 24 位随机密码
	salt := randomBytes(kdfSaltLen)
	kek := deriveKey(master, salt)
	wrapped, err := aesGCMSeal(kek, dek)
	if err != nil {
		return "", err
	}
	m.dek = dek
	m.kdfSalt = salt
	// 写入内存元数据（Save 会一并落盘）；钥匙串由调用方在写盘后处理
	if err := m.stageKeyMetaLocked(salt, wrapped); err != nil {
		return "", err
	}
	return master, nil
}

// stageKeyMetaLocked 把 KDF/wrappedDEK 写入管理器常驻字段，Save 时一并落盘。
func (m *Manager) stageKeyMetaLocked(salt []byte, wrappedDEK *sealed) error {
	m.kdfMeta = &kdfParams{
		Algo:    "argon2id",
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Time:    kdfTime,
		Memory:  kdfMemory,
		Threads: kdfThreads,
	}
	m.wrappedDEK = wrappedDEK
	// 保留已有 recovery（如果有）
	if data, err := os.ReadFile(m.path); err == nil {
		var existing onDiskFile
		if json.Unmarshal(data, &existing) == nil && existing.Recovery != nil {
			m.recoveryMeta = existing.Recovery
		}
	}
	return nil
}

// ── 主密码 / 恢复码 ────────────────────────────────────────

// LocksetInfo 对外暴露的锁状态信息
type LocksetInfo struct {
	IsSet       bool   `json:"isSet"`       // 是否已初始化加密（kdf 存在，含自动加密）
	MasterSet   bool   `json:"masterSet"`   // 用户是否主动设置了主密码（false=自动加密）
	HasRecovery bool   `json:"hasRecovery"` // 是否已生成恢复码
	IsLocked    bool   `json:"isLocked"`    // 当前是否锁定
	Remembered  bool   `json:"remembered"`  // 主密码是否已在本机记住（钥匙串）
}

// GetLocksetInfo 返回锁状态（供设置页判断显示）
func (m *Manager) GetLocksetInfo() LocksetInfo {
	m.mu.RLock()
	hasVault := m.kdfMeta != nil
	recovery := m.recoveryMeta != nil
	masterSet := m.masterSet
	locked := hasVault && m.uiLocked
	m.mu.RUnlock()
	// 钥匙串是否有记住的主密码（不暴露密码本身）
	remembered := false
	if mp, err := keystore.Get(keystore.Account); err == nil && mp != "" {
		remembered = true
	}
	return LocksetInfo{
		IsSet:       hasVault,
		MasterSet:   masterSet,
		HasRecovery: recovery,
		IsLocked:    locked,
		Remembered:  remembered,
	}
}

// SetMasterPassword 用当前未锁定状态（DEK 在内存）设置/更换主密码，
// 并用新密码重新包裹 DEK。remember=true 时把密码写入钥匙串。
// 返回恢复码（首次设置或重新生成时）。
func (m *Manager) SetMasterPassword(password string, remember bool) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("密码不能为空")
	}
	if len(password) < 6 {
		return "", errors.New("密码至少 6 位")
	}
	m.mu.Lock()
	if m.dek == nil {
		m.mu.Unlock()
		return "", errors.New("请先解锁")
	}
	// 用新密码包裹现有 DEK
	salt := randomBytes(kdfSaltLen)
	kek := deriveKey(password, salt)
	wrapped, err := aesGCMSeal(kek, m.dek)
	if err != nil {
		m.mu.Unlock()
		return "", err
	}
	m.kdfSalt = salt
	m.kdfMeta = &kdfParams{
		Algo: "argon2id", Salt: base64.StdEncoding.EncodeToString(salt),
		Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads,
	}
	m.wrappedDEK = wrapped
	m.masterSet = true
	// 生成恢复码并用它包裹同一个 DEK
	recCode := randomRecoveryCode()
	recSalt := randomBytes(kdfSaltLen)
	recKEK := deriveKey(recCode, recSalt)
	recWrapped, err := aesGCMSeal(recKEK, m.dek)
	if err != nil {
		m.mu.Unlock()
		return "", err
	}
	m.recoveryMeta = &recoveryBlob{
		Salt:       base64.StdEncoding.EncodeToString(recSalt),
		WrappedDEK: *recWrapped,
	}
	// 保存内存里的 providers 配置（apiKey 明文），Save 时会用 DEK 加密写盘
	cfg := m.config
	// 快照内存字段（用于 saveConfig 失败时回滚）
	oldKdfMeta := m.kdfMeta
	oldWrappedDEK := m.wrappedDEK
	oldRecoveryMeta := m.recoveryMeta
	oldMasterSet := m.masterSet
	oldKdfSalt := m.kdfSalt
	m.mu.Unlock()

	// 先写磁盘，成功后才更新钥匙串
	if err := m.saveConfig(cfg); err != nil {
		// 回滚内存字段
		m.mu.Lock()
		m.kdfMeta = oldKdfMeta
		m.wrappedDEK = oldWrappedDEK
		m.recoveryMeta = oldRecoveryMeta
		m.masterSet = oldMasterSet
		m.kdfSalt = oldKdfSalt
		m.mu.Unlock()
		return "", err
	}
	if remember {
		_ = keystore.Set(keystore.Account, password)
	} else {
		_ = keystore.Delete(keystore.Account)
	}
	// 不记住密码：立即锁定 UI 会话（不清 DEK，代理调用不受影响）。
	if !remember {
		m.mu.Lock()
		m.uiLocked = true
		m.mu.Unlock()
	}
	return recCode, nil
}

// ClearRememberedPassword 清除"记住密码"（钥匙串），下次启动需手动输主密码。
func (m *Manager) ClearRememberedPassword() error {
	return keystore.Delete(keystore.Account)
}

// ClearMasterPassword 清除用户设置的主密码，回到"自动加密"模式：
// 用新的随机密码重新包裹现有 DEK（apiKey 密文不变，无需重新加密），
// 删掉恢复码，把随机密码写入钥匙串，保持当前会话已解锁。
// 之后启动自动解锁，测评/中转不再需要手动输密码。
// 必须在已解锁（DEK 在内存）状态下调用。
func (m *Manager) ClearMasterPassword() error {
	m.mu.Lock()
	if m.dek == nil {
		m.mu.Unlock()
		return errors.New("请先解锁再清除主密码")
	}
	if m.kdfMeta == nil {
		m.mu.Unlock()
		return errors.New("尚未初始化加密，无需清除")
	}
	// 用新的随机密码包裹同一个 DEK（DEK 不变，apiKey 密文不变）
	master := randomRecoveryCode()
	salt := randomBytes(kdfSaltLen)
	kek := deriveKey(master, salt)
	wrapped, err := aesGCMSeal(kek, m.dek)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.kdfSalt = salt
	m.kdfMeta = &kdfParams{
		Algo: "argon2id", Salt: base64.StdEncoding.EncodeToString(salt),
		Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads,
	}
	m.wrappedDEK = wrapped
	m.recoveryMeta = nil
	m.masterSet = false
	cfg := m.config
	// 快照内存字段（用于 saveConfig 失败时回滚）
	oldKdfMeta := m.kdfMeta
	oldWrappedDEK := m.wrappedDEK
	oldRecoveryMeta := m.recoveryMeta
	oldMasterSet := m.masterSet
	oldKdfSalt := m.kdfSalt
	m.mu.Unlock()

	// 先写磁盘（用新随机密码包裹 DEK）。此时钥匙串仍是旧密码，
	// 若写盘失败回滚内存，旧密码+旧密文仍可解锁，无数据丢失。
	if err := m.saveConfig(cfg); err != nil {
		// 回滚内存字段
		m.mu.Lock()
		m.kdfMeta = oldKdfMeta
		m.wrappedDEK = oldWrappedDEK
		m.recoveryMeta = oldRecoveryMeta
		m.masterSet = oldMasterSet
		m.kdfSalt = oldKdfSalt
		m.mu.Unlock()
		return err
	}
	// 写盘成功后更新钥匙串为新随机密码。若失败，必须把磁盘恢复为旧包裹，
	// 否则磁盘用的是无人知晓的新随机密码、而钥匙串仍是旧密码 → 永久无法解锁。
	if err := keystore.Set(keystore.Account, master); err != nil {
		m.mu.Lock()
		m.kdfMeta = oldKdfMeta
		m.wrappedDEK = oldWrappedDEK
		m.recoveryMeta = oldRecoveryMeta
		m.masterSet = oldMasterSet
		m.kdfSalt = oldKdfSalt
		rollbackCfg := m.config
		m.mu.Unlock()
		_ = m.saveConfig(rollbackCfg) // 尽力恢复磁盘为旧包裹
		return fmt.Errorf("配置已生成但钥匙串写入失败，已回滚: %w", err)
	}
	return nil
}

// RecoverWithCode 用恢复码重置主密码：解出 DEK，再用新密码重新包裹。
func (m *Manager) RecoverWithCode(recoveryCode, newPassword string, remember bool) (string, error) {
	if strings.TrimSpace(recoveryCode) == "" {
		return "", errors.New("请输入恢复码")
	}
	if strings.TrimSpace(newPassword) == "" {
		return "", errors.New("请输入新密码")
	}
	if len(newPassword) < 6 {
		return "", errors.New("新密码至少 6 位")
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		return "", err
	}
	var disk onDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return "", fmt.Errorf("读取配置失败: %w", err)
	}
	if disk.Recovery == nil {
		return "", errors.New("该配置没有恢复码")
	}
	// 用恢复码解出 DEK
	recSalt, _ := base64.StdEncoding.DecodeString(disk.Recovery.Salt)
	recKEK := deriveKey(strings.TrimSpace(recoveryCode), recSalt)
	dek, err := aesGCMOpen(recKEK, &disk.Recovery.WrappedDEK)
	if err != nil {
		return "", errors.New("恢复码错误或配置已损坏")
	}
	// 用新密码重新包裹 DEK
	salt := randomBytes(kdfSaltLen)
	kek := deriveKey(newPassword, salt)
	wrapped, err := aesGCMSeal(kek, dek)
	if err != nil {
		return "", err
	}
	// 用 DEK 解密现有 providers 到内存
	m.mu.Lock()
	m.dek = dek
	m.kdfSalt = salt
	m.kdfMeta = &kdfParams{
		Algo: "argon2id", Salt: base64.StdEncoding.EncodeToString(salt),
		Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads,
	}
	m.wrappedDEK = wrapped
	m.masterSet = true
	m.config = m.decryptConfig(&disk)
	// 生成新的恢复码（旧的作废）
	recCode := randomRecoveryCode()
	newRecSalt := randomBytes(kdfSaltLen)
	newRecKEK := deriveKey(recCode, newRecSalt)
	newRecWrapped, err := aesGCMSeal(newRecKEK, dek)
	if err != nil {
		m.mu.Unlock()
		return "", err
	}
	m.recoveryMeta = &recoveryBlob{
		Salt: base64.StdEncoding.EncodeToString(newRecSalt), WrappedDEK: *newRecWrapped,
	}
	cfg := m.config
	// 快照内存字段（用于 saveConfig 失败时回滚）
	oldKdfMeta := m.kdfMeta
	oldWrappedDEK := m.wrappedDEK
	oldRecoveryMeta := m.recoveryMeta
	oldMasterSet := m.masterSet
	oldKdfSalt := m.kdfSalt
	m.mu.Unlock()

	// 先写磁盘，成功后才更新钥匙串
	if err := m.saveConfig(cfg); err != nil {
		// 回滚内存字段
		m.mu.Lock()
		m.kdfMeta = oldKdfMeta
		m.wrappedDEK = oldWrappedDEK
		m.recoveryMeta = oldRecoveryMeta
		m.masterSet = oldMasterSet
		m.kdfSalt = oldKdfSalt
		m.mu.Unlock()
		return "", err
	}
	if remember {
		_ = keystore.Set(keystore.Account, newPassword)
	} else {
		_ = keystore.Delete(keystore.Account)
	}
	// 不记住密码：立即锁定 UI 会话（不清 DEK）。
	if !remember {
		m.mu.Lock()
		m.uiLocked = true
		m.mu.Unlock()
	}
	return recCode, nil
}

// saveConfig 在不持锁的情况下加密并写盘（调用方已拷贝 cfg）
func (m *Manager) saveConfig(cfg *Config) error {
	m.mu.Lock()
	disk := m.encryptConfig(cfg)
	disk.MasterSet = m.masterSet
	disk.KDF = m.kdfMeta
	disk.WrappedDEK = m.wrappedDEK
	disk.Recovery = m.recoveryMeta
	m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0o600)
}
