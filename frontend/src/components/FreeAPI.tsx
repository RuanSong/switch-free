import { useEffect, useState, useCallback } from "react";
import { FreeAPIService } from "../../bindings/switchfree/service";
import type { Catalog, CatalogModel, ProviderConfig, ProviderModel } from "../../bindings/switchfree/freeapi/models";
import { useWailsEvent } from "../hooks/useWailsEvent";
import ProviderPicker from "./ProviderPicker";

// 能力推断：从模型名判断（工具/视觉/推理）
function inferVision(name: string): boolean {
  return /vision|vl|visual|multimodal/i.test(name);
}
function inferReasoning(name: string): boolean {
  return /reason|think|thinking|r1|o1|deepseek-reasoner|k2-thinking/i.test(name);
}
function inferTool(name: string): boolean {
  return /tool|function|coder|code|agent/i.test(name);
}

// 解析 context 文本（"1M" -> 1000000, "131K" -> 131000）；解析失败返回 0
function parseContext(s: string): number {
  if (!s) return 0;
  const m = s.match(/^([\d.]+)\s*([KMB])?/i);
  if (!m) return 0;
  const num = parseFloat(m[1]);
  const unit = (m[2] ?? "").toUpperCase();
  if (unit === "M") return Math.round(num * 1000000);
  if (unit === "K") return Math.round(num * 1000);
  if (unit === "B") return Math.round(num * 1000000000);
  return Math.round(num);
}

