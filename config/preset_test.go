package config

import (
	"path/filepath"
	"testing"

	"switchdev/proxy"
)

// newTestManager 建一个写临时目录的 Manager，避免污染用户真实配置
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	mgr, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

// setChain 把当前配置改成给定的单模型 auto 链
func setChain(t *testing.T, mgr *Manager, upstream, model string) {
	t.Helper()
	cfg := mgr.Get()
	cfg.Mode = "auto"
	cfg.AutoChain = []AgentModels{{Upstream: upstream, Models: []string{model}}}
	if err := mgr.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestPresetSnapshotSemantics 核心语义：保存后编辑当前配置不应影响已存方案，
// 切回方案应完整还原当初存的样子
func TestPresetSnapshotSemantics(t *testing.T) {
	mgr := newTestManager(t)

	setChain(t, mgr, "joycode", "model-a")
	if err := mgr.SavePreset("省钱档"); err != nil {
		t.Fatalf("SavePreset: %v", err)
	}

	// 存完后改当前配置
	setChain(t, mgr, "deveco", "model-b")

	// 方案本身不应被带着改（快照语义的关键）
	cfg := mgr.Get()
	if got := cfg.Presets[0].AutoChain[0].Models[0]; got != "model-a" {
		t.Errorf("方案被当前配置的改动污染了: 期望 model-a，得到 %s", got)
	}

	// 切回去应完整还原
	if err := mgr.ApplyPreset("省钱档"); err != nil {
		t.Fatalf("ApplyPreset: %v", err)
	}
	cfg = mgr.Get()
	if got := cfg.AutoChain[0].Models[0]; got != "model-a" {
		t.Errorf("还原失败: 期望 model-a，得到 %s", got)
	}
	if cfg.AutoChain[0].Upstream != "joycode" {
		t.Errorf("upstream 未还原: 得到 %s", cfg.AutoChain[0].Upstream)
	}
	if cfg.ActivePreset != "省钱档" {
		t.Errorf("ActivePreset 应为 省钱档，得到 %q", cfg.ActivePreset)
	}
}

// TestSavePresetOverwrite 同名保存应覆盖而非追加
func TestSavePresetOverwrite(t *testing.T) {
	mgr := newTestManager(t)

	setChain(t, mgr, "joycode", "model-a")
	if err := mgr.SavePreset("档"); err != nil {
		t.Fatal(err)
	}
	setChain(t, mgr, "deveco", "model-b")
	if err := mgr.SavePreset("档"); err != nil {
		t.Fatal(err)
	}

	cfg := mgr.Get()
	if len(cfg.Presets) != 1 {
		t.Fatalf("同名应覆盖，期望 1 个方案，得到 %d", len(cfg.Presets))
	}
	if got := cfg.Presets[0].AutoChain[0].Models[0]; got != "model-b" {
		t.Errorf("覆盖后应是新内容 model-b，得到 %s", got)
	}
}

// TestPresetPersistence 方案要能跨重启存活
func TestPresetPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	mgr, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := mgr.Get()
	cfg.Mode = "manual"
	cfg.ManualFallbacks = map[string][]proxy.ModelRef{
		"model-x": {{Upstream: "joycode", Model: "model-y"}},
	}
	cfg.GlobalFallback = proxy.ModelRef{Upstream: "deveco", Model: "model-z"}
	if err := mgr.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SavePreset("手动档"); err != nil {
		t.Fatal(err)
	}

	// 用同一路径重开，模拟重启
	mgr2, err := NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg2 := mgr2.Get()
	if len(cfg2.Presets) != 1 {
		t.Fatalf("重启后方案丢失，得到 %d 个", len(cfg2.Presets))
	}
	p := cfg2.Presets[0]
	if p.Name != "手动档" || p.Mode != "manual" {
		t.Errorf("方案元数据不对: %+v", p)
	}
	if len(p.ManualFallbacks["model-x"]) != 1 {
		t.Errorf("manualFallbacks 未持久化: %+v", p.ManualFallbacks)
	}
	if p.GlobalFallback.Model != "model-z" {
		t.Errorf("globalFallback 未持久化: %+v", p.GlobalFallback)
	}
}

