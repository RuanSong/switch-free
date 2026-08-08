import { useEffect, useState } from "react";
import { LogService } from "../../bindings/switchfree/service";
import type { UsageTrend as TrendData } from "../../bindings/switchfree/service/models";

interface Props {
  startDate: string;
  endDate: string;
  granularity: "hour" | "day"; // 按天看->hour，按周/月看->day
}

// 纯 CSS 条形图（无图表库依赖）+ 缓存命中率
export default function UsageTrendChart({ startDate, endDate, granularity }: Props) {
  const [data, setData] = useState<TrendData | null>(null);

  useEffect(() => {
    let cancelled = false;
    LogService.GetUsageTrend(startDate, endDate, granularity)
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [startDate, endDate, granularity]);

  if (!data || data.points.length === 0) return null;

  const points = data.points;
  const maxTokens = Math.max(...points.map((p) => p.tokens), 1);
  const peak = Math.max(...points.map((p) => p.tokens), 0);

  // 汇总缓存命中
  const totalTokens = points.reduce((s, p) => s + p.tokens, 0);
  const totalReqs = points.reduce((s, p) => s + p.reqs, 0);
  const totalCacheTokens = points.reduce((s, p) => s + p.cacheHitTokens, 0);
  const totalCacheReqs = points.reduce((s, p) => s + p.cacheHitReqs, 0);
  const reqHitRate = totalReqs > 0 ? (totalCacheReqs / totalReqs) * 100 : 0;
  const tokenHitRate = totalTokens > 0 ? (totalCacheTokens / totalTokens) * 100 : 0;

  return (
    <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
      <div className="flex items-center justify-between mb-3 flex-wrap gap-2">
        <h2 className="font-semibold">使用趋势</h2>
        <div className="flex items-center gap-3 text-xs text-[var(--color-text-dim)] flex-wrap">
          <span>{granularity === "hour" ? "按小时" : "按天"} · 峰值 {fmtTokens(peak)} tokens</span>
          {/* 缓存命中汇总 */}
          <span className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-warning)]" />
            缓存命中 {fmtTokens(totalCacheTokens)} ({tokenHitRate.toFixed(1)}%)
          </span>
          <span>
            请求命中 {totalCacheReqs}/{totalReqs} ({reqHitRate.toFixed(1)}%)
          </span>
        </div>
      </div>

      {/* 条形图（总 token 柱 + 命中缓存柱叠加） */}
      <div className="flex items-end gap-[2px] h-32">
        {points.map((p, i) => {
          const totalH = p.tokens > 0 ? Math.max((p.tokens / maxTokens) * 100, 2) : 0;
          const cacheH = p.cacheHitTokens > 0 ? (p.cacheHitTokens / maxTokens) * 100 : 0;
          const hitRate = p.tokens > 0 ? (p.cacheHitTokens / p.tokens) * 100 : 0;
          return (
            <div
              key={i}
              className="flex-1 min-w-0 flex flex-col justify-end items-center group relative"
              title=""
            >
              {/* tooltip */}
              <div className="hidden group-hover:block absolute -top-12 left-1/2 -translate-x-1/2 bg-[var(--color-surface-2)] text-xs px-2 py-1 rounded whitespace-nowrap z-10">
                {p.label}
                <br />
                {fmtTokens(p.tokens)} tokens · {p.reqs} 请求
                <br />
                缓存命中 {fmtTokens(p.cacheHitTokens)} ({hitRate.toFixed(1)}%) · {p.cacheHitReqs} 请求
              </div>
              {/* 总 token 柱（底部） */}
              <div
                className={`w-full rounded-t ${p.tokens > 0 ? "bg-[var(--color-primary)]" : "bg-[var(--color-surface-2)]/30"}`}
                style={{ height: `${totalH}%` }}
              />
              {/* 命中缓存柱（叠加在上层） */}
              {p.cacheHitTokens > 0 && (
                <div
                  className="absolute bottom-0 w-full bg-[var(--color-warning)]/80"
                  style={{ height: `${cacheH}%` }}
                  title={`缓存命中 ${fmtTokens(p.cacheHitTokens)}`}
                />
              )}
            </div>
          );
        })}
      </div>

      {/* 图例 */}
      <div className="flex items-center gap-4 text-xs text-[var(--color-text-dim)] mt-2">
        <span className="flex items-center gap-1">
          <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-primary)]" />总 token
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-warning)]" />缓存命中
        </span>
      </div>

      {/* 时间轴刻度：首/中/尾 3 个标签 */}
      <div className="flex justify-between text-xs text-[var(--color-text-dim)] mt-1.5">
        <span>{points[0]?.label}</span>
        <span>{points[Math.floor(points.length / 2)]?.label}</span>
        <span>{points[points.length - 1]?.label}</span>
      </div>
    </section>
  );
}

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
