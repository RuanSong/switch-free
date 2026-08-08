import { useEffect, useState } from "react";
import { UpdaterService } from "../../bindings/switchfree/service";
import type { UpdateInfo } from "../../bindings/switchfree/updater/models";
import { useWailsEvent } from "../hooks/useWailsEvent";

export default function UpdatePanel() {
  const [currentVersion, setCurrentVersion] = useState<string>("");
  const [checking, setChecking] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [progress, setProgress] = useState<{ percent: number; message: string } | null>(null);
  const [msg, setMsg] = useState<{ type: "ok" | "err"; text: string } | null>(null);

  // 初始拉取当前版本
  useEffect(() => {
    UpdaterService.GetCurrentVersion().then((v) => setCurrentVersion(v ?? ""));
  }, []);

  // 启动时自动检查（main 推送 update:available）
  useWailsEvent("update:available", (data) => {
    setUpdateInfo(data as UpdateInfo);
  });

  // 下载进度
  useWailsEvent("update:progress", (data) => {
    const s = data as { state: string; percent: number; message: string };
    if (s.state === "done") {
      setProgress({ percent: 100, message: s.message || "更新完成，请重启应用" });
      setUpdating(false);
    } else if (s.state === "error") {
      setMsg({ type: "err", text: s.message || "更新失败" });
      setUpdating(false);
    } else {
      setProgress({ percent: s.percent || 0, message: s.message || "下载中..." });
    }
  });

  const check = async () => {
    setChecking(true);
    setMsg(null);
    try {
      const info = await UpdaterService.CheckUpdate();
      if (info) {
        setUpdateInfo(info);
      } else {
        setMsg({ type: "ok", text: "当前已是最新版本" });
      }
    } catch (e) {
      setMsg({ type: "err", text: `检查失败: ${e}` });
    } finally {
      setChecking(false);
    }
  };

  const apply = async () => {
    if (!updateInfo) return;
    setUpdating(true);
    setMsg(null);
    try {
      await UpdaterService.ApplyUpdate(updateInfo);
      // done 事件会处理
    } catch (e) {
      setMsg({ type: "err", text: `更新失败: ${e}` });
      setUpdating(false);
    }
  };

  return (
    <section className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
      <div className="flex items-center justify-between mb-3">
        <h2 className="font-semibold">自动升级</h2>
        <span className="text-xs px-2 py-1 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)]">
          当前版本 {currentVersion || "-"}
        </span>
      </div>

      {msg && (
        <div className={`mb-3 px-4 py-2 rounded-lg text-sm ${msg.type === "ok" ? "bg-[var(--color-success)]/20 text-[var(--color-success)]" : "bg-[var(--color-danger)]/20 text-[var(--color-danger)]"}`}>
          {msg.text}
        </div>
      )}

      {updateInfo ? (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <span className="text-sm">发现新版本：</span>
            <span className="font-mono text-sm font-bold text-[var(--color-primary)]">{updateInfo.version}</span>
            <span className="text-xs text-[var(--color-text-dim)]">({updateInfo.assetSize ? (updateInfo.assetSize / 1024 / 1024).toFixed(1) + " MB" : ""})</span>
          </div>
          {updateInfo.notes && (
            <div className="text-xs text-[var(--color-text-dim)] whitespace-pre-wrap bg-[var(--color-bg)] rounded-lg p-3 max-h-40 overflow-y-auto">
              {updateInfo.notes}
            </div>
          )}
          {/* 下载进度 */}
          {progress && (
            <div>
              <div className="flex justify-between text-xs text-[var(--color-text-dim)] mb-1">
                <span>{progress.message}</span>
                <span>{Math.round(progress.percent)}%</span>
              </div>
              <div className="h-2 rounded bg-[var(--color-bg)] overflow-hidden">
                <div className="h-full rounded bg-[var(--color-primary)] transition-all" style={{ width: `${progress.percent}%` }} />
              </div>
            </div>
          )}
          <div className="flex gap-2">
            <button
              onClick={apply}
              disabled={updating}
              className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
            >
              {updating ? "更新中..." : "立即更新"}
            </button>
            <button
              onClick={() => setUpdateInfo(null)}
              disabled={updating}
              className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
            >
              忽略
            </button>
          </div>
        </div>
      ) : (
        <div className="flex items-center justify-between">
          <p className="text-xs text-[var(--color-text-dim)]">检查 GitHub Releases 获取新版本，更新会替换当前二进制。</p>
          <button
            onClick={check}
            disabled={checking}
            className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
          >
            {checking ? "检查中..." : "🔍 检查更新"}
          </button>
        </div>
      )}
    </section>
  );
}
