import { useEffect, useState } from "react";
import { ConfigService } from "../../bindings/switchfree/service";
import type { Config, AgentModels } from "../../bindings/switchfree/config/models";
import type { ModelRef } from "../../bindings/switchfree/proxy/models";
import type { UpstreamModels, ModelOption } from "../../bindings/switchfree/service/models";
import type { AllCredStatus } from "../../bindings/switchfree/service/models";
import CopyButton from "./CopyButton";
import PricingEditor from "./PricingEditor";
import UpdatePanel from "./UpdatePanel";

const UPSTREAM_LABEL: Record<string, string> = {
  joycode: "JoyCode",
  deveco: "DevEco",
  opencode: "OpenCode",
};

export default function Settings({ creds, config }: { creds: AllCredStatus | null; config: Config | null }) {
  // config 由 App 提供（已在启动时拉好），不再异步等待，避免"加载中"
  const [cfg, setCfg] = useState<Config | null>(config);
  const [available, setAvailable] = useState<UpstreamModels[]>([]);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState<{ type: "ok" | "err"; text: string } | null>(null);
  // 手动降级链添加器的 state（必须在早返回之前，遵守 Hooks 规则）
  const [newManualKey, setNewManualKey] = useState("");
  const [newManualUpstream, setNewManualUpstream] = useState("joycode");
  const [newManualModel, setNewManualModel] = useState("");
  const [refreshingModels, setRefreshingModels] = useState(false);
  const [savingPort, setSavingPort] = useState(false);
  // 设置页 tab：通用 / 运行模式 / 费率 / 更新
  const [tab, setTab] = useState<"general" | "mode" | "pricing" | "update">("general");

  const flash = (type: "ok" | "err", text: string) => {
    setMsg({ type, text });
    setTimeout(() => setMsg(null), 3000);
  };

  const load = async () => {
    // config 用 props 的（App 已提前拉），只拉模型列表（后端有缓存，快）
    const a = await ConfigService.GetAvailableModels();
    setAvailable((a ?? []).filter((x): x is UpstreamModels => x !== null));
  };

  useEffect(() => {
    load();
    // config prop 变化时同步（App 刷新后）
    if (config) setCfg(config);
  }, []);

  const refreshModels = async () => {
    setRefreshingModels(true);
    try {
      const a = await ConfigService.RefreshModels();
      setAvailable((a ?? []).filter((x): x is UpstreamModels => x !== null));
      flash("ok", "模型列表已刷新");
    } catch (e) {
      flash("err", `刷新失败: ${e}`);
    } finally {
      setRefreshingModels(false);
    }
  };

  // 凭据有效性（用于提示哪些 upstream 可用）
  const credValid: Record<string, boolean> = {
    joycode: creds?.joycode?.valid ?? false,
    deveco: creds?.deveco?.valid ?? false,
    opencode: creds?.opencode?.valid ?? false,
  };

  if (!cfg) return <div className="p-6 text-[var(--color-text-dim)]">加载配置中...</div>;

  const save = async () => {
    setSaving(true);
    try {
      await ConfigService.SaveConfig(cfg);
      flash("ok", "配置已保存并生效");
    } catch (e) {
      flash("err", `保存失败: ${e}`);
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    if (!confirm("确定重置为默认配置？")) return;
    try {
      await ConfigService.ResetConfig();
      await load();
      flash("ok", "已重置为默认配置");
    } catch (e) {
      flash("err", `重置失败: ${e}`);
    }
  };

  // 只保存端口（不影响运行模式配置）
  const savePort = async () => {
    setSavingPort(true);
    try {
      const cur = await ConfigService.GetConfig();
      if (!cur) throw new Error("获取配置失败");
      await ConfigService.SaveConfig({ ...cur, port: cfg.port });
      flash("ok", "端口已保存，代理已重启");
    } catch (e) {
      flash("err", `保存端口失败: ${e}`);
    } finally {
      setSavingPort(false);
    }
  };

  // ====== auto 链操作 ======
  const allModels = available.flatMap((u) =>
    u.models.map((m) => ({ upstream: u.upstream, model: m.id, label: m.label }))
  );

  const addAutoItem = (upstream: string, model: string) => {
    const chain = [...cfg.autoChain];
    const existing = chain.find((c) => c.upstream === upstream);
    if (existing) {
      existing.models = [...existing.models, model];
    } else {
      chain.push({ upstream, models: [model] } as AgentModels);
    }
    setCfg({ ...cfg, autoChain: chain });
  };

  const removeAutoItem = (upstream: string, model: string) => {
    const chain = cfg.autoChain
      .map((c) => {
        if (c.upstream !== upstream) return c;
        return { ...c, models: c.models.filter((m) => m !== model) } as AgentModels;
      })
      .filter((c) => c.models.length > 0);
    setCfg({ ...cfg, autoChain: chain });
  };

  const moveAutoItem = (upstream: string, model: string, dir: -1 | 1) => {
    // 扁平化 -> 移动 -> 重新分组（保持同 upstream 相邻）
    const flat: { upstream: string; model: string }[] = [];
    cfg.autoChain.forEach((c) => c.models.forEach((m) => flat.push({ upstream: c.upstream, model: m })));
    const idx = flat.findIndex((f) => f.upstream === upstream && f.model === model);
    if (idx < 0) return;
    const newIdx = idx + dir;
    if (newIdx < 0 || newIdx >= flat.length) return;
    [flat[idx], flat[newIdx]] = [flat[newIdx], flat[idx]];
    // 重新分组
    const chain: AgentModels[] = [];
    for (const f of flat) {
      const last = chain[chain.length - 1];
      if (last && last.upstream === f.upstream) {
        last.models.push(f.model);
      } else {
        chain.push({ upstream: f.upstream, models: [f.model] } as AgentModels);
      }
    }
    setCfg({ ...cfg, autoChain: chain });
  };

  // 扁平化的 auto 链（用于显示和排序）
  const flatChain: { upstream: string; model: string }[] = [];
  cfg.autoChain.forEach((c) => c.models.forEach((m) => flatChain.push({ upstream: c.upstream, model: m })));

  // ====== 手动降级链操作 ======
  const manualKeys = Object.keys(cfg.manualFallbacks || {});

  const addManualFallback = () => {
    if (!newManualKey || !newManualModel) return;
    const fb = { ...(cfg.manualFallbacks || {}) };
    fb[newManualKey] = [...(fb[newManualKey] || []), { upstream: newManualUpstream, model: newManualModel } as ModelRef];
    setCfg({ ...cfg, manualFallbacks: fb });
    setNewManualModel("");
  };

  const removeManualFallback = (key: string, idx: number) => {
    const fb = { ...(cfg.manualFallbacks || {}) };
    fb[key] = (fb[key] || []).filter((_, i) => i !== idx);
    if (fb[key].length === 0) delete fb[key];
    setCfg({ ...cfg, manualFallbacks: fb });
  };

  return (
    <div className="p-6 space-y-6">
      {msg && (
        <div className={`px-4 py-2 rounded-lg text-sm ${msg.type === "ok" ? "bg-[var(--color-success)]/20 text-[var(--color-success)]" : "bg-[var(--color-danger)]/20 text-[var(--color-danger)]"}`}>
          {msg.text}
        </div>
      )}

      {/* 顶部 tab 导航 */}
      <div className="flex gap-1 border-b border-[var(--color-border)] pb-px">
        <TabBtn label="⚙️ 通用" active={tab === "general"} onClick={() => setTab("general")} />
        <TabBtn label="🚀 运行模式" active={tab === "mode"} onClick={() => setTab("mode")} />
        <TabBtn label="💰 费率" active={tab === "pricing"} onClick={() => setTab("pricing")} />
        <TabBtn label="🔄 更新" active={tab === "update"} onClick={() => setTab("update")} />
      </div>

      {/* ===== 通用：代理端口 + 配置 JSON ===== */}
      {tab === "general" && (
      <div className="space-y-6">
        {/* 代理端口 */}
        <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
          <h2 className="font-semibold mb-1">代理端口</h2>
          <p className="text-xs text-[var(--color-text-dim)] mb-3">
            cc-switch 的 baseURL 要对应此端口（http://127.0.0.1:端口）。保存后代理会重启切换端口。
          </p>
          <div className="flex items-center gap-3">
            <input
              type="number"
              min={1024}
              max={65535}
              value={cfg.port || 8787}
              onChange={(e) => {
                const p = parseInt(e.target.value, 10);
                if (!isNaN(p)) setCfg({ ...cfg, port: p });
              }}
              className="w-32 px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)] font-mono"
            />
            <span className="text-sm text-[var(--color-text-dim)]">
              监听 http://127.0.0.1:{cfg.port || 8787}
            </span>
            <button
              onClick={() => setCfg({ ...cfg, port: 8787 })}
              className="px-2.5 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
            >
              恢复默认 8787
            </button>
            <button
              onClick={savePort}
              disabled={savingPort}
              className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
            >
              {savingPort ? "保存中..." : "💾 保存端口"}
            </button>
          </div>
        </section>
      </div>
      )}

      {/* ===== 运行模式：模式切换 + auto链/手动链 + 兜底 + 配置JSON ===== */}
      {tab === "mode" && (
      <div className="space-y-6">

      {/* 运行模式操作按钮 */}
      <div className="flex items-center justify-between">
        <div className="flex gap-2">
          <button
            onClick={save}
            disabled={saving}
            className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
          >
            {saving ? "保存中..." : "💾 保存运行模式"}
          </button>
          <button
            onClick={reset}
            className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
          >
            重置默认
          </button>
        </div>
        <button
          onClick={refreshModels}
          disabled={refreshingModels}
          className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
          title="从上游接口重新拉取模型列表"
        >
          {refreshingModels ? "刷新中..." : "🔄 刷新模型"}
        </button>
      </div>

      {/* 模式切换 */}
      <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
        <h2 className="font-semibold mb-3">运行模式</h2>
        <div className="flex gap-4">
          <label className={`flex items-center gap-2 px-4 py-2 rounded-lg cursor-pointer border ${cfg.mode === "auto" ? "border-[var(--color-primary)] bg-[var(--color-primary)]/10" : "border-[var(--color-border)]"}`}>
            <input
              type="radio"
              checked={cfg.mode === "auto"}
              onChange={() => setCfg({ ...cfg, mode: "auto" })}
              className="accent-[var(--color-primary)]"
            />
            <div>
              <div className="text-sm font-medium">auto 模式</div>
              <div className="text-xs text-[var(--color-text-dim)]">客户端发 auto/不指定时，按优先级链依次尝试</div>
            </div>
          </label>
          <label className={`flex items-center gap-2 px-4 py-2 rounded-lg cursor-pointer border ${cfg.mode === "manual" ? "border-[var(--color-primary)] bg-[var(--color-primary)]/10" : "border-[var(--color-border)]"}`}>
            <input
              type="radio"
              checked={cfg.mode === "manual"}
              onChange={() => setCfg({ ...cfg, mode: "manual" })}
              className="accent-[var(--color-primary)]"
            />
            <div>
              <div className="text-sm font-medium">手动模式</div>
              <div className="text-xs text-[var(--color-text-dim)]">客户端指定具体模型时，严格走该模型 + 其降级链</div>
            </div>
          </label>
        </div>
        <p className="text-xs text-[var(--color-text-dim)] mt-3">
          {cfg.mode === "auto"
            ? "auto 模式：客户端发 auto/不指定时，按下方优先级链依次尝试，末尾追加全局兜底。"
            : "手动模式：客户端指定具体模型时严格走该模型，失败按该模型的降级链走，末尾追加全局兜底。"}
        </p>
      </section>

      {/* auto 优先级链（仅 auto 模式显示） */}
      {cfg.mode === "auto" && (
      <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
        <div className="flex items-center gap-2 mb-1">
          <h2 className="font-semibold">auto 模式优先级链</h2>
          <div className="flex gap-1.5 ml-auto">
            {available.map((u) => (
              <span
                key={u.upstream}
                className={`text-[10px] px-1.5 py-0.5 rounded ${
                  u.source === "live"
                    ? "bg-[var(--color-success)]/20 text-[var(--color-success)]"
                    : "bg-[var(--color-warning)]/20 text-[var(--color-warning)]"
                }`}
                title={u.source === "live" ? "接口实时拉取" : "本地白名单（接口拉取失败或未启用）"}
              >
                {UPSTREAM_LABEL[u.upstream] ?? u.upstream}: {u.source === "live" ? `${u.models.length} 实时` : `${u.models.length} 本地`}
              </span>
            ))}
          </div>
        </div>
        <p className="text-xs text-[var(--color-text-dim)] mb-3">
          按顺序尝试，任意一个成功即返回。凭据无效的会被自动跳过。
        </p>

        {flatChain.length === 0 ? (
          <div className="text-sm text-[var(--color-text-dim)] py-4 text-center">链为空，请添加模型</div>
        ) : (
          <div className="space-y-2 mb-3">
            {flatChain.map((item, idx) => {
              const valid = credValid[item.upstream];
              const opt = allModels.find((m) => m.upstream === item.upstream && m.model === item.model);
              return (
                <div key={`${item.upstream}-${item.model}-${idx}`} className="flex items-center gap-2 bg-[var(--color-bg)] rounded-lg p-2.5 border border-[var(--color-border)]">
                  <span className="text-xs text-[var(--color-text-dim)] w-6 text-center">{idx + 1}</span>
                  <span className="text-xs px-2 py-0.5 rounded bg-[var(--color-surface-2)]">{UPSTREAM_LABEL[item.upstream]}</span>
                  <span className="flex-1 text-sm font-mono truncate">{item.model}</span>
                  {opt && <span className="text-xs text-[var(--color-text-dim)] truncate">{opt.label}</span>}
                  <span className={`text-xs px-1.5 py-0.5 rounded ${valid ? "text-[var(--color-success)]" : "text-[var(--color-danger)]"}`}>
                    {valid ? "✓" : "✗"}
                  </span>
                  <button onClick={() => moveAutoItem(item.upstream, item.model, -1)} disabled={idx === 0} className="w-6 h-6 rounded hover:bg-[var(--color-surface-2)] disabled:opacity-30 text-xs">↑</button>
                  <button onClick={() => moveAutoItem(item.upstream, item.model, 1)} disabled={idx === flatChain.length - 1} className="w-6 h-6 rounded hover:bg-[var(--color-surface-2)] disabled:opacity-30 text-xs">↓</button>
                  <button onClick={() => removeAutoItem(item.upstream, item.model)} className="w-6 h-6 rounded hover:bg-[var(--color-danger)]/20 text-[var(--color-danger)] text-xs">✕</button>
                </div>
              );
            })}
          </div>
        )}

        {/* 添加新项 */}
        <AutoChainAdder available={available} credValid={credValid} onAdd={addAutoItem} />
      </section>
      )}

      {/* 全局兜底 */}
      <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
        <h2 className="font-semibold mb-1">全局兜底</h2>
        <p className="text-xs text-[var(--color-text-dim)] mb-3">
          所有链都失败时的最终保底模型（建议选凭据稳定的 upstream）。
        </p>
        <ModelPicker
          available={available}
          value={cfg.globalFallback}
          onChange={(ref) => setCfg({ ...cfg, globalFallback: ref })}
        />
      </section>

      {/* 手动模式降级链（仅手动模式显示） */}
      {cfg.mode === "manual" && (
      <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
        <h2 className="font-semibold mb-1">手动模式降级链（可选）</h2>
        <p className="text-xs text-[var(--color-text-dim)] mb-3">
          客户端指定某模型失败时，按此链降级。未配置的模型失败后直接走全局兜底。
        </p>

        {manualKeys.length === 0 ? (
          <div className="text-sm text-[var(--color-text-dim)] py-2 mb-2">未配置任何手动降级链</div>
        ) : (
          <div className="space-y-3 mb-3">
            {manualKeys.map((key) => {
              const chain = cfg.manualFallbacks[key] || [];
              return (
                <div key={key} className="bg-[var(--color-bg)] rounded-lg p-3 border border-[var(--color-border)]">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="text-xs text-[var(--color-text-dim)]">指定模型：</span>
                    <code className="px-2 py-0.5 rounded bg-[var(--color-surface-2)] font-mono text-xs">{key}</code>
                    <span className="text-xs text-[var(--color-text-dim)]">→ 失败后尝试：</span>
                  </div>
                  <div className="space-y-1.5 ml-4">
                    {chain.map((ref, idx) => (
                      <div key={idx} className="flex items-center gap-2">
                        <span className="text-xs text-[var(--color-text-dim)] w-4">{idx + 1}</span>
                        <span className="text-xs px-2 py-0.5 rounded bg-[var(--color-surface-2)]">{UPSTREAM_LABEL[ref.upstream]}</span>
                        <code className="flex-1 font-mono text-xs">{ref.model}</code>
                        <button onClick={() => removeManualFallback(key, idx)} className="w-5 h-5 rounded hover:bg-[var(--color-danger)]/20 text-[var(--color-danger)] text-xs">✕</button>
                      </div>
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {/* 添加手动降级 */}
        <div className="flex items-center gap-2 flex-wrap pt-3 border-t border-[var(--color-border)]">
          <span className="text-xs text-[var(--color-text-dim)]">指定模型：</span>
          <select
            value={newManualKey}
            onChange={(e) => setNewManualKey(e.target.value)}
            className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
          >
            <option value="">选择模型...</option>
            {allModels.map((m) => (
              <option key={`${m.upstream}-${m.model}`} value={m.model}>
                {UPSTREAM_LABEL[m.upstream]}/{m.model}
              </option>
            ))}
          </select>
          <span className="text-xs text-[var(--color-text-dim)]">→ 降级到：</span>
          <select
            value={newManualUpstream}
            onChange={(e) => setNewManualUpstream(e.target.value)}
            className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
          >
            {available.map((u) => (
              <option key={u.upstream} value={u.upstream}>{UPSTREAM_LABEL[u.upstream]}</option>
            ))}
          </select>
          <select
            value={newManualModel}
            onChange={(e) => setNewManualModel(e.target.value)}
            className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
          >
            <option value="">选择模型...</option>
            {available.find((u) => u.upstream === newManualUpstream)?.models.map((m: ModelOption) => (
              <option key={m.id} value={m.id}>{m.label}</option>
            ))}
          </select>
          <button
            onClick={addManualFallback}
            disabled={!newManualKey || !newManualModel}
            className="px-3 py-1 text-xs rounded-md bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
          >
            + 添加
          </button>
        </div>
      </section>
      )}

        {/* 配置 JSON 预览 */}
        <details className="bg-[var(--color-surface)] rounded-xl p-4 border border-[var(--color-border)]">
          <summary className="cursor-pointer text-sm text-[var(--color-text-dim)]">查看运行模式配置 JSON</summary>
          <pre className="mt-3 text-xs font-mono p-3 rounded bg-[var(--color-bg)] overflow-x-auto">
{JSON.stringify({ mode: cfg.mode, autoChain: cfg.autoChain, manualFallbacks: cfg.manualFallbacks, globalFallback: cfg.globalFallback }, null, 2)}
          </pre>
          <div className="mt-2">
            <CopyButton text={JSON.stringify({ mode: cfg.mode, autoChain: cfg.autoChain, manualFallbacks: cfg.manualFallbacks, globalFallback: cfg.globalFallback }, null, 2)} label="复制运行模式配置" />
          </div>
        </details>
      </div>
      )}

      {/* ===== 费率管理 ===== */}
      {tab === "pricing" && (
      <div className="space-y-6">
        <PricingEditor />
      </div>
      )}

      {/* ===== 自动升级 ===== */}
      {tab === "update" && (
      <div className="space-y-6">
        <UpdatePanel />
      </div>
      )}
    </div>
  );
}

// ====== TabBtn：设置页顶部 tab ======
function TabBtn({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors -mb-px ${
        active
          ? "border border-b-0 border-[var(--color-border)] bg-[var(--color-surface)] text-[var(--color-primary)]"
          : "text-[var(--color-text-dim)] hover:text-[var(--color-text)] hover:bg-[var(--color-surface)]/50"
      }`}
    >
      {label}
    </button>
  );
}

// ====== AutoChainAdder：auto 链添加器 ======
function AutoChainAdder({
  available,
  credValid,
  onAdd,
}: {
  available: UpstreamModels[];
  credValid: Record<string, boolean>;
  onAdd: (upstream: string, model: string) => void;
}) {
  const [upstream, setUpstream] = useState("deveco");
  const [model, setModel] = useState("");

  const models = available.find((u) => u.upstream === upstream)?.models ?? [];

  return (
    <div className="flex items-center gap-2 flex-wrap pt-3 border-t border-[var(--color-border)]">
      <select
        value={upstream}
        onChange={(e) => {
          setUpstream(e.target.value);
          setModel("");
        }}
        className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
      >
        {available.map((u) => (
          <option key={u.upstream} value={u.upstream}>
            {UPSTREAM_LABEL[u.upstream]} {credValid[u.upstream] ? "✓" : "✗"}
          </option>
        ))}
      </select>
      <select
        value={model}
        onChange={(e) => setModel(e.target.value)}
        className="flex-1 min-w-[200px] px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
      >
        <option value="">选择模型...</option>
        {models.map((m: ModelOption) => (
          <option key={m.id} value={m.id}>{m.label}</option>
        ))}
      </select>
      <button
        onClick={() => {
          if (model) {
            onAdd(upstream, model);
            setModel("");
          }
        }}
        disabled={!model}
        className="px-3 py-1 text-xs rounded-md bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
      >
        + 添加到链
      </button>
    </div>
  );
}

// ====== ModelPicker：单个模型选择器（兜底用） ======
function ModelPicker({
  available,
  value,
  onChange,
}: {
  available: UpstreamModels[];
  value: ModelRef;
  onChange: (ref: ModelRef) => void;
}) {
  const models = available.find((u) => u.upstream === value.upstream)?.models ?? [];
  return (
    <div className="flex items-center gap-2">
      <select
        value={value.upstream}
        onChange={(e) => {
          const first = available.find((u) => u.upstream === e.target.value)?.models[0];
          onChange({ upstream: e.target.value, model: first?.id ?? "" } as ModelRef);
        }}
        className="px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
      >
        {available.map((u) => (
          <option key={u.upstream} value={u.upstream}>{UPSTREAM_LABEL[u.upstream]}</option>
        ))}
      </select>
      <select
        value={value.model}
        onChange={(e) => onChange({ upstream: value.upstream, model: e.target.value } as ModelRef)}
        className="flex-1 px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
      >
        {models.map((m: ModelOption) => (
          <option key={m.id} value={m.id}>{m.label}</option>
        ))}
      </select>
    </div>
  );
}