export default function FreeAPI() {
  const [providers, setProviders] = useState<Record<string, ProviderConfig | null>>({});
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [msg, setMsg] = useState<{ type: "ok" | "err"; text: string } | null>(null);

  // 添加表单状态
  const [showAdd, setShowAdd] = useState(false);
  const [fromCatalog, setFromCatalog] = useState(true);
  const [selectedCatalog, setSelectedCatalog] = useState<string>("");
  const [customName, setCustomName] = useState("");
  const [customId, setCustomId] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [candidateModels, setCandidateModels] = useState<CatalogModel[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);

  // 评测状态
  const [benchmarking, setBenchmarking] = useState<Record<string, boolean>>({});
  const [benchResult, setBenchResult] = useState<Record<string, any>>({});

  const [refreshing, setRefreshing] = useState(false);
  // 正在编辑的供应商 id（空表示新增）
  const [editingId, setEditingId] = useState<string>("");

  const load = useCallback(async () => {
    const [ps, cat] = await Promise.all([
      FreeAPIService.GetProviders(),
      FreeAPIService.GetCatalog(),
    ]);
    const cleaned: Record<string, ProviderConfig | null> = {};
    for (const k of Object.keys(ps ?? {})) {
      cleaned[k] = ps[k] ?? null;
    }
    setProviders(cleaned);
    setCatalog(cat);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // 凭据变化时刷新（新增/删除供应商后）
  useWailsEvent("freeapi:change", () => load());
  useWailsEvent("cred:change", () => load());

  const flash = (type: "ok" | "err", text: string) => {
    setMsg({ type, text });
    setTimeout(() => setMsg(null), 3000);
  };

  // 选择目录供应商 -> 自动填 base_url
  const onSelectCatalog = (id: string) => {
    setSelectedCatalog(id);
    const p = catalog?.providers.find((x) => x.id === id);
    setBaseURL(p?.base_url ?? "");
    setCandidateModels(p?.free_models ?? []);
    setBenchResult({});
  };

  // 自定义：id 自动生成（若为空）
  const ensureCustomId = () => {
    if (!customId.trim()) {
      return "custom-" + Math.random().toString(36).slice(2, 8);
    }
    return customId.trim();
  };

  // 测试连接 + 拉取模型（用 baseURL + apiKey）
  const testAndFetch = async () => {
    if (!baseURL.trim() || !apiKey.trim()) {
      flash("err", "请先填写 BaseURL 和 API Key");
      return;
    }
    setLoadingModels(true);
    setBenchResult({});
    try {
      const fetched = await FreeAPIService.FetchProviderModels(baseURL.trim(), apiKey.trim());
      if (!fetched || fetched.length === 0) {
        flash("err", "未拉取到模型，请检查 BaseURL 和 API Key");
        setCandidateModels([]);
        return;
      }
      // 合并目录模型信息（context/rate_limit）+ 实时模型
      const catModels = selectedCatalog
        ? catalog?.providers.find((x) => x.id === selectedCatalog)?.free_models ?? []
        : [];
      const merged: CatalogModel[] = fetched.map((m) => {
        const mid = m.ID;
        const known = catModels.find((c) => c.id === mid || c.name === mid);
        return {
          id: mid,
          name: known?.name ?? mid,
          context: known?.context ?? "",
          rate_limit: known?.rate_limit ?? "",
        } as CatalogModel;
      });
      setCandidateModels(merged);
      flash("ok", `拉取到 ${fetched.length} 个模型`);
    } catch (e) {
      flash("err", `拉取失败: ${e}`);
    } finally {
      setLoadingModels(false);
    }
  };

  // 评测单个模型
  const benchmarkOne = async (model: CatalogModel) => {
    if (!baseURL.trim() || !apiKey.trim()) {
      flash("err", "请先填写 API Key");
      return;
    }
    setBenchmarking((p) => ({ ...p, [model.id]: true }));
    try {
      const res = await FreeAPIService.BenchmarkModel(baseURL.trim(), apiKey.trim(), model.id, "", 256);
      setBenchResult((p) => ({ ...p, [model.id]: res ?? {} }));
    } catch (e) {
      setBenchResult((p) => ({ ...p, [model.id]: { success: false, errorMsg: String(e) } }));
    } finally {
      setBenchmarking((p) => ({ ...p, [model.id]: false }));
    }
  };

  // 评测通过 -> 加入供应商
  const addVerifiedModel = async (model: CatalogModel) => {
    const providerId = selectedCatalog || ensureCustomId();
    const p = providers[providerId];
    if (!p) {
      flash("err", "请先保存供应商配置");
      return;
    }
    const pm: ProviderModel = {
      id: model.id,
      context: parseContext(model.context),
      verified: true,
      healthy: true,
      failCount: 0,
    };
    try {
      await FreeAPIService.AddVerifiedModel(providerId, pm);
      flash("ok", `模型 ${model.id} 已评测通过并加入`);
      await load();
    } catch (e) {
      flash("err", `添加失败: ${e}`);
    }
  };

  // 保存供应商（新建/编辑）
  // 编辑模式（editingId 非空）：用原 id，保留已验证模型和 verified 状态
  const saveProvider = async () => {
    const isEdit = !!editingId;
    const providerId = isEdit ? editingId : (selectedCatalog || ensureCustomId());
    // 编辑时保留已通过的模型；新建时为空
    const existing = isEdit ? providers[providerId] : null;
    const cfg: ProviderConfig = {
      id: providerId,
      name: fromCatalog
        ? (catalog?.providers.find((x) => x.id === selectedCatalog)?.name ?? providerId)
        : (customName.trim() || providerId),
      baseURL: baseURL.trim(),
      apiKey: apiKey.trim(),
      getAPIKeyURL: fromCatalog
        ? (catalog?.providers.find((x) => x.id === selectedCatalog)?.get_api_key_url ?? "")
        : (existing?.getAPIKeyURL ?? ""),
      maxContext: existing?.maxContext ?? 0,
      custom: isEdit ? (existing?.custom ?? true) : !fromCatalog,
      verified: existing?.verified ?? false,
      models: existing?.models ?? [],
    };
    try {
      await FreeAPIService.UpsertProvider(cfg);
      flash("ok", isEdit ? `供应商「${cfg.name}」已更新` : `供应商「${cfg.name}」已保存`);
      cancelEdit();
      await load();
    } catch (e) {
      flash("err", `保存失败: ${e}`);
    }
  };

  // 删除供应商
  const removeProvider = async (id: string) => {
    try {
      await FreeAPIService.RemoveProvider(id);
      flash("ok", "已删除");
      await load();
    } catch (e) {
      flash("err", `删除失败: ${e}`);
    }
  };

  // 刷新目录（从 GitHub 拉最新）
  const refreshCatalog = async () => {
    setRefreshing(true);
    try {
      const [cat, changed] = await FreeAPIService.RefreshCatalog();
      if (cat) setCatalog(cat);
      flash("ok", changed ? "目录已更新" : "目录已是最新");
    } catch (e) {
      flash("err", `刷新失败: ${e}`);
    } finally {
      setRefreshing(false);
    }
  };

  // 用系统浏览器打开外部链接（Wails WebView 里 window.open 不可用）
  const openGetKey = (url: string) => {
    if (!url) return;
    FreeAPIService.OpenURL(url).catch(() => {});
  };

  // 开始编辑已添加的供应商
  const startEdit = (id: string) => {
    const p = providers[id];
    if (!p) return;
    setEditingId(id);
    setShowAdd(true);
    setFromCatalog(false); // 编辑统一走自定义模式（可改 name/baseURL/key）
    setSelectedCatalog("");
    setCustomName(p.name);
    setCustomId(p.id);
    setBaseURL(p.baseURL);
    setApiKey(p.apiKey);
    setCandidateModels([]);
    setBenchResult({});
  };

  // 取消编辑/新增
  const cancelEdit = () => {
    setEditingId("");
    setShowAdd(false);
    setApiKey("");
    setBaseURL("");
    setCustomName("");
    setCustomId("");
    setCandidateModels([]);
    setSelectedCatalog("");
  };

  const providerList = Object.entries(providers ?? {}).filter(([, p]) => p !== null) as [string, ProviderConfig][];
  const selProvider = providers[selectedCatalog] ?? null;

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-semibold">免费 API</h2>
          <p className="text-xs text-[var(--color-text-dim)] mt-0.5">
            配置免费的大模型 API 供应商，评测通过的模型与内置上游平级使用。
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={refreshCatalog}
            disabled={refreshing}
            className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
          >
            {refreshing ? "刷新中..." : "🔄 刷新目录"}
          </button>
          <button
            onClick={() => (showAdd ? cancelEdit() : (setShowAdd(true), setEditingId(""), setFromCatalog(true)))}
            className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90"
          >
            {showAdd ? (editingId ? "取消编辑" : "取消添加") : "＋ 添加供应商"}
          </button>
        </div>
      </div>

      {msg && (
        <div className={`px-4 py-2 rounded-lg text-sm ${msg.type === "ok" ? "bg-[var(--color-success)]/20 text-[var(--color-success)]" : "bg-[var(--color-danger)]/20 text-[var(--color-danger)]"}`}>
          {msg.text}
        </div>
      )}

      {/* 添加/编辑表单 */}
      {showAdd && (
        <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)] space-y-3">
          {editingId ? (
            <div className="text-sm font-medium text-[var(--color-primary)]">
              ✏️ 编辑供应商：{providers[editingId]?.name ?? editingId}
            </div>
          ) : (
          <div className="flex gap-4 text-sm">
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={fromCatalog} onChange={() => setFromCatalog(true)} className="accent-[var(--color-primary)]" />
              从内置目录选
            </label>
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={!fromCatalog} onChange={() => setFromCatalog(false)} className="accent-[var(--color-primary)]" />
              自定义添加
            </label>
          </div>
          )}

          {fromCatalog ? (
            <div>
              <label className="text-xs text-[var(--color-text-dim)] block mb-1">选择供应商（可搜索名称或模型）</label>
              <ProviderPicker
                providers={catalog?.providers ?? []}
                value={selectedCatalog}
                onChange={onSelectCatalog}
                existingIds={Object.keys(providers ?? {})}
                placeholder="搜索或选择供应商..."
              />
              {selectedCatalog && (
                <div className="mt-2 flex flex-wrap items-center gap-2 text-xs">
                  {(() => {
                    const p = catalog?.providers.find((x) => x.id === selectedCatalog);
                    if (!p) return null;
                    return (
                      <>
                        <span className="px-2 py-0.5 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)]">
                          {p.free_models_count ?? 0} 免费模型
                        </span>
                        {p.get_api_key_url && (
                          <button
                            onClick={() => openGetKey(p.get_api_key_url!)}
                            className="px-2 py-0.5 rounded bg-[var(--color-primary)]/15 text-[var(--color-primary)] hover:opacity-90"
                          >
                            获取 API Key ↗
                          </button>
                        )}
                      </>
                    );
                  })()}
                </div>
              )}
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3">
              <input
                value={customName}
                onChange={(e) => setCustomName(e.target.value)}
                placeholder="供应商名称（如 My API）"
                className="px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
              />
              <input
                value={customId}
                onChange={(e) => setCustomId(e.target.value)}
                disabled={!!editingId}
                title={editingId ? "id 不可修改" : ""}
                placeholder="id（留空自动生成）"
                className="px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] disabled:opacity-50 disabled:cursor-not-allowed"
              />
            </div>
          )}

          <div className="grid grid-cols-1 gap-3">
            <input
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
              placeholder="BaseURL（如 https://api.groq.com/openai/v1）"
              className="w-full px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
            />
            <div className="flex gap-2">
              <input
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                type="password"
                placeholder="API Key"
                className="flex-1 px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
              />
              <button
                onClick={testAndFetch}
                disabled={loadingModels}
                className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50 whitespace-nowrap"
              >
                {loadingModels ? "拉取中..." : "🔍 拉取模型"}
              </button>
            </div>
          </div>

          {/* 候选模型列表（带信息展示 + 评测按钮） */}
          {candidateModels.length > 0 && (
            <div>
              <div className="text-xs font-medium text-[var(--color-text-dim)] mb-2">
                候选模型（{candidateModels.length} 个，评测通过后加入）
              </div>
              <div className="space-y-1.5 max-h-72 overflow-y-auto">
                {candidateModels.map((m) => {
                  const res = benchResult[m.id];
                  return (
                    <div key={m.id} className="flex items-center gap-2 bg-[var(--color-bg)] rounded-lg p-2 border border-[var(--color-border)]">
                      <div className="flex-1 min-w-0">
                        <div className="text-sm truncate flex items-center gap-1.5">
                          {m.name || m.id}
                          {inferVision(m.id) && <span className="text-[10px] px-1 rounded bg-blue-500/20 text-blue-400 shrink-0">视觉</span>}
                          {inferReasoning(m.id) && <span className="text-[10px] px-1 rounded bg-purple-500/20 text-purple-400 shrink-0">推理</span>}
                          {inferTool(m.id) && <span className="text-[10px] px-1 rounded bg-emerald-500/20 text-emerald-400 shrink-0">工具</span>}
                        </div>
                        <div className="text-[11px] text-[var(--color-text-dim)] truncate">
                          {m.context && <span>上下文 {m.context}</span>}
                          {m.rate_limit && <span className="ml-2">限流：{m.rate_limit}</span>}
                        </div>
                      </div>
                      {res?.success ? (
                        <span className="text-xs text-[var(--color-success)] whitespace-nowrap">
                          ✓ {res.tps?.toFixed(1) ?? "-"} tok/s
                        </span>
                      ) : res && !res.success ? (
                        <span className="text-xs text-[var(--color-danger)] whitespace-nowrap">✗ 失败</span>
                      ) : null}
                      {selProvider?.models?.some((x) => x.id === m.id && x.verified) ? (
                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-success)]/20 text-[var(--color-success)] whitespace-nowrap">
                          ✓ 已加入
                        </span>
                      ) : (
                        <>
                          <button
                            onClick={() => benchmarkOne(m)}
                            disabled={benchmarking[m.id]}
                            className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50 whitespace-nowrap"
                          >
                            {benchmarking[m.id] ? "评测中..." : res ? "重新评测" : "评测"}
                          </button>
                          {res?.success && (
                            <button
                              onClick={() => addVerifiedModel(m)}
                              className="px-2 py-1 text-xs rounded-md bg-[var(--color-primary)] hover:opacity-90 whitespace-nowrap"
                            >
                              ✓ 加入
                            </button>
                          )}
                        </>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* 保存供应商（编辑模式 / 未保存时显示；已从目录选且已保存则隐藏，因为可通过编辑改） */}
          {(editingId || !selProvider) && (
            <button
              onClick={saveProvider}
              disabled={!baseURL.trim() || !apiKey.trim()}
              className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
            >
              {editingId ? "💾 保存修改" : "💾 保存供应商"}
            </button>
          )}
        </section>
      )}

      {/* 已添加供应商列表 */}
      {providerList.length === 0 && !showAdd ? (
        <div className="text-sm text-[var(--color-text-dim)] py-8 text-center bg-[var(--color-surface)] rounded-xl border border-[var(--color-border)]">
          尚未添加免费 API 供应商。点击「＋ 添加供应商」开始。
        </div>
      ) : (
        <div className="space-y-3">
          {providerList.map(([id, p]) => {
            const verified = p.models?.filter((m) => m.verified) ?? [];
            const unhealthy = verified.filter((m) => !m.healthy);
            return (
              <div key={id} className="bg-[var(--color-surface)] rounded-xl p-4 border border-[var(--color-border)]">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{p.name}</span>
                  {p.custom && <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)]">自定义</span>}
                  <span className={`text-[10px] px-1.5 py-0.5 rounded ${p.verified ? "bg-[var(--color-success)]/20 text-[var(--color-success)]" : "bg-[var(--color-surface-2)] text-[var(--color-text-dim)]"}`}>
                    {p.verified ? `✓ ${verified.length} 模型` : "未评测"}
                  </span>
                  {unhealthy.length > 0 && (
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-danger)]/20 text-[var(--color-danger)]">
                      ⚠ {unhealthy.length} 不健康
                    </span>
                  )}
                  <div className="ml-auto flex gap-2">
                    {p.getAPIKeyURL && (
                      <button
                        onClick={() => openGetKey(p.getAPIKeyURL!)}
                        className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
                      >
                        获取 Key ↗
                      </button>
                    )}
                    <button
                      onClick={() => startEdit(id)}
                      className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
                    >
                      编辑
                    </button>
                    <button
                      onClick={() => removeProvider(id)}
                      className="px-2 py-1 text-xs rounded-md bg-[var(--color-danger)]/15 text-[var(--color-danger)] hover:bg-[var(--color-danger)]/25"
                    >
                      删除
                    </button>
                  </div>
                </div>
                <div className="text-[11px] text-[var(--color-text-dim)] mt-1 truncate">{p.baseURL}</div>
                {/* 已通过模型 */}
                {verified.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {verified.map((m) => (
                      <span
                        key={m.id}
                        className={`text-[11px] px-2 py-0.5 rounded-full border ${
                          m.healthy
                            ? "bg-[var(--color-bg)] border-[var(--color-border)] text-[var(--color-text)]"
                            : "bg-[var(--color-danger)]/10 border-[var(--color-danger)]/40 text-[var(--color-danger)]"
                        }`}
                        title={m.healthy ? "健康" : "健康监控异常（权重降级）"}
                      >
                        {m.id}
                        {m.healthy ? "●" : "○"}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
