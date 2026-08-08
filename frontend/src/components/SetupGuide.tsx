import { useState } from "react";
import { CredsService } from "../../bindings/switchfree/service";
import type { AgentDetail } from "../../bindings/switchfree/service/models";
import CopyButton from "./CopyButton";

interface Props {
  agents: AgentDetail[];
  onClose: () => void;
}

export default function SetupGuide({ agents, onClose }: Props) {
  const [dismissed, setDismissed] = useState(false);

  // 全部就绪则不显示
  const allReady = agents.every((a) => a.valid);
  if (allReady || dismissed) {
    return null;
  }

  const handleClose = () => {
    setDismissed(true);
    onClose();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-[var(--color-surface)] rounded-2xl border border-[var(--color-border)] w-[640px] max-h-[80vh] overflow-y-auto shadow-2xl">
        {/* 头部 */}
        <div className="flex items-center justify-between p-5 border-b border-[var(--color-border)] sticky top-0 bg-[var(--color-surface)]">
          <div>
            <h2 className="text-lg font-bold">🚀 欢迎使用 Switch Free</h2>
            <p className="text-xs text-[var(--color-text-dim)] mt-1">
              代理通过复用本地 AI 编程工具的登录态来调用大模型。请至少安装并登录一个工具。
            </p>
          </div>
          <button
            onClick={handleClose}
            className="w-8 h-8 flex items-center justify-center rounded-lg text-[var(--color-text-dim)] hover:bg-[var(--color-surface-2)]"
          >
            ✕
          </button>
        </div>

        {/* agent 列表 */}
        <div className="p-5 space-y-4">
          {agents.map((agent) => (
            <AgentSetupCard key={agent.upstream} agent={agent} />
          ))}
        </div>

        {/* 底部 */}
        <div className="p-5 border-t border-[var(--color-border)] flex items-center gap-4 sticky bottom-0 bg-[var(--color-surface)]">
          <p className="text-xs text-[var(--color-text-dim)] flex-1 leading-relaxed">
            💡 装一个登录一个，代理会自动恢复（无需重启）。auto 模式：DevEco 主力，失败降级 JoyCode。
          </p>
          <button
            onClick={handleClose}
            className="px-4 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90 whitespace-nowrap shrink-0"
          >
            稍后再说
          </button>
        </div>
      </div>
    </div>
  );
}

function AgentSetupCard({ agent }: { agent: AgentDetail }) {
  const isGui = agent.type === "gui";

  // 状态徽章
  let badge = (
    <span className="text-xs px-2 py-0.5 rounded-full bg-[var(--color-danger)]/20 text-[var(--color-danger)]">
      ✗ 未安装
    </span>
  );
  if (agent.valid) {
    badge = (
      <span className="text-xs px-2 py-0.5 rounded-full bg-[var(--color-success)]/20 text-[var(--color-success)]">
        ✓ 已就绪
      </span>
    );
  } else if (agent.installed) {
    badge = (
      <span className="text-xs px-2 py-0.5 rounded-full bg-[var(--color-warning)]/20 text-[var(--color-warning)]">
        ⚠ 已装未登录
      </span>
    );
  }

  return (
    <div className="bg-[var(--color-bg)] rounded-xl p-4 border border-[var(--color-border)]">
      {/* 标题行 */}
      <div className="flex items-center gap-2 mb-2">
        <span className="font-semibold">{agent.name}</span>
        <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--color-surface-2)] text-[var(--color-text-dim)] uppercase">
          {isGui ? "GUI" : "CLI"}
        </span>
        {badge}
      </div>
      <p className="text-xs text-[var(--color-text-dim)] mb-3">{agent.desc}</p>

      {/* 步骤 */}
      <div className="space-y-2">
        {/* 步骤 1：安装 */}
        <div className="flex items-start gap-2">
          <span className="text-xs text-[var(--color-text-dim)] mt-1 w-4">1</span>
          <div className="flex-1">
            <div className="text-xs text-[var(--color-text-dim)] mb-1">安装</div>
            {isGui ? (
              <button
                onClick={() => CredsService.OpenDownloadURL(agent.upstream)}
                className="text-sm px-3 py-1.5 rounded-lg bg-[var(--color-primary)] hover:opacity-90"
              >
                ⬇ 打开下载页
              </button>
            ) : agent.installCmd ? (
              <div className="flex items-center gap-2">
                <code className="flex-1 px-2.5 py-1.5 rounded-md bg-[var(--color-surface-2)] font-mono text-xs truncate">
                  {agent.installCmd}
                </code>
                <CopyButton text={agent.installCmd} />
              </div>
            ) : null}
          </div>
        </div>

        {/* 步骤 2：登录（已装才显示） */}
        {(agent.installed || isGui) && (
          <div className="flex items-start gap-2">
            <span className="text-xs text-[var(--color-text-dim)] mt-1 w-4">2</span>
            <div className="flex-1">
              <div className="text-xs text-[var(--color-text-dim)] mb-1">登录</div>
              <div className="flex items-center gap-2 flex-wrap">
                {isGui ? (
                  <span className="text-xs text-[var(--color-text)]">{agent.loginCmd}</span>
                ) : (
                  <code className="px-2.5 py-1.5 rounded-md bg-[var(--color-surface-2)] font-mono text-xs">
                    {agent.loginCmd}
                  </code>
                )}
                {!isGui && agent.loginCmd && <CopyButton text={agent.loginCmd} label="复制登录命令" />}
                {agent.loginUrl && (
                  <button
                    onClick={() => CredsService.OpenLoginURL(agent.upstream)}
                    className="text-xs px-2.5 py-1 rounded-md bg-[var(--color-surface-2)] hover:bg-[var(--color-border)]"
                  >
                    打开登录页
                  </button>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
