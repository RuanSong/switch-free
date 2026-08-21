package providerapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"switchdev/providerapi/keystore"
)

// withKeystoreIsolated 在测试期间把真实钥匙串项暂存并在结束后恢复，
// 避免测试写入/删除影响开发者本机真实的 providerapi 主密码。
func withKeystoreIsolated(t *testing.T) {
	t.Helper()
	orig, _ := keystore.Get(keystore.Account)
	_ = keystore.Delete(keystore.Account)
	t.Cleanup(func() {
		_ = keystore.Delete(keystore.Account)
		if orig != "" {
			_ = keystore.Set(keystore.Account, orig)
		}
	})
}

func readDisk(t *testing.T, path string) *onDiskFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}
	var disk onDiskFile
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal disk: %v", err)
	}
	return &disk
}

func writeDisk(t *testing.T, path string, disk *onDiskFile) {
	t.Helper()
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// 机器包裹纯函数往返 + 错误机器码解不开。
func TestMachineWrapRoundTrip(t *testing.T) {
	dek := randomBytes(32)
	mw, err := wrapDEKForMachine(dek, "machine-A")
	if err != nil {
		t.Fatal(err)
	}
	if mw.Salt == "" || mw.WrappedDEK.Ciphertext == "" {
		t.Fatalf("machine wrap incomplete: %+v", mw)
	}
	got, err := openMachineWrap(mw, "machine-A")
	if err != nil {
		t.Fatalf("open with same id: %v", err)
	}
	if string(got) != string(dek) {
		t.Fatal("DEK mismatch after round trip")
	}
	if _, err := openMachineWrap(mw, "machine-B"); err == nil {
		t.Fatal("expected wrong machine id to fail, got nil")
	}
}

// 核心：新用户保存后磁盘含 machine 包裹；删掉钥匙串（模拟 Mac 自更新后
// 钥匙串 ACL 失效）后重新加载，仍应靠机器包裹自动解锁并读回 apiKey。
func TestMachineWrapUnlocksWithoutKeystore(t *testing.T) {
	withKeystoreIsolated(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "free_apis.json")

	mgr, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpsertProvider(&ProviderConfig{ID: "p", Name: "P", BaseURL: "https://x", APIKey: "sk-secret"}); err != nil {
		t.Fatal(err)
	}
	disk := readDisk(t, path)
	if disk.Machine == nil {
		t.Fatal("expected machine wrap on disk after first save")
	}

	// 模拟钥匙串/文件兜底全部丢失
	if err := keystore.Delete(keystore.Account); err != nil {
		t.Fatalf("delete keystore: %v", err)
	}
	if got, _ := keystore.Get(keystore.Account); got != "" {
		t.Fatal("keystore should be empty after delete")
	}

	// 重新加载（模拟重启）：必须靠机器包裹自动解锁
	mgr2, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if mgr2.IsLocked() {
		t.Fatal("expected auto-unlock via machine wrap, but manager is locked")
	}
	if info := mgr2.GetLocksetInfo(); info.AutoLockout {
		t.Fatal("should not be auto-lockout when machine wrap works")
	}
	p := mgr2.GetProvider("p")
	if p == nil || p.APIKey != "sk-secret" {
		t.Fatalf("apiKey not recovered via machine wrap: %+v", p)
	}
}

// 老文件（无 machine 字段）+ 钥匙串可解 → 解锁后应自动补上 machine 包裹。
func TestOldFileBackfillsMachineWrap(t *testing.T) {
	withKeystoreIsolated(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "free_apis.json")

	mgr, _ := NewManager(path)
	if err := mgr.UpsertProvider(&ProviderConfig{ID: "p", Name: "P", BaseURL: "https://x", APIKey: "sk-secret"}); err != nil {
		t.Fatal(err)
	}
	// 抹掉磁盘上的 machine 字段（模拟老版本写入的文件），保留 KDF/wrappedDEK
	disk := readDisk(t, path)
	disk.Machine = nil
	writeDisk(t, path, disk)
	if readDisk(t, path).Machine != nil {
		t.Fatal("setup failed: machine should be nil")
	}

	mgr2, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if mgr2.IsLocked() {
		t.Fatalf("expected unlock via keystore, locked. info=%+v", mgr2.GetLocksetInfo())
	}
	// 解锁后应已补建 machine 包裹并落盘
	if got := readDisk(t, path); got.Machine == nil {
		t.Fatal("expected machine wrap to be backfilled after unlock")
	}
}

// 设主密码且「不记住」→ 机器包裹必须移除（安全边界：必须输主密码）。
func TestSetMasterPasswordNoRememberRemovesMachine(t *testing.T) {
	withKeystoreIsolated(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "free_apis.json")

	mgr, _ := NewManager(path)
	_ = mgr.UpsertProvider(&ProviderConfig{ID: "p", Name: "P", BaseURL: "https://x", APIKey: "sk-secret"})
	if _, err := mgr.SetMasterPassword("secret123", false); err != nil {
		t.Fatalf("SetMasterPassword: %v", err)
	}
	if disk := readDisk(t, path); disk.Machine != nil {
		t.Fatal("machine wrap must be removed when master password set without remember")
	}
	// 钥匙串也应被清空
	if got, _ := keystore.Get(keystore.Account); got != "" {
		t.Fatal("keystore must be empty when not remembered")
	}
}

// 设主密码且「记住」→ 保留机器包裹（抗自更新兜底）。
func TestSetMasterPasswordRememberKeepsMachine(t *testing.T) {
	withKeystoreIsolated(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "free_apis.json")

	mgr, _ := NewManager(path)
	_ = mgr.UpsertProvider(&ProviderConfig{ID: "p", Name: "P", BaseURL: "https://x", APIKey: "sk-secret"})
	if _, err := mgr.SetMasterPassword("secret123", true); err != nil {
		t.Fatalf("SetMasterPassword: %v", err)
	}
	if disk := readDisk(t, path); disk.Machine == nil {
		t.Fatal("machine wrap should be retained when password is remembered")
	}
}

// 自动加密锁死 + ResetForAutoLockout：保留供应商元数据、清空 apiKey、可用标记置 false、解锁。
func TestResetForAutoLockoutPreservesMetadata(t *testing.T) {
	withKeystoreIsolated(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "free_apis.json")

	mgr, _ := NewManager(path)
	_ = mgr.UpsertProvider(&ProviderConfig{
		ID: "p", Name: "P", BaseURL: "https://x", APIKey: "sk-secret",
		Verified: true, Models: []ProviderModel{{ID: "m1", Verified: true, Healthy: true}},
	})

	// 制造自动加密锁死：把磁盘 machine 包裹替换成「另一台机器」的包裹，并清空钥匙串。
	// DEK 在内存里，用它 + 假机器码重新包裹即可产生一个本机解不开的 machine 字段。
	mgr.mu.RLock()
	dek := append([]byte(nil), mgr.dek...)
	mgr.mu.RUnlock()
	bogus, err := wrapDEKForMachine(dek, "not-this-machine")
	if err != nil {
		t.Fatal(err)
	}
	disk := readDisk(t, path)
	disk.Machine = bogus
	writeDisk(t, path, disk)
	if err := keystore.Delete(keystore.Account); err != nil {
		t.Fatal(err)
	}

	// 重新加载：应进入自动加密锁死
	mgr2, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	info := mgr2.GetLocksetInfo()
	if !info.IsLocked || info.MasterSet || !info.AutoLockout {
		t.Fatalf("expected auto-lockout, got %+v", info)
	}

	// 温和重配
	if err := mgr2.ResetForAutoLockout(); err != nil {
		t.Fatalf("ResetForAutoLockout: %v", err)
	}
	if mgr2.IsLocked() {
		t.Fatal("expected unlocked after reset")
	}
	p := mgr2.GetProvider("p")
	if p == nil {
		t.Fatal("provider metadata should be preserved")
	}
	if p.Name != "P" || p.BaseURL != "https://x" {
		t.Fatalf("metadata changed: %+v", p)
	}
	if p.APIKey != "" {
		t.Fatalf("apiKey must be cleared, got %q", p.APIKey)
	}
	if p.Verified {
		t.Fatal("provider Verified must be reset to false")
	}
	if len(p.Models) != 1 || p.Models[0].ID != "m1" {
		t.Fatalf("models should be preserved by id, got %+v", p.Models)
	}
	if p.Models[0].Verified || p.Models[0].Healthy {
		t.Fatal("model Verified/Healthy must be reset to false")
	}
	// 重配后磁盘应重新具备机器包裹，且新 DEK 加密的 apiKey 为空
	nd := readDisk(t, path)
	if nd.Machine == nil {
		t.Fatal("machine wrap should be re-established after reset")
	}
	if dp := nd.Providers["p"]; dp.APIKey != nil {
		t.Fatalf("apiKey on disk should be nil after clear, got %+v", dp.APIKey)
	}
}

// 主密码用户不允许用 ResetForAutoLockout 绕过。
func TestResetForAutoLockoutRejectedForMasterSet(t *testing.T) {
	withKeystoreIsolated(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "free_apis.json")

	mgr, _ := NewManager(path)
	_ = mgr.UpsertProvider(&ProviderConfig{ID: "p", Name: "P", BaseURL: "https://x", APIKey: "sk-secret"})
	if _, err := mgr.SetMasterPassword("secret123", false); err != nil {
		t.Fatal(err)
	}
	// 即便 UI 锁定，masterSet=true 也不能走自动锁死重配
	mgr.mu.Lock()
	mgr.uiLocked = true
	mgr.mu.Unlock()
	if err := mgr.ResetForAutoLockout(); err == nil {
		t.Fatal("expected ResetForAutoLockout to be rejected for master-set vault")
	}
}
