"use client";

import Link from "next/link";
import { Button } from "@tinio/ui";
import { Fragment, useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Avatar, status, useFrame } from "@/components/app-frame";
import { FileIcon, kindOf } from "@/components/file-icon";
import { MessageText } from "@/components/message-text";
import { WorkPreview } from "@/components/work-preview";
import { NoKey } from "@/components/first-run";
import { JobAskCard, JobCard } from "@/components/jobs";
import { PanelIcons, SidePanel, useJobs, usePanel } from "@/components/chat-panel";
import { ComposerFolders } from "@/components/folders";
import { resets, UsageBanner } from "@/components/usage";
import { call, type ChatRow, type Topic } from "@/lib/api";

// Step 5: the chat with an agent, in the app's frame. The history first, then everything as
// it happens, over one stream from the last row already here. Opened for the first time,
// the agent says hello. Their requests and calls on your judgment are cards you answer with
// a tap. What you see is marked read. The header opens the panel about them.
//
// It stays one chat: the agent sorts what's said into topics, a small label shows where
// the subject changes, and a chip shows one topic only (its address says which, so About's
// Topics opens one). You can start a topic, reply to a message, or move one to another.

// waitFor is how long "writing…" shows after you write; past it, they're taking longer
// than a reply does (resting, or something went wrong), and the chat says so.
const waitFor = 2 * 60_000;

const shown = new Set<ChatRow["kind"]>(["message", "ask", "choice", "file", "job", "job_ask", "job_reply"]);

