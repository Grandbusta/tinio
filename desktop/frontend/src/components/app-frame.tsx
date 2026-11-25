"use client";

import { Mark, Portrait, portraits, type Character as Who } from "@tinio/brand";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { flushSync } from "react-dom";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { useTheme } from "@/components/theme";
import { NoKey, ready, type Sources } from "@/components/first-run";
import { MeterLine } from "@/components/usage";
import { useAccount } from "@/lib/account";
import { call, prefetch, type Account, type Character, type Overview, type Usage } from "@/lib/api";
import { plainText } from "@/components/message-text";

// The app's frame, as on the canvas: the rail, your agents' list (each one's chat), the
// page, and beside a chat the panel about the agent. On a phone, the list and a page are
// screens of their own. Pages read what the frame loaded with useFrame.

export type Summary = Overview["agents"][number];

type Frame = {
  account: Account | null;
  overview: Overview | null;
  usage: Usage | null; // the week your agents share
  keySet: boolean | null; // whether they have something to think with, Claude Code or your key (null until known): without it, nobody can reply
  agent: Summary | null; // the open agent, once loaded
  missing: boolean; // the open agent isn't among yours
  about: (a: Summary) => string;
  aboutOpen: boolean;
  toggleAbout: () => void;
  refresh: () => void;
};

const FrameContext = createContext<Frame | null>(null);

export function useFrame(): Frame {
  const f = useContext(FrameContext);
  if (!f) throw new Error("useFrame outside AppFrame");
  return f;
}

const refreshEvery = 30_000;

// The chat list folds to a column of faces, from its button or ⌘\. Folded or not is
// remembered on this computer. A job or a work opened folds it on its own, as does a
// window between 901 and 1100px wide; there, unfolding is for now only, and the list
// opens over the page.
const foldKey = "tinio.chats-folded";
const between = "(min-width: 901px) and (max-width: 1100px)";

function foldedBefore() {
  try {
    return localStorage.getItem(foldKey) === "1";
  } catch {
    return false;
  }
}

const fullWidth: Page[] = ["you", "settings", "connections"];

type Page = "list" | "jobs" | "you" | "settings" | "connections" | "chat" | "today" | "works" | "growth" | "judgment" | "mind" | "memory" | "character" | "settings" | "skills";

// where is the agent open and the page shown, from the path: /agents is the list, /you
// the week's usage, /settings your keys, models and the app, /connections[/...] the
// accounts you connect, /agents/<id>
// their chat, /agents/<id>/<page>[/...] one of their pages.
function where(path: string): { open?: string; page: Page } {
  const [, top, id, page] = path.split("/");
  if (top === "you" || top === "settings" || top === "connections" || top === "today") return { page: top };
  if (!id) return { page: "list" };
  return { open: decodeURIComponent(id), page: (page as Page) || "chat" };
}

