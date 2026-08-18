import { useEffect, useState } from "react";
import { LogService } from "../../bindings/switchdev/service";
import type { UsageTrend as TrendData } from "../../bindings/switchdev/service/models";

interface Props {
  startDate: string;
  endDate: string;
  granularity: "hour" | "day"; // 按天看->hour，按周/月看->day
}

// 纯 CSS 条形图（无图表库依赖）：输入 token + 输出 token 分两个图，缓存命中叠加在输入图
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
  const inputMax = Math.max(...points.map((p) => p.inputTokens), 1);
  const outputMax = Math.max(...points.map((p) => p.outputTokens), 1);
  const peakIn = Math.max(...points.map((p) => p.inputTokens), 0);
  const peakOut = Math.max(...points.map((p) => p.outputTokens), 0);

  // 汇总
  const totalInput = points.reduce((s, p) => s + p.inputTokens, 0);
  const totalOutput = points.reduce((s, p) => s + p.outputTokens, 0);
  const totalReqs = points.reduce((s, p) => s + p.reqs, 0);
  const totalCacheTokens = points.reduce((s, p) => s + p.cacheHitTokens, 0);
  const totalCacheReqs = points.reduce((s, p) => s + p.cacheHitReqs, 0);
  const tokenHitRate = totalInput > 0 ? (totalCacheTokens / totalInput) * 100 : 0;
  const reqHitRate = totalReqs > 0 ? (totalCacheReqs / totalReqs) * 100 : 0;

  return (
    <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
      <div className="flex items-center justify-between mb-3 flex-wrap gap-2">
        <h2 className="font-semibold">使用趋势</h2>
        <div className="flex items-center gap-3 text-xs text-[var(--color-text-dim)] flex-wrap">
          <span>{granularity === "hour" ? "按小时" : "按天"}</span>
          <span>输入峰值 {fmtTokens(peakIn)}</span>
          <span>输出峰值 {fmtTokens(peakOut)}</span>
          <span className="flex items-center gap-1">
            <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-warning)]" />
            缓存命中 {fmtTokens(totalCacheTokens)} ({tokenHitRate.toFixed(1)}%)
          </span>
          <span>
            请求命中 {totalCacheReqs}/{totalReqs} ({reqHitRate.toFixed(1)}%)
          </span>
        </div>
      </div>

      {/* 输入 token 图 */}
      <div className="text-xs text-[var(--color-text-dim)] mb-1">输入 token（共 {fmtTokens(totalInput)}）</div>
      <div className="flex items-stretch gap-[2px] h-24">
        {points.map((p, i) => {
          const inH = p.inputTokens > 0 ? Math.max((p.inputTokens / inputMax) * 100, 2) : 0;
          const cacheH = p.cacheHitTokens > 0 ? (p.cacheHitTokens / inputMax) * 100 : 0;
          const hitRate = p.inputTokens > 0 ? (p.cacheHitTokens / p.inputTokens) * 100 : 0;
          return (
            <div key={i} className="flex-1 min-w-0 flex flex-col justify-end items-center group relative">
              <div className="hidden group-hover:block absolute -top-14 left-1/2 -translate-x-1/2 bg-[var(--color-surface-2)] text-xs px-2 py-1 rounded whitespace-nowrap z-10">
                {p.label}
                <br />
                输入 {fmtTokens(p.inputTokens)} · 输出 {fmtTokens(p.outputTokens)} · {p.reqs} 请求
                <br />
                缓存命中 {fmtTokens(p.cacheHitTokens)} ({hitRate.toFixed(1)}%) · {p.cacheHitReqs} 请求
              </div>
              <div
                className={`w-full rounded-t ${p.inputTokens > 0 ? "bg-[var(--color-primary)]" : "bg-[var(--color-surface-2)]/30"}`}
                style={{ height: `${inH}%` }}
              />
              {p.cacheHitTokens > 0 && (
                <div
                  className="absolute bottom-0 w-full bg-[var(--color-warning)]/80"
                  style={{ height: `${cacheH}%` }}
                />
              )}
            </div>
          );
        })}
      </div>

      {/* 输出 token 图 */}
      <div className="text-xs text-[var(--color-text-dim)] mb-1 mt-3">输出 token（共 {fmtTokens(totalOutput)}）</div>
      <div className="flex items-stretch gap-[2px] h-24">
        {points.map((p, i) => {
          const outH = p.outputTokens > 0 ? Math.max((p.outputTokens / outputMax) * 100, 2) : 0;
          return (
            <div key={i} className="flex-1 min-w-0 flex flex-col justify-end items-center group relative">
              <div className="hidden group-hover:block absolute -top-10 left-1/2 -translate-x-1/2 bg-[var(--color-surface-2)] text-xs px-2 py-1 rounded whitespace-nowrap z-10">
                {p.label}
                <br />
                输出 {fmtTokens(p.outputTokens)} · {p.reqs} 请求
              </div>
              <div
                className={`w-full rounded-t ${p.outputTokens > 0 ? "bg-[var(--color-success)]" : "bg-[var(--color-surface-2)]/30"}`}
                style={{ height: `${outH}%` }}
              />
            </div>
          );
        })}
      </div>

      {/* 时间轴刻度：首/中/尾 3 个标签 */}
      <div className="flex justify-between text-xs text-[var(--color-text-dim)] mt-1.5">
        <span>{points[0]?.label}</span>
        <span>{points[Math.floor(points.length / 2)]?.label}</span>
        <span>{points[points.length - 1]?.label}</span>
      </div>

      {/* 图例 */}
      <div className="flex items-center gap-4 text-xs text-[var(--color-text-dim)] mt-2">
        <span className="flex items-center gap-1">
          <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-primary)]" />输入 token
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-success)]" />输出 token
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block w-2 h-2 rounded-sm bg-[var(--color-warning)]" />缓存命中
        </span>
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
