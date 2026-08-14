import { useEffect, useMemo, useRef, useState } from "react";
import { FreeAPIService } from "../../bindings/switchfree/service";
import { ImportItem, ImportStrategy, type ProviderConfig, type ShareProvider } from "../../bindings/switchfree/freeapi/models";

// 分享/导入供应商对话框（.sds：一次性强密码加密）
// 导出：勾选供应商 -> 自动生成一次性密码 -> 保存 .sds
// 导入：选 .sds -> 输入密码解密 -> 预览/解决 id 冲突 -> 导入

type Mode = "menu" | "export" | "import";

interface Props {
  providers: Record<string, ProviderConfig | null>;
  onClose: () => void;
  onImported: () => void;
  /** 快捷分享：打开时直接进入导出并预勾选这些供应商 id */
  initialIds?: string[];
}

type Conflict = "new" | "overwrite" | "skip" | "rename";

interface ImportRow {
  sp: ShareProvider;
  conflict: boolean; // id 是否已存在
  strategy: Conflict;
  newId: string;
  selected: boolean;
}

async function readFileAsB64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => {
      const result = r.result as string;
      // result 是 data URL，去掉前缀
      resolve(result.split(",")[1] ?? "");
    };
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

export default function ShareDialog({ providers, onClose, onImported, initialIds }: Props) {
  const [mode, setMode] = useState<Mode>(initialIds?.length ? "export" : "menu");
  const flash = (t: string) => setMsg(t);

  // ===== 导出状态 =====
  const list = useMemo(
    () => Object.values(providers).filter((p): p is ProviderConfig => !!p),
    [providers]
  );
  const [exportIds, setExportIds] = useState<string[]>(initialIds ?? []);
  const [password, setPassword] = useState("");
  const [copied, setCopied] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false); // 文件保存成功后才展示一次性密码
  // 分享选项：默认不分享 API Key
  const [includeKey, setIncludeKey] = useState(false);
  // 不含 Key 时可选是否设密码（默认不设，走内置密钥混淆）；含 Key 时强制设密码
  const [usePassword, setUsePassword] = useState(false);
  const needPassword = includeKey || usePassword; // 该次导出是否需要密码

  // ===== 导入状态 =====
  const [importRows, setImportRows] = useState<ImportRow[]>([]);
  const [impPassword, setImpPassword] = useState("");
  const [fileEncrypted, setFileEncrypted] = useState(true);
  const [importing, setImporting] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const fileB64Ref = useRef("");

  const [msg, setMsg] = useState("");

  const showError = (e: unknown) => setMsg(`❌ ${String(e)}`);

  // ───── 导出 ─────
  const toggleExport = (id: string) =>
    setExportIds((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]));

  const genPassword = async (): Promise<string> => {
    try {
      const p = await FreeAPIService.GenerateSharePassword();
      setPassword(p);
      setCopied(false);
      return p;
    } catch (e) {
      showError(e);
      return "";
    }
  };

  // 进入导出模式：重置选项；需要密码时才生成
  const openExport = async (ids?: string[]) => {
    setMode("export");
    setMsg("");
    setSaved(false);
    setIncludeKey(false);
    setUsePassword(false);
    setPassword("");
    if (ids?.length) setExportIds(ids);
  };

  // 返回主菜单
  const backToMenu = () => {
    setMode("menu");
    setMsg("");
    setSaved(false);
  };

  // 快捷分享：对话框已带 initialIds 打开时，进入导出态
  useEffect(() => {
    if (initialIds?.length) {
      setMode("export");
      setSaved(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 勾选"分享 API 密钥"时强制开启密码保护
  const toggleIncludeKey = (v: boolean) => {
    setIncludeKey(v);
    if (v) setUsePassword(true);
  };

  const copyPassword = async () => {
    try {
      await navigator.clipboard.writeText(password);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* 剪贴板不可用时静默 */
    }
  };

  const doExport = async () => {
    if (exportIds.length === 0) return flash("请至少选择一个供应商");
    setSaving(true);
    try {
      // 需要密码时确保有一次性密码；不需要则传空，后端走内置密钥混淆
      let pwd = "";
      if (needPassword) {
        pwd = password || (await genPassword());
        if (!pwd) throw new Error("生成密码失败");
      }
      const b64 = await FreeAPIService.ExportShare(exportIds, pwd, includeKey);
      const savedPath = await FreeAPIService.SaveShareFile(b64);
      if (savedPath) {
        setSaved(true);
        if (needPassword) {
          setMsg("✅ 文件已保存。请复制下面的密码，通过另一渠道发给对方。");
        } else {
          setMsg("✅ 文件已保存（不含 API 密钥，无需密码即可导入）。");
        }
      }
    } catch (e) {
      showError(e);
    } finally {
      setSaving(false);
    }
  };

  // ───── 导入 ─────
  const onPickFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setMsg("");
    try {
      const b64 = await readFileAsB64(file);
      fileB64Ref.current = b64;
      const preview = await FreeAPIService.InspectShare(b64);
      if (!preview) throw new Error("无法读取文件头");
      if (!preview.needPasswd) {
        // 内置密钥混淆模式或未加密：无需密码，直接解密
        setFileEncrypted(false);
        setImpPassword("");
        await decryptAndBuild(b64, "");
        setMsg("✅ 已读取文件，请选择要导入的供应商");
      } else {
        // 密码模式：等用户输入密码
        setFileEncrypted(true);
        setImpPassword("");
        setImportRows([]);
      }
    } catch (err) {
      showError(err);
    }
  };

  const decryptAndBuild = async (b64: string, pwd: string) => {
    const sps = await FreeAPIService.DecryptShare(b64, pwd);
    const existing = new Set(Object.keys(providers));
    const rows: ImportRow[] = sps.map((sp) => ({
      sp,
      conflict: existing.has(sp.id),
      strategy: (existing.has(sp.id) ? "overwrite" : "new") as Conflict,
      newId: "",
      selected: true,
    }));
    setImportRows(rows);
  };

  const doDecrypt = async () => {
    if (!impPassword) return flash("请输入分享密码");
    try {
      await decryptAndBuild(fileB64Ref.current, impPassword);
      setMsg("✅ 解密成功，请选择要导入的供应商");
    } catch (e) {
      showError(e);
    }
  };

  const setRow = (idx: number, patch: Partial<ImportRow>) =>
    setImportRows((rs) => rs.map((r, i) => (i === idx ? { ...r, ...patch } : r)));

  const doImport = async () => {
    const chosen = importRows.filter((r) => r.selected);
    if (chosen.length === 0) return flash("请至少选择一个供应商");
    setImporting(true);
    try {
      const items = chosen.map((r) =>
        ImportItem.createFrom({
          provider: r.sp,
          strategy:
            r.strategy === "new"
              ? ImportStrategy.ImportOverwrite
              : r.strategy === "skip"
                ? ImportStrategy.ImportSkip
                : r.strategy === "rename"
                  ? ImportStrategy.ImportRename
                  : ImportStrategy.ImportOverwrite,
          newId: r.newId,
        })
      );
      await FreeAPIService.ImportShare(items);
      setMsg("✅ 导入完成");
      onImported();
      setTimeout(onClose, 800);
    } catch (e) {
      showError(e);
    } finally {
      setImporting(false);
    }
  };

  // ───── 渲染 ─────
  return (
    <div
      className="fixed inset-0 z-[300] flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg max-h-[85vh] overflow-y-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold">
            {mode === "export" ? "📤 分享供应商" : mode === "import" ? "📥 导入供应商" : "供应商分享"}
          </h3>
          <button onClick={onClose} className="text-[var(--color-text-dim)] hover:text-[var(--color-text)]">✕</button>
        </div>

        {msg && (
          <div className="mb-3 px-3 py-2 rounded-lg text-xs bg-[var(--color-surface-2)] break-words">{msg}</div>
        )}

        {mode === "menu" && (
          <div className="grid grid-cols-2 gap-3">
            <button
              onClick={() => openExport()}
              disabled={list.length === 0}
              className="p-5 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50 text-sm"
            >
              <div className="text-2xl mb-1">📤</div>
              导出分享
              <div className="text-[11px] text-[var(--color-text-dim)] mt-1">导出供应商配置或带密钥的 .sds 文件</div>
            </button>
            <button
              onClick={() => {
                setMode("import");
                setMsg("");
              }}
              className="p-5 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] text-sm"
            >
              <div className="text-2xl mb-1">📥</div>
              导入
              <div className="text-[11px] text-[var(--color-text-dim)] mt-1">从 .sds 文件导入</div>
            </button>
            {list.length === 0 && (
              <p className="col-span-2 text-xs text-[var(--color-text-dim)]">没有可导出的供应商，请先添加。</p>
            )}
          </div>
        )}

        {mode === "export" && (
          <div className="space-y-3">
            <div>
              <div className="text-xs text-[var(--color-text-dim)] mb-1.5">选择要分享的供应商（{exportIds.length}/{list.length}）</div>
              <div className="space-y-1 max-h-44 overflow-y-auto">
                {list.map((p) => {
                  const n = p.models?.filter((m) => m.verified).length ?? 0;
                  const checked = exportIds.includes(p.id);
                  return (
                    <label
                      key={p.id}
                      className="flex items-center gap-2 p-2 rounded-md bg-[var(--color-bg)] border border-[var(--color-border)] cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleExport(p.id)}
                        className="w-4 h-4 accent-[var(--color-primary)]"
                      />
                      <span className="text-sm flex-1 truncate">{p.name}</span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)]">{n} 模型</span>
                    </label>
                  );
                })}
              </div>
            </div>

            {/* 分享选项 */}
            <div className="space-y-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-bg)] p-2.5">
              <label className="flex items-start gap-2 text-xs cursor-pointer">
                <input
                  type="checkbox"
                  checked={includeKey}
                  onChange={(e) => toggleIncludeKey(e.target.checked)}
                  className="w-4 h-4 mt-0.5 accent-[var(--color-primary)]"
                />
                <span>
                  <span className="text-[var(--color-text)]">分享 API 密钥</span>
                  <span className="block text-[10px] text-[var(--color-text-dim)] mt-0.5">
                    勾选后对方导入即可直接使用；不勾选则只分享供应商配置，对方需自行填写密钥。
                  </span>
                </span>
              </label>
              <label className="flex items-start gap-2 text-xs cursor-pointer">
                <input
                  type="checkbox"
                  checked={usePassword}
                  disabled={includeKey}
                  onChange={(e) => setUsePassword(e.target.checked)}
                  className="w-4 h-4 mt-0.5 accent-[var(--color-primary)] disabled:cursor-not-allowed"
                />
                <span>
                  <span className="text-[var(--color-text)]">用一次性密码保护</span>
                  <span className="block text-[10px] text-[var(--color-text-dim)] mt-0.5">
                    {includeKey
                      ? "含 API 密钥，已强制开启。"
                      : "不勾选则文件仍加密，但无需密码即可导入（适合仅分享配置）。"}
                  </span>
                </span>
              </label>
            </div>

            {saved && needPassword ? (
              <div>
                <label className="text-xs text-[var(--color-success)] block mb-1.5">
                  一次性分享密码（只显示这一次，请复制后发给对方）
                </label>
                <div className="flex gap-2">
                  <input
                    readOnly
                    value={password}
                    className="flex-1 px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-primary)]/50 font-mono"
                  />
                  <button
                    onClick={copyPassword}
                    className="px-3 py-1.5 text-xs rounded-md bg-[var(--color-primary)] hover:opacity-90 whitespace-nowrap"
                  >
                    {copied ? "已复制" : "复制密码"}
                  </button>
                </div>
                <p className="text-[11px] text-[var(--color-text-dim)] mt-1.5 leading-relaxed">
                  ⚠️ 请通过<strong>另一个渠道</strong>（微信/口头）把密码发给对方，关闭后无法再查看密码。
                </p>
              </div>
            ) : (
              <p className="text-[11px] leading-relaxed rounded-md px-3 py-2 min-h-[2.75rem] flex items-center bg-[var(--color-surface-2)] text-[var(--color-text-dim)]">
                {needPassword
                  ? "🔒 点击「保存 .sds 文件」后生成一次性密码并显示在此处，请通过另一个渠道发给对方。"
                  : "🔓 文件将加密保存（不含 API 密钥），对方导入无需密码。"}
              </p>
            )}

            <div className="flex justify-end gap-2 pt-1">
              <button onClick={backToMenu} className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]">
                返回
              </button>
              <button
                onClick={doExport}
                disabled={saving || exportIds.length === 0}
                className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
              >
                {saving ? "保存中..." : "💾 保存 .sds 文件"}
              </button>
            </div>
          </div>
        )}

        {mode === "import" && (
          <div className="space-y-3">
            <div>
              <label className="text-xs text-[var(--color-text-dim)] block mb-1.5">选择 .sds 文件</label>
              <input
                ref={fileRef}
                type="file"
                accept=".sds,application/octet-stream"
                onChange={onPickFile}
                className="w-full text-xs text-[var(--color-text-dim)] file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0 file:bg-[var(--color-surface-2)] file:text-[var(--color-text)] hover:file:bg-[var(--color-border)]"
              />
            </div>

            {fileEncrypted && fileB64Ref.current && importRows.length === 0 && (
              <div className="flex gap-2">
                <input
                  type="password"
                  value={impPassword}
                  onChange={(e) => setImpPassword(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && doDecrypt()}
                  placeholder="输入分享密码"
                  className="flex-1 px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
                />
                <button
                  onClick={doDecrypt}
                  className="px-3 py-1.5 text-sm rounded-md bg-[var(--color-primary)] hover:opacity-90 whitespace-nowrap"
                >
                  解密
                </button>
              </div>
            )}

            {importRows.length > 0 && (
              <div className="space-y-1.5 max-h-60 overflow-y-auto">
                {importRows.map((r, idx) => (
                  <div
                    key={r.sp.id + idx}
                    className="p-2 rounded-md bg-[var(--color-bg)] border border-[var(--color-border)] space-y-1.5"
                  >
                    <div className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={r.selected}
                        onChange={(e) => setRow(idx, { selected: e.target.checked })}
                        className="w-4 h-4 accent-[var(--color-primary)]"
                      />
                      <span className="text-sm flex-1 truncate">{r.sp.name || r.sp.id}</span>
                      <span className="text-[10px] text-[var(--color-text-dim)]">
                        {r.sp.models?.length ?? 0} 模型
                        {!r.sp.apiKey && " · 不含密钥"}
                      </span>
                    </div>
                    {r.conflict && (
                      <div className="flex items-center gap-1.5 pl-6 text-xs flex-wrap">
                        <span className="text-[var(--color-warning)]">id 已存在：</span>
                        {(["overwrite", "skip", "rename"] as Conflict[]).map((s) => (
                          <button
                            key={s}
                            onClick={() => setRow(idx, { strategy: s })}
                            className={`px-2 py-0.5 rounded text-[11px] ${
                              r.strategy === s
                                ? "bg-[var(--color-primary)] text-white"
                                : "bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
                            }`}
                          >
                            {s === "overwrite" ? "覆盖" : s === "skip" ? "跳过" : "重命名"}
                          </button>
                        ))}
                        {r.strategy === "rename" && (
                          <span className="text-[var(--color-text-dim)]">
                            将自动保存为「{r.sp.id}-xxxxxx」
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}

            <div className="flex justify-end gap-2 pt-1">
              <button onClick={backToMenu} className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]">
                返回
              </button>
              <button
                onClick={doImport}
                disabled={importing || importRows.length === 0}
                className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
              >
                {importing ? "导入中..." : "✓ 导入所选"}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
