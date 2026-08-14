import { useState, useEffect, useMemo, useRef, useCallback } from "react";
import { BenchmarkService, ModelService, FreeAPIService } from "../../bindings/switchfree/service";
import type { BenchmarkResult, ModelDetail, AllCredStatus } from "../../bindings/switchfree/service/models";
import { useWailsEvent } from "../hooks/useWailsEvent";
import { ModelSelect } from "./ModelSelect";
import ConfirmPopover from "./ConfirmPopover";

const DEFAULT_PROMPT = "请详细介绍 Go 语言的 goroutine 和 channel 并发模型，包括基本概念、使用示例和注意事项。";
const STORAGE_KEY = "benchmark.items.v1";

const UPSTREAM_LABEL: Record<string, string> = {
  joycode: "JoyCode",
  deveco: "DevEco",
  workbuddy: "WorkBuddy",
};

const UPSTREAM_COLOR: Record<string, string> = {
  joycode: "bg-[var(--color-success)]/20 text-[var(--color-success)]",
  deveco: "bg-[var(--color-danger)]/20 text-[var(--color-danger)]",
  workbuddy: "bg-pink-500/20 text-pink-400",
};

interface BenchItem {
  id: string; // 稳定的本地 id（React key，切换模型时不重挂载卡片）
  upstream: string;
  model: string;
}

// 结果/流式按 upstream|model 索引（每次测评都对应当前选中的模型）
const itemKey = (it: BenchItem) => `${it.upstream}|${it.model}`;

let _seq = 0;
const newItemId = () => {
  _seq += 1;
  return `bi-${Date.now().toString(36)}-${_seq}`;
};

function upstreamDisplayName(upstream: string, creds: AllCredStatus | null): string {
  if (UPSTREAM_LABEL[upstream]) return UPSTREAM_LABEL[upstream];
  const src = creds?.freeAPIs?.[upstream]?.source ?? "";
  return src.split(" (")[0].trim() || upstream;
}

function defaultModel(models: ModelDetail[], upstream: string): string {
  const up = models.filter((m) => m.upstream === upstream);
  return up.find((m) => m.free)?.id ?? up[0]?.id ?? "";
}