function byId(a: ChatRow, b: ChatRow) {
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

// put adds r to rows, or puts it in its place: rows are kept by id.
function put(rows: ChatRow[], r: ChatRow) {
  const i = rows.findIndex((x) => x.id === r.id);
  if (i >= 0) return rows.map((x, j) => (j === i ? r : x));
  return [...rows, r].sort(byId);
}

// shownTopic is the topic in the page's address; "" for the whole chat.
function shownTopic() {
  return typeof window === "undefined" ? "" : (new URLSearchParams(window.location.search).get("topic") ?? "");
}

// The agent names a topic in their reply, saved just after it's sent: the list is read
// again a moment after one of their rows in a topic arrives.
const namedWithin = 2500;

export function Chat({ id }: { id: string }) {
  const { account, agent, missing, about, aboutOpen, toggleAbout, refresh, usage, keySet } = useFrame();
  const [rows, setRows] = useState<ChatRow[] | null>(null);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [tapped, setTapped] = useState<Record<string, string>>({}); // row id: what you said
  const [answering, setAnswering] = useState<Record<string, boolean>>({}); // row id: your tap is on its way
  const [cardError, setCardError] = useState<Record<string, string>>({}); // row id: why your tap didn't count
  const inFlight = useRef(new Set<string>()); // the same, at once, so a double tap sends one answer
  const [draft, setDraft] = useState(""); // their reply as it's being written; "" when none
  const [making, setMaking] = useState(""); // what they're making for you now (a work you asked for); "" when nothing
  const [now, setNow] = useState(() => Date.now());
  const end = useRef<HTMLDivElement>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const [topics, setTopics] = useState<Topic[]>([]);
  const [inTopic, setInTopic] = useState("");
  const [topicRows, setTopicRows] = useState<Record<string, ChatRow[]>>({}); // each topic shown so far, all of it
  const [replyTo, setReplyTo] = useState<ChatRow | null>(null);
  const [starting, setStarting] = useState(false); // what you send next starts a topic
  const [menu, setMenu] = useState(""); // the row whose menu is open
  const [searching, setSearching] = useState(false);
  const [panel, setPanel] = usePanel();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<ChatRow[] | null>(null); // what a search found, newest first
  const [hit, setHit] = useState(0); // the one shown
  const log = useRef<HTMLDivElement>(null);

  const loadTopics = useCallback(
    () => call<{ topics: Topic[] }>(`/agents/${id}/topics`).then((t) => t.ok && setTopics(t.data.topics)),
    [id],
  );
  const later = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const keep = useCallback(
    (r: ChatRow) => {
      setRows((prev) => put(prev ?? [], r));
      setTopicRows((prev) =>
        Object.fromEntries(Object.entries(prev).map(([t, rs]) => [t, r.topic === t ? put(rs, r) : rs.filter((x) => x.id !== r.id)])),
      );
      if (r.topic && r.author === "agent") {
        clearTimeout(later.current);
        later.current = setTimeout(loadTopics, namedWithin);
      }
    },
    [loadTopics],
  );

  useEffect(() => {
    const follow = () => setInTopic(shownTopic());
    follow();
    window.addEventListener("popstate", follow);
    return () => window.removeEventListener("popstate", follow);
  }, []);
  function show(topic: string) {
    setInTopic(topic);
    setMenu("");
    window.history.replaceState(null, "", topic ? `?topic=${topic}` : window.location.pathname);
  }
  useEffect(() => {
    if (account) loadTopics();
  }, [account, loadTopics]);
  useEffect(() => {
    if (!account || !inTopic || topicRows[inTopic]) return;
    call<{ messages: ChatRow[] }>(`/agents/${id}/messages?topic=${inTopic}&limit=200`).then((m) => {
      if (m.ok) setTopicRows((prev) => ({ ...prev, [inTopic]: m.data.messages }));
    });
    loadTopics();
  }, [account, id, inTopic, topicRows, loadTopics]);

  useEffect(() => {
    if (!account) return;
    call<{ messages: ChatRow[]; making?: string }>(`/agents/${id}/messages?limit=200`).then((m) => {
      if (!m.ok) return setError(m.error.message);
      setRows(m.data.messages);
      setMaking(m.data.making ?? "");
      if (m.data.messages.length === 0) call(`/agents/${id}/hello`, {});
    });
  }, [account, id]);

  // The stream starts after the newest row here, so nothing between is missed. The browser
  // reconnects by itself after a dropped connection; after an error it can't get past (a
  // restart, an expired session) it's reopened here, a little later each time. Either way
  // the history is read again, for requests settled while it was away.
  const ready = rows !== null;
  const newest = useRef("");
  newest.current = rows?.[rows.length - 1]?.id ?? "";
  useEffect(() => {
    if (!ready) return;
    let es: EventSource | null = null;
    let stopped = false;
    let away = false;
    let wait = 1000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const catchUp = () =>
      call<{ messages: ChatRow[] }>(`/agents/${id}/messages?limit=200`).then((m) => m.ok && m.data.messages.forEach(keep));
    const open = () => {
      es = new EventSource(`/api/agents/${id}/stream${newest.current ? `?after=${newest.current}` : ""}`);
      es.addEventListener("chat", (e) => {
        const r: ChatRow = JSON.parse((e as MessageEvent).data);
        if (r.author === "agent") setDraft("");
        keep(r);
      });
      es.addEventListener("draft", (e) => setDraft(JSON.parse((e as MessageEvent).data).text ?? ""));
      es.addEventListener("making", (e) => setMaking(JSON.parse((e as MessageEvent).data).text ?? ""));
      es.onopen = () => {
        wait = 1000;
        if (away) catchUp();
        away = false;
      };
      es.onerror = () => {
        away = true;
        if (stopped || es?.readyState !== EventSource.CLOSED) return;
        timer = setTimeout(async () => {
          if (stopped) return;
          await call("/me"); // signed out: call sends them to sign in
          open();
        }, wait);
        wait = Math.min(wait * 2, 30_000);
      };
    };
    open();
    return () => {
      stopped = true;
      es?.close();
      clearTimeout(timer);
    };
  }, [ready, id, keep]);

  // Whether they're writing is read from the whole chat, whichever topic is shown.
  const all = (rows ?? []).filter((r) => shown.has(r.kind));
  const last = all[all.length - 1];
  const waiting = ready && (all.length === 0 || last.author === "owner");
  // The questions a job is waiting on you for, oldest first: the first is docked above
  // the message box.
  const asking = all.filter((r) => r.kind === "job_ask" && !r.settled_at && !tapped[r.id]);
  // The jobs, read again whenever a job's card changes.
  const jobs = useJobs(id, all.filter((r) => r.kind === "job").map((r) => r.id + (r.payload?.status ?? "") + (r.payload?.commit ?? "")).join());
  const visible = inTopic ? (topicRows[inTopic] ?? []).filter((r) => shown.has(r.kind)) : all;
  // Your last thumb on each work, from the rows you sent.
  const reactions: Record<string, { good: boolean; note?: string }> = {};
  for (const r of rows ?? []) if (r.kind === "react" && r.payload?.work) reactions[r.payload.work] = { good: !!r.payload.good, note: r.payload.note };
  const titleOf = (t?: string) => {
    const found = topics.find((x) => x.id === t);
    return found ? found.title || "New topic" : "An earlier topic";
  };
  const quoted = (r: ChatRow) => {
    if (!r.reply_to) return undefined;
    const q = [...(rows ?? []), ...Object.values(topicRows).flat()].find((x) => x.id === r.reply_to);
    return q ? { who: q.author === "owner" ? "You" : (agent?.name ?? ""), text: q.text || q.payload?.name || "" } : { who: "", text: "An earlier message" };
  };
  const since = last ? new Date(last.at).getTime() : agent ? new Date(agent.created_at).getTime() : now;
  const thinking = waiting && now - since < waitFor;
  const writing = thinking && draft !== ""; // a draft never outlives the wait: a reply that never came
  useEffect(() => {
    if (!waiting) return;
    const tick = setInterval(() => setNow(Date.now()), 15_000);
    return () => clearInterval(tick);
  }, [waiting]);

  // The composer grows with what's written (max-h caps it).
  useEffect(() => {
    const b = box.current;
    if (!b) return;
    b.style.height = "auto";
    b.style.height = `${b.scrollHeight + 2}px`;
  }, [text]);

  // The chat follows what's new only if you're at its end already: reading back isn't
  // interrupted. It scrolls the page (the frame's main), so nothing ends up under the
  // composer.
  const atEnd = useRef(true);
  useEffect(() => {
    const page = end.current?.closest("main");
    if (!page) return;
    const track = () => (atEnd.current = page.scrollHeight - page.scrollTop - page.clientHeight < 160);
    page.addEventListener("scroll", track, { passive: true });
    return () => page.removeEventListener("scroll", track);
  }, [ready]);
  useEffect(() => {
    const page = end.current?.closest("main");
    if (page && atEnd.current) page.scrollTop = page.scrollHeight;
  }, [visible.length, thinking, draft]);

  // What's on screen is read: marked when it arrives while you're looking, or when you
  // come back to the tab.
  const marked = useRef("");
  useEffect(() => {
    const newest = rows?.[rows.length - 1]?.id;
    const mark = () => {
      if (!newest || newest <= marked.current || document.visibilityState !== "visible") return;
      marked.current = newest;
      call(`/agents/${id}/read`, { until: newest }).then(refresh);
    };
    mark();
    document.addEventListener("visibilitychange", mark);
    return () => document.removeEventListener("visibilitychange", mark);
  }, [rows, id, refresh]);

  // send sends what's typed, or an answer tapped (answer), which goes in the topic of
  // what it answers.
  async function send(e?: FormEvent, tapped?: string) {
    e?.preventDefault();
    const said = (tapped ?? text).trim();
    if (!said || sending) return;
    setSending(true);
    setError("");
    const res = await call<{ message: ChatRow }>(`/agents/${id}/messages`, {
      text: said,
      topic: starting ? "new" : inTopic || (tapped ? (last?.topic ?? "") : ""),
      reply_to: replyTo?.id ?? "",
    });
    setSending(false);
    if (!res.ok) return setError(res.error.message);
    atEnd.current = true;
    setDraft("");
    keep(res.data.message);
    if (tapped === undefined) setText((now) => (now === text ? "" : now)); // anything typed while it went stays
    setReplyTo(null);
    setStarting(false);
    if (res.data.message.topic) loadTopics();
  }

  // move puts a row in another topic ("new", or "" for none); the stream brings it back
  // in its new place, with the replies that went with it.
  async function move(row: ChatRow, to: string) {
    setMenu("");
    const res = await call(`/agents/${id}/messages/${row.id}/topic`, { topic: to });
    if (!res.ok) return setError(res.error.message);
    loadTopics();
  }

  // search finds q anywhere in the chat; the newest match is shown first.
  async function search(e: FormEvent) {
    e.preventDefault();
    const q = query.trim();
    if (!q) return;
    const res = await call<{ messages: ChatRow[] }>(`/agents/${id}/search?q=${encodeURIComponent(q)}`);
    if (!res.ok) return setError(res.error.message);
    setHits(res.data.messages);
    setHit(0);
    if (res.data.messages.length) goTo(res.data.messages[0]);
  }

  // goTo shows a row found: in the whole chat, its older pages read until it's there.
  async function goTo(r: ChatRow) {
    if (inTopic) show("");
    atEnd.current = false;
    let have = rows ?? [];
    for (let page = 0; page < 20 && !have.some((x) => x.id === r.id) && have.length; page++) {
      const m = await call<{ messages: ChatRow[] }>(`/agents/${id}/messages?before=${have[0].id}&limit=200`);
      if (!m.ok || !m.data.messages.length) break;
      have = [...m.data.messages, ...have];
    }
    setRows((prev) => have.reduce(put, prev ?? []));
    requestAnimationFrame(() => document.getElementById(`row-${r.id}`)?.scrollIntoView({ block: "center" }));
  }

  function step(by: number) {
    if (!hits?.length) return;
    const next = (hit + by + hits.length) % hits.length;
    setHit(next);
    goTo(hits[next]);
  }

  // The words found are marked where they are, without changing what's drawn (the CSS
  // Highlight API); the one shown now is marked more strongly. A browser without it
  // still scrolls to each.
  useEffect(() => {
    const marks = typeof CSS !== "undefined" ? (CSS as unknown as { highlights?: Map<string, unknown> }).highlights : undefined;
    if (!marks) return;
    marks.delete("chat-found");
    marks.delete("chat-here");
    const q = query.trim().toLowerCase();
    if (!searching || !hits?.length || !q || !log.current) return;
    const found: Range[] = [];
    const here: Range[] = [];
    const current = document.getElementById(`row-${hits[hit]?.id}`);
    const walk = document.createTreeWalker(log.current, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const text = n.textContent?.toLowerCase() ?? "";
      for (let at = text.indexOf(q); at >= 0; at = text.indexOf(q, at + q.length)) {
        const range = new Range();
        range.setStart(n, at);
        range.setEnd(n, at + q.length);
        (current?.contains(n) ? here : found).push(range);
      }
    }
    const Highlight = (window as unknown as { Highlight: new (...r: Range[]) => unknown }).Highlight;
    marks.set("chat-found", new Highlight(...found));
    marks.set("chat-here", new Highlight(...here));
  }, [searching, hits, hit, rows, query]);

  // react sends your thumb on a work, with what was off if it's down.
  async function react(work: string, good: boolean, note = "") {
    const res = await call<{ message: ChatRow }>(`/agents/${id}/reactions`, { work, good, note });
    if (!res.ok) return setError(res.error.message);
    keep(res.data.message);
  }

  async function copy(row: ChatRow) {
    setMenu("");
    await navigator.clipboard.writeText(row.text);
  }

  function reply(row: ChatRow) {
    setMenu("");
    setStarting(false);
    setReplyTo(row);
    box.current?.focus();
  }

  // answer sends your tap on a card once: the card's buttons wait while it's on its way,
  // and if it doesn't count, the card says why.
  async function answer(row: ChatRow, body: Record<string, unknown>, said: string, path?: string) {
    if (inFlight.current.has(row.id)) return;
    inFlight.current.add(row.id);
    setAnswering((a) => ({ ...a, [row.id]: true }));
    setCardError((e) => ({ ...e, [row.id]: "" }));
    const res = await call(path ?? `/agents/${id}/answers`, body);
    inFlight.current.delete(row.id);
    setAnswering((a) => ({ ...a, [row.id]: false }));
    if (!res.ok) return setCardError((e) => ({ ...e, [row.id]: res.error.message }));
    setTapped((t) => ({ ...t, [row.id]: said }));
    refresh();
  }

  if (missing) {
    return (
      <div className="grid flex-1 place-items-center px-6">
        <p role="alert" className="text-bad-text">We can&apos;t find that agent among yours.</p>
      </div>
    );
  }

  const name = agent?.name ?? "";
  const startTopic = () => {
    setStarting(true);
    setReplyTo(null);
    show("");
    box.current?.focus();
  };
  return (
    <div className="flex min-h-full">
      <div className="flex min-w-0 flex-1 flex-col">
      <header className="sticky top-0 z-10 flex flex-col border-b border-line bg-bg">
        <div className="flex items-center gap-1 px-4 py-2 max-[900px]:px-2 max-[900px]:py-1.5">
          <Link href="/agents" aria-label="Back to chats" className="grid size-11 flex-none place-items-center rounded-full text-muted hover:text-fg min-[901px]:hidden">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M15 6l-6 6 6 6" />
            </svg>
          </Link>
          <button type="button" aria-expanded={aboutOpen} onClick={toggleAbout} className="flex min-h-12 min-w-0 flex-auto items-center gap-2.5 rounded-lg px-2 py-1 text-left hover:bg-surface-2">
            {agent && <Avatar agent={agent} size={40} />}
            <span className="flex min-w-0 flex-col">
              <b className="font-display text-lg font-semibold tracking-[-0.02em]">{name}</b>
              <span className="truncate text-[13px] text-muted">{agent ? status(agent, about(agent)) : ""}</span>
            </span>
          </button>
          <button
          type="button"
          aria-label="Search this chat"
          aria-pressed={searching}
          onClick={() => {
            setSearching(!searching);
            setHits(null);
          }}
          className="grid size-11 flex-none place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg aria-pressed:bg-accent-soft aria-pressed:text-accent-text"
        >
          <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <circle cx="11" cy="11" r="6.5" />
            <path d="M16 16l4.5 4.5" />
          </svg>
        </button>
        {inTopic && (
          <span className="mx-1 inline-flex max-w-[220px] flex-none items-center gap-1 rounded-lg bg-line py-1 pr-1 pl-2.5 text-[13px] font-semibold max-[560px]:hidden">
            <span className="truncate">In: {titleOf(inTopic)}</span>
            <button type="button" aria-label="Show everything again" onClick={() => show("")} className="grid size-6 flex-none place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-fg">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </span>
        )}
        <PanelIcons panel={panel} open={setPanel} needsYou={jobs.some((j) => j.status === "waiting")} />
        <button type="button" aria-label={`About ${name}`} aria-expanded={aboutOpen} onClick={toggleAbout} className="grid size-11 flex-none place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <circle cx="12" cy="12" r="9" />
              <path d="M12 11v6" />
              <path d="M12 7.5v.5" />
            </svg>
          </button>
        </div>
        {searching && (
          <form role="search" onSubmit={search} className="flex items-center gap-1.5 px-5 pb-2.5 max-[900px]:px-3">
            <input
              type="search"
              aria-label="Search this chat"
              placeholder="Search this chat"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setHits(null);
              }}
              autoFocus
              maxLength={100}
              className="min-h-10 min-w-0 flex-1 rounded-lg border border-line-strong bg-surface px-4 text-sm focus:outline-2 focus:outline-offset-1 focus:outline-accent"
            />
            <span role="status" className="px-1 text-[13px] whitespace-nowrap text-muted">
              {hits === null ? "" : hits.length === 0 ? "Not found" : `${hit + 1} of ${hits.length}${hits.length === 50 ? "+" : ""}`}
            </span>
            <button type="button" aria-label="Older match" disabled={!hits?.length} onClick={() => step(1)} className="grid size-10 place-items-center rounded-full text-muted hover:bg-surface-2 disabled:opacity-40">
              <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M6 15l6-6 6 6" />
              </svg>
            </button>
            <button type="button" aria-label="Newer match" disabled={!hits?.length} onClick={() => step(-1)} className="grid size-10 place-items-center rounded-full text-muted hover:bg-surface-2 disabled:opacity-40">
              <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M6 9l6 6 6-6" />
              </svg>
            </button>
          </form>
        )}
      </header>
      {keySet === false ? <NoKey name={agent?.name} /> : <UsageBanner />}

      <div ref={log} role="log" aria-live="polite" aria-label={`Chat with ${name}`} className="mx-auto flex w-full max-w-[960px] flex-1 flex-col px-10 py-6 max-[900px]:px-3 max-[900px]:py-4">
        {agent && <span className="mb-1 self-center text-xs text-muted">{name} was raised {day(agent.created_at)}</span>}
        {visible.map((r, i) => {
          const before = visible[i - 1];
          const newDay = !before || new Date(before.at).toDateString() !== new Date(r.at).toDateString();
          // A label shows where the subject changes; small talk has none.
          const label = !inTopic && !!r.topic && (newDay || (before.topic ?? "") !== r.topic);
          // A run of messages from one side sits close, and only its first has a tail.
          const first = newDay || label || before.author !== r.author || before.kind !== "message";
          const mine = r.author === "owner";
          return (
            <Fragment key={r.id}>
              {newDay && <span className={`${chip} mt-3`}>{dayName(r.at)}</span>}
              {label && (
                <button
                  type="button"
                  onClick={() => show(r.topic!)}
                  aria-label={`Show only ${titleOf(r.topic)}`}
                  className={`mt-3.5 -mb-1.5 inline-flex min-h-8 items-center gap-1 rounded-md px-1.5 text-[12.5px] font-bold text-accent-text hover:bg-accent-soft ${mine ? "self-end" : "self-start"}`}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M5 5v7a3 3 0 0 0 3 3h10" />
                    <path d="M14 11l4 4-4 4" />
                  </svg>
                  {titleOf(r.topic)}
                </button>
              )}
              <div id={`row-${r.id}`} className={`group relative flex w-full scroll-mt-40 items-center gap-1 ${mine ? "flex-row-reverse" : ""} ${first ? "mt-3" : "mt-1"}`}>
                <Row row={r} first={first} agent={id} name={name} quote={quoted(r)} tapped={tapped[r.id]} busy={answering[r.id]} failed={cardError[r.id]} answer={answer} />
                <div className={`flex flex-none self-center ${menu === r.id ? "" : "opacity-0 group-hover:opacity-100 focus-within:opacity-100 max-[900px]:opacity-100"}`}>
                  <button type="button" aria-label="Reply to this message" onClick={() => reply(r)} className={`${rowAction} max-[900px]:hidden`}>
                    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                      <path d="M9 14L4 9l5-5" />
                      <path d="M4 9h10a6 6 0 0 1 6 6v4" />
                    </svg>
                  </button>
                  <button type="button" aria-label="More for this message" aria-haspopup="menu" aria-expanded={menu === r.id} onClick={() => setMenu(menu === r.id ? "" : r.id)} className={rowAction}>
                    <svg width="17" height="17" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                      <circle cx="5" cy="12" r="1.8" />
                      <circle cx="12" cy="12" r="1.8" />
                      <circle cx="19" cy="12" r="1.8" />
                    </svg>
                  </button>
                </div>
                {menu === r.id && (
                  <RowMenu
                    row={r}
                    mine={mine}
                    topics={topics.filter((t) => !t.quiet && t.id !== r.topic)}
                    close={() => setMenu("")}
                    reply={() => reply(r)}
                    copy={() => copy(r)}
                    move={(to) => move(r, to)}
                  />
                )}
              </div>
              {!mine && !!r.payload?.read?.length && (
                <span className="mt-1 flex flex-wrap items-center gap-1.5 self-start pl-1.5 text-xs text-muted">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
                    <circle cx="12" cy="12" r="3" />
                  </svg>
                  Read {r.payload.read.join(" · ")}
                </span>
              )}
              {r.kind === "file" && r.payload?.work && <Thumbs key={r.payload.work} said={reactions[r.payload.work]} name={name} react={(good, note) => react(r.payload!.work!, good, note)} />}
              {r.id === last?.id && !mine && !!r.payload?.answers?.length && (
                <Answers answers={r.payload.answers} many={!!r.payload.many} busy={sending} send={(said) => send(undefined, said)} />
              )}
            </Fragment>
          );
        })}
        {inTopic && topicRows[inTopic] && visible.length === 0 && (
          <p className="mt-6 self-center text-sm text-muted">Nothing in this topic yet.</p>
        )}
        {/* Read out once, when it's said: the draft changes too often for a screen reader. */}
        {writing && (
          // Drawn as the message it becomes, so nothing jumps when it lands.
          <div className={`${theirs} ${tail.theirs} mt-3`} aria-hidden="true">
            <div className="whitespace-normal">
              <MessageText text={settled(draft)} />
            </div>
            <span aria-hidden="true" className="inline-block w-12" />
          </div>
        )}
        {thinking && agent && keySet !== false && (
          <span className={writing ? "sr-only" : "mt-3 self-start text-[13px] text-muted"} aria-live="polite">
            {all.length === 0 ? `${name} is saying hello…` : `${name} is writing…`}
          </span>
        )}
        {making && !writing && agent && (
          <span className="mt-3 flex items-center gap-2 self-start text-[13px] text-muted" role="status">
            <span aria-hidden="true" className="size-2 flex-none animate-pulse rounded-full bg-accent" />
            {making.startsWith(name + " ") ? making : `${name} is on it: ${making}`}
          </span>
        )}
        {waiting && !thinking && agent && keySet !== false && (
          <span className="mt-3 self-start text-[13px] text-muted">
            {all.length === 0 ? `${name} hasn't said hello yet. They will when they're back.` : `No answer yet. ${name} will answer when they can.`}
          </span>
        )}
        <div ref={end} />
      </div>

      <form onSubmit={send} className="sticky bottom-0 mx-auto flex w-full max-w-[960px] flex-wrap items-end gap-2.5 bg-chat px-10 pt-1 pb-6 max-[900px]:px-3 max-[900px]:pb-3.5">
        {asking.length > 0 && (
          <JobAskCard
            row={asking[0]}
            agent={id}
            name={name}
            tapped={tapped[asking[0].id]}
            busy={answering[asking[0].id]}
            failed={cardError[asking[0].id]}
            answer={answer}
            docked
            more={asking.length - 1}
          />
        )}
        {replyTo && (
          <ComposerBar
            title={`Replying to ${replyTo.author === "owner" ? "yourself" : name}${replyTo.topic ? ` · ${titleOf(replyTo.topic)}` : ""}`}
            text={replyTo.text || replyTo.payload?.name || ""}
            stop="Stop replying to this message"
            onStop={() => setReplyTo(null)}
          />
        )}
        {starting && (
          <ComposerBar
            title="New topic"
            text={`Your first message starts it, and ${name} gives it a name. Nothing is made until you send.`}
            stop="Don't start a topic"
            onStop={() => setStarting(false)}
            dashed
          />
        )}
        <ComposerFolders id={id} name={name} />
        <label htmlFor="msg" className="sr-only">Message {name}</label>
        {/* Enter sends; Shift+Enter starts a new line. It grows with what's written, up to a point. */}
        <textarea
          ref={box}
          id="msg"
          rows={1}
          autoComplete="off"
          maxLength={4000}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              send();
            }
          }}
          placeholder={starting ? "What's it about?" : inTopic ? `Message ${name} about ${titleOf(inTopic)}` : `Message ${name}`}
          className="max-h-48 min-h-12 min-w-0 flex-1 resize-none rounded-xl border border-line-strong bg-surface px-[18px] py-3 text-[15px] leading-6 text-fg focus:outline-2 focus:outline-offset-1 focus:outline-accent"
        />
        <button
          type="submit"
          aria-label="Send"
          disabled={sending || !text.trim()}
          className="grid size-12 flex-none place-items-center rounded-full bg-accent text-accent-ink hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M5 12h14" />
            <path d="M13 6l6 6-6 6" />
          </svg>
        </button>
        {inTopic && !starting && (
          <p className="m-0 basis-full px-[18px] text-xs text-muted">
            What you send here stays in {titleOf(inTopic)}. <button type="button" onClick={() => show("")} className="font-semibold text-accent-text">Everything</button> shows the whole chat.
          </p>
        )}
        {agent?.paused && (
          <p className="m-0 basis-full px-[18px] text-xs text-muted">{name} is paused. What you send is read when you resume them, in their settings.</p>
        )}
        {usage?.state === "resting" && !agent?.paused && (
          <p className="m-0 basis-full px-[18px] text-xs text-muted">What you send now is read at the reset, {resets(usage.resets_at)}.</p>
        )}
      </form>
      {error && <p role="alert" className="mx-auto w-full max-w-[960px] px-10 pb-4 text-sm text-bad-text max-[900px]:px-3">{error}</p>}
      </div>
      <SidePanel
        agent={id}
        name={name}
        panel={panel}
        close={() => setPanel("")}
        topics={topics}
        inTopic={inTopic}
        show={show}
        startTopic={startTopic}
        jobs={jobs}
      />
    </div>
  );
}

