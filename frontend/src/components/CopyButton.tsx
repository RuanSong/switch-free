import { useState } from "react";

interface Props {
  text: string;
  label?: string;
  className?: string;
}

export default function CopyButton({ text, label = "复制", className = "" }: Props) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // clipboard 不可用时降级：选中提示
      console.warn("clipboard 写入失败");
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <button
      onClick={copy}
      className={`px-2.5 py-1 text-xs rounded-md transition-colors ${
        copied
          ? "bg-[var(--color-success)]/20 text-[var(--color-success)]"
          : "bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] text-[var(--color-text)]"
      } ${className}`}
    >
      {copied ? "✓ 已复制" : label}
    </button>
  );
}
