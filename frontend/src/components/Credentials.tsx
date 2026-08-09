import { useState } from "react";
import { CredsService } from "../../bindings/switchfree/service";
import type { AllCredStatus } from "../../bindings/switchfree/service/models";
import type { CredStatusInfo } from "../../bindings/switchfree/creds/models";
import CopyButton from "./CopyButton";

interface Props {
  creds: AllCredStatus | null;
}

// 上游 -> 首字母圆形图标（与仪表盘一致：蓝/琥珀/紫）
const AGENT_ICON: Record<string, { initial: string; color: string }> = {
  joycode: { initial: "J", color: "bg-[var(--color-primary)]/20 text-[var(--color-primary)]" },
  deveco: { initial: "D", color: "bg-amber-500/20 text-amber-400" },
  opencode: { initial: "O", color: "bg-purple-500/20 text-purple-400" },
  workbuddy: { initial: "W", color: "bg-pink-500/20 text-pink-400" },
};

export default function Credentials({ creds }: Props) {
  return (
    <div className="p-6 space-y-6">
      <div className="space-y-4">
        <CredCard upstream="joycode" status={creds?.joycode ?? null} />
        <CredCard upstream="deveco" status={creds?.deveco ?? null} />
        <CredCard upstream="opencode" status={creds?.opencode ?? null} />
        <CredCard upstream="workbuddy" status={creds?.workbuddy ?? null} />
      </div>
    </div>
  );
}

function CredCard({ upstream, status }: { upstream: string; status: CredStatusInfo | null }) {
  const [busy, setBusy] = useState(false);
  const valid = status?.valid ?? false;
  const installed = status?.installed ?? false;
  const isGui = status?.agentType === "gui";
  const name = status ? nameFromUpstream(upstream) : upstream;
  const icon = AGENT_ICON[upstream] ?? { initial: name[0]?.toUpperCase() ?? "?", color: "bg-[var(--color-surface-2)] text-[var(--color-text-dim)]" };
  const installCmd = status?.installCmd ?? "";
  const loginCmd = status?.loginCmd ?? "";

  const refresh = async () => {
    setBusy(true);
    try {
      await CredsService.RefreshCreds(upstream);
    } finally {
      setBusy(false);
    }
  };

  // 状态徽章：三态
  let badge = (
    <span className="text-xs px-2 py-0.5 rounded-full bg-[var(--color-danger)]/20 text-[var(--color-danger)]">
      ✗ 未安装
    </span>
  );
  if (valid) {
    badge = (
      <span className="text-xs px-2 py-0.5 rounded-full bg-[var(--color-success)]/20 text-[var(--color-success)]">
        ✓ 有效
      </span>
    );
  } else if (installed) {
    badge = (
      <span className="text-xs px-2 py-0.5 rounded-full bg-[var(--color-warning)]/20 text-[var(--color-warning)]">
        ⚠ 已装未登录
      </span>
    );
  }

  return (
    <div className="bg-[var(--color-surface)] rounded-xl p-5 border border-[var(--color-border)]">
      {/* 标题行 */}
      <div className="flex items-start justify-between mb-3">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <span className={`w-7 h-7 rounded-full flex items-center justify-center text-sm font-bold ${icon.color}`}>
              {icon.initial}
            </span>
            <span className="font-semibold text-lg">{name}</span>
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)] uppercase">
              {isGui ? "GUI" : "CLI"}
            </span>
            {badge}
          </div>
        </div>
        <div className="flex gap-2">
          {installed && (
            <button
              onClick={refresh}
              disabled={busy}
              className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)] disabled:opacity-50"
            >
              {busy ? "..." : "刷新"}
            </button>
          )}
          {status?.loginUrl && (installed || isGui) && (
            <button
              onClick={() => CredsService.OpenLoginURL(upstream)}
              className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
            >
              登录页
            </button>
          )}
        </div>
      </div>

      {/* 三态操作区 */}
      {!installed ? (
        // 未安装：显示安装命令/下载
        <InstallGuide
          isGui={isGui}
          installCmd={installCmd}
          downloadUrl={status?.downloadUrl ?? ""}
          upstream={upstream}
        />
      ) : !valid ? (
        // 已装未登录：显示登录命令
        <LoginGuide isGui={isGui} loginCmd={loginCmd} upstream={upstream} />
      ) : (
        // 正常：显示凭据详情
        <Details status={status} />
      )}
    </div>
  );
}

function InstallGuide({
  isGui,
  installCmd,
  downloadUrl,
  upstream,
}: {
  isGui: boolean;
  installCmd: string;
  downloadUrl: string;
  upstream: string;
}) {
  return (
    <div className="mt-1 pt-3 border-t border-[var(--color-border)]">
      <div className="text-xs text-[var(--color-text-dim)] mb-2">未检测到该工具，请先安装：</div>
      {isGui ? (
        <button
          onClick={() => CredsService.OpenDownloadURL(upstream)}
          className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90"
        >
          ⬇ 打开下载页
        </button>
      ) : installCmd ? (
        <div className="flex items-center gap-2">
          <code className="flex-1 px-2.5 py-1.5 rounded-md bg-[var(--color-surface-2)] font-mono text-xs truncate">
            {installCmd}
          </code>
          <CopyButton text={installCmd} />
        </div>
      ) : downloadUrl ? (
        <button
          onClick={() => CredsService.OpenDownloadURL(upstream)}
          className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90"
        >
          ⬇ 打开下载页
        </button>
      ) : null}
    </div>
  );
}

function LoginGuide({ isGui, loginCmd, upstream }: { isGui: boolean; loginCmd: string; upstream: string }) {
  return (
    <div className="mt-1 pt-3 border-t border-[var(--color-border)]">
      <div className="text-xs text-[var(--color-text-dim)] mb-2">已安装但凭据无效，请登录：</div>
      <div className="flex items-center gap-2 flex-wrap">
        {isGui ? (
          <span className="text-sm text-[var(--color-text)]">{loginCmd}</span>
        ) : (
          <code className="px-2.5 py-1.5 rounded-md bg-[var(--color-surface-2)] font-mono text-xs">
            {loginCmd}
          </code>
        )}
        {!isGui && loginCmd && <CopyButton text={loginCmd} label="复制登录命令" />}
      </div>
    </div>
  );
}

function Details({ status }: { status: CredStatusInfo | null }) {
  return (
    <div className="grid grid-cols-2 gap-3 text-sm">
      <Detail label="用户" value={status?.userId || "-"} />
      <Detail label="来源" value={status?.source || "-"} noTruncate />
      <Detail label="key 预览" value={status?.keyPreview || "-"} mono />
      <Detail label="过期时间" value={status?.expiresAt || "-"} />
      <Detail label="最近校验" value={status?.lastCheck || "-"} />
    </div>
  );
}

function Detail({ label, value, mono, noTruncate }: { label: string; value: string; mono?: boolean; noTruncate?: boolean }) {
  return (
    <div>
      <div className="text-xs text-[var(--color-text-dim)] mb-0.5">{label}</div>
      <div className={`${noTruncate ? "" : "truncate"} ${mono ? "font-mono text-xs" : ""}`}>{value}</div>
    </div>
  );
}

function nameFromUpstream(upstream: string): string {
  switch (upstream) {
    case "joycode":
      return "JoyCode";
    case "deveco":
      return "DevEco Code";
    case "opencode":
      return "OpenCode Zen";
    case "workbuddy":
      return "WorkBuddy";
    default:
      return upstream;
  }
}
