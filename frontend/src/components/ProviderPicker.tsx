import { useEffect, useMemo, useRef, useState, useCallback } from "react";
import type { CatalogProvider } from "../../bindings/switchdev/providerapi/models";

// 标签定义
type TagCategory = "region" | "credit" | "context" | "count";
interface Tag {
  key: string;       // 唯一标识，如 "region:domestic"
  category: TagCategory;
  label: string;
  match: (p: CatalogProvider) => boolean;
}

// 解析上下文文本为字节数（"1M" -> 1000000）
function parseContextToNum(s: string): number {
  if (!s) return 0;
  const m = s.match(/([\d.]+)\s*([KMB])?/i);
  if (!m) return 0;
  const num = parseFloat(m[1]);
  const unit = (m[2] ?? "").toUpperCase();
  if (unit === "M") return num * 1_000_000;
  if (unit === "K") return num * 1_000;
  if (unit === "B") return num * 1_000_000_000;
  return num;
}

// 从所有供应商提取标签（去重，固定四类顺序）
function buildTags(providers: CatalogProvider[]): Tag[] {
  const tags: Tag[] = [];

  // 地区
  const hasDomestic = providers.some((p) => p.region === "domestic");
  const hasIntl = providers.some((p) => p.region === "international");
  if (hasIntl) tags.push({ key: "region:international", category: "region", label: "🌍 国际", match: (p) => p.region === "international" });
  if (hasDomestic) tags.push({ key: "region:domestic", category: "region", label: "🇨🇳 国内", match: (p) => p.region === "domestic" });

  // 信用卡
  if (providers.some((p) => !p.credit_card_required)) {
    tags.push({ key: "credit:no", category: "credit", label: "💳 无需信用卡", match: (p) => !p.credit_card_required });
  }
  if (providers.some((p) => p.credit_card_required)) {
    tags.push({ key: "credit:yes", category: "credit", label: "💳 需信用卡", match: (p) => p.credit_card_required });
  }

  // 上下文规模（按解析后的字节分桶）
  const ctxBuckets = [
    { key: "context:small", label: "📏 <128K", test: (n: number) => n > 0 && n < 128_000 },
    { key: "context:mid", label: "📏 128K-1M", test: (n: number) => n >= 128_000 && n < 1_000_000 },
    { key: "context:large", label: "📏 ≥1M", test: (n: number) => n >= 1_000_000 },
  ];
  for (const b of ctxBuckets) {
    if (providers.some((p) => b.test(parseContextToNum(p.max_context)))) {
      tags.push({ key: b.key, category: "context", label: b.label, match: (p) => b.test(parseContextToNum(p.max_context)) });
    }
  }

  // 模型数分桶
  const countBuckets = [
    { key: "count:small", label: "📦 <10", test: (n: number) => n > 0 && n < 10 },
    { key: "count:mid", label: "📦 10-50", test: (n: number) => n >= 10 && n <= 50 },
    { key: "count:large", label: "📦 >50", test: (n: number) => n > 50 },
  ];
  for (const b of countBuckets) {
    if (providers.some((p) => b.test(p.free_models_count))) {
      tags.push({ key: b.key, category: "count", label: b.label, match: (p) => b.test(p.free_models_count) });
    }
  }

  return tags;
}

interface Props {
  providers: CatalogProvider[];
  value: string;
  onChange: (id: string) => void;
  existingIds?: string[];
  placeholder?: string;
  disabled?: boolean;
}

