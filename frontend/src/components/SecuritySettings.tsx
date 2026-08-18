import { useEffect, useState } from "react";
import { ProviderAPIService } from "../../bindings/switchdev/service";

interface Props {
  onClose: () => void;
  onChanged: () => void;
}

type Mode = "status" | "set" | "recovery";

// 安全设置：查看状态、设置/修改主密码、在本机记住、显示恢复码
export default function SecuritySettings({ onClose, onChanged }: Props) {
  const [mode, setMode] = useState<Mode>("status");
  const [masterSet, setMasterSet] = useState(false);
  const [remembered, setRemembered] = useState(false);
  const [hasRecovery, setHasRecovery] = useState(false);

  const [newPass, setNewPass] = useState("");
  const [newPass2, setNewPass2] = useState("");
  const [remember, setRemember] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [recoveryCode, setRecoveryCode] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);

  const refresh = async () => {
    try {
      const info = await ProviderAPIService.GetLockStatus();
      setMasterSet(!!info?.masterSet);
      setHasRecovery(!!info?.hasRecovery);
      setRemembered(!!info?.remembered);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    refresh();
    // 默认勾选"记住"
    setRemember(true);
  }, []);

  const save = async () => {
    if (newPass.length < 6) {
      setMsg("新密码至少 6 位");
      return;
    }
    if (newPass !== newPass2) {
      setMsg("两次输入的新密码不一致");
      return;
    }
    setBusy(true);
    setMsg("");
    try {
      const code = await ProviderAPIService.SetMasterPassword(newPass, remember);
      setRecoveryCode(code);
      setRemembered(remember);
      setMode("recovery");
      setNewPass("");
      setNewPass2("");
      setMasterSet(true);
      setHasRecovery(true);
      onChanged();
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const clearRemember = async () => {
    setBusy(true);
    try {
      await ProviderAPIService.ClearRememberedPassword();
      setRemembered(false);
      setMsg("✅ 已清除本机记住的密码，下次启动需手动输入");
      onChanged();
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  const clearMaster = async () => {
    setBusy(true);
    setMsg("");
    try {
      await ProviderAPIService.ClearMasterPassword();
      setMasterSet(false);
      setHasRecovery(false);
      setRemembered(true);
      setConfirmClear(false);
      setMsg("✅ 已清除主密码，回到自动加密模式，启动自动解锁");
      onChanged();
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[300] flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md p-5 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold">🔐 安全设置</h3>
          <button onClick={onClose} className="text-[var(--color-text-dim)]">✕</button>
        </div>

        {mode === "recovery" && recoveryCode ? (
          <div className="space-y-3">
            <div className="p-3 rounded-md bg-[var(--color-warning)]/10 border border-[var(--color-warning)]/30">
              <p className="text-xs font-medium mb-2">⚠️ 请立即保存恢复码</p>
              <p className="text-[11px] text-[var(--color-text-dim)] mb-2">
                忘记主密码时可用它重置。恢复码只显示这一次，旧恢复码已作废。
              </p>
              <code className="block px-2 py-1.5 rounded bg-[var(--color-surface-2)] font-mono text-xs break-all select-all">
                {recoveryCode}
              </code>
            </div>
            <button
              onClick={async () => {
                setRecoveryCode("");
                // 如果设置的是"不记住密码"，后端已立即锁定，
                // 关闭弹窗让主界面显示解锁页；否则回到状态页。
                try {
                  const info = await ProviderAPIService.GetLockStatus();
                  if (info?.isLocked) {
                    onChanged();
                    onClose();
                    return;
                  }
                } catch {
                  // ignore
                }
                await refresh();
                setMode("status");
              }}
              className="w-full px-3 py-2 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90"
            >
              我已妥善保存
            </button>
          </div>
        ) : mode === "set" ? (
          <div className="space-y-3">
            <p className="text-xs text-[var(--color-text-dim)]">
              {masterSet ? "修改主密码" : "设置主密码后，启动应用时需要输入它才能查看 API Key。"}
            </p>
            <div>
              <label className="text-xs text-[var(--color-text-dim)] block mb-1">
                {masterSet ? "新主密码" : "主密码"}
              </label>
              <input
                type="password"
                autoFocus
                value={newPass}
                onChange={(e) => setNewPass(e.target.value)}
                placeholder="至少 6 位"
                className="w-full px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
              />
            </div>
            <div>
              <label className="text-xs text-[var(--color-text-dim)] block mb-1">确认密码</label>
              <input
                type="password"
                value={newPass2}
                onChange={(e) => setNewPass2(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && save()}
                className="w-full px-2 py-1.5 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)]"
              />
            </div>
            <label className="flex items-center gap-2 text-xs cursor-pointer">
              <input
                type="checkbox"
                checked={remember}
                onChange={(e) => setRemember(e.target.checked)}
                className="w-4 h-4 accent-[var(--color-primary)]"
              />
              在本机记住密码（下次启动自动解锁）
            </label>
            {msg && <p className="text-xs text-[var(--color-danger)]">{msg}</p>}
            <div className="flex gap-2 pt-1">
              <button
                onClick={() => { setMode("status"); setMsg(""); }}
                disabled={busy}
                className="px-3 py-2 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
              >
                取消
              </button>
              <button
                onClick={save}
                disabled={busy}
                className="flex-1 px-3 py-2 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
              >
                {busy ? "保存中..." : "保存主密码"}
              </button>
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <div className="p-3 rounded-md bg-[var(--color-surface-2)] text-xs space-y-1.5">
              <div className="flex justify-between">
                <span className="text-[var(--color-text-dim)]">主密码</span>
                <span>{masterSet ? "已设置" : "未设置（自动加密中）"}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-[var(--color-text-dim)]">启动时</span>
                <span>
                  {masterSet
                    ? (remembered ? "自动解锁（已记住）" : "需要输入主密码")
                    : "自动解锁（随机密码存钥匙串）"}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-[var(--color-text-dim)]">恢复码</span>
                <span>{hasRecovery ? "已生成" : "未生成"}</span>
              </div>
            </div>
            <p className="text-[11px] text-[var(--color-text-dim)] leading-relaxed">
              主密码用于解锁本地加密的凭据。设置后磁盘上的 API Key 仍为加密状态，只有输入主密码才能查看；忘记密码可用恢复码重置。
              {masterSet && " 清除主密码后回到自动加密模式：启动自动解锁，测评和中转调用无需再手动输密码，但 API Key 仍加密保存在本机。"}
            </p>
            {msg && <p className="text-xs text-[var(--color-success)]">{msg}</p>}
            <div className="flex gap-2 pt-1">
              <button
                onClick={() => { setMode("set"); setMsg(""); }}
                disabled={busy}
                className="flex-1 px-3 py-2 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
              >
                {masterSet ? "修改主密码" : "设置主密码"}
              </button>
              {masterSet && (
                remembered ? (
                  <button
                    onClick={clearRemember}
                    disabled={busy}
                    title="清除后下次启动需输主密码"
                    className="px-3 py-2 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
                  >
                    清除记住
                  </button>
                ) : null
              )}
            </div>
            {masterSet && (
              <div className="pt-1">
                {confirmClear ? (
                  <div className="p-2.5 rounded-md bg-[var(--color-danger)]/10 border border-[var(--color-danger)]/30 space-y-2">
                    <p className="text-[11px] text-[var(--color-danger)]">
                      确认清除主密码？清除后回到自动加密模式，随机密码写入钥匙串，之后启动自动解锁、无需手动输密码。已加密的 API Key 不受影响。
                    </p>
                    <div className="flex gap-2">
                      <button
                        onClick={() => setConfirmClear(false)}
                        disabled={busy}
                        className="flex-1 px-2 py-1.5 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
                      >
                        取消
                      </button>
                      <button
                        onClick={clearMaster}
                        disabled={busy}
                        className="flex-1 px-2 py-1.5 text-xs rounded-md bg-[var(--color-danger)] hover:opacity-90 disabled:opacity-50"
                      >
                        {busy ? "清除中..." : "确认清除"}
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    onClick={() => setConfirmClear(true)}
                    disabled={busy}
                    className="w-full px-3 py-1.5 text-xs rounded-lg text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10 disabled:opacity-50"
                  >
                    清除主密码（回到自动加密）
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