function day(at: string) {
  const d = new Date(at);
  const today = new Date();
  return d.toDateString() === today.toDateString()
    ? `today, ${d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
    : `on ${d.toLocaleDateString([], { day: "numeric", month: "long" })}`;
}

// Messages are bubbles, theirs light and yours in the accent, each with its time in the
// corner; the first of a run has a tail on its side.
const lift = "shadow-[0_1px_0.5px_rgba(11,20,26,0.13)]";
// settled is a reply being written, without a mark it hasn't closed yet (a lone ** or
// `), so the words don't jump from plain to bold when the other half arrives.
function settled(text: string) {
  let t = text;
  if ((t.match(/\*\*/g)?.length ?? 0) % 2 === 1) t = t.slice(0, t.lastIndexOf("**")) + t.slice(t.lastIndexOf("**") + 2);
  if ((t.match(/`/g)?.length ?? 0) % 2 === 1) t = t.slice(0, t.lastIndexOf("`")) + t.slice(t.lastIndexOf("`") + 1);
  return t;
}

const bubble = `relative max-w-[78%] whitespace-pre-wrap rounded-lg px-3 pt-1.5 pb-2 text-[15px] leading-[1.45] ${lift}`;
const theirs = `${bubble} self-start bg-surface`;
const yours = `${bubble} self-end bg-accent text-accent-ink`;
const tail = {
  theirs: "rounded-tl-none before:absolute before:top-0 before:-left-2 before:h-3 before:w-2 before:bg-surface before:[clip-path:polygon(0_0,100%_0,100%_100%)]",
  yours: "rounded-tr-none before:absolute before:top-0 before:-right-2 before:h-3 before:w-2 before:bg-accent before:[clip-path:polygon(0_0,100%_0,0_100%)]",
};
const card = `relative flex w-[78%] max-w-full flex-col gap-3 self-start rounded-lg bg-surface px-4 pt-3.5 pb-7 ${lift} max-[560px]:w-full`;
const chip = `self-center rounded-lg bg-surface px-3 py-1 text-xs font-medium text-muted ${lift}`;

function hm(at: string) {
  return new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function dayName(at: string) {
  const d = new Date(at);
  const now = new Date();
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === now.toDateString()) return "Today";
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return d.toLocaleDateString([], { weekday: "long", day: "numeric", month: "long", ...(d.getFullYear() !== now.getFullYear() && { year: "numeric" }) });
}

// Stamp is a row's time, in its bottom right corner.
function Stamp({ at, mine }: { at: string; mine?: boolean }) {
  return (
    <time dateTime={at} title={new Date(at).toLocaleString()} className={`absolute right-2.5 bottom-1.5 text-[11px] leading-none ${mine ? "text-accent-ink/75" : "text-muted"}`}>
      {hm(at)}
    </time>
  );
}

// Card's busy: your tap on it is on its way; failed: why it didn't count.
type Card = {
  row: ChatRow;
  name: string;
  tapped?: string;
  busy?: boolean;
  failed?: string;
  answer: (row: ChatRow, body: Record<string, unknown>, said: string, path?: string) => void;
};

// quote is what a message replies to, and who said it ("" if it's no longer here).
function Row({ agent, first, quote, ...props }: Card & { agent: string; first: boolean; quote?: { who: string; text: string } }) {
  const { row, name, tapped, busy, failed, answer } = props;
  if (row.kind === "job") return <JobCard row={row} agent={agent} name={name} />;
  if (row.kind === "job_ask") return <JobAskCard {...props} agent={agent} />;
  if (row.kind === "message" || row.kind === "job_reply") {
    const mine = row.author === "owner";
    return (
      <div className={`${mine ? yours : theirs} ${first ? (mine ? tail.yours : tail.theirs) : ""}`}>
        {quote && (
          <span className={`mb-1.5 flex flex-col rounded-[4px_8px_8px_4px] border-l-[3px] px-2.5 py-1.5 text-[13px] leading-snug ${mine ? "border-accent-ink/75 bg-accent-ink/15" : "border-accent bg-surface-2"}`}>
            {quote.who && <b className="font-semibold">{quote.who}</b>}
            <span className="line-clamp-3 opacity-90">{quote.text}</span>
          </span>
        )}
        {mine ? (
          row.text
        ) : (
          <div className="whitespace-normal">
            <MessageText text={row.text} />
          </div>
        )}
        {/* Room on the last line for the time, as in any messenger. */}
        <span aria-hidden="true" className="inline-block w-12" />
        <Stamp at={row.at} mine={mine} />
      </div>
    );
  }
  if (row.kind === "file") {
    const p = row.payload ?? {};
    return (
      <div className={card}>
        <Stamp at={row.at} />
        {row.text && <span className="whitespace-pre-wrap">{row.text}</span>}
        <div className="flex flex-wrap items-center gap-3.5">
          <FileIcon file={p.name ?? ""} size={40} />
          <div className="flex min-w-0 flex-[1_1_160px] flex-col">
            <b className="truncate text-[15px]">{p.name}</b>
            <span className="text-[13px] text-muted">
              {kindOf(p.name ?? "").name} · {size(p.size ?? 0)}
            </span>
          </div>
          <a className="inline-flex min-h-9 items-center rounded-lg border border-line-strong px-3.5 text-[13px] font-semibold text-fg hover:border-fg" href={`/api/agents/${agent}/files/${p.file}`} download={p.name}>
            {kindOf(p.name ?? "").name === "Calendar" ? "Add to calendar" : "Download"}
          </a>
        </div>
        {p.work && p.format && <WorkPreview agent={agent} work={p.work} format={p.format} />}
      </div>
    );
  }
  if (row.kind === "ask" && row.payload?.title) return <AskCard {...props} />;
  // An ask or a choice: open until settled, when its text says how it ended.
  const p = row.payload ?? {};
  const open = !row.settled_at && !tapped;
  return (
    <div className={card}>
      <Stamp at={row.at} />
      <span className="text-xs font-semibold tracking-[0.04em] text-muted uppercase">{row.kind === "ask" ? `${name} asks` : "Your call"}</span>
      <span className="whitespace-pre-wrap">{row.text}</span>
      {open && row.kind === "ask" && (
        <div className="flex flex-wrap gap-2">
          <Button kind="primary" type="button" disabled={busy} onClick={() => answer(row, { approval: p.approval, approve: true }, "You said yes.")}>Approve</Button>
          <Button type="button" disabled={busy} onClick={() => answer(row, { approval: p.approval, approve: false }, "You said no.")}>Deny</Button>
        </div>
      )}
      {open && row.kind === "choice" && (
        <div className="flex flex-wrap gap-2">
          {Array.from({ length: p.options ?? 0 }, (_, i) => (
            <Button key={i} type="button" disabled={busy} kind={i === p.pick ? "primary" : "secondary"} onClick={() => answer(row, { call: p.call, option: i }, `You picked ${i + 1}.`)}>
              {i + 1}
              {i === p.pick ? ` (${name}'s pick)` : ""}
            </Button>
          ))}
          <Button type="button" disabled={busy} onClick={() => answer(row, { call: p.call, option: -1 }, `Tell ${name} in a message.`)}>Something else</Button>
        </div>
      )}
      {failed && <span role="alert" className="text-[13px] text-bad-text">{failed}</span>}
      {!row.settled_at && tapped && <span className="text-[13px] text-muted">{tapped}</span>}
    </div>
  );
}

