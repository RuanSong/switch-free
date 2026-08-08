import { useEffect, useState, useCallback } from "react";
import { LogService } from "../../bindings/switchfree/service";
import type { UsageStats as UsageStatsData } from "../../bindings/switchfree/service/models";

type Range = "today" | "week" | "month" | "custom";

const pad = (n: number) => String(n).padStart(2, "0");
function fmtDate(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
function dateAddDays(d: Date, days: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + days);
}
function startOfWeek(d: Date): Date {
  const day = d.getDay() || 7;
  return dateAddDays(d, 1 - day);
}

export default function UsageStats() {
  const [range, setRange] = useState<Range>("week");
  const [customStart, setCustomStart] = useState("");
  const [customEnd, setCustomEnd] = useState("");
  const [data, setData] = useState<UsageStatsData | null>(null);
  const [loading, setLoading] = useState(false);

  // 计算当前范围的日期区间
  const calcRange = useCallback((): { start: string; end: string } => {
    const today = new Date();
    const todayStr = fmtDate(today);
    if (range === "today") return { start: todayStr, end: todayStr };
    if (range === "week") {
      return { start: fmtDate(startOfWeek(today)), end: todayStr };
    }
    if (range === "month") {
      return { start: fmtDate(new Date(today.getFullYear(), today.getMonth(), 1)), end: todayStr };
    }
    // custom
    return { start: customStart || todayStr, end: customEnd || todayStr };
  }, [range, customStart, customEnd]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const { start, end } = calcRange();
      const res = await LogService.GetUsageStats(start, end);
      setData(res);
    } catch (e) {
      console.error("统计失败", e);
    } finally {
      setLoading(false);
    }
  }, [calcRange]);

  useEffect(() => {
    load();
  }, [load]);

  // 最大 token 数（条形图比例用）
  const maxAgentTokens = Math.max(...(data?.byAgent ?? []).map((a) => a.tokens), 1);
  const maxModelTokens = Math.max(...(data?.byModel ?? []).map((m) => m.tokens), 1);

  return (
    <div className="p-6 space-y-6">
      {/* 时间范围切换 */}
      <div className="flex items-center gap-2 flex-wrap">
        <RangeBtn label="今天" active={range === "today"} onClick={() => setRange("today")} />
        <RangeBtn label="本周" active={range === "week"} onClick={() => setRange("week")} />
        <RangeBtn label="本月" active={range === "month"} onClick={() => setRange("month")} />
        <RangeBtn label="自定义" active={range === "custom"} onClick={() => setRange("custom")} />
        {range === "custom" && (
          <div className="flex items-center gap-2">
            <input type="date" value={customStart} onChange={(e) => setCustomStart(e.target.value)} className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]" />
            <span className="text-[var(--color-text-dim)]">至</span>
            <input type="date" value={customEnd} onChange={(e) => setCustomEnd(e.target.value)} className="px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]" />
          </div>
        )}
        <span className="text-xs text-[var(--color-text-dim)] ml-2">
          范围：{calcRange().start} ~ {calcRange().end}
        </span>
      </div>

      {loading && !data ? (
        <div className="p-8 text-center text-[var(--color-text-dim)]">统计中...</div>
      ) : !data ? (
        <div className="p-8 text-center text-[var(--color-text-dim)]">暂无数据</div>
      ) : (
        <>
          {/* 汇总卡片 */}
          <div className="grid grid-cols-4 gap-4">
            <SumCard label="总 Token" value={fmtTokens(data.totalTokens)} sub={`输入 ${fmtTokens(data.totalInput)} / 输出 ${fmtTokens(data.totalOutput)}`} color="text-[var(--color-text)]" />
            <SumCard label="总请求" value={String(data.totalReqs)} sub={`成功 ${data.successReqs}`} color="text-[var(--color-primary)]" />
            <SumCard label="总费用" value={`$${data.totalCost.toFixed(5)}`} sub="按费率表计算" color="text-[var(--color-success)]" />
            <SumCard label="Agent 数" value={String(data.byAgent.length)} sub={`模型 ${data.byModel.length} 个`} color="text-[var(--color-warning)]" />
          </div>

          {/* Agent 维度 */}
          <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
            <h2 className="font-semibold mb-3">按 Agent 统计</h2>
            {data.byAgent.length === 0 ? (
              <div className="text-sm text-[var(--color-text-dim)] text-center py-4">该范围无数据</div>
            ) : (
              <div className="space-y-3">
                {data.byAgent.map((a) => (
                  <div key={a.agent}>
                    <div className="flex items-center justify-between mb-1 text-sm">
                      <span className="font-medium">{a.agentLabel}</span>
                      <span className="text-[var(--color-text-dim)] text-xs">
                        {fmtTokens(a.tokens)} tokens · {a.requests} 请求 · ${a.cost.toFixed(5)}
                      </span>
                    </div>
                    <div className="h-2 rounded bg-[var(--color-bg)] overflow-hidden">
                      <div
                        className="h-full rounded bg-[var(--color-primary)]"
                        style={{ width: `${(a.tokens / maxAgentTokens) * 100}%` }}
                      />
                    </div>
                    <div className="text-xs text-[var(--color-text-dim)] mt-0.5">
                      输入 {fmtTokens(a.input)} · 输出 {fmtTokens(a.output)}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>

          {/* 模型维度 */}
          <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
            <h2 className="font-semibold mb-3">按模型统计（实际使用）</h2>
            {data.byModel.length === 0 ? (
              <div className="text-sm text-[var(--color-text-dim)] text-center py-4">该范围无数据</div>
            ) : (
              <div className="space-y-3">
                {data.byModel.map((m) => (
                  <div key={m.model}>
                    <div className="flex items-center justify-between mb-1 text-sm">
                      <span className="font-mono">{m.model}</span>
                      <span className="text-[var(--color-text-dim)] text-xs">
                        {fmtTokens(m.tokens)} tokens · {m.requests} 请求 · {m.percent.toFixed(1)}% · ${m.cost.toFixed(5)}
                      </span>
                    </div>
                    <div className="h-2 rounded bg-[var(--color-bg)] overflow-hidden">
                      <div
                        className="h-full rounded bg-[var(--color-primary)]"
                        style={{ width: `${(m.tokens / maxModelTokens) * 100}%` }}
                      />
                    </div>
                    <div className="text-xs text-[var(--color-text-dim)] mt-0.5">
                      输入 {fmtTokens(m.input)} · 输出 {fmtTokens(m.output)} · 占比 {m.percent.toFixed(1)}%
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>
        </>
      )}
    </div>
  );
}

function RangeBtn({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1.5 text-sm rounded-lg transition-colors ${
        active ? "bg-[var(--color-primary)] text-white" : "bg-[var(--color-surface)] hover:bg-[var(--color-surface-2)]"
      }`}
    >
      {label}
    </button>
  );
}

function SumCard({ label, value, sub, color }: { label: string; value: string; sub: string; color: string }) {
  return (
    <div className="bg-[var(--color-surface)] rounded-xl p-4 border border-[var(--color-border)]">
      <div className="text-xs text-[var(--color-text-dim)] mb-1">{label}</div>
      <div className={`text-xl font-bold ${color}`}>{value}</div>
      <div className="text-xs text-[var(--color-text-dim)] mt-0.5">{sub}</div>
    </div>
  );
}

// fmtTokens token 数格式化（千分位）
function fmtTokens(n: number): string {
  if (n === 0) return "0";
  const s = String(n);
  let out = "";
  for (let i = s.length; i > 0; i -= 3) {
    if (out) out = "," + out;
    out = s.slice(Math.max(0, i - 3), i) + out;
  }
  return out;
}
