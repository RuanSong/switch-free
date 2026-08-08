import { useEffect, useState } from "react";
import { ModelService } from "../../bindings/switchfree/service";
import type { ModelDetail } from "../../bindings/switchfree/service/models";
import type { Config } from "../../bindings/switchfree/config/models";

const UPSTREAM_LABEL: Record<string, string> = {
  joycode: "JoyCode",
  deveco: "DevEco",
  opencode: "OpenCode",
};

const UPSTREAM_COLOR: Record<string, string> = {
  joycode: "bg-[var(--color-success)]/20 text-[var(--color-success)]",
  deveco: "bg-[var(--color-danger)]/20 text-[var(--color-danger)]",
  opencode: "bg-[var(--color-primary)]/20 text-[var(--color-primary)]",
};

export default function Models({ config }: { config: Config | null }) {
  const [models, setModels] = useState<ModelDetail[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    ModelService.GetModels()
      .then((m) => setModels((m ?? []).filter((x): x is ModelDetail => x !== null)))
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="p-6 text-[var(--color-text-dim)]">加载中...</div>;

  // 计算当前配置选中的模型集合（auto 链 + 手动降级 + 兜底）
  const selectedSet = new Set<string>();
  if (config) {
    const addRef = (u: string, m: string) => selectedSet.add(`${u}/${m}`);
    (config.autoChain ?? []).forEach((c) => c.models.forEach((m) => addRef(c.upstream, m)));
    Object.entries(config.manualFallbacks ?? {}).forEach(([key, chain]) => {
      // 手动模式的 key 是模型 id，upstream 需推断
      selectedSet.add(key); // 标记模型本身
      (chain ?? []).forEach((r) => addRef(r.upstream, r.model));
    });
    if (config.globalFallback) addRef(config.globalFallback.upstream, config.globalFallback.model);
  }

  // 排序：选中的排前面
  const sorted = [...models].sort((a, b) => {
    const aSel = isSelected(selectedSet, a) ? 0 : 1;
    const bSel = isSelected(selectedSet, b) ? 0 : 1;
    return aSel - bSel;
  });

  return (
    <div className="p-6 space-y-6">

      {/* 当前配置概览 */}
      {config && (
        <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
          <h2 className="text-sm font-semibold text-[var(--color-text-dim)] uppercase mb-3">
            当前配置（{config.mode === "manual" ? "手动模式" : "auto 模式"}）
          </h2>
          {config.mode === "auto" ? (
            <div className="space-y-2">
              <div className="text-xs text-[var(--color-text-dim)]">auto 优先级链（按序尝试）：</div>
              <div className="flex items-center gap-2 flex-wrap">
                {(config.autoChain ?? []).flatMap((c, ci) =>
                  c.models.map((m, mi) => (
                    <span key={`${ci}-${mi}`} className="flex items-center gap-2">
                      <span className="font-mono text-xs px-2 py-1 rounded bg-[var(--color-surface-2)]">
                        {UPSTREAM_LABEL[c.upstream] ?? c.upstream}/{m}
                      </span>
                      <span className="text-[var(--color-text-dim)] text-xs">→</span>
                    </span>
                  ))
                )}
                <span className="font-mono text-xs px-2 py-1 rounded bg-[var(--color-warning)]/20 text-[var(--color-warning)]">
                  兜底：{UPSTREAM_LABEL[config.globalFallback?.upstream] ?? "?"}/{config.globalFallback?.model}
                </span>
              </div>
            </div>
          ) : (
            <div className="space-y-2">
              <div className="text-xs text-[var(--color-text-dim)]">手动模式：客户端指定模型，失败按降级链走</div>
              {Object.keys(config.manualFallbacks ?? {}).length === 0 ? (
                <div className="text-sm text-[var(--color-text-dim)]">未配置降级链（指定模型失败直接走全局兜底）</div>
              ) : (
                Object.entries(config.manualFallbacks ?? {}).map(([key, chain]) => (
                  <div key={key} className="flex items-center gap-2 flex-wrap text-xs">
                    <span className="font-mono px-2 py-1 rounded bg-[var(--color-primary)]/20 text-[var(--color-primary)]">
                      {key}
                    </span>
                    <span className="text-[var(--color-text-dim)]">→ 失败降级 →</span>
                    {(chain ?? []).map((r, i) => (
                      <span key={i} className="flex items-center gap-2">
                        <span className="font-mono px-2 py-1 rounded bg-[var(--color-surface-2)]">
                          {UPSTREAM_LABEL[r.upstream] ?? r.upstream}/{r.model}
                        </span>
                        {i < (chain?.length ?? 0) - 1 && <span className="text-[var(--color-text-dim)]">→</span>}
                      </span>
                    ))}
                  </div>
                ))
              )}
              <div className="flex items-center gap-2 text-xs pt-2 border-t border-[var(--color-border)] mt-2">
                <span className="text-[var(--color-text-dim)]">全局兜底：</span>
                <span className="font-mono px-2 py-1 rounded bg-[var(--color-warning)]/20 text-[var(--color-warning)]">
                  {UPSTREAM_LABEL[config.globalFallback?.upstream] ?? "?"}/{config.globalFallback?.model}
                </span>
              </div>
            </div>
          )}
        </section>
      )}

      {/* 模型表格 */}
      <section className="bg-[var(--color-surface)] rounded-xl border border-[var(--color-border)] overflow-x-auto">
        <table className="w-full text-sm min-w-[720px]">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-[var(--color-text-dim)]">
              <th className="text-left px-4 py-3 font-medium w-8 whitespace-nowrap"></th>
              <th className="text-left px-4 py-3 font-medium whitespace-nowrap">模型 ID</th>
              <th className="text-left px-4 py-3 font-medium whitespace-nowrap">显示名</th>
              <th className="text-left px-4 py-3 font-medium whitespace-nowrap">上游</th>
              <th className="text-left px-4 py-3 font-medium whitespace-nowrap">流式</th>
              <th className="text-right px-4 py-3 font-medium whitespace-nowrap">Context</th>
              <th className="text-right px-4 py-3 font-medium whitespace-nowrap">Output</th>
              <th className="text-left px-4 py-3 font-medium whitespace-nowrap">能力</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((m) => {
              const sel = isSelected(selectedSet, m);
              return (
                <tr key={m.id} className={`border-b border-[var(--color-border)] last:border-0 ${sel ? "bg-[var(--color-primary)]/5" : ""}`}>
                  <td className="px-4 py-3">
                    {sel && <span className="text-[var(--color-primary)]" title="当前配置选中">★</span>}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs">{m.id}</td>
                  <td className="px-4 py-3">{m.label}</td>
                  <td className="px-4 py-3">
                    <span
                      className={`text-xs px-2 py-0.5 rounded-full ${
                        UPSTREAM_COLOR[m.upstream] ?? "bg-[var(--color-surface-2)]"
                      }`}
                    >
                      {UPSTREAM_LABEL[m.upstream] ?? m.upstream}
                    </span>
                  </td>
                  <td className="px-4 py-3">{m.stream ? "✓" : "✗"}</td>
                  <td className="px-4 py-3 text-right font-mono text-xs">
                    {m.context > 0 ? (m.context / 1000).toFixed(0) + "k" : "-"}
                  </td>
                  <td className="px-4 py-3 text-right font-mono text-xs">
                    {m.output > 0 ? (m.output / 1000).toFixed(0) + "k" : "-"}
                  </td>
                  <td className="px-4 py-3 text-xs space-x-1">
                    {m.toolCall && (
                      <span className="px-1.5 py-0.5 rounded bg-[var(--color-surface-2)]">tool</span>
                    )}
                    {m.vision && (
                      <span className="px-1.5 py-0.5 rounded bg-[var(--color-surface-2)]">vision</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </section>
    </div>
  );
}

// isSelected 判断模型是否在当前配置选中集合里
function isSelected(set: Set<string>, m: ModelDetail): boolean {
  return set.has(`${m.upstream}/${m.id}`) || set.has(m.id);
}
