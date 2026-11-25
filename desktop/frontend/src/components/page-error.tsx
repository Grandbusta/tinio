import { Component, type ErrorInfo, type ReactNode } from "react";
import { useLocation } from "react-router";

// A page that breaks shows that it did, and a way on, instead of leaving the window
// blank; the rest of the app keeps working. Moving to another page starts afresh.

class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("A page broke", error, info.componentStack);
  }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <div role="alert" className="mx-auto flex w-full max-w-[560px] flex-col items-start gap-3 px-6 py-16">
        <h1 className="font-display text-[24px] font-semibold tracking-[-0.02em]">This page didn&apos;t load</h1>
        <p className="m-0 text-[15px] text-muted">Something went wrong showing it. Your agents and everything they keep are fine.</p>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="min-h-11 rounded-lg bg-accent px-5 text-sm font-semibold text-accent-ink hover:bg-accent-hover"
          >
            Try again
          </button>
          <a href="/agents" className="inline-flex min-h-11 items-center rounded-lg border border-line-strong px-5 text-sm font-semibold text-fg no-underline hover:border-fg">
            Your chats
          </a>
        </div>
      </div>
    );
  }
}

export function PageError({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  return <Boundary key={pathname}>{children}</Boundary>;
}