export default function ProviderPicker({ providers, value, onChange, existingIds = [], placeholder = "选择供应商...", disabled = false }: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set());
  const [highlight, setHighlight] = useState(0);
  const [flipped, setFlipped] = useState(false); // 弹层向上翻转

  const containerRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const allTags = useMemo(() => buildTags(providers), [providers]);

  // 隐藏已添加的供应商
  const existingSet = useMemo(() => new Set(existingIds), [existingIds]);
  const visibleProviders = useMemo(
    () => providers.filter((p) => !existingSet.has(p.id)),
    [providers, existingSet]
  );

  // 选中的供应商对象（用于触发器显示）
  const selected = useMemo(
    () => providers.find((p) => p.id === value) ?? null,
    [providers, value]
  );

  // 搜索匹配（名称、id、base_url、旗下模型名/id）
  const matchesQuery = useCallback((p: CatalogProvider, q: string): boolean => {
    if (!q) return true;
    const needle = q.toLowerCase();
    if (p.name.toLowerCase().includes(needle)) return true;
    if (p.id.toLowerCase().includes(needle)) return true;
    if (p.base_url?.toLowerCase().includes(needle)) return true;
    // 匹配旗下模型
    for (const m of p.free_models ?? []) {
      if (m.id?.toLowerCase().includes(needle)) return true;
      if (m.name?.toLowerCase().includes(needle)) return true;
    }
    return false;
  }, []);

  // 搜索相关度打分：厂商名/id 命中优先于旗下模型命中；精确/前缀命中再优先。
  // 分数越高越靠前；同分时保持目录原始顺序。
  const matchScore = useCallback((p: CatalogProvider, q: string): number => {
    if (!q) return 0;
    const needle = q.toLowerCase();
    const name = p.name.toLowerCase();
    const id = p.id.toLowerCase();
    const base = p.base_url?.toLowerCase() ?? "";
    if (name === needle || id === needle) return 100;
    if (name.startsWith(needle) || id.startsWith(needle)) return 90;
    if (name.includes(needle) || id.includes(needle)) return 80;
    if (base.includes(needle)) return 60;
    for (const m of p.free_models ?? []) {
      const mid = m.id?.toLowerCase() ?? "";
      const mname = m.name?.toLowerCase() ?? "";
      if (mid === needle || mname === needle) return 50;
      if (mid.startsWith(needle) || mname.startsWith(needle)) return 45;
      if (mid.includes(needle) || mname.includes(needle)) return 40;
    }
    return 0;
  }, []);

  // 组合筛选：搜索 + 标签。
  // 无搜索词时只列出未添加的供应商（保持目录顺序）；
  // 有搜索词时把已添加的供应商也纳入结果（正常参与相关度排序），但标记为已添加、禁选，
  // 否则像搜 "deepseek" 时 DeepSeek 已添加会被整体隐藏，反而排在一堆有同名模型的别家后面。
  const filtered = useMemo(() => {
    const activeTags = allTags.filter((t) => selectedTags.has(t.key));
    const hasQuery = query.trim().length > 0;
    const base = hasQuery ? providers : visibleProviders;
    const list = base.filter((p) => {
      if (!matchesQuery(p, query)) return false;
      // 同类别 OR：该供应商要满足每个类别中至少一个选中的标签
      const byCategory = new Map<TagCategory, Tag[]>();
      for (const t of activeTags) {
        if (!byCategory.has(t.category)) byCategory.set(t.category, []);
        byCategory.get(t.category)!.push(t);
      }
      for (const tags of byCategory.values()) {
        if (!tags.some((t) => t.match(p))) return false;
      }
      return true;
    });
    const q = query.trim();
    if (!q) return list;
    return list
      .map((p, idx) => ({ p, idx, score: matchScore(p, q) }))
      .sort((a, b) => b.score - a.score || a.idx - b.idx)
      .map((x) => x.p);
  }, [providers, visibleProviders, query, selectedTags, allTags, matchesQuery, matchScore]);

  // 每个标签在当前筛选（搜索 + 其他标签）下的计数
  const tagCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    // 计数时排除自身类别（同类别不互斥计数），这样选了"国内"后"国际"仍显示总数
    const otherTags = allTags.filter((t) => selectedTags.has(t.key));
    for (const tag of allTags) {
      counts[tag.key] = visibleProviders.filter((p) => {
        if (!matchesQuery(p, query)) return false;
        for (const ot of otherTags) {
          if (ot.category === tag.category) continue;
          if (!ot.match(p)) return false;
        }
        return true;
      }).length;
    }
    return counts;
  }, [allTags, visibleProviders, query, selectedTags, matchesQuery]);

  // 打开/关闭
  const handleOpen = useCallback(() => {
    setOpen(true);
    setQuery("");
    setSelectedTags(new Set());
    setHighlight(0);
  }, []);

  const handleClose = useCallback(() => {
    setOpen(false);
    setQuery("");
  }, []);

  // 打开时自动聚焦搜索框 + 计算翻转方向
  useEffect(() => {
    if (open) {
      setTimeout(() => searchRef.current?.focus(), 10);
      // 计算下方空间是否足够（需要约 360px 展示列表）
      if (containerRef.current) {
        const rect = containerRef.current.getBoundingClientRect();
        const spaceBelow = window.innerHeight - rect.bottom;
        const spaceAbove = rect.top;
        const needHeight = 380;
        setFlipped(spaceBelow < needHeight && spaceAbove > spaceBelow);
      }
    }
  }, [open]);

  // 点击外部关闭
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        handleClose();
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, handleClose]);

  // 高亮项滚动到可视区
  useEffect(() => {
    if (!open || !listRef.current) return;
    const el = listRef.current.children[highlight] as HTMLElement;
    if (el) el.scrollIntoView({ block: "nearest" });
  }, [highlight, open]);

  // 高亮范围随筛选结果变化
  useEffect(() => {
    setHighlight(0);
  }, [query, selectedTags]);

  // 键盘导航
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (!open) {
      if (e.key === "ArrowDown" || e.key === "Enter") {
        e.preventDefault();
        handleOpen();
      }
      return;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      handleClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlight((h) => {
        for (let i = h + 1; i < filtered.length; i++) {
          if (!existingSet.has(filtered[i].id)) return i;
        }
        return h;
      });
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlight((h) => {
        for (let i = h - 1; i >= 0; i--) {
          if (!existingSet.has(filtered[i].id)) return i;
        }
        return h;
      });
    } else if (e.key === "Enter") {
      e.preventDefault();
      const p = filtered[highlight];
      if (p && !existingSet.has(p.id)) {
        onChange(p.id);
        handleClose();
      }
    }
  };

  const toggleTag = (key: string) => {
    setSelectedTags((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const selectProvider = (p: CatalogProvider) => {
    onChange(p.id);
    handleClose();
  };

  return (
    <div ref={containerRef} className="relative w-full" onKeyDown={onKeyDown}>
      {/* 触发器 */}
      <button
        type="button"
        disabled={disabled}
        onClick={() => !disabled && (open ? handleClose() : handleOpen())}
        className="w-full px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] text-left flex items-center gap-1 disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {selected ? (
          <span className="truncate flex-1 flex items-center gap-1.5">
            <span>{selected.name}</span>
            <span className="text-[10px] px-1 rounded bg-[var(--color-primary)]/15 text-[var(--color-primary)]">
              {selected.free_models_count} 模型
            </span>
          </span>
        ) : (
          <span className="text-[var(--color-text-dim)]">{placeholder}</span>
        )}
        <span className="text-[var(--color-text-dim)] ml-auto text-[10px]">{open ? "▲" : "▼"}</span>
      </button>

      {/* 弹层 */}
      {open && (
        <div
          className={`absolute z-50 w-full bg-[var(--color-surface)] border border-[var(--color-border)] rounded-lg shadow-xl flex flex-col ${
            flipped ? "bottom-full mb-1" : "top-full mt-1"
          }`}
          style={{ maxHeight: 420 }}
        >
          {/* 搜索框 */}
          <div className="p-2 border-b border-[var(--color-border)]">
            <input
              ref={searchRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="🔍 搜索供应商或模型..."
              className="w-full px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] outline-none focus:border-[var(--color-primary)]"
            />
          </div>

          {/* 标签筛选 */}
          {allTags.length > 0 && (
            <div className="px-2 py-1.5 border-b border-[var(--color-border)] flex flex-wrap gap-1">
              {selectedTags.size > 0 && (
                <button
                  onClick={() => setSelectedTags(new Set())}
                  className="text-[11px] px-1.5 py-0.5 rounded bg-[var(--color-primary)]/15 text-[var(--color-primary)] hover:opacity-80"
                >
                  全部
                </button>
              )}
              {allTags.map((tag) => {
                const active = selectedTags.has(tag.key);
                const count = tagCounts[tag.key] ?? 0;
                return (
                  <button
                    key={tag.key}
                    onClick={() => toggleTag(tag.key)}
                    className={`text-[11px] px-1.5 py-0.5 rounded border transition-colors ${
                      active
                        ? "bg-[var(--color-primary)]/20 border-[var(--color-primary)]/40 text-[var(--color-primary)]"
                        : "bg-[var(--color-surface-2)] border-transparent text-[var(--color-text-dim)] hover:text-[var(--color-text)]"
                    }`}
                  >
                    {tag.label} <span className="opacity-60">{count}</span>
                  </button>
                );
              })}
            </div>
          )}

          {/* 供应商列表 */}
          <div ref={listRef} className="flex-1 overflow-y-auto py-1">
            {filtered.length === 0 ? (
              <div className="px-3 py-6 text-center text-sm text-[var(--color-text-dim)]">
                未找到匹配的供应商
              </div>
            ) : (
              filtered.map((p, idx) => {
                const isHighlight = idx === highlight;
                const ctxNum = parseContextToNum(p.max_context);
                const already = existingSet.has(p.id);
                return (
                  <button
                    key={p.id}
                    type="button"
                    disabled={already}
                    onMouseEnter={() => setHighlight(idx)}
                    onClick={() => !already && selectProvider(p)}
                    className={`w-full text-left px-3 py-2 flex items-start gap-2 ${
                      already
                        ? "opacity-50 cursor-not-allowed"
                        : isHighlight
                        ? "bg-[var(--color-primary)]/10"
                        : "hover:bg-[var(--color-surface-2)]"
                    }`}
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="text-sm font-medium truncate">{p.name}</span>
                        {already && (
                          <span className="text-[10px] px-1 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)] shrink-0">已添加</span>
                        )}
                        {p.region === "domestic" && (
                          <span className="text-[10px] px-1 rounded bg-amber-500/15 text-amber-400 shrink-0">国内</span>
                        )}
                      </div>
                      <div className="text-[11px] text-[var(--color-text-dim)] truncate">{p.base_url}</div>
                      <div className="flex flex-wrap gap-1 mt-1">
                        <span className="text-[10px] px-1 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)]">
                          {ctxNum >= 1_000_000 ? `${(ctxNum / 1_000_000).toFixed(0)}M` : ctxNum >= 1_000 ? `${(ctxNum / 1_000).toFixed(0)}K` : p.max_context || "-"} 上下文
                        </span>
                        <span
                          className={`text-[10px] px-1 rounded ${
                            p.credit_card_required
                              ? "bg-amber-500/15 text-amber-400"
                              : "bg-[var(--color-success)]/15 text-[var(--color-success)]"
                          }`}
                        >
                          {p.credit_card_required ? "需信用卡" : "无需信用卡"}
                        </span>
                      </div>
                    </div>
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-primary)]/15 text-[var(--color-primary)] whitespace-nowrap shrink-0 mt-0.5">
                      {p.free_models_count} 模型
                    </span>
                  </button>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}