// AppFrame is the app around every page under /agents, kept by its layout from page to
// page: the rail, your agents' list and what it has loaded stay put, and only the page
// area changes, easing in.
export function AppFrame({ children }: { children: ReactNode }) {
  const path = usePathname();
  const { open, page } = where(path);
  const router = useRouter();
  const account = useAccount();
  const [overview, setOverview] = useState<Overview | null>(null);
  const [usage, setUsage] = useState<Usage | null>(null);
  const [keySet, setKeySet] = useState<boolean | null>(null);
  const [characters, setCharacters] = useState<Character[]>([]);
  const [aboutOpen, setAboutOpen] = useState(false);
  // showAbout opens or closes About with the same easing as moving between pages, where
  // the browser can.
  // About open is in the address (?about), so going back from one of its pages, by its
  // Back or the browser's, finds it open again.
  const [aboutEases, setAboutEases] = useState(false); // opened by arriving, so it eases in like a page
  const showAbout = useCallback((open: boolean) => {
    window.history.replaceState(null, "", open ? `${window.location.pathname}?about` : window.location.pathname);
    setAboutEases(false);
    if (!document.startViewTransition) return setAboutOpen(open);
    document.startViewTransition(() => flushSync(() => setAboutOpen(open)));
  }, []);

  const refresh = useCallback(() => {
    call<Overview>("/overview").then((res) => res.ok && setOverview(res.data));
    call<Usage>("/usage").then((res) => res.ok && setUsage(res.data));
    call<Sources>("/sources").then((res) => res.ok && setKeySet(ready(res.data)));
  }, []);

  // With no agents, the chats have nothing to show: raise one first. You and connections
  // work without one.
  useEffect(() => {
    if (overview?.agents.length === 0 && !fullWidth.includes(page)) router.replace("/raise");
  }, [overview, page, router]);

  useEffect(() => {
    if (!account) return;
    refresh();
    call<{ characters: Character[] }>("/characters").then((res) => res.ok && setCharacters(res.data.characters));
    const every = setInterval(refresh, refreshEvery);
    window.addEventListener("focus", refresh);
    return () => {
      clearInterval(every);
      window.removeEventListener("focus", refresh);
    };
  }, [account, refresh]);

  const [chosenFold, setChosenFold] = useState(foldedBefore);
  const [narrow, setNarrow] = useState(() => window.matchMedia(between).matches);
  const [foldNow, setFoldNow] = useState<boolean | null>(null); // unfolded or folded for this page only
  useEffect(() => {
    const m = window.matchMedia(between);
    const on = () => setNarrow(m.matches);
    m.addEventListener("change", on);
    return () => m.removeEventListener("change", on);
  }, []);
  useEffect(() => setFoldNow(null), [path]);
  const onOne = page === "jobs" || (page === "works" && path.split("/").length > 4);
  const auto = narrow || onOne;
  const folded = foldNow ?? (chosenFold || auto);
  const toggleFold = useCallback(() => {
    if (auto) return setFoldNow(!folded);
    setChosenFold(!folded);
    try {
      localStorage.setItem(foldKey, folded ? "0" : "1");
    } catch {
      // not remembered, then
    }
  }, [auto, folded]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "\\") {
        e.preventDefault();
        toggleFold();
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [toggleFold]);
  const over = narrow && !folded && page !== "list"; // the list opened over the page
  useEffect(() => {
    if (!over) return;
    const close = (e: KeyboardEvent) => e.key === "Escape" && setFoldNow(true);
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [over]);

  const agent = overview?.agents.find((a) => a.id === open) ?? null;
  const frame: Frame = {
    account,
    overview,
    usage,
    keySet,
    agent,
    missing: !!open && !!overview && !agent,
    about: (a) => characters.find((c) => c.key === a.character)?.about ?? "",
    aboutOpen,
    toggleAbout: () => showAbout(!aboutOpen),
    refresh,
  };

  // About opens in the page's place, over the chat, at every width; the page stays
  // underneath, so closing it is back where you were. On a phone the list hides too.
  const showsAbout = !!agent && aboutOpen;
  // A new page starts at its top, with About open only if the address says so.
  const pageArea = useRef<HTMLElement>(null);
  useEffect(() => {
    const open = new URLSearchParams(window.location.search).has("about");
    setAboutOpen(open);
    setAboutEases(open);
    pageArea.current?.scrollTo({ top: 0 });
  }, [path]);
  useEffect(() => {
    if (!showsAbout) return;
    const close = (e: KeyboardEvent) => e.key === "Escape" && showAbout(false);
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [showsAbout, showAbout]);
  return (
    <FrameContext.Provider value={frame}>
      <div className="relative flex h-dvh overflow-hidden bg-bg text-fg">
        <Rail account={account} on={page === "you" || page === "settings" || page === "connections" || page === "today" ? page : "chats"} />
        {/* You and connections aren't chats: they have the whole width, with the rail to go back. */}
        {!fullWidth.includes(page) && folded && <Faces open={open} unfold={toggleFold} />}
        {!fullWidth.includes(page) && (!folded || page === "list") && (
          <List
            open={open}
            fold={toggleFold}
            over={over}
            className={`${folded ? "min-[901px]:hidden" : page === "list" ? "" : "max-[900px]:hidden"} ${over ? "absolute inset-y-0 left-[76px] z-30 bg-bg shadow-2xl" : ""}`}
          />
        )}
        <main
          ref={pageArea}
          className={`flex min-w-0 flex-1 flex-col overflow-y-auto [view-transition-name:page] ${page === "chat" ? "bg-chat" : ""} ${showsAbout ? "hidden" : ""}`}
        >
          <div key={path} className="page-in flex min-w-0 flex-1 flex-col">
            {children}
          </div>
        </main>
        {showsAbout && agent && <AboutPanel agent={agent} eases={aboutEases} />}
      </div>
    </FrameContext.Provider>
  );
}

export function Avatar({ agent, size }: { agent: { character: string; name: string }; size: number }) {
  if (agent.character in portraits) return <Portrait who={agent.character as Who} size={size} />;
  return (
    <span aria-hidden="true" style={{ width: size, height: size }} className="grid flex-none place-items-center rounded-full bg-surface-2 font-semibold">
      {agent.name[0]}
    </span>
  );
}

// initials are from your name; this computer's owner has none until they give one, so
// it's a person instead.
function initials(account: Account | null) {
  const from = account?.name || "";
  return from
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join("");
}

const railState = (on: boolean) => (on ? "bg-accent-soft text-accent-text" : "text-muted hover:bg-surface-2 hover:text-fg");

const railItem = "relative flex min-h-14 w-16 flex-col items-center justify-center gap-[3px] rounded-xl py-1.5 text-[11px] leading-tight font-semibold";

// The rail: Chats, Connect, Settings and You; at its foot, light or dark.
// Each agent's settings are theirs, from About; yours are on You.
function Rail({ account, on }: { account: Account | null; on: "chats" | "today" | "you" | "settings" | "connections" }) {
  const { dark, choose } = useTheme();
  return (
    <nav aria-label="Main" className="flex w-[76px] flex-none flex-col items-center gap-1 border-r border-line bg-rail pb-4 max-[900px]:hidden">
      {/* Room for the window's buttons (macOS puts them here, with the same room either side in the rail's 76px), and a place to drag it by. */}
      <div aria-hidden="true" className="h-8 w-full flex-none [--wails-draggable:drag]" />
      <Link href="/agents" aria-label="Tinio, your agents" className="mb-2 grid size-14 place-items-center text-accent-text">
        <Mark size={28} title="" aria-hidden="true" />
      </Link>
      <Link href="/agents" aria-current={on === "chats" ? "page" : undefined} className={`${railItem} ${railState(on === "chats")}`}>
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z" />
        </svg>
        <span>Chats</span>
      </Link>
      <Link href="/today" aria-current={on === "today" ? "page" : undefined} className={`${railItem} ${railState(on === "today")}`}>
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
        </svg>
        <span>Your day</span>
      </Link>
      <Link href="/connections" aria-current={on === "connections" ? "page" : undefined} className={`${railItem} ${railState(on === "connections")}`}>
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M9 3v5" />
          <path d="M15 3v5" />
          <path d="M6 8h12v3a6 6 0 0 1-12 0z" />
          <path d="M12 17v4" />
        </svg>
        <span>Connect</span>
      </Link>
      <div className="relative mt-auto flex flex-col items-center gap-1">
        <button
          type="button"
          aria-pressed={dark}
          onClick={() => choose(dark ? "light" : "dark")}
          className={`${railItem} text-muted hover:bg-surface-2 hover:text-fg`}
        >
          {dark ? (
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <circle cx="12" cy="12" r="4" />
              <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
            </svg>
          ) : (
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z" />
            </svg>
          )}
          <span>Dark</span>
        </button>
        <Link href="/settings" aria-current={on === "settings" ? "page" : undefined} className={`${railItem} ${railState(on === "settings")}`}>
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
          </svg>
          <span>Settings</span>
        </Link>
        <Link href="/you" aria-current={on === "you" ? "page" : undefined} className={`${railItem} ${railState(on === "you")}`}>
          <span aria-hidden="true" className="grid size-7 place-items-center rounded-full bg-line text-[11px] font-bold text-fg">
            {initials(account) || (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <circle cx="12" cy="8" r="4" />
                <path d="M4 21a8 8 0 0 1 16 0" />
              </svg>
            )}
          </span>
          <span>You</span>
        </Link>
      </div>
    </nav>
  );
}

type Filter = "all" | "needs" | "unread";

// The list of your agents, each one's chat: the last thing said, whether they need you,
// and what's unread. The open one is current, and has nothing unread.
// A filter shows only the chats that match it. One opened from the filtered list stays
// until the filter changes, though reading it means it no longer matches.
function List({ open, fold, over, className }: { open?: string; fold: () => void; over: boolean; className: string }) {
  const { overview } = useFrame();
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [kept, setKept] = useState("");
  const agents = overview?.agents ?? [];
  const needs = agents.filter((a) => a.waiting > 0).length;
  const unread = (a: Summary) => (a.id === open ? 0 : a.unread);
  const shown = agents.filter(
    (a) =>
      (a.id === kept || filter === "all" || (filter === "needs" && a.waiting > 0) || (filter === "unread" && unread(a) > 0)) &&
      a.name.toLowerCase().includes(search.trim().toLowerCase()),
  );
  const filters: [Filter, string][] = [
    ["all", "All"],
    ["needs", needs ? `Needs you · ${needs}` : "Needs you"],
    ["unread", "Unread"],
  ];
  return (
    <section aria-label="Chats" className={`flex w-[320px] flex-none flex-col border-r border-line max-[900px]:w-full max-[900px]:border-r-0 ${className}`}>
      <div className="flex flex-col gap-3 px-4 pt-3.5 pb-2.5">
        <div className="flex items-center justify-between gap-2">
          <h1 className="font-display text-[22px] font-semibold tracking-[-0.02em] max-[900px]:text-[28px] max-[900px]:font-medium">Chats</h1>
          <div className="flex items-center gap-0.5">
            <button
              type="button"
              onClick={fold}
              aria-label="Fold the list (⌘\)"
              title="Fold the list (⌘\)"
              className="grid size-11 place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg max-[900px]:hidden"
            >
              <PanelIcon folded={false} />
            </button>
            <Link href="/agents" aria-label="Overview of your agents" className="grid size-11 place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg max-[900px]:hidden">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <rect x="4" y="4" width="6" height="6" rx="1" />
                <rect x="14" y="4" width="6" height="6" rx="1" />
                <rect x="4" y="14" width="6" height="6" rx="1" />
                <rect x="14" y="14" width="6" height="6" rx="1" />
              </svg>
            </Link>
            <Link href="/raise" aria-label="Raise an agent" className="grid size-11 place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                <path d="M12 5v14" />
                <path d="M5 12h14" />
              </svg>
            </Link>
          </div>
        </div>
        <label htmlFor="search-agents" className="sr-only">Search your agents</label>
        <input
          id="search-agents"
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search your agents"
          className="min-h-11 rounded-lg border border-line-strong bg-surface px-4 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent"
        />
        <div role="group" aria-label="Show" className="flex flex-wrap gap-2">
          {filters.map(([id, label]) => (
            <button
              key={id}
              type="button"
              aria-pressed={filter === id}
              onClick={() => {
                setFilter(id);
                setKept("");
              }}
              className={`min-h-9 rounded-lg px-3.5 text-[13px] font-semibold ${filter === id ? "bg-line text-fg" : "text-muted hover:bg-surface-2 hover:text-fg"}`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-auto overflow-y-auto border-t border-line">
        {!open && <NoKey compact />}
        {shown.map((a) => (
          <Row key={a.id} agent={a} current={a.id === open} unread={unread(a)} onOpen={() => (setKept(a.id), over && fold())} />
        ))}
        {overview && shown.length === 0 && (
          <p className="m-0 px-4 py-5 text-sm text-muted">
            {filter === "needs" ? "Nobody is waiting on you." : filter === "unread" ? "Nothing unread." : "No agent by that name."}
          </p>
        )}
        <Link href="/raise" className="flex items-center gap-3 px-4 py-3 font-semibold text-accent-text">
          <span aria-hidden="true" className="grid size-12 flex-none place-items-center rounded-full border border-dashed border-line-strong text-[22px]">+</span>
          <span className="flex flex-col">
            Raise another agent
          </span>
        </Link>
      </div>

      <MeterLine />
      <nav aria-label="Main" className="flex border-t border-line bg-bg min-[901px]:hidden">
        <Link href="/agents" aria-current="page" className="flex min-h-14 flex-1 items-center justify-center text-[13px] font-semibold text-accent-text">
          Chats
        </Link>
        <Link href="/connections" className="flex min-h-14 flex-1 items-center justify-center text-[13px] font-semibold text-muted">
          Connect
        </Link>
        <Link href="/settings" className="flex min-h-14 flex-1 items-center justify-center text-[13px] font-semibold text-muted">
          Settings
        </Link>
        <Link href="/you" className="flex min-h-14 flex-1 items-center justify-center text-[13px] font-semibold text-muted">
          You
        </Link>
      </nav>
    </section>
  );
}

function PanelIcon({ folded }: { folded: boolean }) {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="4" width="18" height="16" rx="3" />
      <path d="M9 4v16" />
      <path d={folded ? "M14 10l2 2-2 2" : "M16 10l-2 2 2 2"} />
    </svg>
  );
}

// Faces is the chat list folded: each agent's face, a ring when they need you, a dot
// when something's unread, and their name on hover and to screen readers.
function Faces({ open, unfold }: { open?: string; unfold: () => void }) {
  const { overview } = useFrame();
  const agents = overview?.agents ?? [];
  return (
    <section aria-label="Chats" className="flex w-[72px] flex-none flex-col items-center gap-1.5 border-r border-line pt-3.5 pb-3 max-[900px]:hidden">
      <button
        type="button"
        onClick={unfold}
        aria-label="Open the list (⌘\)"
        title="Open the list (⌘\)"
        className="grid size-11 flex-none place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg"
      >
        <PanelIcon folded />
      </button>
      <span aria-hidden="true" className="my-1 h-px w-8 flex-none bg-line" />
      <nav aria-label="Your agents" className="flex min-h-0 flex-auto flex-col items-center gap-1 overflow-y-auto">
        {agents.map((a) => {
          const current = a.id === open;
          const unread = current ? 0 : a.unread;
          const label = `${a.name}${a.waiting > 0 ? ", needs you" : ""}${unread > 0 ? `, ${unread} unread` : ""}`;
          return (
            <Link
              key={a.id}
              href={`/agents/${a.id}`}
              aria-current={current ? "page" : undefined}
              aria-label={label}
              title={label}
              className={`relative grid size-[52px] flex-none place-items-center rounded-xl ${current ? "bg-accent-soft" : "hover:bg-surface-2"}`}
            >
              <span className={`grid place-items-center rounded-full ${a.waiting > 0 ? "ring-[3px] ring-warn-text" : ""}`}>
                <Avatar agent={a} size={40} />
              </span>
              {unread > 0 && <span aria-hidden="true" className="absolute top-1 right-1 size-[11px] rounded-full border-2 border-bg bg-accent" />}
            </Link>
          );
        })}
      </nav>
      <Link
        href="/raise"
        aria-label="Raise another agent"
        title="Raise another agent"
        className="grid size-11 flex-none place-items-center rounded-full border border-dashed border-line-strong text-[20px] text-accent-text"
      >
        +
      </Link>
    </section>
  );
}

function Row({ agent: a, current, unread, onOpen }: { agent: Summary; current: boolean; unread: number; onOpen: () => void }) {
  const preview = !a.last
    ? "Say hello"
    : `${a.last.author === "owner" ? "You: " : ""}${a.last.kind === "file" ? `Sent a file${a.last.text ? `: ${plainText(a.last.text)}` : ""}` : plainText(a.last.text)}`;
  return (
    <Link
      href={`/agents/${a.id}`}
      onClick={onOpen}
      aria-current={current ? "page" : undefined}
      className={`flex w-full items-center gap-3 border-b border-line px-4 py-3 ${current ? "bg-accent-soft/60" : "hover:bg-surface-2"}`}
    >
      <Avatar agent={a} size={48} />
      <span className="flex min-w-0 flex-auto flex-col gap-0.5">
        <span className="flex items-baseline justify-between gap-2">
          <b className="text-[15px] font-semibold">{a.name}</b>
          {a.last && <span className={`text-xs whitespace-nowrap ${unread ? "font-bold text-accent-text" : "text-muted"}`}>{when(a.last.at)}</span>}
        </span>
        <span className="flex items-center gap-1.5">
          <span className={`min-w-0 flex-auto truncate text-sm ${unread ? "font-medium text-fg" : "text-muted"}`}>{preview}</span>
          {a.waiting > 0 && <span className="flex-none rounded-md bg-warn-soft px-2 py-0.5 text-[11.5px] font-bold text-warn-text">Needs you</span>}
          {unread > 0 && (
            <span className="grid h-5 min-w-5 flex-none place-items-center rounded-full bg-accent px-1.5 text-xs font-bold text-accent-ink" aria-label={`${unread} unread`}>
              {unread}
            </span>
          )}
        </span>
      </span>
    </Link>
  );
}

// Back leads from any of an agent's pages back to About, where they're all reached from,
// at every width.
export function Back({ id }: { id: string }) {
  const { agent } = useFrame();
  return (
    <Link
      href={`/agents/${id}?about`}
      aria-label={agent ? `Back to About ${agent.name}` : "Back"}
      className="-ml-2 inline-flex min-h-11 items-center gap-1 self-start rounded-lg px-2 text-sm font-semibold text-accent-text hover:bg-accent-soft"
    >
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M15 6l-6 6 6 6" />
      </svg>
      Back
    </Link>
  );
}

export function when(at: string) {
  const d = new Date(at);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return d.toLocaleDateString([], { day: "numeric", month: "short" });
}

// status is what the agent is doing, as their chat's header and panel show it.
export function status(a: Summary, about: string) {
  if (a.paused) return "Paused";
  if (a.now) return a.now;
  if (a.next) return `Next: ${a.next}`;
  return about;
}

// early has a link start loading its page's data when it's pointed at, touched or focused:
// every page under an agent reads from the API path that is its own address.
export function early(href: string) {
  const start = () => prefetch(href);
  return { onMouseEnter: start, onFocus: start, onTouchStart: start };
}

// The panel about the open agent: who they are, their pages, what they're doing now and
// next. Pages still to come are shown, not linked.
function AboutPanel({ agent: a, eases }: { agent: Summary; eases: boolean }) {
  const { about, toggleAbout } = useFrame();
  const tiles = [
    { label: "Your day", sub: "Their morning for you", href: `/agents/${a.id}/today` },
    { label: "Works", sub: "What they made", href: `/agents/${a.id}/works` },
    { label: "Skills", sub: "And their ladder", href: `/agents/${a.id}/skills` },
  ];
  const pages: { label: string; href?: string }[] = [
    { label: "Topics", href: `/agents/${a.id}/topics` },
    { label: "Character", href: `/agents/${a.id}/character` },
    { label: "How you decide", href: `/agents/${a.id}/judgment` },
    { label: "Memory", href: `/agents/${a.id}/memory` },
    { label: "Mind", href: `/agents/${a.id}/mind` },
    { label: "Helping you", href: `/agents/${a.id}/growth` },
    { label: "Tools", href: `/agents/${a.id}/tools` },
    { label: "Mornings", href: `/agents/${a.id}/settings#mornings` },
    { label: "Settings", href: `/agents/${a.id}/settings` },
  ];
  return (
    <aside aria-labelledby="about-agent" className={`flex min-w-0 flex-1 flex-col bg-bg [view-transition-name:page] ${eases ? "page-in" : ""}`}>
      <div className="flex items-center gap-1 border-b border-line py-2 pr-5 pl-2 max-[900px]:pr-4">
        <button type="button" aria-label={`Close, back to the chat with ${a.name}`} onClick={toggleAbout} className="grid size-11 flex-none place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <path d="M6 6l12 12" />
            <path d="M18 6L6 18" />
          </svg>
        </button>
        <h2 id="about-agent" className="flex-auto font-display text-[17px] font-semibold tracking-[-0.02em]">About {a.name}</h2>
      </div>
      <div className="min-h-0 flex-auto overflow-y-auto">
        <div className="mx-auto flex w-full max-w-[560px] flex-col gap-5 p-5">
        <div className="flex flex-col items-center gap-1.5 text-center">
          <Avatar agent={a} size={96} />
          <b className="mt-1.5 font-display text-[22px] font-semibold">{a.name}</b>
          <span className="text-sm text-muted">{about(a)}</span>
        </div>
        <div className="grid grid-cols-3 gap-2">
          {tiles.map((t) =>
            t.href ? (
              <Link key={t.label} href={t.href} {...early(t.href)} className="flex flex-col gap-0.5 rounded-lg border border-line px-3 py-2.5 hover:border-line-strong">
                <b className="text-sm font-semibold">{t.label}</b>
                <span className="text-xs text-muted">{t.sub}</span>
              </Link>
            ) : (
              <span key={t.label} aria-disabled="true" className="flex flex-col gap-0.5 rounded-lg border border-line px-3 py-2.5 text-muted">
                <b className="text-sm font-semibold">{t.label}</b>
                <span className="text-xs">{t.sub}</span>
              </span>
            ),
          )}
        </div>
        <section className="flex flex-col gap-1.5">
          <h3 className="text-[15px] font-semibold">Now</h3>
          <span className="text-sm">{a.now || "Nothing under way."}</span>
          {a.next && <span className="text-[13px] text-muted">Next: {a.next}</span>}
        </section>
        <nav aria-label={a.name} className="flex flex-col border-t border-line">
          {pages.map((p) =>
            p.href ? (
              <Link key={p.label} href={p.href} {...early(p.href)} className="flex min-h-12 items-center justify-between gap-2.5 border-b border-line px-1 text-[15px] text-fg hover:text-accent-text">
                <span>{p.label}</span>
                <span aria-hidden="true" className="text-muted">›</span>
              </Link>
            ) : (
              <span key={p.label} aria-disabled="true" className="flex min-h-12 items-center justify-between gap-2.5 border-b border-line px-1 text-[15px] text-muted">
                <span>{p.label}</span>
                <span className="text-xs">Soon</span>
              </span>
            ),
          )}
        </nav>
        <span className="text-xs text-muted">{a.name} is an AI agent with a character, and says so whenever it matters.</span>
        </div>
      </div>
    </aside>
  );
}
