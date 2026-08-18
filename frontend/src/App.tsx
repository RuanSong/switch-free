import { useEffect, useState, useCallback } from "react";
import { System } from "@wailsio/runtime";
import { ProxyService, ConfigService, ProviderAPIService } from "../bindings/switchdev/service";
import type {
  AllCredStatus,
  LogStats,
} from "../bindings/switchdev/service/models";
import type { ProxyStatus, LogEntry } from "../bindings/switchdev/proxy/models";
import type { Config } from "../bindings/switchdev/config/models";
import { useWailsEvent } from "./hooks/useWailsEvent";
import Dashboard from "./components/Dashboard";
import Credentials from "./components/Credentials";
import Models from "./components/Models";
import Logs from "./components/Logs";
import Settings from "./components/Settings";
import UsageStats from "./components/UsageStats";
import Benchmark from "./components/Benchmark";
import ProviderAPI from "./components/ProviderAPI";
import ErrorBoundary from "./components/ErrorBoundary";

type Tab = "dashboard" | "providerapi" | "credentials" | "models" | "stats" | "logs" | "settings" | "benchmark";

const IS_MAC = System.IsMac();

export default function App() {
  const [tab, setTab] = useState<Tab>("dashboard");
  const [proxy, setProxy] = useState<ProxyStatus | null>(null);
  const [creds, setCreds] = useState<AllCredStatus | null>(null);
  const [stats, setStats] = useState<LogStats | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [loading, setLoading] = useState(true);

  // 拉取仪表盘聚合数据 + 配置。后端会静默探测本地已安装的 upstream。
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
    } catch (e) {
      console.error("拉取仪表盘失败", e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshDashboard();
  }, [refreshDashboard]);

  // 配置变化时刷新（设置页保存/重置后触发）
  useWailsEvent("config:change", (data) => {
    setConfig(data as Config);
  });

  // 托盘菜单"功能"子菜单切换主界面 tab（"保存方案..."入口也走 settings）
  useWailsEvent("navigate:tab", (data) => {
    const validTabs: Tab[] = ["dashboard", "providerapi", "credentials", "models", "stats", "logs", "benchmark", "settings"];
    const key = data as Tab;
    if (validTabs.includes(key)) {
      setTab(key);
    }
  });

  // 订阅事件
  useWailsEvent("proxy:status", (data) => {
    setProxy(data as ProxyStatus);
  });
  useWailsEvent("cred:change", (data) => {
    setCreds(data as AllCredStatus);
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
    { key: "providerapi", label: "供应商", icon: "🌐" },
    { key: "credentials", label: "凭据", icon: "🔑" },
    { key: "models", label: "模型", icon: "🤖" },
    { key: "stats", label: "统计", icon: "📈" },
    { key: "logs", label: "日志", icon: "📋" },
    { key: "benchmark", label: "测评", icon: "🏁" },
    { key: "settings", label: "设置", icon: "⚙️" },
  ];

  return (
    <div className="flex flex-col h-screen w-screen overflow-hidden">
      {/* macOS 隐藏标题栏：留 50px 不可见拖动区给红绿灯按钮；Windows/Linux 有原生标题栏，不留 */}
      {IS_MAC && <div className="h-[50px] flex-shrink-0 bg-[var(--color-surface)]" />}

      {/* 顶部栏：应用名（大字号）+ 横排导航 tab */}
      <header className="h-[56px] flex-shrink-0 flex items-center gap-6 px-6 bg-[var(--color-surface)] border-b border-[var(--color-border)]">
        <button
          type="button"
          onClick={() => ProviderAPIService.OpenURL("https://github.com/rosanruan/switch-dev")}
          title="在 GitHub 上查看项目"
          className="leading-tight flex items-baseline gap-2 bg-transparent border-0 p-0"
        >
          <span className="text-xl font-bold text-[var(--color-primary)] tracking-widest">SWITCH</span>
          <span className="text-base font-semibold text-[var(--color-text-dim)] tracking-widest">DEV</span>
        </button>
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
          ) : tab === "providerapi" ? (
            <ProviderAPI />
          ) : tab === "credentials" ? (
            <Credentials creds={creds} />
          ) : tab === "models" ? (
            <Models config={config} creds={creds} />
          ) : tab === "stats" ? (
            <UsageStats />
          ) : tab === "logs" ? (
            <Logs logs={logs} stats={stats} />
          ) : tab === "settings" ? (
            <Settings creds={creds} config={config} />
          ) : tab === "benchmark" ? (
            <Benchmark creds={creds} />
          ) : (
            <UsageStats />
          )}
          </ErrorBoundary>
        </main>
    </div>
  );
}
