import { Component, ReactNode } from "react";

interface State {
  hasError: boolean;
  error: Error | null;
}

export default class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: { componentStack: string }) {
    console.error("渲染错误:", error, info);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="p-6 space-y-3">
          <h2 className="text-lg font-bold text-[var(--color-danger)]">⚠️ 页面渲染出错</h2>
          <pre className="text-xs font-mono p-3 rounded bg-[var(--color-surface)] text-[var(--color-danger)] overflow-x-auto whitespace-pre-wrap">
            {this.state.error?.message}
            {"\n\n"}
            {this.state.error?.stack}
          </pre>
          <button
            onClick={() => this.setState({ hasError: false, error: null })}
            className="px-3 py-1.5 text-sm rounded-lg bg-[var(--color-primary)] hover:opacity-90"
          >
            重试
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