// A request as a card: what, the exact words, where, when and whether it can be undone,
// why, and what silence means. Approve, Deny, or (when it has words to change) Change it,
// and approve your version. Settled, its text ends with how.
function AskCard({ row, name, tapped, busy, failed, answer }: Card) {
  const p = row.payload ?? {};
  const [why, setWhy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(p.quote ?? "");
  const open = !row.settled_at && !tapped;
  const ended = row.settled_at ? row.text.split("\n\n").pop() : "";
  const expires = p.expires ? new Date(p.expires) : null;
  const yours = draft.trim();
  return (
    <div className={card}>
      <Stamp at={row.at} />
      <span className="flex justify-between gap-2 text-xs font-bold tracking-[0.04em] text-muted uppercase">
        <span>{name} asks</span>
        {open && expires && <span className="font-semibold tracking-normal normal-case">{left(expires)}</span>}
      </span>
      <b className="font-semibold">{p.title}</b>
      {editing && !open ? (
        <div className="flex flex-col gap-2">
          <div className="flex flex-col gap-1">
            <span className="text-xs font-semibold text-muted">{name}&apos;s draft</span>
            <span className="rounded-md bg-surface-2 px-3 py-2.5 text-sm whitespace-pre-wrap text-muted">{p.quote}</span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-xs font-semibold text-accent-text">Yours, going out</span>
            <span className="rounded-md bg-accent-soft px-3 py-2.5 text-sm whitespace-pre-wrap">{yours}</span>
          </div>
        </div>
      ) : editing ? (
        <div className="flex flex-col gap-1.5">
          <label htmlFor={`draft-${row.id}`} className="text-[13px] font-semibold">Your version</label>
          <textarea
            id={`draft-${row.id}`}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={4}
            maxLength={4000}
            className="min-h-24 w-full rounded-md border border-accent bg-surface px-3 py-2.5 text-sm text-fg focus:outline-2 focus:outline-offset-1 focus:outline-accent"
          />
          <span className="text-xs text-muted">{name} uses your version, exactly as you write it.</span>
        </div>
      ) : (
        p.quote && <span className="rounded-md bg-surface-2 px-3 py-2.5 text-sm whitespace-pre-wrap">{p.quote}</span>
      )}
      {(p.where || p.when || p.undo) && (
        <dl className="m-0 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
          {p.where && (
            <>
              <dt className="text-muted">Where</dt>
              <dd className="m-0">{p.where}</dd>
            </>
          )}
          {p.when && (
            <>
              <dt className="text-muted">When</dt>
              <dd className="m-0">{p.when}</dd>
            </>
          )}
          {p.undo && (
            <>
              <dt className="text-muted">Undo</dt>
              <dd className={`m-0 flex items-center gap-1.5 ${p.final ? "font-semibold text-warn-text" : ""}`}>
                {p.final && (
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none">
                    <path d="M12 3l10 18H2z" />
                    <path d="M12 10v5" />
                    <path d="M12 18h.01" />
                  </svg>
                )}
                <span>{p.undo}</span>
              </dd>
            </>
          )}
        </dl>
      )}
      {p.why && (
        <>
          <button type="button" aria-expanded={why} onClick={() => setWhy((w) => !w)} className="inline-flex min-h-11 items-center gap-1.5 self-start text-[13px] font-semibold text-accent-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={why ? "rotate-90" : ""}>
              <path d="M9 6l6 6-6 6" />
            </svg>
            Why this?
          </button>
          {why && <span className="-mt-1.5 rounded-md bg-surface-2 px-3 py-2.5 text-[13px]">{p.why}</span>}
        </>
      )}
      {open && !editing && (
        <div className="flex flex-wrap items-center gap-2">
          <Button kind="primary" type="button" disabled={busy} onClick={() => answer(row, { approval: p.approval, approve: true }, "You said yes.")}>Approve</Button>
          <Button type="button" disabled={busy} onClick={() => answer(row, { approval: p.approval, approve: false }, "You said no.")}>Deny</Button>
          {p.editable && <Button type="button" disabled={busy} onClick={() => setEditing(true)}>Change it</Button>}
        </div>
      )}
      {open && editing && (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            kind="primary"
            type="button"
            disabled={!yours || busy}
            onClick={() => answer(row, { approval: p.approval, approve: true, text: yours }, "You said yes, with your changes.")}
          >
            Approve your version
          </Button>
          <Button
            type="button"
            onClick={() => {
              setEditing(false);
              setDraft(p.quote ?? "");
            }}
          >
            Cancel
          </Button>
        </div>
      )}
      {failed && <span role="alert" className="text-[13px] text-bad-text">{failed}</span>}
      {!row.settled_at && tapped && <span className="text-[13px] font-semibold text-muted">{tapped}</span>}
      {ended && <span className="text-[13px] font-semibold text-muted">{ended}</span>}
      {open && expires && (
        <span className="text-xs text-muted">No answer by {expires.toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })} means no.</span>
      )}
    </div>
  );
}