// TestRenamePreset 重命名应同步激活标记，并拒绝撞名
func TestRenamePreset(t *testing.T) {
	mgr := newTestManager(t)

	setChain(t, mgr, "joycode", "model-a")
	if err := mgr.SavePreset("旧名"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenamePreset("旧名", "新名"); err != nil {
		t.Fatalf("RenamePreset: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Presets[0].Name != "新名" {
		t.Errorf("重命名失败: %s", cfg.Presets[0].Name)
	}
	if cfg.ActivePreset != "新名" {
		t.Errorf("激活标记未跟着改名: %q", cfg.ActivePreset)
	}

	// 撞名要报错
	if err := mgr.SavePreset("另一个"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RenamePreset("新名", "另一个"); err == nil {
		t.Error("重命名到已存在的名字应报错")
	}

	// 空名要报错
	if err := mgr.RenamePreset("新名", "   "); err == nil {
		t.Error("空方案名应报错")
	}
}

// TestDeletePreset 删除激活方案应清空激活标记，但不动当前配置
func TestDeletePreset(t *testing.T) {
	mgr := newTestManager(t)

	setChain(t, mgr, "joycode", "model-a")
	if err := mgr.SavePreset("待删"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeletePreset("待删"); err != nil {
		t.Fatalf("DeletePreset: %v", err)
	}

	cfg := mgr.Get()
	if len(cfg.Presets) != 0 {
		t.Errorf("方案未删除，剩 %d 个", len(cfg.Presets))
	}
	if cfg.ActivePreset != "" {
		t.Errorf("删除激活方案后应清空标记，得到 %q", cfg.ActivePreset)
	}
	// 当前配置内容不受影响
	if cfg.AutoChain[0].Models[0] != "model-a" {
		t.Error("删除方案不应影响当前配置")
	}

	if err := mgr.DeletePreset("不存在"); err == nil {
		t.Error("删除不存在的方案应报错")
	}
}

// TestSavePresetEmptyName 空方案名要挡住
func TestSavePresetEmptyName(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SavePreset("   "); err == nil {
		t.Error("空方案名应报错")
	}
}

// TestCloneIsolatesPresets Clone 必须深拷贝方案，否则外部改动会串回 Manager
func TestCloneIsolatesPresets(t *testing.T) {
	mgr := newTestManager(t)
	setChain(t, mgr, "joycode", "model-a")
	if err := mgr.SavePreset("档"); err != nil {
		t.Fatal(err)
	}

	// 拿一份克隆并就地篡改
	c1 := mgr.Get()
	c1.Presets[0].Name = "被改了"
	c1.Presets[0].AutoChain[0].Models[0] = "被改了"

	// Manager 内部不应受影响
	c2 := mgr.Get()
	if c2.Presets[0].Name != "档" {
		t.Errorf("Clone 未隔离方案名: %s", c2.Presets[0].Name)
	}
	if c2.Presets[0].AutoChain[0].Models[0] != "model-a" {
		t.Errorf("Clone 未深拷贝方案内的链: %s", c2.Presets[0].AutoChain[0].Models[0])
	}
}

// TestActivePresetSurvivesDanglingName 悬空的 ActivePreset 不该让整份配置被判非法
// （Load 校验失败会重置为默认，用户会丢掉所有配置）
func TestActivePresetSurvivesDanglingName(t *testing.T) {
	mgr := newTestManager(t)
	cfg := mgr.Get()
	cfg.ActivePreset = "根本不存在的方案"
	if err := mgr.SaveConfig(cfg); err != nil {
		t.Fatalf("悬空 ActivePreset 不应导致校验失败: %v", err)
	}
}
