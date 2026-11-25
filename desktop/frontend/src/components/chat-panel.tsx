import Link from "next/link";
import { useEffect, useState } from "react";
import { early } from "@/components/app-frame";
import { ServiceLogo } from "@/components/service-logo";
import { call, type Topic, type Work } from "@/lib/api";

// The chat's side panel, opened from the icons in its header (the way a coding app opens
// its panes): Topics (every topic, open first, the one you're in with where it stands),
// Coding jobs (every job, by what it wants from you, narrowed to a folder if you like) and
// Files (all they've sent). Beside the chat on a wide window; over it on a narrow one (a
// negative margin, not fixed: the page's easing in would pin a fixed panel to the page).

export type Panel = "" | "topics" | "jobs" | "files";

export type JobRow = { id: string; title: string; folder: string; topic: string; status: string; started_at: string; rounds: number; commit?: string; pr?: string };

const panelKey = "tinio.chat-panel";

// usePanel is the panel open, remembered on this computer from chat to chat.
export function usePanel(): [Panel, (p: Panel) => void] {
  const [panel, setPanel] = useState<Panel>("");
  useEffect(() => {
    try {
      const p = localStorage.getItem(panelKey);
      if (p === "topics" || p === "jobs" || p === "files") setPanel(p);
    } catch {
      // not remembered, then
    }
  }, []);
  const set = (p: Panel) => {
    setPanel(p);
    try {
      localStorage.setItem(panelKey, p);
    } catch {
      // not remembered, then
    }
  };
  return [panel, set];
}

// useJobs is the agent's coding jobs, read again whenever their cards change.
export function useJobs(agent: string, changed: string): JobRow[] {
  const [jobs, setJobs] = useState<JobRow[]>([]);
  useEffect(() => {
    call<{ jobs: JobRow[] }>(`/agents/${agent}/jobs`).then((res) => res.ok && setJobs(res.data.jobs ?? []));
  }, [agent, changed]);
  return jobs;
}

const icon = "relative grid size-10 flex-none place-items-center rounded-lg text-muted hover:bg-surface-2 hover:text-fg aria-pressed:bg-line aria-pressed:text-fg";

// PanelIcons are the header's switches for the panel; a dot on Coding jobs when one needs you.
export function PanelIcons({ panel, open, needsYou }: { panel: Panel; open: (p: Panel) => void; needsYou: boolean }) {
  const toggle = (p: Panel) => () => open(panel === p ? "" : p);
  return (
    <>
      <button type="button" aria-label="Topics" title="Topics" aria-pressed={panel === "topics"} onClick={toggle("topics")} className={icon}>
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
          <path d="M8 6h12M8 12h12M8 18h12" />
          <circle cx="4" cy="6" r="1" />
          <circle cx="4" cy="12" r="1" />
          <circle cx="4" cy="18" r="1" />
        </svg>
      </button>
      <button
        type="button"
        aria-label={needsYou ? "Coding jobs, one needs you" : "Coding jobs"}
        title="Coding jobs"
        aria-pressed={panel === "jobs"}
        onClick={toggle("jobs")}
        className={icon}
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M5 7l5 5-5 5" />
          <path d="M12 18h7" />
        </svg>
        {needsYou && <span aria-hidden="true" className="absolute top-1.5 right-1.5 size-2.5 rounded-full border-2 border-bg bg-warn-text" />}
      </button>
      <button type="button" aria-label="Files" title="Files" aria-pressed={panel === "files"} onClick={toggle("files")} className={icon}>
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
          <path d="M14 3v5h5" />
        </svg>
      </button>
    </>
  );
}

const row = "flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left text-fg no-underline hover:bg-surface-2 aria-pressed:bg-line";
const chip = "min-h-8 rounded-lg px-2.5 text-[12.5px] font-semibold text-muted hover:bg-surface-2 hover:text-fg aria-pressed:bg-line aria-pressed:text-fg";

function FolderLabel({ name }: { name: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-surface-2 px-1.5 text-[11.5px] font-semibold text-fg/80">
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinejoin="round" aria-hidden="true">
        <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      </svg>
      {name}
    </span>
  );
}

const day = (at: string) => {
  const d = new Date(at);
  const today = new Date();
  if (d.toDateString() === today.toDateString()) return "today";
  const yesterday = new Date(today.getTime() - 86400_000);
  if (d.toDateString() === yesterday.toDateString()) return "yesterday";
  return d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
};

