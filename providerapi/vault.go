package providerapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

	// 依次尝试「机器包裹」和「钥匙串记住的主密码」自动解锁。
	// 机器包裹不依赖系统钥匙串/DPAPI，因此不受 macOS 自更新后二进制签名变化导致
	// 钥匙串 ACL 失效的影响，是抗自更新锁死的主路径。
	// 任何副本解不开都【不删除】——自动加密模式下记住的密码是唯一凭证，删除会导致
	// 磁盘密文永久无法解开（历史上曾因此造成凭据丢失）。
	dek, via, err := m.unlockFromDisk(disk)
	if err == nil {
		m.mu.Lock()
		m.installUnlockedLocked(disk, dek)
		m.config = m.decryptConfig(disk)
		m.uiLocked = false
		m.machineWrap = disk.Machine
		m.mu.Unlock()
		log.Printf("[providerapi] 自动解锁成功 via=%s", via)
		// 解锁成功后 best-effort 补/刷新机器包裹（老文件无 machine，或换机后机器码变了）。
		m.ensureMachineWrap(disk, via)
		return nil
	}
	log.Printf("[providerapi] 自动解锁失败: %v", err)

	// 未能自动解锁：保留磁盘上的加密元数据，但仍加载供应商的明文元数据
	// （id/名称/baseURL/模型列表/verified 等在磁盘上本就是明文，仅 apiKey 加密），
	// 这样锁定状态下也能列出/选择模型，只是没有 apiKey 无法真正发起调用。
	// 钥匙串/文件副本一律保留，等待用户手动输入正确主密码（Unlock 会刷新主存储）。
	m.mu.Lock()
	m.kdfMeta = disk.KDF
	m.wrappedDEK = disk.WrappedDEK
	m.recoveryMeta = disk.Recovery
	m.machineWrap = disk.Machine
	m.masterSet = disk.MasterSet || disk.Recovery != nil
	m.dek = nil
	m.uiLocked = true
	m.config = m.decryptConfig(disk) // dek==nil：元数据照常填充，apiKey 留空
	m.mu.Unlock()
	return nil
}

// installUnlockedLocked 在已解出 DEK 后，把磁盘上的密钥元数据装载进 Manager 常驻字段。
// 调用方必须持 m.mu。
func (m *Manager) installUnlockedLocked(disk *onDiskFile, dek []byte) {
	m.dek = dek
	if disk.KDF != nil {
		m.kdfSalt, _ = base64.StdEncoding.DecodeString(disk.KDF.Salt)
	}
	m.kdfMeta = disk.KDF
	m.wrappedDEK = disk.WrappedDEK
	m.recoveryMeta = disk.Recovery
	m.masterSet = disk.MasterSet || disk.Recovery != nil
}

// unlockFromDisk 按「机器包裹 → 钥匙串记住的主密码」顺序尝试解开 DEK。
// 成功返回 DEK 及使用的路径（"machine" / "keystore"）；两者都失败返回 error。
func (m *Manager) unlockFromDisk(disk *onDiskFile) ([]byte, string, error) {
	// 1. 机器包裹
	if disk.Machine != nil {
		if id, err := machineID(); err == nil && id != "" {
			if dek, err := openMachineWrap(disk.Machine, id); err == nil {
				return dek, "machine", nil
			} else {
				log.Printf("[providerapi] 机器包裹解锁失败（可能换机/重装）: %v", err)
			}
		} else {
			log.Printf("[providerapi] 机器码不可用，跳过机器包裹: %v", err)
		}
	}
	// 2. 钥匙串记住的主密码（keystore.Recover 以钥匙串为主、文件副本兜底，
	//    用「能否解开 DEK」作校验，正确副本会双向自愈）。
	if disk.KDF != nil {
		mp := keystore.Recover(keystore.Account, func(pw string) bool {
			_, err := m.unwrapDEK(pw, disk.KDF, disk.WrappedDEK)
			return err == nil
		})
		if mp != "" {
			if dek, err := m.unwrapDEK(mp, disk.KDF, disk.WrappedDEK); err == nil {
				return dek, "keystore", nil
			}
		}
	}
	return nil, "", errors.New("机器包裹与记住的密码均不可用")
}

// shouldHaveMachineWrap 在当前内存状态下是否应保留机器包裹：
// 自动加密（masterSet=false）或钥匙串里有记住的主密码时，本就允许无密码解锁，
// 机器包裹与之同信任边界；否则（用户显式设主密码且不记住）不应有机器包裹。
// 调用方持 m.mu。
func (m *Manager) shouldHaveMachineWrap() bool {
	if !m.masterSet {
		return true
	}
	if mp, err := keystore.Get(keystore.Account); err == nil && mp != "" {
		return true
	}
	return false
}