// left is the time until expires, like "20 h left".
function left(expires: Date) {
  const minutes = Math.max(0, Math.round((expires.getTime() - Date.now()) / 60_000));
  if (minutes < 60) return `${minutes} min left`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours} h left` : `${Math.round(hours / 24)} days left`;
}

function size(bytes: number) {
  if (bytes < 1024) return `${bytes} bytes`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

const rowAction = "grid size-9 place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg";

// RowMenu is a message's menu: reply to it, or move it to another topic, a new one or none.
function RowMenu({ row, mine, topics, close, reply, copy, move }: {
  row: ChatRow;
  mine: boolean;
  topics: Topic[];
  close: () => void;
  reply: () => void;
  copy: () => void;
  move: (to: string) => void;
}) {
  const first = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    first.current?.focus();
    const esc = (e: KeyboardEvent) => e.key === "Escape" && close();
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [close]);
  const item = "flex min-h-10 w-full items-center rounded-md px-3 text-left text-sm text-fg hover:bg-surface-2";
  return (
    <>
      <button type="button" aria-label="Close the menu" tabIndex={-1} onClick={close} className="fixed inset-0 z-20 cursor-default" />
      <div role="menu" aria-label="This message" className={`absolute top-full z-30 mt-1 flex w-64 flex-col rounded-xl border border-line-strong bg-bg p-1.5 shadow-lg ${mine ? "right-0" : "left-0"}`}>
        <button ref={first} type="button" role="menuitem" onClick={reply} className={item}>
          Reply
        </button>
        {row.text && (
          <button type="button" role="menuitem" onClick={copy} className={item}>
            Copy
          </button>
        )}
        <span className="px-3 pt-2 pb-0.5 text-[11.5px] font-bold tracking-[0.05em] text-muted uppercase">Move to</span>
        {topics.map((t) => (
          <button key={t.id} type="button" role="menuitem" onClick={() => move(t.id)} className={item}>
            {t.title || "New topic"}
          </button>
        ))}
        <button type="button" role="menuitem" onClick={() => move("new")} className={`${item} font-semibold text-accent-text`}>
          New topic
        </button>
        {row.topic && (
          <button type="button" role="menuitem" onClick={() => move("")} className={item}>
            No topic
          </button>
        )}
        {mine && <span className="mt-1 border-t border-line px-3 pt-1.5 pb-1 text-xs text-muted">Their answer moves with it.</span>}
      </div>
    </>
  );
}

// ComposerBar sits on the composer: what you're replying to, or that you're starting a topic.
function ComposerBar({ title, text, stop, onStop, dashed }: { title: string; text: string; stop: string; onStop: () => void; dashed?: boolean }) {
  return (
    <div className={`flex w-full min-w-0 basis-full items-center gap-2.5 rounded-xl border-l-4 border-l-accent bg-surface py-2 pr-1.5 pl-3.5 ${dashed ? "border border-dashed border-accent/50" : "border border-line"}`}>
      <div className="flex min-w-0 flex-1 flex-col text-[13px] leading-snug">
        <b className="text-accent-text">{title}</b>
        <span className="truncate text-muted">{text}</span>
      </div>
      <button type="button" aria-label={stop} onClick={onStop} className="grid size-10 flex-none place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
          <path d="M6 6l12 12" />
          <path d="M18 6L6 18" />
        </svg>
      </button>
    </div>
  );
}

// Answers are a reply's likely answers to tap, under the newest message only: one sends at
// a tap; several are picked, then sent. Typing anything else works as ever.
function Answers({ answers, many, busy, send }: { answers: string[]; many: boolean; busy: boolean; send: (said: string) => void }) {
  const [picked, setPicked] = useState<string[]>([]);
  const answer = "inline-flex min-h-10 items-center gap-1.5 rounded-lg border border-accent px-4 text-sm font-semibold text-accent-text hover:bg-accent-soft disabled:opacity-60";
  const join = (xs: string[]) => new Intl.ListFormat("en-GB", { type: "conjunction" }).format(xs);
  return (
    <div role="group" aria-label="Answers to tap" className="mt-2 flex flex-col gap-2 self-start pl-1">
      <div className="flex flex-wrap gap-2">
        {answers.map((a) =>
          many ? (
            <button
              key={a}
              type="button"
              aria-pressed={picked.includes(a)}
              onClick={() => setPicked((p) => (p.includes(a) ? p.filter((x) => x !== a) : [...p, a]))}
              className={`${answer} aria-pressed:bg-accent aria-pressed:text-accent-ink`}
            >
              {picked.includes(a) && (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M5 12l5 5 9-10" />
                </svg>
              )}
              {a}
            </button>
          ) : (
            <button key={a} type="button" disabled={busy} onClick={() => send(a)} className={answer}>
              {a}
            </button>
          ),
        )}
      </div>
      {many && (
        <span className="flex items-center gap-2.5">
          <Button kind="primary" type="button" disabled={busy || !picked.length} onClick={() => send(join(answers.filter((a) => picked.includes(a))))} className="min-h-10 px-4">
            Send{picked.length ? ` (${picked.length})` : ""}
          </Button>
          <span className="text-xs text-muted">Pick any, or type instead.</span>
        </span>
      )}
    </div>
  );
}

// Thumbs are your word on something they made: up counts toward the skills it shows; down
// asks what was off, in a line, and that goes with it.
function Thumbs({ said, name, react }: { said?: { good: boolean; note?: string }; name: string; react: (good: boolean, note?: string) => Promise<void> }) {
  const [asking, setAsking] = useState(false);
  const [note, setNote] = useState("");
  const thumb = "grid size-9 place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg aria-pressed:bg-accent-soft aria-pressed:text-accent-text";
  return (
    <div className="mt-1 flex flex-col gap-1.5 self-start pl-1">
      <div className="flex items-center gap-0.5">
        <button type="button" aria-label="Good" aria-pressed={said?.good === true} onClick={() => react(true)} className={thumb}>
          <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M7 11v9H4v-9z" />
            <path d="M7 11l4-8c1.7 0 2.7 1.2 2.4 2.8L13 9h5.4a2 2 0 0 1 2 2.4l-1.3 7A2 2 0 0 1 17.1 20H7" />
          </svg>
        </button>
        <button type="button" aria-label="Not quite" aria-pressed={said?.good === false || asking} onClick={() => setAsking(true)} className={thumb}>
          <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M17 13V4h3v9z" />
            <path d="M17 13l-4 8c-1.7 0-2.7-1.2-2.4-2.8L11 15H5.6a2 2 0 0 1-2-2.4l1.3-7A2 2 0 0 1 6.9 4H17" />
          </svg>
        </button>
        {said && !asking && (
          <span role="status" className="pl-1 text-xs text-muted">
            {said.good ? `${name} counts it toward their skills.` : `Sent to ${name} as your feedback.`}
          </span>
        )}
      </div>
      {asking && (
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            await react(false, note.trim());
            setAsking(false);
          }}
          className="flex max-w-[460px] flex-wrap items-center gap-2 rounded-xl border border-line bg-surface px-3 py-2.5"
        >
          <label htmlFor="what-was-off" className="basis-full text-[13px] font-semibold">
            What was off? {name} learns from it.
          </label>
          <input id="what-was-off" value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} autoFocus className="min-h-9 min-w-0 flex-[1_1_220px] rounded-lg border border-line-strong bg-bg px-2.5 text-sm" />
          <Button kind="primary" type="submit" className="min-h-9 px-4 text-[13px]">Tell {name}</Button>
          <button type="button" onClick={() => setAsking(false)} className="min-h-9 px-2 text-[13px] font-semibold text-muted">
            Cancel
          </button>
        </form>
      )}
    </div>
  );
}