export function SidePanel(props: {
  agent: string;
  name: string;
  panel: Panel;
  close: () => void;
  topics: Topic[];
  inTopic: string;
  show: (topic: string) => void;
  startTopic: () => void;
  jobs: JobRow[];
}) {
  const { panel, close } = props;
  useEffect(() => {
    const esc = (e: KeyboardEvent) => e.key === "Escape" && window.matchMedia("(max-width: 1100px)").matches && close();
    window.addEventListener("keydown", esc);
    return () => window.removeEventListener("keydown", esc);
  }, [close]);
  if (!panel) return null;
  const title = { topics: "Topics", jobs: "Coding jobs", files: "Files" }[panel];
  return (
    <aside
      aria-label={title}
      className="sticky top-0 z-30 flex h-dvh w-[340px] flex-none flex-col border-l border-line bg-bg max-[1100px]:-ml-[340px] max-[1100px]:shadow-2xl max-[560px]:-ml-[100vw] max-[560px]:w-screen"
    >
      <div className="flex items-center gap-2 px-4 pt-4 pb-2">
        <h2 className="flex-1 font-display text-base font-semibold">{title}</h2>
        <button type="button" aria-label="Close the panel" onClick={close} className="grid size-8 place-items-center rounded-lg text-muted hover:bg-surface-2 hover:text-fg">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
        {panel === "topics" && <Topics {...props} />}
        {panel === "jobs" && <Jobs agent={props.agent} jobs={props.jobs} />}
        {panel === "files" && <Files agent={props.agent} />}
      </div>
    </aside>
  );
}

function Topics({ agent, name, topics, inTopic, show, startTopic, jobs, close }: Parameters<typeof SidePanel>[0]) {
  const folders: Record<string, string[]> = {};
  for (const j of jobs) if (j.topic && !(folders[j.topic] ?? []).includes(j.folder)) folders[j.topic] = [...(folders[j.topic] ?? []), j.folder];
  const count: Record<string, number> = {};
  for (const j of jobs) if (j.topic) count[j.topic] = (count[j.topic] ?? 0) + 1;
  const pick = (t: string) => {
    show(t);
    if (window.matchMedia("(max-width: 1100px)").matches) close();
  };
  const sorted = [...topics.filter((t) => !t.quiet), ...topics.filter((t) => t.quiet)];
  return (
    <div className="flex flex-col gap-0.5">
      <button type="button" aria-pressed={!inTopic} onClick={() => pick("")} className={row}>
        <b className="text-[13.5px] font-semibold">Everything</b>
      </button>
      {sorted.map((t) => {
        const on = inTopic === t.id;
        return (
          <button key={t.id} type="button" aria-pressed={on} onClick={() => pick(t.id)} className={`${row} ${t.quiet && !on ? "opacity-70" : ""}`}>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="flex items-center gap-1.5">
                <b className="text-[13.5px] font-semibold">{t.title || "New topic"}</b>
                {t.waiting > 0 && <span aria-label="needs you" className="size-2 flex-none rounded-full bg-warn-text" />}
              </span>
              <span className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted">
                {t.quiet ? "Quiet" : "Open"} · {day(t.last_at || t.started_at)}
                {!!count[t.id] && ` · ${count[t.id] === 1 ? "1 job" : `${count[t.id]} jobs`}`}
                {(folders[t.id] ?? []).map((f) => (
                  <FolderLabel key={f} name={f} />
                ))}
              </span>
              {on && (
                <span className="mt-1.5 flex flex-col gap-1 text-[13px] leading-snug text-fg/85">
                  <span className="line-clamp-8">{t.summary || t.status || `${name} writes down where it stands once you pause.`}</span>
                  {!!t.settled.length && (
                    <span className="line-clamp-3 text-muted">
                      <b className="font-semibold text-fg">Settled:</b> {t.settled.join(" · ")}
                    </span>
                  )}
                  <Link href={`/agents/${agent}/topics`} onClick={(e) => e.stopPropagation()} className="self-start text-xs font-semibold text-accent-text">
                    Read it all
                  </Link>
                </span>
              )}
            </span>
          </button>
        );
      })}
      <div className="mt-2 flex flex-wrap gap-1 px-1">
        <button type="button" onClick={startTopic} className={chip}>
          + Start a topic
        </button>
        <Link href={`/agents/${agent}/topics`} className={`${chip} inline-flex items-center no-underline`}>
          All topics
        </Link>
      </div>
    </div>
  );
}

