import { useEffect, useRef, useState } from "react";

// ModelSelectOption 模型下拉选项的最小接口（ModelOption/ModelDetail 均满足）
export interface ModelSelectOption {
  id: string;
  label: string;
  free?: boolean;
}

// ModelSelect 自定义模型下拉（支持 FREE 上标徽章）
export function ModelSelect({
  options,
  value,
  onChange,
  placeholder,
  className,
  disabledIds,
  hideFreeBadge,
}: {
  options: ModelSelectOption[];
  value: string;
  onChange: (id: string) => void;
  placeholder?: string;
  className?: string;
  disabledIds?: Set<string>;
  hideFreeBadge?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // 点击外部关闭
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const selected = options.find((m) => m.id === value);

  return (
    <div ref={ref} className={`relative ${className ?? ""}`}>
      {/* 触发器 */}
      <button
        type="button"
        onClick={() => setOpen(!open)}
        title={selected?.label}
        className="w-full px-2 py-1 text-xs rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] text-left flex items-center gap-1"
      >
        {selected ? (
          <>
            <span className="truncate flex-1">{selected.label}</span>
            {selected.free && !hideFreeBadge && <FreeBadge />}
          </>
        ) : (
          <span className="text-[var(--color-text-dim)]">{placeholder ?? "选择模型..."}</span>
        )}
        <span className="text-[var(--color-text-dim)] ml-auto text-[10px]">{open ? "▲" : "▼"}</span>
      </button>
      {/* 下拉列表 */}
      {open && (
        <div className="absolute z-50 mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] rounded-md shadow-lg max-h-60 overflow-y-auto">
          {options.length === 0 && (
            <div className="px-2 py-1.5 text-xs text-[var(--color-text-dim)]">无模型</div>
          )}
          {options.map((m) => {
            const disabled = m.id !== value && (disabledIds?.has(m.id) ?? false);
            return (
              <button
                key={m.id}
                type="button"
                disabled={disabled}
                onClick={() => { onChange(m.id); setOpen(false); }}
                className={`w-full px-2 py-1.5 text-xs text-left flex items-center gap-1 ${
                  disabled
                    ? "opacity-40 cursor-not-allowed"
                    : "hover:bg-[var(--color-surface-2)]"
                } ${m.id === value ? "bg-[var(--color-primary)]/10" : ""}`}
              >
                <span className="truncate flex-1">{m.label}</span>
                {m.free && !hideFreeBadge && <FreeBadge />}
                {disabled && <span className="text-[9px] text-[var(--color-text-dim)]">已添加</span>}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

// FreeBadge 限时免费上标文案（绿色，与模型名区分）
export function FreeBadge() {
  return (
    <sup className="text-[10px] font-semibold text-[var(--color-success)] ml-0.5 align-super shrink-0">
      free
    </sup>
  );
}
