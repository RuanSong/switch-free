import { useEffect, useState, useCallback } from "react";
import { ProxyService, CredsService, ConfigService } from "../bindings/switchfree/service";
import type {
  AllCredStatus,
  LogStats,
  AgentDetail,
} from "../bindings/switchfree/service/models";
import type { ProxyStatus, LogEntry } from "../bindings/switchfree/proxy/models";
import type { Config } from "../bindings/switchfree/config/models";
import { useWailsEvent } from "./hooks/useWailsEvent";
import Dashboard from "./components/Dashboard";
import Credentials from "./components/Credentials";
import Models from "./components/Models";
import Logs from "./components/Logs";
import SetupGuide from "./components/SetupGuide";
import Settings from "./components/Settings";
import UsageStats from "./components/UsageStats";
import Benchmark from "./components/Benchmark";
import ErrorBoundary from "./components/ErrorBoundary";

type Tab = "dashboard" | "credentials" | "models" | "stats" | "logs" | "settings" | "benchmark";

export default function App() {
  const [tab, setTab] = useState<Tab>("dashboard");
  const [proxy, setProxy] = useState<ProxyStatus | null>(null);
  const [creds, setCreds] = useState<AllCredStatus | null>(null);
  const [agents, setAgents] = useState<AgentDetail[]>([]);
  const [showSetup, setShowSetup] = useState(false);
  const [stats, setStats] = useState<LogStats | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [loading, setLoading] = useState(true);

  // 拉取 agent 列表（实时探测安装状态）
  const fetchAgents = useCallback(async () => {
    try {
      const list = await CredsService.GetAgents();
      const next = (list ?? []).filter((a): a is AgentDetail => a !== null);
      setAgents(next);
      return next;
    } catch (e) {
      console.error("拉取 agent 列表失败", e);
      return [];
    }
  }, []);

  // 重新探测安装状态 + 校验已安装但未登录的凭据
  const rescanAgents = useCallback(async () => {
    await CredsService.RefreshAllCreds().catch(() => {});
    await fetchAgents();
  }, [fetchAgents]);

  // 拉取仪表盘聚合数据 + agent 列表 + 配置
  const refreshDashboard = useCallback(async () => {
    try {
      const [d, cfg] = await Promise.all([
        ProxyService.GetDashboard(),
        ConfigService.GetConfig(),
      ]);
      if (d) {
        setProxy(d.proxy);
        setCreds(d.creds);
        setStats(d.stats);
        setLogs(d.recentLogs?.filter((l): l is LogEntry => l !== null) ?? []);
      }
      if (cfg) setConfig(cfg);
      const agents = await fetchAgents();
      // 首次加载：若三上游全部未安装，自动弹引导
      if (agents.length > 0 && agents.every((a) => !a.installed)) {
        setShowSetup(true);
      }
    } catch (e) {
      console.error("拉取仪表盘失败", e);
    } finally {
      setLoading(false);
    }
  }, [fetchAgents]);

  useEffect(() => {
    refreshDashboard();
  }, [refreshDashboard]);

  // 配置变化时刷新（设置页保存/重置后触发）
  useWailsEvent("config:change", (data) => {
    setConfig(data as Config);
  });

  // 订阅事件
  useWailsEvent("proxy:status", (data) => {
    setProxy(data as ProxyStatus);
  });
  useWailsEvent("cred:change", async (data) => {
    setCreds(data as AllCredStatus);
    // 凭据变化时刷新 agent 列表（拿到最新 installed/valid）
    await fetchAgents();
  });
  useWailsEvent("log:new", (data) => {
    const entry = data as LogEntry;
    setLogs((prev) => [entry, ...prev].slice(0, 200));
    setStats((prev) => {
      if (!prev) return prev;
      const next = { ...prev, total: prev.total + 1 };
      if (entry.status === "success") next.success++;
      else if (entry.status === "error") next.error++;
      else if (entry.status === "auth_error") next.authError++;
      return next;
    });
  });

  const navItems: { key: Tab; label: string; icon: string }[] = [
    { key: "dashboard", label: "仪表盘", icon: "📊" },
    { key: "credentials", label: "凭据", icon: "🔑" },
    { key: "models", label: "模型", icon: "🤖" },
    { key: "stats", label: "统计", icon: "📈" },
    { key: "logs", label: "日志", icon: "📋" },
    { key: "benchmark", label: "测评", icon: "🏁" },
    { key: "settings", label: "设置", icon: "⚙️" },
  ];

  return (
    <div className="flex flex-col h-screen w-screen overflow-hidden">
      {/* macOS 隐藏标题栏的不可见拖动区（高 50px）+ 红绿灯按钮区，顶部栏避开 */}
      <div className="h-[50px] flex-shrink-0 bg-[var(--color-surface)]" />

      {/* 顶部栏：应用名（大字号）+ 横排导航 tab */}
      <header className="h-[56px] flex-shrink-0 flex items-center gap-6 px-6 bg-[var(--color-surface)] border-b border-[var(--color-border)]">
        <div className="leading-tight flex items-baseline gap-2">
          <span className="text-xl font-bold text-[var(--color-primary)] tracking-widest">SWITCH</span>
          <span className="text-base font-semibold text-[var(--color-text-dim)] tracking-widest">FREE</span>
        </div>
        <nav className="flex items-center gap-1 ml-6">
          {navItems.map((item) => (
            <button
              key={item.key}
              onClick={() => setTab(item.key)}
              className={`px-3 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-1.5 ${
                tab === item.key
                  ? "text-[var(--color-primary)] bg-[var(--color-primary)]/10"
                  : "text-[var(--color-text-dim)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-text)]"
              }`}
            >
              <span className="text-base leading-none">{item.icon}</span>
              {item.label}
            </button>
          ))}
        </nav>
      </header>

      {/* 主内容区 */}
      <main className="flex-1 overflow-y-auto">
        <ErrorBoundary>
          {loading ? (
            <div className="flex items-center justify-center h-full text-[var(--color-text-dim)]">
              加载中...
            </div>
          ) : tab === "dashboard" ? (
            <Dashboard
              proxy={proxy}
              creds={creds}
              config={config}
              onGoCredentials={() => setTab("credentials")}
            />
          ) : tab === "credentials" ? (
            <Credentials creds={creds} />
          ) : tab === "models" ? (
            <Models config={config} />
          ) : tab === "stats" ? (
            <UsageStats />
          ) : tab === "logs" ? (
            <Logs logs={logs} stats={stats} />
          ) : tab === "settings" ? (
            <Settings creds={creds} config={config} />
          ) : tab === "benchmark" ? (
            <Benchmark />
          ) : (
            <UsageStats />
          )}
          </ErrorBoundary>
        </main>

      {/* 首次启动引导弹窗：三上游全部未安装时自动弹出 */}
      {showSetup && agents.length > 0 && (
        <SetupGuide agents={agents} onClose={() => setShowSetup(false)} onRefresh={rescanAgents} />
      )}
    </div>
  );
}