export default function Benchmark({ creds }: { creds: AllCredStatus | null }) {
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [maxTokens, setMaxTokens] = useState(1024);
  const [models, setModels] = useState<ModelDetail[]>([]);
  const [items, setItems] = useState<BenchItem[]>([]);
  const [results, setResults] = useState<Record<string, BenchmarkResult | null>>({});
  const [running, setRunning] = useState(false);
  const [singleRunning, setSingleRunning] = useState<Record<string, boolean>>({});
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [streamingContent, setStreamingContent] = useState<Record<string, string>>({});
  const [addOpen, setAddOpen] = useState(false);
  const addRef = useRef<HTMLDivElement>(null);
  const seeded = useRef(false);

  // 主密码锁定状态：锁定时仍可列出/选择供应商模型（模型元数据不加密），
  // 只是发起测评需要密钥，因此这里仅做提示，不阻断模型选择。
  const [locked, setLocked] = useState(false);
  const refreshModels = useCallback(() => {
    ModelService.GetModels()
      .then((m) =>
        setModels(
          (m ?? [])
            .filter((x): x is ModelDetail => x !== null)
            .filter((x) => x.upstream !== "opencode")
        )
      )
      .catch(() => {});
  }, []);
  const checkLock = useCallback(() => {
    FreeAPIService.GetLockStatus()
      .then((info) => setLocked(!!info?.isLocked))
      .catch(() => {});
  }, []);
  useEffect(() => {
    checkLock();
  }, [checkLock]);
  // 供应商增删/验证/加锁解锁都会推 freeapi:change：同步刷新锁状态 + 模型
  useWailsEvent("freeapi:change", () => {
    checkLock();
    refreshModels();
  });

  useEffect(() => {
    ModelService.GetModels()
      .then((m) => {
        const list = (m ?? [])
          .filter((x): x is ModelDetail => x !== null)
          .filter((x) => x.upstream !== "opencode");
        setModels(list);
      })
      .catch(() => {});
  }, []);

  // 模型加载后：localStorage 有就恢复，否则每个可用 upstream 自动建一项
  useEffect(() => {
    if (models.length === 0 || seeded.current) return;
    seeded.current = true;
    let initial: BenchItem[] = [];
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved) {
        const parsed = JSON.parse(saved) as Array<Partial<BenchItem>>;
        if (Array.isArray(parsed)) {
          initial = parsed
            .filter((p) => p && p.upstream && p.model)
            .map((p) => ({ id: p.id || newItemId(), upstream: p.upstream!, model: p.model! }));
        }
      }
    } catch { /* ignore */ }
    if (initial.length === 0) {
      const ups = Array.from(new Set(models.map((m) => m.upstream)));
      initial = ups
        .map((up) => ({ id: newItemId(), upstream: up, model: defaultModel(models, up) }))
        .filter((it) => it.model);
    }
    setItems(initial);
  }, [models]);

  // 模型列表变化（如供应商新增验证模型）后，清理已不存在的选项
  useEffect(() => {
    if (models.length === 0) return;
    setItems((prev) => {
      const valid = new Set(models.map((m) => `${m.upstream}|${m.id}`));
      const next = prev.map((it) => {
        if (valid.has(`${it.upstream}|${it.model}`)) return it;
        // 之前选的模型没了：回退到该 upstream 的第一个可用模型
        const fallback = defaultModel(models, it.upstream);
        return fallback ? { ...it, model: fallback } : it;
      }).filter((it) => valid.has(`${it.upstream}|${it.model}`));
      return next.length === prev.length && next.every((it, i) => it.model === prev[i].model) ? prev : next;
    });
  }, [models]);

  // 持久化
  useEffect(() => {
    if (seeded.current) localStorage.setItem(STORAGE_KEY, JSON.stringify(items));
  }, [items]);

  // 点击外部关闭添加菜单
  useEffect(() => {
    if (!addOpen) return;
    const handler = (e: MouseEvent) => {
      if (addRef.current && !addRef.current.contains(e.target as Node)) setAddOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [addOpen]);

  // 进度事件：按 upstream|model 索引
  useWailsEvent("benchmark:progress", (data) => {
    const r = data as BenchmarkResult;
    if (r && r.upstream) setResults((prev) => ({ ...prev, [`${r.upstream}|${r.model}`]: r }));
  });

  // 流式 chunk
  useWailsEvent("benchmark:chunk", (data) => {
    const d = data as { upstream: string; model: string; delta: string };
    if (d?.upstream && d.model) {
      const k = `${d.upstream}|${d.model}`;
      setStreamingContent((prev) => ({ ...prev, [k]: (prev[k] || "") + d.delta }));
    }
  });

  const modelsFor = (up: string) => models.filter((m) => m.upstream === up);

  // 每个 upstream 已被其他 item 选中的模型（用于下拉去重；同 upstream 不可重复，不同 upstream 各自独立）
  const usedModelsByUpstream = useMemo(() => {
    const m = new Map<string, Set<string>>();
    items.forEach((it) => {
      if (!it.model) return;
      if (!m.has(it.upstream)) m.set(it.upstream, new Set());
      m.get(it.upstream)!.add(it.model);
    });
    return m;
  }, [items]);

  // 所有 upstream + 是否还有可添加的模型
  const allUpstreams = useMemo(
    () => Array.from(new Set(models.map((m) => m.upstream))),
    [models]
  );
  const addableUpstreams = useMemo(
    () =>
      allUpstreams
        .map((up) => {
          const used = usedModelsByUpstream.get(up);
          return {
            upstream: up,
            label: upstreamDisplayName(up, creds),
            count: modelsFor(up).length,
            hasMore: modelsFor(up).some((m) => !used?.has(m.id)),
          };
        })
        .filter((u) => u.hasMore),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [allUpstreams, models, usedModelsByUpstream, creds]
  );

  const updateItem = (idx: number, patch: Partial<BenchItem>) =>
    setItems((prev) => prev.map((it, i) => (i === idx ? { ...it, ...patch } : it)));
  const removeItem = (idx: number) =>
    setItems((prev) => prev.filter((_, i) => i !== idx));
  const addUpstream = (up: string) => {
    const used = usedModelsByUpstream.get(up);
    const m = modelsFor(up).find((x) => !used?.has(x.id));
    if (!m) return;
    setItems((prev) => [...prev, { id: newItemId(), upstream: up, model: m.id }]);
    setAddOpen(false);
  };

  const run = async () => {
    const valid = items.filter((it) => it.model);
    if (valid.length === 0) return;
    setRunning(true);
    setResults({});
    setStreamingContent({});
    try {
      await BenchmarkService.RunBenchmark(valid, prompt, maxTokens);
    } finally {
      setRunning(false);
    }
  };

  const runSingle = async (it: BenchItem) => {
    const k = itemKey(it);
    setSingleRunning((prev) => ({ ...prev, [k]: true }));
    setResults((prev) => ({ ...prev, [k]: null }));
    setStreamingContent((prev) => ({ ...prev, [k]: "" }));
    try {
      await BenchmarkService.RunBenchmark([it], prompt, maxTokens);
    } finally {
      setSingleRunning((prev) => ({ ...prev, [k]: false }));
    }
  };

  const ranked = items
    .map((it) => results[itemKey(it)])
    .filter((r): r is BenchmarkResult => !!r && r.success)
    .sort((a, b) => b.tps - a.tps);
  const rankOf = (k: string) => ranked.findIndex((r) => `${r.upstream}|${r.model}` === k) + 1;

  return (
    <div className="p-6 space-y-6">
      {/* 锁定提示：仍可选择模型，但发起测评需要密钥 */}
      {locked && (
        <div className="rounded-xl border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/10 px-4 py-3 text-sm text-[var(--color-warning)]">
          🔒 供应商已锁定，可照常选择供应商模型；发起测评需先到「供应商」页输入主密码解锁。
        </div>
      )}
      {/* 配置区 */}
      <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)] space-y-3">
        <div>
          <label className="text-sm text-[var(--color-text-dim)]">测评提示词（统一基准）</label>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            rows={3}
            className="w-full mt-1 px-3 py-2 rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)] text-sm resize-y focus:outline-none focus:border-[var(--color-primary)]"
          />
        </div>
        <div className="flex items-center gap-3 flex-wrap">
          <label className="text-sm text-[var(--color-text-dim)]">最大输出</label>
          <select
            value={maxTokens}
            onChange={(e) => setMaxTokens(Number(e.target.value))}
            className="px-2 py-1 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
          >
            <option value={256}>256</option>
            <option value={512}>512</option>
            <option value={1024}>1024</option>
            <option value={2048}>2048</option>
          </select>
          <span className="text-xs text-[var(--color-text-dim)]">token（越大越能测稳态速率，但更慢）</span>
          <div ref={addRef} className="relative ml-auto">
            <button
              onClick={() => setAddOpen((v) => !v)}
              disabled={running || addableUpstreams.length === 0}
              className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
            >
              ＋ 添加测评项
            </button>
            {addOpen && (
              <div className="absolute right-0 z-50 mt-1 w-64 bg-[var(--color-surface)] border border-[var(--color-border)] rounded-md shadow-lg max-h-72 overflow-y-auto">
                {addableUpstreams.map((u) => (
                  <button
                    key={u.upstream}
                    onClick={() => addUpstream(u.upstream)}
                    className="w-full px-3 py-2 text-sm text-left hover:bg-[var(--color-surface-2)] flex items-center justify-between gap-2"
                  >
                    <span>{u.label}</span>
                    <span className="text-xs text-[var(--color-text-dim)]">{u.count} 个模型</span>
                  </button>
                ))}
              </div>
            )}
          </div>
          {running ? (
            <button
              onClick={() => BenchmarkService.StopBenchmark()}
              className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-danger)] hover:opacity-90"
            >
              ⏹ 停止测评
            </button>
          ) : (
            <button
              onClick={run}
              disabled={items.length === 0 || Object.values(singleRunning).some(Boolean)}
              className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
            >
              🏁 开始测评
            </button>
          )}
        </div>
      </section>

      {/* 测评项列表 */}
      <section className="space-y-3">
        {items.map((it, idx) => {
          const k = itemKey(it);
          const r = results[k];
          const rank = rankOf(k);
          const upModels = modelsFor(it.upstream);
          const medal = rank > 0 ? ["🥇", "🥈", "🥉", "4", "5"][rank - 1] : "";
          const isBusy = (running && !r) || !!singleRunning[k];
          return (
            <div key={it.id} className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
              <div className="flex items-center gap-3 mb-3 flex-wrap">
                <span
                  className={`text-xs px-2 py-0.5 rounded-full shrink-0 ${
                    UPSTREAM_COLOR[it.upstream] ?? "bg-[var(--color-surface-2)]"
                  }`}
                >
                  {upstreamDisplayName(it.upstream, creds)}
                </span>
                <ModelSelect
                  options={upModels}
                  value={it.model}
                  onChange={(id) => updateItem(idx, { model: id })}
                  disabledIds={usedModelsByUpstream.get(it.upstream)}
                  hideFreeBadge
                  className="w-56"
                />
                {isBusy ? (
                  <button
                    onClick={() => BenchmarkService.StopBenchmarkItem(it.upstream, it.model)}
                    title="停止该测评项"
                    className="px-2 py-1 text-xs rounded-md bg-[var(--color-danger)]/80 hover:bg-[var(--color-danger)] text-white"
                  >
                    ⏹ 停止
                  </button>
                ) : (
                  <button
                    onClick={() => runSingle(it)}
                    disabled={running || !it.model}
                    className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
                  >
                    单独测评
                  </button>
                )}
                <ConfirmPopover
                  title="移除该测评项？"
                  confirmLabel="移除"
                  onConfirm={() => removeItem(idx)}
                  triggerClassName={`px-2 py-1 text-xs rounded-md text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10 disabled:opacity-50 ${isBusy ? "pointer-events-none opacity-40" : ""}`}
                >
                  ✕
                </ConfirmPopover>
                {isBusy && (
                  <span className="text-xs text-[var(--color-text-dim)] animate-pulse">⏳ 请求中...</span>
                )}
                {rank > 0 && <span className="ml-auto text-lg font-bold">{medal}</span>}
              </div>
              {r ? (
                r.success ? (
                  <>
                    <div className="grid grid-cols-3 gap-4">
                      <div>
                        <div className="text-xs text-[var(--color-text-dim)] mb-0.5">输出速率</div>
                        <div className="text-xl font-bold text-[var(--color-primary)]">
                          {r.tps.toFixed(1)} <span className="text-xs font-normal text-[var(--color-text-dim)]">tok/s</span>
                        </div>
                      </div>
                      <div>
                        <div className="text-xs text-[var(--color-text-dim)] mb-0.5">总耗时</div>
                        <div className="text-xl font-bold">
                          {(r.durationMs / 1000).toFixed(2)} <span className="text-xs font-normal text-[var(--color-text-dim)]">s</span>
                        </div>
                      </div>
                      <div>
                        <div className="text-xs text-[var(--color-text-dim)] mb-0.5">输出 token</div>
                        <div className="text-xl font-bold">{r.outputTokens}</div>
                      </div>
                    </div>
                    {r.content && (
                      <div className="mt-3 pt-3 border-t border-[var(--color-border)]">
                        <button
                          onClick={() => setExpanded((prev) => ({ ...prev, [k]: !prev[k] }))}
                          className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]"
                        >
                          {expanded[k] ? "▼ 收起返回内容" : "▶ 查看返回内容"}
                        </button>
                        {expanded[k] && (
                          <div className="mt-2 px-3 py-2 rounded-md bg-[var(--color-surface-2)] text-xs whitespace-pre-wrap max-h-60 overflow-y-auto">
                            {r.content}
                          </div>
                        )}
                      </div>
                    )}
                  </>
                ) : r && r.errorMsg === "已停止" ? (
                  // 被停止：保留停止前已流出的文字，末尾标记已停止，不替换为错误行
                  <div className="mt-3 px-3 py-2 rounded-md bg-[var(--color-surface-2)] text-xs whitespace-pre-wrap max-h-60 overflow-y-auto">
                    {streamingContent[k] || ""}
                    {streamingContent[k] ? "\n" : ""}
                    <span className="text-[var(--color-text-dim)]">⏹ 已停止</span>
                  </div>
                ) : (
                  <div className="text-sm text-[var(--color-danger)]">✗ {r.errorMsg || "测评失败"}</div>
                )
              ) : isBusy && streamingContent[k] ? (
                <div className="mt-3 px-3 py-2 rounded-md bg-[var(--color-surface-2)] text-xs whitespace-pre-wrap max-h-60 overflow-y-auto">
                  {streamingContent[k]}<span className="animate-pulse">▋</span>
                </div>
              ) : (
                <div className="text-xs text-[var(--color-text-dim)]">
                  {isBusy ? <span className="animate-pulse">⏳ 等待模型响应...</span> : "未测评"}
                </div>
              )}
            </div>
          );
        })}
      </section>

      <div className="text-xs text-[var(--color-text-dim)] text-center">
        口径：走本代理 /v1/messages 端到端，tps = 输出 token / 总耗时（含 TTFT + 网络 + 代理开销），与首页速率统计一致
      </div>
    </div>
  );
}