// ensureMachineWrap 在成功解锁后 best-effort 补建/刷新/移除机器包裹并落盘。
// 失败只记日志，不影响本次解锁结果。
func (m *Manager) ensureMachineWrap(disk *onDiskFile, via string) {
	m.mu.Lock()
	want := m.shouldHaveMachineWrap()
	have := disk.Machine != nil
	// 机器包裹在「换机/重装导致机器码变化」时可能存在但解不开（via != "machine"），
	// 此时需要用新机器码重新包裹。
	stale := have && via != "machine" && want
	m.mu.Unlock()

	if !want {
		// 不应有机器包裹（用户设了主密码且不记住）：若磁盘上残留则移除。
		if have {
			m.mu.Lock()
			m.machineWrap = nil
			cfg := m.config
			m.mu.Unlock()
			if err := m.saveConfig(cfg); err != nil {
				log.Printf("[providerapi] 移除残留机器包裹失败: %v", err)
			} else {
				log.Printf("[providerapi] 已移除残留机器包裹（需要主密码解锁）")
			}
		}
		return
	}

	if have && !stale {
		return // 机器包裹存在且当前机器码可用，无需补
	}

	// 需要补建/刷新
	m.mu.Lock()
	if m.dek == nil {
		m.mu.Unlock()
		return
	}
	mw := m.buildMachineWrapLocked()
	cfg := m.config
	m.mu.Unlock()
	if mw == nil {
		log.Printf("[providerapi] 补机器包裹跳过：机器码不可用")
		return
	}
	m.mu.Lock()
	m.machineWrap = mw
	m.mu.Unlock()
	if err := m.saveConfig(cfg); err != nil {
		log.Printf("[providerapi] 补机器包裹落盘失败: %v", err)
		// 回滚内存，避免与磁盘不一致
		m.mu.Lock()
		m.machineWrap = disk.Machine
		m.mu.Unlock()
		return
	}
	log.Printf("[providerapi] 已补建机器包裹 via=%s", via)
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

// TryAutoUnlock 尝试自动解锁（机器包裹优先，钥匙串记住的密码兜底）。
// 任一路径能解开 DEK 即成功；正确的文件副本会回填钥匙串自愈。
// 任何副本都解不开时返回错误，但【不删除】已存储的密码，避免自动加密模式下
// 唯一凭证被清除导致磁盘密文永久不可解。
func (m *Manager) TryAutoUnlock() error {
	m.mu.RLock()
	dekInMemory := m.dek != nil
	kdfInMemory := m.kdfMeta
	m.mu.RUnlock()

	// DEK 已在内存（UI 锁定）：UI 只是一道门，代理调用照常。若当前允许无密码解锁
	// （自动加密或记住了密码），直接解除 UI 锁定，无需再去磁盘解一次。
	if dekInMemory {
		m.mu.RLock()
		allowed := m.masterSet == false || m.machineWrap != nil
		m.mu.RUnlock()
		// masterSet=true 时仍走钥匙串校验（与历史行为一致）。
		if !allowed {
			mp := keystore.Recover(keystore.Account, func(pw string) bool {
				_, err := m.unwrapDEK(pw, kdfInMemory, m.wrappedDEKRef())
				return err == nil
			})
			if mp == "" {
				return errors.New("没有可用的记住密码，或记住的密码与配置不匹配")
			}
			return m.Unlock(mp)
		}
		m.mu.Lock()
		m.uiLocked = false
		m.mu.Unlock()
		return nil
	}

	// DEK 不在内存（应用重启）：从磁盘走机器包裹 → 钥匙串。
	data, err := os.ReadFile(m.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var disk onDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("读取配置失败: %w", err)
	}
	if disk.Version < 2 || disk.WrappedDEK == nil {
		return errors.New("配置未加密，无需解锁")
	}
	dek, via, err := m.unlockFromDisk(&disk)
	if err != nil {
		return errors.New("没有可用的记住密码，或记住的密码与配置不匹配")
	}
	m.mu.Lock()
	m.installUnlockedLocked(&disk, dek)
	m.config = m.decryptConfig(&disk)
	m.uiLocked = false
	m.machineWrap = disk.Machine
	m.mu.Unlock()
	log.Printf("[providerapi] TryAutoUnlock 成功 via=%s", via)
	m.ensureMachineWrap(&disk, via)
	return nil
}

// wrappedDEKRef 在 RLock 下返回当前 wrappedDEK 指针（供解锁校验用）。
func (m *Manager) wrappedDEKRef() *sealed {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.wrappedDEK
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
	m.machineWrap = disk.Machine
	m.masterSet = disk.MasterSet || disk.Recovery != nil
	m.config = m.decryptConfig(&disk)
	m.uiLocked = false
	m.mu.Unlock()
	m.syncRememberedPassword(password)
	// 用户用密码解锁成功：若该用户「记住密码」，补建机器包裹作为抗自更新兜底；
	// 若不记住密码，ensureMachineWrap 会移除任何残留机器包裹以维持「必须输密码」边界。
	m.ensureMachineWrap(&disk, "password")
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
	m.machineWrap = nil
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

// ResetForAutoLockout 自动加密锁死时的温和自愈：
// 保留供应商列表元数据（id/名称/baseURL/模型清单），只清空 apiKey 并把可用标记置 false，
// 然后重新初始化加密（新 DEK + 机器包裹 + 随机主密码），解锁进入空密钥状态，让用户重填 Key。
//
// 仅允许在「从未设主密码 + UI 锁定 + DEK 不在内存 + 已有加密元数据」时调用。
// 主密码用户（masterSet=true）应走密码/恢复码，不能用此方法绕过。
// 比 ResetVault 温和：不删供应商。
func (m *Manager) ResetForAutoLockout() error {
	m.mu.Lock()
	if m.masterSet {
		m.mu.Unlock()
		return errors.New("已设置主密码，请用主密码或恢复码解锁")
	}
	if !m.uiLocked || m.dek != nil || m.kdfMeta == nil {
		m.mu.Unlock()
		return errors.New("当前不是自动加密锁死状态，无需重配")
	}
	// 复用已加载的元数据配置（apiKey 本就为空），清空可用标记。
	cleaned := m.config
	if cleaned == nil {
		cleaned = &Config{Providers: map[string]*ProviderConfig{}}
	}
	for _, p := range cleaned.Providers {
		if p == nil {
			continue
		}
		p.APIKey = ""
		p.Verified = false
		for i := range p.Models {
			p.Models[i].Verified = false
			p.Models[i].Healthy = false
			p.Models[i].FailCount = 0
		}
	}
	// 重置加密字段，让 Save 走全新初始化路径（生成 DEK + 机器包裹 + 随机主密码，
	// 并严格「先写钥匙串成功再写磁盘」）。
	m.dek = nil
	m.kdfSalt = nil
	m.kdfMeta = nil
	m.wrappedDEK = nil
	m.recoveryMeta = nil
	m.machineWrap = nil
	m.masterSet = false
	m.uiLocked = false
	m.config = cleaned
	m.mu.Unlock()

	if err := m.Save(); err != nil {
		return err
	}
	log.Printf("[providerapi] 自动加密锁死自愈完成：已保留 %d 个供应商元数据，密钥已清空待重填", len(cleaned.Providers))
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
	disk.Machine = m.machineWrap
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
	m.machineWrap = nil
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
	if err := m.stageEncryptionLocked(dek, salt, wrapped); err != nil {
		return "", err
	}
	return master, nil
}

// stageEncryptionLocked 装载给定 DEK + 主密码包裹，并生成机器包裹。
// 调用方持锁。抽出供 initializeEncryptionLocked 与 ResetForAutoLockout 复用。
func (m *Manager) stageEncryptionLocked(dek, salt []byte, wrapped *sealed) error {
	m.dek = dek
	m.kdfSalt = salt
	// 写入内存元数据（Save 会一并落盘）；钥匙串由调用方在写盘后处理
	if err := m.stageKeyMetaLocked(salt, wrapped); err != nil {
		return err
	}
	// 自动加密初始化：同时生成机器码包裹（抗自更新锁死的主路径）。
	// 取不到机器码时为 nil，仍可正常运行，仅少一条解锁路径。
	m.machineWrap = m.buildMachineWrapLocked()
	return nil
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
	IsSet       bool `json:"isSet"`       // 是否已初始化加密（kdf 存在，含自动加密）
	MasterSet   bool `json:"masterSet"`   // 用户是否主动设置了主密码（false=自动加密）
	HasRecovery bool `json:"hasRecovery"` // 是否已生成恢复码
	IsLocked    bool `json:"isLocked"`    // 当前是否锁定
	Remembered  bool `json:"remembered"`  // 主密码是否已在本机记住（钥匙串）
	// AutoLockout 自动加密模式（masterSet=false）下所有无密码解锁路径都失败：
	// 用户从未设过主密码，弹密码框无意义。前端据此显示「重新配置」自愈入口。
	AutoLockout bool `json:"autoLockout"`
}

// GetLocksetInfo 返回锁状态（供设置页判断显示）
func (m *Manager) GetLocksetInfo() LocksetInfo {
	m.mu.RLock()
	hasVault := m.kdfMeta != nil
	recovery := m.recoveryMeta != nil
	masterSet := m.masterSet
	locked := hasVault && m.uiLocked
	dek := m.dek
	m.mu.RUnlock()
	// 钥匙串是否有记住的主密码（不暴露密码本身）
	remembered := false
	if mp, err := keystore.Get(keystore.Account); err == nil && mp != "" {
		remembered = true
	}
	// 自动加密锁死：从未设主密码 + 锁着 + DEK 不在内存（重启后解不开）。
	// 注意：DEK 仍在内存的 UI 锁定不算锁死（TryAutoUnlock 可直接解除）。
	autoLockout := hasVault && !masterSet && locked && dek == nil
	return LocksetInfo{
		IsSet:       hasVault,
		MasterSet:   masterSet,
		HasRecovery: recovery,
		IsLocked:    locked,
		Remembered:  remembered,
		AutoLockout: autoLockout,
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
	// 记住密码：保留机器包裹（与「记住密码」同信任边界，抗自更新）；
	// 不记住：移除机器包裹，维持「必须输主密码」的安全边界。
	if remember {
		m.machineWrap = m.buildMachineWrapLocked()
	} else {
		m.machineWrap = nil
	}
	// 保存内存里的 providers 配置（apiKey 明文），Save 时会用 DEK 加密写盘
	cfg := m.config
	// 快照内存字段（用于 saveConfig 失败时回滚）
	oldKdfMeta := m.kdfMeta
	oldWrappedDEK := m.wrappedDEK
	oldRecoveryMeta := m.recoveryMeta
	oldMachineWrap := m.machineWrap
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
		m.machineWrap = oldMachineWrap
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

// ClearRememberedPassword 清除"记住密码"（钥匙串）及机器包裹，下次启动需手动输主密码。
// 仅在已解锁（DEK 在内存）时有效；用空 DEK 时无法重写磁盘，回退为仅清钥匙串。
func (m *Manager) ClearRememberedPassword() error {
	m.mu.Lock()
	hadMachine := m.machineWrap != nil
	if m.dek != nil {
		m.machineWrap = nil
	}
	cfg := m.config
	dek := m.dek
	m.mu.Unlock()

	if err := keystore.Delete(keystore.Account); err != nil {
		// 钥匙串删除失败：回滚内存机器包裹，保持一致
		if hadMachine && dek != nil {
			m.mu.Lock()
			m.machineWrap = m.buildMachineWrapLocked()
			m.mu.Unlock()
		}
		return err
	}
	// 钥匙串已清：把「移除机器包裹」落盘，确保重启后必须输主密码。
	if dek != nil {
		if err := m.saveConfig(cfg); err != nil {
			log.Printf("[providerapi] ClearRememberedPassword 落盘失败: %v", err)
			return err
		}
	}
	return nil
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
	// 回到自动加密：重建机器包裹（抗自更新主路径）。
	m.machineWrap = m.buildMachineWrapLocked()
	cfg := m.config
	// 快照内存字段（用于 saveConfig 失败时回滚）
	oldKdfMeta := m.kdfMeta
	oldWrappedDEK := m.wrappedDEK
	oldRecoveryMeta := m.recoveryMeta
	oldMachineWrap := m.machineWrap
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
		m.machineWrap = oldMachineWrap
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
		m.machineWrap = oldMachineWrap
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
	// 记住密码：保留机器包裹；不记住：移除（必须输主密码）。
	if remember {
		m.machineWrap = m.buildMachineWrapLocked()
	} else {
		m.machineWrap = nil
	}
	cfg := m.config
	// 快照内存字段（用于 saveConfig 失败时回滚）
	oldKdfMeta := m.kdfMeta
	oldWrappedDEK := m.wrappedDEK
	oldRecoveryMeta := m.recoveryMeta
	oldMachineWrap := m.machineWrap
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
		m.machineWrap = oldMachineWrap
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
	disk.Machine = m.machineWrap
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
