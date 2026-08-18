import { useEffect, useState } from "react";
import { PricingService } from "../../bindings/switchdev/service";
import type { PricingItem } from "../../bindings/switchdev/service/models";

export default function PricingEditor() {
  const [items, setItems] = useState<PricingItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<PricingItem | null>(null);
  const [syncing, setSyncing] = useState(false);
  const [msg, setMsg] = useState<{ type: "ok" | "err"; text: string } | null>(null);
  const [confirmSync, setConfirmSync] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const list = await PricingService.ListPrices();
      setItems((list ?? []).filter((x): x is PricingItem => x !== null));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const sync = async () => {
    setSyncing(true);
    setMsg(null);
    setConfirmSync(false);
    try {
      const n = await PricingService.SyncFromGitHub();
      setMsg({ type: "ok", text: `已从 GitHub 同步 ${n} 条费率` });
      await load();
    } catch (e) {
      setMsg({ type: "err", text: `同步失败: ${e}` });
    } finally {
      setSyncing(false);
      setTimeout(() => setMsg(null), 5000);
    }
  };

  const filtered = filter
    ? items.filter(
        (i) => i.modelId.toLowerCase().includes(filter.toLowerCase()) || (i.displayName || "").toLowerCase().includes(filter.toLowerCase())
      )
    : items;

  const saveItem = async (item: PricingItem) => {
    await PricingService.SetPrice(item);
    setEditing(null);
    await load();
  };

  const removeItem = async (id: string) => {
    if (!confirm(`删除费率 ${id}？`)) return;
    await PricingService.DeletePrice(id);
    await load();
  };

  return (
    <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
      <div className="flex items-center justify-between mb-1">
        <h2 className="font-semibold">费率管理</h2>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setConfirmSync(true)}
            disabled={syncing}
            title="从 GitHub 拉取最新默认费率覆盖本地"
            className="px-3 py-1 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
          >
            {syncing ? "同步中..." : "🔄 同步"}
          </button>
          <button
            onClick={() => setEditing({ modelId: "", displayName: "", inputPerMillion: 0, outputPerMillion: 0, cacheRead: 0, cacheCreation: 0 } as PricingItem)}
            className="px-3 py-1 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90"
          >
            + 新增
          </button>
        </div>
      </div>
      <p className="text-xs text-[var(--color-text-dim)] mb-3">
        共 {items.length} 条费率（每百万 token 成本，美元）。日志费用按此表计算。
      </p>
      {msg && (
        <div className={`mb-3 px-3 py-1.5 rounded-lg text-xs ${msg.type === "ok" ? "bg-[var(--color-success)]/20 text-[var(--color-success)]" : "bg-[var(--color-danger)]/20 text-[var(--color-danger)]"}`}>
          {msg.text}
        </div>
      )}

      {/* 搜索 */}
      <input
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="搜索模型..."
        className="w-full px-3 py-1.5 text-sm rounded-lg bg-[var(--color-bg)] border border-[var(--color-border)] mb-3"
      />

      {/* 编辑表单 */}
      {editing && (
        <PriceForm
          initial={editing}
          onSave={saveItem}
          onCancel={() => setEditing(null)}
        />
      )}

      {/* 费率表格 */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm min-w-[600px]">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-[var(--color-text-dim)]">
              <th className="text-left px-3 py-2 font-medium">模型 ID</th>
              <th className="text-left px-3 py-2 font-medium">显示名</th>
              <th className="text-right px-3 py-2 font-medium">输入($/M)</th>
              <th className="text-right px-3 py-2 font-medium">输出($/M)</th>
              <th className="text-right px-3 py-2 font-medium">缓存读</th>
              <th className="text-right px-3 py-2 font-medium">缓存写</th>
              <th className="text-center px-3 py-2 font-medium w-20">操作</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((item) => (
              <tr key={item.modelId} className="border-b border-[var(--color-border)] last:border-0">
                <td className="px-3 py-2 font-mono text-xs">{item.modelId}</td>
                <td className="px-3 py-2 text-xs">{item.displayName}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{fmtRate(item.inputPerMillion)}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{fmtRate(item.outputPerMillion)}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{fmtRate(item.cacheRead || 0)}</td>
                <td className="px-3 py-2 text-right font-mono text-xs">{fmtRate(item.cacheCreation || 0)}</td>
                <td className="px-3 py-2 text-center space-x-1">
                  <button
                    onClick={() => setEditing(item)}
                    className="text-xs px-2 py-0.5 rounded bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
                  >
                    编辑
                  </button>
                  <button
                    onClick={() => removeItem(item.modelId)}
                    className="text-xs px-2 py-0.5 rounded bg-[var(--color-danger)]/20 text-[var(--color-danger)] hover:opacity-80"
                  >
                    删除
                  </button>
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={7} className="px-3 py-6 text-center text-[var(--color-text-dim)] text-sm">
                  {loading ? "加载中..." : "暂无费率"}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 同步确认弹窗 */}
      {confirmSync && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setConfirmSync(false)}>
          <div className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)] shadow-xl max-w-sm w-full mx-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="font-semibold mb-2">同步费率</h3>
            <p className="text-sm text-[var(--color-text-dim)] mb-4">
              从 GitHub 拉取最新默认费率将覆盖本地所有自定义修改，确认继续？
            </p>
            <div className="flex gap-2 justify-end">
              <button
                onClick={() => setConfirmSync(false)}
                disabled={syncing}
                className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
              >
                取消
              </button>
              <button
                onClick={sync}
                disabled={syncing}
                className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
              >
                {syncing ? "同步中..." : "确认覆盖同步"}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function PriceForm({
  initial,
  onSave,
  onCancel,
}: {
  initial: PricingItem;
  onSave: (item: PricingItem) => void;
  onCancel: () => void;
}) {
  const [item, setItem] = useState<PricingItem>({ ...initial });
  const [busy, setBusy] = useState(false);

  const num = (v: string) => {
    const n = parseFloat(v);
    return isNaN(n) ? 0 : n;
  };

  const save = async () => {
    if (!item.modelId.trim()) return;
    setBusy(true);
    try {
      await onSave(item);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mb-4 p-4 rounded-lg bg-[var(--color-bg)] border border-[var(--color-primary)]/40">
      <div className="text-xs text-[var(--color-text-dim)] mb-2">编辑费率</div>
      <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
        <Field label="模型 ID" value={item.modelId} onChange={(v) => setItem({ ...item, modelId: v })} placeholder="glm-5.1" />
        <Field label="显示名" value={item.displayName} onChange={(v) => setItem({ ...item, displayName: v })} placeholder="GLM-5.1" />
        <NumField label="输入 $/M" value={item.inputPerMillion} onChange={(v) => setItem({ ...item, inputPerMillion: num(v) })} />
        <NumField label="输出 $/M" value={item.outputPerMillion} onChange={(v) => setItem({ ...item, outputPerMillion: num(v) })} />
        <NumField label="缓存读 $/M" value={item.cacheRead} onChange={(v) => setItem({ ...item, cacheRead: num(v) })} />
        <NumField label="缓存写 $/M" value={item.cacheCreation} onChange={(v) => setItem({ ...item, cacheCreation: num(v) })} />
      </div>
      <div className="flex gap-2 mt-3">
        <button
          onClick={save}
          disabled={busy || !item.modelId.trim()}
          className="px-3 py-1 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
        >
          {busy ? "保存中..." : "保存"}
        </button>
        <button onClick={onCancel} className="px-3 py-1 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]">
          取消
        </button>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <div>
      <div className="text-xs text-[var(--color-text-dim)] mb-0.5">{label}</div>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full px-2 py-1 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
      />
    </div>
  );
}

function NumField({ label, value, onChange }: { label: string; value: number; onChange: (v: string) => void }) {
  return (
    <div>
      <div className="text-xs text-[var(--color-text-dim)] mb-0.5">{label}</div>
      <input
        type="number"
        step="0.00001"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full px-2 py-1 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
      />
    </div>
  );
}

// fmtRate 费率显示：保留 5 位小数，去尾零（1.4 -> "1.4"）
function fmtRate(n: number): string {
  if (!isFinite(n)) return "0";
  return parseFloat(n.toFixed(5)).toString();
}
