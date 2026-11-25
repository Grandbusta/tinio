import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { call } from "./lib/api";
import "./globals.css";

// Inside the desktop app's window, Wails gives window.runtime: what the API says live
// comes as its events, and links out open in your browser.
type WailsRuntime = {
  EventsOn: (name: string, cb: (data: unknown) => void) => () => void;
  EventsEmit: (name: string, data?: unknown) => void;
  BrowserOpenURL: (url: string) => void;
};
// Wails puts its runtime only into a page at an address ending in "/", so a page loaded
// at any other (reloading a chat) loads it itself. In a plain browser there's none.
async function runtimeOf(): Promise<WailsRuntime | undefined> {
  const w = window as unknown as { runtime?: WailsRuntime };
  if (w.runtime) return w.runtime;
  for (const src of ["/wails/ipc.js", "/wails/runtime.js"]) {
    const ok = await new Promise<boolean>((done) => {
      const s = document.createElement("script");
      s.src = src;
      s.onload = () => done(true);
      s.onerror = () => done(false);
      document.head.appendChild(s);
    });
    if (!ok) return undefined;
  }
  return w.runtime;
}

function inTheWindow(wails: WailsRuntime) {
  // A chat's stream is an EventSource on the web. The window can't hold one open on
  // every system, so here it's the app's events, for that agent: "chat", "draft" and "making" as
  // the stream sends them, and "behind" closing it, so the chat catches up and opens
  // it again.
  class AppEvents extends EventTarget {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSED = 2;
    readyState = 0;
    onopen: ((e: Event) => void) | null = null;
    onerror: ((e: Event) => void) | null = null;
    private off: (() => void)[] = [];
    constructor(url: string) {
      super();
      const agent = decodeURIComponent(/\/agents\/([^/]+)\/stream/.exec(url)?.[1] ?? "");
      const send = (type: string, data: unknown) => this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data) }));
      this.off.push(
        wails.EventsOn("chat", (d) => {
          const e = d as { agent: string; row: unknown };
          if (e.agent === agent) send("chat", e.row);
        }),
        wails.EventsOn("draft", (d) => {
          const e = d as { agent: string; text: string };
          if (e.agent === agent) send("draft", { text: e.text });
        }),
        wails.EventsOn("making", (d) => {
          const e = d as { agent: string; text: string };
          if (e.agent === agent) send("making", { text: e.text });
        }),
        wails.EventsOn("behind", () => {
          this.close();
          this.onerror?.(new Event("error"));
        }),
      );
      setTimeout(() => {
        if (this.readyState !== 0) return;
        this.readyState = 1;
        this.onopen?.(new Event("open"));
      });
    }
    close() {
      this.readyState = 2;
      this.off.forEach((f) => f());
      this.off = [];
    }
  }
  (window as unknown as { EventSource: unknown }).EventSource = AppEvents;

  document.addEventListener("click", (e) => {
    const a = (e.target as Element | null)?.closest?.("a[href]") as HTMLAnchorElement | null;
    if (!a) return;
    const href = a.getAttribute("href") ?? "";
    // A file of an agent's: the window can't download, so the app asks where to save it.
    if (a.hasAttribute("download") && href.startsWith("/api/")) {
      e.preventDefault();
      void call("/app/save", { url: href, name: a.getAttribute("download") ?? "" }).then((res) => {
        if (!res.ok) window.alert(res.error.message);
      });
      return;
    }
    if (!/^(https?|mailto):/.test(a.href) || a.origin === window.location.origin) return;
    e.preventDefault();
    wails.BrowserOpenURL(a.href);
  });

  // Whether you're looking at the window: when you aren't, what an agent says is a
  // notification too.
  const looking = () => {
    try {
      wails.EventsEmit("focus", document.visibilityState === "visible" && document.hasFocus());
    } catch {
      setTimeout(looking, 500); // its connection isn't open yet (wails dev in a browser)
    }
  };
  window.addEventListener("focus", looking);
  window.addEventListener("blur", looking);
  document.addEventListener("visibilitychange", looking);
  looking();

  // A notification clicked: that agent's chat.
  wails.EventsOn("open", (agent) => {
    const to = `/agents/${encodeURIComponent(String(agent))}`;
    if (window.location.pathname === to) return;
    window.history.pushState(null, "", to);
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
}

runtimeOf().then((wails) => {
  if (wails) inTheWindow(wails);
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
});
