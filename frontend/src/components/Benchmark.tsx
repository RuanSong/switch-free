import { useState, useEffect } from "react";
import { BenchmarkService, ModelService } from "../../bindings/switchfree/service";
import type { BenchmarkResult, ModelDetail } from "../../bindings/switchfree/service/models";
import { useWailsEvent } from "../hooks/useWailsEvent";
import { ModelSelect } from "./ModelSelect";

const DEFAULT_PROMPT = "请详细介绍 Go 语言的 goroutine 和 channel 并发模型，包括基本概念、使用示例和注意事项。";

// 各上游默认测评模型（均为 free 模型，useEffect 加载后会再次按 free 字段校准）
const DEFAULT_TARGETS: Record<string, string> = {
  joycode: "JoyAI-Code-1.5",
  deveco: "glm-5.1",
  opencode: "deepseek-v4-flash-free",
  workbuddy: "wb/hy3",
};

const UPSTREAM_LABEL: Record<string, string> = {
  joycode: "JoyCode",
  deveco: "DevEco",
  opencode: "OpenCode",
  workbuddy: "WorkBuddy",
};

const UPSTREAM_ORDER = ["joycode", "deveco", "opencode", "workbuddy"];

export default function Benchmark() {
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [maxTokens, setMaxTokens] = useState(1024);
  const [models, setModels] = useState<ModelDetail[]>([]);
  const [targets, setTargets] = useState<Record<string, string>>(DEFAULT_TARGETS);
  const [results, setResults] = useState<Record<string, BenchmarkResult | null>>({});
  const [running, setRunning] = useState(false);
  const [singleRunning, setSingleRunning] = useState<Record<string, boolean>>({});
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [streamingContent, setStreamingContent] = useState<Record<string, string>>({});

  useEffect(() => {
    ModelService.GetModels()
      .then((m) => {
        const list = (m ?? []).filter((x): x is ModelDetail => x !== null);
        setModels(list);
        // 默认选每个上游的第一个 free 模型（无 free 则保留 DEFAULT_TARGETS）
        setTargets((prev) => {
          const updated = { ...prev };
          for (const up of UPSTREAM_ORDER) {
            const freeModel = list.find((md) => md.upstream === up && md.free);
            if (freeModel) updated[up] = freeModel.id;
          }
          return updated;
        });
      })
      .catch(() => {});
  }, []);

  // 订阅进度事件：每完成一个上游即时更新
  useWailsEvent("benchmark:progress", (data) => {
    const r = data as BenchmarkResult;
    if (r && r.upstream) {
      setResults((prev) => ({ ...prev, [r.upstream]: r }));
    }
  });

  // 订阅流式 chunk 事件：实时追加 content（流式输出可见）
  useWailsEvent("benchmark:chunk", (data) => {
    const d = data as { upstream: string; delta: string };
    if (d && d.upstream) {
      setStreamingContent((prev) => ({ ...prev, [d.upstream]: (prev[d.upstream] || "") + d.delta }));
    }
  });

  const modelsFor = (up: string) => models.filter((m) => m.upstream === up);

  const run = async () => {
    setRunning(true);
    setResults({});
    setStreamingContent({});
    try {
      const ts = UPSTREAM_ORDER.map((up) => ({ upstream: up, model: targets[up] }));
      await BenchmarkService.RunBenchmark(ts, prompt, maxTokens);
    } finally {
      setRunning(false);
    }
  };

  // 单模型测评：只重测指定上游，其他卡片结果保留
  const runSingle = async (up: string) => {
    setSingleRunning((prev) => ({ ...prev, [up]: true }));
    setResults((prev) => ({ ...prev, [up]: null }));
    setStreamingContent((prev) => ({ ...prev, [up]: "" }));
    try {
      await BenchmarkService.RunBenchmark([{ upstream: up, model: targets[up] }], prompt, maxTokens);
    } finally {
      setSingleRunning((prev) => ({ ...prev, [up]: false }));
    }
  };

  // 成功结果按 tps 降序排名
  const ranked = UPSTREAM_ORDER
    .map((up) => results[up])
    .filter((r): r is BenchmarkResult => !!r && r.success)
    .sort((a, b) => b.tps - a.tps);
  const rankOf = (up: string) => {
    const idx = ranked.findIndex((r) => r.upstream === up);
    return idx >= 0 ? idx + 1 : -1;
  };

  return (
    <div className="p-6 space-y-6">
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
          <button
            onClick={run}
            disabled={running}
            className="ml-auto px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
          >
            {running ? "测评中..." : "🏁 开始测评"}
          </button>
        </div>
      </section>

      {/* 模型选择 + 结果卡片 */}
      <section className="space-y-3">
        {UPSTREAM_ORDER.map((up) => {
          const r = results[up];
          const rank = rankOf(up);
          const upModels = modelsFor(up);
          const medal = rank > 0 ? ["🥇", "🥈", "🥉", "4", "5"][rank - 1] : "";
          return (
            <div key={up} className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
              <div className="flex items-center gap-3 mb-3 flex-wrap">
                <span className="font-semibold">{UPSTREAM_LABEL[up]}</span>
                <ModelSelect
                  options={upModels}
                  value={targets[up]}
                  onChange={(id) => setTargets({ ...targets, [up]: id })}
                  className="w-56"
                />
                <button
                  onClick={() => runSingle(up)}
                  disabled={running || !!singleRunning[up]}
                  className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
                >
                  {singleRunning[up] ? "..." : "单独测评"}
                </button>
                {((running && !r) || singleRunning[up]) && (
                  <span className="text-xs text-[var(--color-text-dim)] animate-pulse">⏳ 请求中...</span>
                )}
                {rank > 0 && (
                  <span className="ml-auto text-lg font-bold">{medal}</span>
                )}
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
                          onClick={() => setExpanded((prev) => ({ ...prev, [up]: !prev[up] }))}
                          className="text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]"
                        >
                          {expanded[up] ? "▼ 收起返回内容" : "▶ 查看返回内容"}
                        </button>
                        {expanded[up] && (
                          <div className="mt-2 px-3 py-2 rounded-md bg-[var(--color-surface-2)] text-xs whitespace-pre-wrap max-h-60 overflow-y-auto">
                            {r.content}
                          </div>
                        )}
                      </div>
                    )}
                  </>
                ) : (
                  <div className="text-sm text-[var(--color-danger)]">✗ {r.errorMsg || "测评失败"}</div>
                )
              ) : (singleRunning[up] || running) && streamingContent[up] ? (
                <div className="mt-3 px-3 py-2 rounded-md bg-[var(--color-surface-2)] text-xs whitespace-pre-wrap max-h-60 overflow-y-auto">
                  {streamingContent[up]}<span className="animate-pulse">▋</span>
                </div>
              ) : (
                <div className="text-xs text-[var(--color-text-dim)]">
                  {singleRunning[up] || running ? (
                    <span className="animate-pulse">
                      ⏳ 等待模型响应{up === "opencode" ? "（free 模型排队中，首字节可能需数十秒）" : "..."}
                    </span>
                  ) : (
                    "未测评"
                  )}
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
