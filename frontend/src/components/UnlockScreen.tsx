import { useState } from "react";
import { FreeAPIService } from "../../bindings/switchfree/service";

interface Props {
  onUnlocked: () => void;
}

// 启动锁界面：钥匙串读不到主密码时显示，要求输入主密码解锁。
export default function UnlockScreen({ onUnlocked }: Props) {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [showRecover, setShowRecover] = useState(false);

  // 恢复码流程
  const [recCode, setRecCode] = useState("");
  const [newPass, setNewPass] = useState("");
  const [newPass2, setNewPass2] = useState("");
  const [recoverMsg, setRecoverMsg] = useState("");

  // 销毁配置二次确认
  const [confirmReset, setConfirmReset] = useState(false);
  const [resetting, setResetting] = useState(false);

  const doReset = async () => {
    setResetting(true);
    try {
      await FreeAPIService.ResetVault();
      onUnlocked(); // 重置后视为"解锁"，进入空列表
    } catch (e) {
      setError(String(e));
    } finally {
      setResetting(false);
    }
  };

  const doUnlock = async () => {
    if (!password) return;
    setBusy(true);
    setError("");
    try {
      await FreeAPIService.Unlock(password);
      onUnlocked();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const doRecover = async () => {
    if (newPass.length < 6) {
      setRecoverMsg("新密码至少 6 位");
      return;
    }
    if (newPass !== newPass2) {
      setRecoverMsg("两次输入的新密码不一致");
      return;
    }
    setBusy(true);
    setRecoverMsg("");
    try {
      // 恢复成功后返回新恢复码，这里用新密码直接解锁（新恢复码会在进入后由安全设置查看）
      await FreeAPIService.RecoverWithCode(recCode.trim(), newPass, false);
      await FreeAPIService.Unlock(newPass);
      onUnlocked();
    } catch (e) {
      setRecoverMsg(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex items-center justify-center min-h-[60vh]">
      <div className="w-full max-w-sm p-6 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)]">
        <h2 className="text-base font-semibold mb-1">🔒 解锁凭据</h2>
        <p className="text-xs text-[var(--color-text-dim)] mb-4">
          本地 API Key 已加密，请输入主密码解锁。
        </p>

        {!showRecover ? (
          <>
            <input
              type="password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && doUnlock()}
              placeholder="主密码"
              className="w-full px-3 py-2 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] mb-3"
            />
            {error && <p className="text-xs text-[var(--color-danger)] mb-2">{error}</p>}
            <button
              onClick={doUnlock}
              disabled={busy || !password}
              className="w-full px-3 py-2 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
            >
              {busy ? "解锁中..." : "解锁"}
            </button>
            <button
              onClick={() => setShowRecover(true)}
              className="w-full mt-2 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]"
            >
              忘记密码？用恢复码重置
            </button>

            {!confirmReset ? (
              <button
                onClick={() => setConfirmReset(true)}
                className="w-full mt-1 text-xs text-[var(--color-danger)]/80 hover:text-[var(--color-danger)]"
              >
                密码和恢复码都丢了？销毁现有配置
              </button>
            ) : (
              <div className="mt-3 p-2.5 rounded-md border border-[var(--color-danger)]/40 bg-[var(--color-danger)]/5">
                <p className="text-xs text-[var(--color-danger)] mb-2 leading-relaxed">
                  确认销毁？所有已保存的供应商和 API Key 将被永久删除，无法恢复。
                </p>
                <div className="flex gap-2">
                  <button
                    onClick={() => setConfirmReset(false)}
                    disabled={resetting}
                    className="flex-1 px-2 py-1.5 text-xs rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
                  >
                    取消
                  </button>
                  <button
                    onClick={doReset}
                    disabled={resetting}
                    className="flex-1 px-2 py-1.5 text-xs rounded-md bg-[var(--color-danger)] text-white hover:opacity-90 disabled:opacity-50"
                  >
                    {resetting ? "销毁中..." : "确认销毁"}
                  </button>
                </div>
              </div>
            )}
          </>
        ) : (
          <>
            <p className="text-xs text-[var(--color-text-dim)] mb-3">
              输入你保存的恢复码，设置一个新主密码。恢复码用过后将作废。
            </p>
            <input
              type="text"
              value={recCode}
              onChange={(e) => setRecCode(e.target.value)}
              placeholder="恢复码（xxxxxx-...）"
              className="w-full px-3 py-2 text-sm font-mono rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] mb-2"
            />
            <input
              type="password"
              value={newPass}
              onChange={(e) => setNewPass(e.target.value)}
              placeholder="新主密码（至少 6 位）"
              className="w-full px-3 py-2 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] mb-2"
            />
            <input
              type="password"
              value={newPass2}
              onChange={(e) => setNewPass2(e.target.value)}
              placeholder="再次输入新密码"
              className="w-full px-3 py-2 text-sm rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] mb-2"
            />
            {recoverMsg && <p className="text-xs text-[var(--color-danger)] mb-2">{recoverMsg}</p>}
            <button
              onClick={doRecover}
              disabled={busy}
              className="w-full px-3 py-2 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 disabled:opacity-50"
            >
              {busy ? "处理中..." : "重置并解锁"}
            </button>
            <button
              onClick={() => setShowRecover(false)}
              className="w-full mt-2 text-xs text-[var(--color-text-dim)] hover:text-[var(--color-text)]"
            >
              返回
            </button>
          </>
        )}
      </div>
    </div>
  );
}
