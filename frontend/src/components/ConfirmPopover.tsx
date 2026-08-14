import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

// ConfirmPopover 点击触发按钮后在按钮附近弹出一个小气泡二次确认。
//
// 用 createPortal 挂到 body：方案下拉容器有 overflow-y-auto，
// 内部绝对定位会被裁掉，portal 能绕开裁剪和所有 z-index 堆叠上下文。
export default function ConfirmPopover({
  onConfirm,
  title = "确认删除？",
  confirmLabel = "删除",
  triggerClassName,
  children,
}: {
  onConfirm: () => void;
  title?: string;
  confirmLabel?: string;
  triggerClassName?: string;
  /** 触发按钮内容；不传则显示默认的 ✕ */
  children?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const btnRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);

  // 打开时计算触发按钮的位置；滚动/缩放时重算
  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const compute = () => {
      const r = btnRef.current?.getBoundingClientRect();
      if (!r) return;
      // 气泡底沿对齐按钮顶沿，右侧对齐按钮右侧
      setPos({ top: r.top - 8, left: r.right });
    };
    compute();
    window.addEventListener("scroll", compute, true);
    window.addEventListener("resize", compute);
    return () => {
      window.removeEventListener("scroll", compute, true);
      window.removeEventListener("resize", compute);
    };
  }, [open]);

  // 点外部 / Esc 关闭
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (btnRef.current?.contains(t) || popRef.current?.contains(t)) return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // 4 秒后自动收起，避免一直挂着
  useEffect(() => {
    if (!open) return;
    const t = setTimeout(() => setOpen(false), 4000);
    return () => clearTimeout(t);
  }, [open]);

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        title={open ? "" : title}
        className={`${triggerClassName ?? ""} ${open ? "!opacity-100" : ""}`}
      >
        {children ?? "✕"}
      </button>

      {open && pos &&
        createPortal(
          <div
            ref={popRef}
            data-confirm-popover
            role="dialog"
            aria-label={title}
            className="fixed z-[200] flex flex-col gap-2.5 px-3.5 py-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface)] shadow-xl w-[min(90vw,300px)]"
            style={{ top: pos.top, left: pos.left, transform: "translate(-100%, -100%)" }}
            onClick={(e) => e.stopPropagation()}
          >
            <span className="text-xs leading-relaxed text-[var(--color-text)]">{title}</span>
            <div className="flex flex-row items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="shrink-0 px-2.5 py-1 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] text-[var(--color-text)] whitespace-nowrap"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => {
                  onConfirm();
                  setOpen(false);
                }}
                className="shrink-0 px-2.5 py-1 text-xs rounded-md bg-[var(--color-danger)]/80 hover:bg-[var(--color-danger)] text-white whitespace-nowrap"
              >
                {confirmLabel}
              </button>
            </div>
          </div>,
          document.body
        )}
    </>
  );
}