const groups: { head: string; has: (j: JobRow) => boolean; pill: string; word: (j: JobRow) => string }[] = [
  { head: "Needs you", has: (j) => j.status === "waiting", pill: "bg-warn-soft text-warn-text", word: () => "Needs you" },
  { head: "Working", has: (j) => j.status === "working", pill: "bg-accent-soft text-accent-text", word: () => "Working" },
  { head: "Ready for you", has: (j) => j.status === "ready" && !j.commit, pill: "bg-good-soft text-good-text", word: () => "Ready" },
  { head: "Done", has: (j) => !!j.commit, pill: "bg-surface-2 text-muted", word: (j) => (j.pr ? "PR open" : "Committed") },
  { head: "Stopped", has: (j) => ["stopped", "failed", "cancelled"].includes(j.status) && !j.commit, pill: "bg-surface-2 text-muted", word: (j) => (j.status === "failed" ? "Didn't work" : "Stopped") },
];

function Jobs({ agent, jobs }: { agent: string; jobs: JobRow[] }) {
  const [folder, setFolder] = useState("");
  const folders = [...new Set(jobs.map((j) => j.folder))];
  const shown = jobs.filter((j) => !folder || j.folder === folder);
  if (jobs.length === 0) return <p className="m-0 px-2.5 py-2 text-sm text-muted">No coding jobs yet. Ask for one in the chat.</p>;
  return (
    <div className="flex flex-col gap-3">
      {folders.length > 1 && (
        <div role="group" aria-label="Which folder" className="flex flex-wrap gap-1 px-1">
          <button type="button" aria-pressed={!folder} onClick={() => setFolder("")} className={chip}>
            All folders
          </button>
          {folders.map((f) => (
            <button key={f} type="button" aria-pressed={folder === f} onClick={() => setFolder(f)} className={chip}>
              {f}
            </button>
          ))}
        </div>
      )}
      {groups.map((g) => {
        const in_ = shown.filter(g.has);
        if (in_.length === 0) return null;
        return (
          <div key={g.head} className="flex flex-col gap-0.5">
            <span className="px-2.5 pb-1 text-xs font-semibold text-muted">{g.head}</span>
            {in_.map((j) => (
              <Link key={j.id} href={`/agents/${agent}/jobs/${j.id}`} {...early(`/agents/${agent}/jobs/${j.id}`)} className={row}>
                <ServiceLogo service="claude" size={26} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <b className="text-[13.5px] leading-snug font-semibold">{j.title}</b>
                  <span className="text-xs text-muted">
                    {j.folder} · {day(j.started_at)}
                    {j.rounds > 1 ? ` · ${j.rounds} rounds` : ""}
                  </span>
                </span>
                <span className={`flex-none rounded-md px-2 py-0.5 text-[11.5px] font-semibold ${g.pill}`}>{g.word(j)}</span>
              </Link>
            ))}
          </div>
        );
      })}
    </div>
  );
}

function Files({ agent }: { agent: string }) {
  const [works, setWorks] = useState<Work[] | null>(null);
  useEffect(() => {
    call<{ works: Work[] }>(`/agents/${agent}/works`).then((res) => setWorks(res.ok ? res.data.works : []));
  }, [agent]);
  if (works === null) return <p className="m-0 px-2.5 py-2 text-sm text-muted">Loading</p>;
  if (works.length === 0) return <p className="m-0 px-2.5 py-2 text-sm text-muted">Nothing sent yet.</p>;
  return (
    <div className="flex flex-col gap-0.5">
      {works.map((w) => (
        <Link key={w.id} href={`/agents/${agent}/works/${w.id}`} className={row}>
          <span aria-hidden="true" className="grid h-9 w-7 flex-none place-items-center rounded-md bg-accent-soft text-[9.5px] font-bold text-accent-text uppercase">
            {(w.formats?.[0] ?? w.kind).slice(0, 4)}
          </span>
          <span className="flex min-w-0 flex-1 flex-col">
            <b className="text-[13.5px] leading-snug font-semibold">{w.title}</b>
            <span className="text-xs text-muted first-letter:uppercase">
              {w.kind.replaceAll("_", " ")} · {day(w.at)}
            </span>
          </span>
        </Link>
      ))}
    </div>
  );
}
