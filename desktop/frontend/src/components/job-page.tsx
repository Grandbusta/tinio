import Link from "next/link";
import { Button } from "@tinio/ui";
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Avatar, useFrame } from "@/components/app-frame";
import { MessageText } from "@/components/message-text";
import { ServiceLogo } from "@/components/service-logo";
import { call } from "@/lib/api";

// A coding job, opened from its card: what happened, told by who did it; its changes, by
// file and by round, where a tapped line takes a note that goes with "Ask for changes";
// and the decision: commit it (on its branch, on this computer), ask for changes, or undo
// a round. Once committed, pushing it and opening a pull request is a second yes.

type Job = {
  id: string;
  title: string;
  brief?: string;
  folder: string;
  branch: string;
  base: string;
  model: string;
  status: "working" | "waiting" | "ready" | "stopped" | "failed" | "cancelled";
  card: string;
  rounds: number;
  commit?: string;
  pr?: string;
  cost_usd: number;
};
type Step = { at: string; who: "agent" | "cli" | "owner" | "tinio"; what: string; detail: string };
type Change = { path: string; added: number; removed: number; patch: string };
type Page = {
  job: Job;
  steps: Step[];
  changes: Change[];
  github: { installed: boolean; signed_in: boolean; account: string };
  origin: boolean;
  notes: string[];
  saying?: string; // what Claude Code is writing now, so far
  checking?: boolean; // the agent is checking the last round (it's ready meanwhile)
};
type Note = { file: string; line: number; text: string };

const pill: Record<Job["status"], [string, string]> = {
  working: ["Working", "bg-accent-soft text-accent-text"],
  waiting: ["Needs you", "bg-warn-soft text-fg"],
  ready: ["Ready for you", "bg-good-soft text-good-text"],
  stopped: ["Stopped", "bg-surface-2 text-muted"],
  failed: ["Didn't work", "bg-bad-soft text-bad-text"],
  cancelled: ["Stopped", "bg-surface-2 text-muted"],
};

// lines are a patch's lines with their numbers in the new file (or the old, for one taken out).
function lines(patch: string) {
  const out: { n: number; old: number; new: number; sign: "+" | "-" | " " | "@"; text: string }[] = [];
  let oldN = 0;
  let newN = 0;
  for (const l of patch.split("\n")) {
    const h = /^@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(l);
    if (h) {
      oldN = Number(h[1]);
      newN = Number(h[2]);
      out.push({ n: 0, old: 0, new: 0, sign: "@", text: l });
    } else if (l.startsWith("+")) out.push({ n: newN, old: 0, new: newN++, sign: "+", text: l.slice(1) });
    else if (l.startsWith("-")) out.push({ n: oldN, old: oldN++, new: 0, sign: "-", text: l.slice(1) });
    else if (l.startsWith("\\")) continue;
    else {
      out.push({ n: newN, old: oldN, new: newN, sign: " ", text: l.slice(1) });
      oldN++;
      newN++;
    }
  }
  return out;
}

// seen is each job page as last read, so going back to one draws it at once while it's read
// again.
const seen = new Map<string, Page>();

// back, when it's open in the chat's place, puts the chat back; ask is its question
// waiting on you, as the chat docks it.
export function JobPage({ agent, job, back, ask }: { agent: string; job: string; back?: () => void; ask?: ReactNode }) {
  const { agent: a } = useFrame();
  const [page, setPage] = useState<Page | null>(() => seen.get(`${agent}/${job}`) ?? null);
  const [round, setRound] = useState(0); // 0: all of it
  const [shut, setShut] = useState<Set<string>>(new Set()); // files folded away
  const [noting, setNoting] = useState<{ file: string; line: number } | null>(null);
  const [draft, setDraft] = useState("");
  const [notes, setNotes] = useState<Note[]>([]);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [committing, setCommitting] = useState(false);
  const [message, setMessage] = useState("");
  const [pushing, setPushing] = useState(false);
  const [undoing, setUndoing] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [wholeTop, setWholeTop] = useState(false); // a long last word, read in full
  // Two views: its changes, and how it went. Working, it opens on how it went (live);
  // ready, on the changes. Your pick holds until it starts or stops working.
  const [picked, setPicked] = useState<"" | "changes" | "story">("");
  const box = useRef<HTMLTextAreaElement>(null);
  // The header stays at the top; what else sticks (the file list, each file's header)
  // sticks under it, at its height (--head).
  const head = useRef<HTMLDivElement>(null);
  const [headH, setHeadH] = useState(0);
  useEffect(() => {
    const h = head.current;
    if (!h) return;
    const o = new ResizeObserver(() => setHeadH(h.offsetHeight));
    o.observe(h);
    return () => o.disconnect();
  });
  // The message box grows with what's written, up to a point, as the chat's does.
  useEffect(() => {
    const b = box.current;
    if (!b) return;
    b.style.height = "auto";
    b.style.height = `${Math.min(b.scrollHeight, 192)}px`;
  }, [note]);

  const load = useCallback(() => {
    call<Page>(`/agents/${agent}/jobs/${job}${round ? `?round=${round}` : ""}`).then((res) => {
      if (!res.ok) return setError(res.error.message);
      if (!round) seen.set(`${agent}/${job}`, res.data);
      setPage(res.data);
    });
  }, [agent, job, round]);
  useEffect(() => {
    load();
  }, [load]);
  const going = page?.job.status === "working" || page?.job.status === "waiting";
  useEffect(() => setPicked(""), [going]);
  const checking = !!page?.checking;
  useEffect(() => {
    if (!going && !checking) return;
    const t = setInterval(load, 1000); // what it does shows as it does it
    return () => clearInterval(t);
  }, [going, checking, load]);

  async function act(what: string, path: string, body: unknown, after?: () => void) {
    setBusy(what);
    setError("");
    const res = await call(`/agents/${agent}/jobs/${job}/${path}`, body);
    setBusy("");
    if (!res.ok) return setError(res.error.message);
    after?.();
    load();
  }

  // Read as the link was pointed at, it's rarely not here yet: nothing is shown meanwhile.
  if (!page) return error ? <div role="alert" className="p-10 text-sm text-bad-text">{error}</div> : null;
  const j = page.job;
  const name = a?.name ?? "Your agent";
  const added = page.changes.reduce((n, c) => n + c.added, 0);
  const removed = page.changes.reduce((n, c) => n + c.removed, 0);
  const finished = [...page.steps].reverse().find((s) => s.who === "cli" && (s.what === "Finished" || s.what === "Stopped"));
  const checked = [...page.steps].reverse().find((s) => s.who === "agent" && s.what.startsWith("Checked it"));
  const verdict = j.status === "ready" && checked && checked.what !== "Checked it and sent it back" ? checked.detail : "";
  const [label, tone] = pill[j.status] ?? pill.working;
  const canDecide = !going && !j.commit;

  const tab = picked || (going ? "story" : "changes");
  const story = roundsOf(page.steps, name);
  const current = story[story.length - 1];
  const live = going ? current : undefined;
  const plan = live?.plan ?? [];
  const planDone = plan.filter((t) => t.status === "completed").length;
  // What it's doing now: its latest action or words, this round.
  const nowStep = live && [...live.items].reverse().find((s) => s.what !== "Planned" && !s.what.startsWith("Asked to "));
  const nowText = page.saying ? firstLine(page.saying) : nowStep ? (nowStep.what === "Said" ? firstLine(nowStep.detail) : nowStep.what) : "Starting";

  // The top of the bar: the last word on the job, by who said it.
  const top: { agent: boolean; who: string; text: ReactNode } = going
    ? {
        agent: false,
        who: `Claude Code${story.length > 1 ? ` · round ${story.length}` : ""}${plan.length ? ` · ${planDone} of ${plan.length}` : ""}`,
        text: (
          <span className="inline-flex items-center gap-2">
            <span aria-hidden="true" className="size-2 flex-none animate-pulse rounded-full bg-accent" />
            {j.status === "waiting" ? (ask ? "Waiting on you: its question is just below" : "Waiting on you") : nowText}
          </span>
        ),
      }
    : j.pr
      ? { agent: false, who: "Pull request open", text: <>On <code>{j.branch}</code>. Pushing again adds what&apos;s committed since.</> }
      : j.commit
        ? { agent: false, who: "Committed", text: <>On <code>{j.branch || j.base}</code>, on this computer, as you. Nothing has left it.</> }
        : checking
          ? {
              agent: true,
              who: `${name} is checking it`,
              text: (
                <span className="inline-flex items-center gap-2">
                  <span aria-hidden="true" className="size-2 flex-none animate-pulse rounded-full bg-accent" />
                  Claude Code finished: it&apos;s ready for you while {name} runs the tests and reads the changes.
                </span>
              ),
            }
          : verdict
          ? { agent: true, who: `${name} checked it`, text: <MessageText text={verdict} /> }
          : finished?.detail
            ? { agent: false, who: j.status === "ready" ? "Claude Code finished" : "Claude Code stopped", text: <MessageText text={finished.detail} /> }
            : { agent: false, who: "Claude Code", text: page.changes.length ? "Look it over: commit it, ask for changes, or undo it." : "It made no changes." };
  const long = !going && !j.commit && !j.pr && ((verdict || finished?.detail || "").length > 240 || (verdict || finished?.detail || "").split("\n").length > 3);
  const placeholder = going
    ? `Tell the job something; ${name} passes it on`
    : j.commit
      ? "Carry the job on: what next?"
      : notes.length
        ? "Anything else to change?"
        : `Answer ${name}, or ask for changes`;
  const hint = going
    ? "It reaches Claude Code between steps."
    : j.commit
      ? "Enter starts another round on the same branch."
      : notes.length
        ? `${notes.length === 1 ? "A note on a line goes" : `${notes.length} notes on lines go`} with it · Enter sends it back as a new round`
        : "Enter sends it back as a new round · tap a line above to say something about it";
  const canSend = !!note.trim() || (canDecide && notes.length > 0);
  function say() {
    if (!canSend || busy) return;
    if (canDecide) return act("back", "back", { note, lines: notes }, () => (setNotes([]), setNote("")));
    act("back", "back", { note, plain: true }, () => setNote(""));
  }

  return (
    <div className="page-in flex min-h-full flex-col bg-bg" style={{ "--head": `${headH}px` } as CSSProperties}>
      <div ref={head} className="fs-top sticky top-0 z-20 flex flex-col gap-1.5 border-b border-line bg-bg px-8 pt-4 pb-3 max-[900px]:px-4">
        <Link
          href={`/agents/${agent}`}
          onClick={(e) => {
            if (!back) return;
            e.preventDefault();
            back();
          }}
          className="-ml-2 inline-flex min-h-9 items-center gap-1 self-start px-2 text-sm font-semibold text-accent-text"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M15 6l-6 6 6 6" />
          </svg>
          Chat with {name}
        </Link>
        <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2">
          <h1 className="font-display text-[24px] leading-tight font-medium tracking-[-0.02em]">{j.title}</h1>
          <span className={`rounded-md px-2.5 py-0.5 text-[12.5px] font-semibold ${tone}`}>{going ? label : j.commit && !j.pr ? "Committed" : j.pr ? "Pull request open" : label}</span>
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[13.5px] text-fg/75">
          <span>{j.folder}</span>
          {j.branch && (
            <span>
              <code>{j.branch}</code> from <code>{j.base}</code>
            </span>
          )}
          <span className="inline-flex items-center gap-1.5">
            <ServiceLogo service="claude" size={20} />
            Claude Code · {j.model}
          </span>
          {j.pr && (
            <a href={j.pr} target="_blank" rel="noreferrer" className="font-semibold text-accent-text">
              The pull request
            </a>
          )}
        </div>
        <div role="tablist" aria-label="The job" className="-mb-3 flex gap-5 pt-1">
          {(
            [
              ["changes", "Changes", `${page.changes.length} ${page.changes.length === 1 ? "file" : "files"}`],
              ["story", "How it went", story.length === 1 ? "1 round" : `${story.length} rounds`],
            ] as const
          ).map(([id, label, count]) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={tab === id}
              onClick={() => setPicked(id)}
              className="inline-flex min-h-10 items-center gap-2 border-b-2 border-transparent text-sm font-semibold text-muted hover:text-fg aria-selected:border-fg aria-selected:text-fg"
            >
              {label}
              <span className="rounded-full bg-surface-2 px-2 text-xs font-semibold text-fg/75">{count}</span>
              {id === "changes" && (added > 0 || removed > 0) && (
                <span className="text-xs">
                  <b className="text-good-text">+{added}</b> <b className="text-bad-text">−{removed}</b>
                </span>
              )}
              {id === "story" && going && tab !== "story" && <span aria-label="working" className="size-2 animate-pulse rounded-full bg-accent" />}
            </button>
          ))}
        </div>
      </div>

      <div className="flex flex-1 flex-col">
        {tab === "story" && (
          <section aria-label="How it went" className="flex min-w-0 flex-col gap-3 px-8 pt-5 pb-8 max-[900px]:px-4">
            {verdict && (
              <div className="flex items-start gap-3 rounded-lg border border-line px-4 py-3">
                {a && <Avatar agent={a} size={28} />}
                <span className="flex min-w-0 flex-col">
                  <span className="text-[12.5px] font-semibold text-muted">{name} checked it</span>
                  <span className="text-sm">
                    <MessageText text={verdict} />
                  </span>
                </span>
              </div>
            )}
            {[...story].reverse().map((r, i) =>
              r === live ? (
                <LiveRound key={r.n} r={r} total={story.length} saying={page.saying} />
              ) : (
                <PastRound
                  key={r.n}
                  r={r}
                  total={story.length}
                  open={i === 0 || (!!live && i === 1)}
                  changes={
                    // Its own changes, when the copy was kept after it.
                    story.length > 1 && r.n <= j.rounds
                      ? () => {
                          setRound(r.n);
                          setPicked("changes");
                        }
                      : undefined
                  }
                />
              ),
            )}
        {page.notes.length > 0 && (
          <details className="group rounded-lg border border-line px-4 py-1">
            <summary className="flex min-h-9 cursor-pointer list-none items-center gap-3 text-sm">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none text-muted transition-transform group-open:rotate-90">
                <path d="M9 6l6 6-6 6" />
              </svg>
              <h2 className="flex-none font-display text-[15px] font-semibold">
                What {name} keeps about {j.folder}
              </h2>
              <span className="min-w-0 truncate text-[13px] text-muted">
                {page.notes.length} {page.notes.length === 1 ? "note" : "notes"} · the next job here starts with them
              </span>
            </summary>
            <div className="max-w-[760px] pt-2 pb-2">
              <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
                {page.notes.map((n, i) => (
                  <li key={i} className="flex items-start gap-2 text-[13px]">
                    <span className="min-w-0 flex-1">{n}</span>
                    <button
                      type="button"
                      aria-label={`Take out: ${n}`}
                      title="Take this out"
                      disabled={!!busy}
                      onClick={() => act("notes", "notes", { notes: page.notes.filter((_, k) => k !== i) })}
                      className="grid size-7 flex-none place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-fg"
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                        <path d="M6 6l12 12M18 6L6 18" />
                      </svg>
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          </details>
        )}

          </section>
        )}
        <section aria-label="Changes" hidden={tab !== "changes"} className="flex min-w-0 flex-1 flex-col gap-3 px-8 pt-5 pb-8 max-[900px]:px-4">
          <span className="text-[13px] text-muted">{going ? "Filling in as it works · " : ""}Tap a line to say something about it</span>
          {round > 0 && (
            // One round's changes, opened from How it went.
            <span className="inline-flex items-center gap-1 self-start rounded-lg bg-line py-1 pr-1 pl-3 text-[13px] font-semibold">
              Round {round}&apos;s changes only
              <button type="button" aria-label="Show all the changes" onClick={() => setRound(0)} className="grid size-6 place-items-center rounded-md text-muted hover:bg-surface-2 hover:text-fg">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                  <path d="M6 6l12 12M18 6L6 18" />
                </svg>
              </button>
            </span>
          )}
          {page.changes.length === 0 && <p className="m-0 text-sm text-muted">{going ? "Nothing changed yet." : "No changes."}</p>}
          {page.changes.length > 0 && (
            <div className="flex items-start gap-4 max-[900px]:flex-col">
              <div
                role="group"
                aria-label="Files changed"
                className="sticky top-[calc(var(--head)+12px)] flex max-h-[calc(100dvh-var(--head)-200px)] w-[300px] flex-none flex-col gap-0.5 overflow-y-auto rounded-lg border border-line p-1 max-[900px]:static max-[900px]:max-h-none max-[900px]:w-full"
              >
                {page.changes.map((c, i) => {
                  const slash = c.path.lastIndexOf("/");
                  return (
                    <button
                      key={c.path}
                      type="button"
                      onClick={() => document.getElementById(`file-${i}`)?.scrollIntoView({ behavior: "smooth", block: "start" })}
                      title={c.path}
                      className="flex items-start justify-between gap-2.5 rounded-md px-2.5 py-1.5 text-left hover:bg-surface-2"
                    >
                      <span className="flex min-w-0 flex-col">
                        <span className="font-mono text-[12.5px] [overflow-wrap:anywhere]">{c.path.slice(slash + 1)}</span>
                        {slash > 0 && <span className="font-mono text-[11px] text-muted [overflow-wrap:anywhere]">{c.path.slice(0, slash)}</span>}
                      </span>
                      <span className="flex-none font-mono text-[12px]">
                        <b className="text-good-text">+{c.added}</b> <b className="text-bad-text">−{c.removed}</b>
                      </span>
                    </button>
                  );
                })}
              </div>
              <div className="flex min-w-0 flex-1 flex-col gap-3 max-[900px]:w-full">
          {page.changes.map((change, ci) => (
                <div role="group" aria-label={change.path} id={`file-${ci}`} className="scroll-mt-[calc(var(--head)+12px)] overflow-clip rounded-lg border border-line">
                  <button
                    type="button"
                    aria-expanded={!shut.has(change.path)}
                    onClick={() =>
                      setShut((x) => {
                        const y = new Set(x);
                        if (y.has(change.path)) y.delete(change.path);
                        else y.add(change.path);
                        return y;
                      })
                    }
                    className="sticky top-[var(--head)] z-10 flex w-full items-center gap-2 border-b border-line bg-surface-2 px-3 py-2 text-left font-mono text-[12.5px]"
                  >
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={`flex-none text-muted transition-transform ${shut.has(change.path) ? "" : "rotate-90"}`}>
                      <path d="M9 6l6 6-6 6" />
                    </svg>
                    <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">{change.path}</span>
                    <span className="flex-none">
                      <b className="text-good-text">+{change.added}</b> <b className="text-bad-text">−{change.removed}</b>
                    </span>
                  </button>
                  {!shut.has(change.path) &&
                    lines(change.patch).map((l, i) =>
                    l.sign === "@" ? (
                      <div key={i} className="bg-surface-2 px-3 py-0.5 font-mono text-[11.5px] text-muted">
                        {l.text}
                      </div>
                    ) : (
                      <div key={i}>
                        <button
                          type="button"
                          aria-label={`${l.sign === "+" ? "Added" : l.sign === "-" ? "Taken out" : "Unchanged"}, line ${l.n}: ${l.text || "blank"}. Tap to say something about it.`}
                          onClick={() => {
                            setNoting({ file: change.path, line: l.n });
                            setDraft("");
                          }}
                          className={`flex w-full text-left font-mono text-[12.5px] leading-[1.7] whitespace-pre-wrap hover:outline hover:outline-1 hover:outline-line-strong ${
                            l.sign === "+" ? "bg-good-soft text-good-text" : l.sign === "-" ? "bg-bad-soft text-bad-text" : ""
                          }`}
                        >
                          <span aria-hidden="true" className="w-10 flex-none pr-2 text-right text-muted/70">
                            {l.old || ""}
                          </span>
                          <span aria-hidden="true" className="w-10 flex-none pr-2.5 text-right text-muted">
                            {l.new || ""}
                          </span>
                          <span aria-hidden="true" className="w-4 flex-none">
                            {l.sign === " " ? "" : l.sign === "-" ? "−" : "+"}
                          </span>
                          <span className="min-w-0 flex-1 pr-3 break-all">{l.text}</span>
                        </button>
                        {noting?.file === change.path && noting.line === l.n && (
                          <form
                            className="flex flex-col gap-2 border-y border-accent/30 bg-accent-soft/40 py-2.5 pr-3 pl-15"
                            onSubmit={(e) => {
                              e.preventDefault();
                              if (draft.trim()) setNotes((ns) => [...ns, { file: change.path, line: l.n, text: draft.trim() }]);
                              setNoting(null);
                            }}
                          >
                            <label htmlFor="line-note" className="text-[13px] font-semibold">
                              About line {l.n}, to Claude Code
                            </label>
                            <textarea
                              id="line-note"
                              autoFocus
                              value={draft}
                              onChange={(e) => setDraft(e.target.value)}
                              rows={2}
                              className="rounded-md border border-line-strong bg-surface px-3 py-2 text-sm"
                            />
                            <div className="flex gap-2">
                              <Button kind="primary" type="submit">
                                Add to what goes back
                              </Button>
                              <Button type="button" onClick={() => setNoting(null)}>
                                Cancel
                              </Button>
                            </div>
                          </form>
                        )}
                      </div>
                    ),
                  )}
                </div>
              ))}
              </div>
            </div>
          )}
        </section>
      </div>

      <div className="sticky bottom-0 z-20 flex flex-col gap-3 border-t border-line bg-bg px-8 py-3.5 max-[900px]:px-4">
        {error && (
          <span role="alert" className="text-[13px] text-bad-text">
            {error}
          </span>
        )}
        {committing ? (
          <form
            className="flex flex-col gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              act("commit", "commit", { message }, () => setCommitting(false));
            }}
          >
            <label htmlFor="commit-message" className="text-sm font-semibold">
              The commit&apos;s message
            </label>
            <textarea id="commit-message" autoFocus value={message} onChange={(e) => setMessage(e.target.value)} rows={3} className="rounded-md border border-line-strong bg-surface px-3 py-2 text-sm" />
            <span className="text-xs text-muted">
              It&apos;s committed on {j.branch || j.base}, on this computer, as you. Nothing leaves it.
            </span>
            <div className="flex gap-2">
              <Button kind="primary" type="submit" disabled={!!busy}>
                {busy === "commit" ? "Committing" : "Commit"}
              </Button>
              <Button type="button" onClick={() => setCommitting(false)}>
                Cancel
              </Button>
            </div>
          </form>
        ) : undoing ? (
          <div className="flex flex-col gap-2">
            <span className="text-sm">
              {j.rounds > 1
                ? `This puts the job's copy back as it was after round ${j.rounds - 1}, and round ${j.rounds}'s changes are gone.`
                : "This puts the job's copy back as it was before the job started, and all its changes are gone."}{" "}
              Only the job's own copy changes: your checkout and {j.base} are untouched. It can't be redone.
            </span>
            <div className="flex gap-2">
              <Button kind="primary" type="button" disabled={!!busy} onClick={() => act("goback", "goback", {}, () => setUndoing(false))}>
                {busy === "goback" ? "Undoing" : j.rounds > 1 ? "Undo the last round" : "Discard the changes"}
              </Button>
              <Button type="button" onClick={() => setUndoing(false)}>
                Keep them
              </Button>
            </div>
          </div>
        ) : pushing ? (
          <div className="flex flex-col gap-2">
            <span className="text-sm">
              Push <code>{j.branch}</code> to the repository&apos;s origin, and open a pull request into <code>{j.base}</code>, as {page.github.account || "you"} on GitHub. This leaves your computer: everyone with access to the repository can see it.
            </span>
            <div className="flex gap-2">
              <Button kind="primary" type="button" disabled={!!busy} onClick={() => act("push", "push", { body: finished?.detail ?? "" }, () => setPushing(false))}>
                {busy === "push" ? "Pushing" : "Push and open it"}
              </Button>
              <Button type="button" onClick={() => setPushing(false)}>
                Not now
              </Button>
            </div>
          </div>
        ) : stopping ? (
          <div className="flex flex-col gap-2">
            <span className="text-sm">Stop it where it is? What it has changed so far stays in the job&apos;s copy, to look over. It can&apos;t be picked up again.</span>
            <div className="flex gap-2">
              <Button kind="primary" type="button" disabled={!!busy} onClick={() => act("stop", "cancel", {}, () => setStopping(false))}>
                {busy === "stop" ? "Stopping" : "Stop it"}
              </Button>
              <Button type="button" onClick={() => setStopping(false)}>
                Let it carry on
              </Button>
            </div>
          </div>
        ) : (
          <>
            {/* Who said what last, as in the chat, with what you can decide beside it. */}
            <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
              <span className="flex min-w-0 flex-[1_1_420px] items-start gap-3">
                {top.agent && a ? <Avatar agent={a} size={32} /> : <ServiceLogo service="claude" size={32} />}
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-[12.5px] font-semibold text-muted">{top.who}</span>
                  {/* Three lines at most, so the changes keep the room; the rest on asking. */}
                  <span className={`max-w-[72ch] text-[14.5px] leading-normal ${long && !wholeTop ? "max-h-[4.5em] overflow-hidden [mask-image:linear-gradient(to_bottom,black_55%,transparent)]" : long ? "max-h-[40vh] overflow-y-auto" : ""}`}>
                    {top.text}
                  </span>
                  {long && (
                    <button type="button" aria-expanded={wholeTop} onClick={() => setWholeTop(!wholeTop)} className="self-start text-[13px] font-semibold text-accent-text">
                      {wholeTop ? "Show less" : "Show all"}
                    </button>
                  )}
                </span>
              </span>
              <span className="ml-auto flex flex-none flex-wrap items-center gap-1.5">
                {going && (
                  <button type="button" onClick={() => setStopping(true)} className="min-h-10 rounded-lg px-3.5 text-[13.5px] font-semibold text-muted hover:bg-surface-2 hover:text-fg">
                    Stop it
                  </button>
                )}
                {canDecide && j.rounds > 0 && (
                  <button type="button" disabled={!!busy} onClick={() => setUndoing(true)} className="min-h-10 rounded-lg px-3.5 text-[13.5px] font-semibold text-muted hover:bg-surface-2 hover:text-fg">
                    {j.rounds > 1 ? "Undo the last round" : "Discard the changes"}
                  </button>
                )}
                {canDecide && page.changes.length > 0 && (
                  <Button
                    kind="primary"
                    type="button"
                    disabled={!!busy}
                    onClick={() => {
                      setMessage(j.title);
                      setCommitting(true);
                    }}
                  >
                    Commit it
                  </Button>
                )}
                {j.commit && j.branch && !going && page.origin && (
                  <Button kind="primary" type="button" disabled={!page.github.signed_in} onClick={() => setPushing(true)}>
                    {j.pr ? "Push again" : "Push and open a pull request"}
                  </Button>
                )}
              </span>
            </div>
            {going && ask}
            <form
              onSubmit={(e) => {
                e.preventDefault();
                say();
              }}
              className="flex flex-col gap-2 rounded-xl border border-line-strong bg-surface py-2 pr-2 pl-4 focus-within:outline-2 focus-within:outline-offset-1 focus-within:outline-accent"
            >
              {canDecide && notes.length > 0 && (
                <span className="flex flex-wrap gap-1.5 pt-0.5">
                  {notes.map((n, i) => (
                    <span key={i} className="inline-flex max-w-full items-center gap-2 rounded-lg bg-surface-2 py-1 pr-1 pl-2.5 text-[13px]">
                      <code className="flex-none">
                        {n.file.split("/").pop()}:{n.line}
                      </code>
                      <span className="min-w-0 truncate">{n.text}</span>
                      <button type="button" aria-label="Take this note out" onClick={() => setNotes((ns) => ns.filter((_, k) => k !== i))} className="grid size-6 flex-none place-items-center rounded-md text-muted hover:bg-line hover:text-fg">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                          <path d="M6 6l12 12M18 6L6 18" />
                        </svg>
                      </button>
                    </span>
                  ))}
                </span>
              )}
              <span className="flex items-end gap-2.5">
                <label htmlFor="job-say" className="sr-only">
                  {placeholder}
                </label>
                {/* Enter sends; Shift+Enter starts a new line. */}
                <textarea
                  ref={box}
                  id="job-say"
                  rows={1}
                  value={note}
                  maxLength={4000}
                  onChange={(e) => setNote(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                      e.preventDefault();
                      say();
                    }
                  }}
                  placeholder={placeholder}
                  className="max-h-48 min-h-10 min-w-0 flex-1 resize-none bg-transparent py-2 text-[14.5px] leading-6 text-fg outline-none"
                />
                <button
                  type="submit"
                  aria-label="Send"
                  disabled={!!busy || !canSend}
                  className="grid size-10 flex-none place-items-center rounded-full bg-accent text-accent-ink hover:bg-accent-hover disabled:cursor-not-allowed disabled:bg-line disabled:text-muted"
                >
                  <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M5 12h14" />
                    <path d="M13 6l6 6-6 6" />
                  </svg>
                </button>
              </span>
            </form>
            <span className="-mt-1 pl-1 text-xs text-muted">{hint}</span>
            {j.commit && j.branch && !page.origin && <span className="text-xs text-muted">The repository has no origin, so there&apos;s nowhere to push it.</span>}
            {j.commit && page.origin && !page.github.signed_in && <span className="text-xs text-muted">Sign in to GitHub&apos;s gh on this computer to push.</span>}
          </>
        )}
      </div>
    </div>
  );
}

// A round of the job, from its steps: what started it, Claude Code's words, its plan (the
// latest of its to-do list), what it did, and how it ended. Steps from before rounds
// were marked make up the first.
type Todo = { content: string; status: "pending" | "in_progress" | "completed"; activeForm?: string };
type Round = { n: number; from: string; at: string; items: Step[]; said: string[]; plan: Todo[]; actions: Step[]; end?: Step };

const endings = ["Finished", "Stopped", "Didn't work out", "Stopped by you"];

function roundsOf(steps: Step[], name: string): Round[] {
  const out: Round[] = [];
  let before: Step | undefined;
  for (const st of steps) {
    if (st.who === "tinio" && st.what === "Round started") {
      const n = out.length + 1;
      const from =
        n === 1
          ? `${name}'s brief`
          : before?.who === "owner"
            ? `what you said: "${firstLine((before.detail || before.what).replace(/^The owner looked at your changes and sends them back:\s*/, ""))}"`
            : before?.who === "agent"
              ? `${name} sent it back`
              : "carried on";
      out.push({ n, from, at: st.at, items: [], said: [], plan: [], actions: [] });
      continue;
    }
    before = st.who === "owner" || st.who === "agent" ? st : before;
    if (!out.length) out.push({ n: 1, from: `${name}'s brief`, at: st.at, items: [], said: [], plan: [], actions: [] });
    if (st.who !== "cli") continue;
    // A job from before rounds were marked: a new one after each ending.
    if (out[out.length - 1].end) {
      const from = before?.who === "owner" ? `what you said: "${firstLine((before.detail || before.what).replace(/^The owner looked at your changes and sends them back:\s*/, ""))}"` : before?.who === "agent" ? `${name} sent it back` : "carried on";
      out.push({ n: out.length + 1, from, at: st.at, items: [], said: [], plan: [], actions: [] });
    }
    const r = out[out.length - 1];
    r.items.push(st);
    if (st.what === "Said") r.said.push(st.detail);
    else if (st.what === "Planned") {
      try {
        r.plan = JSON.parse(st.detail) ?? [];
      } catch {
        // an unreadable plan: the last one stands
      }
    } else if (endings.includes(st.what)) r.end = st;
    else if (!st.what.startsWith("Asked to ")) r.actions.push(st); // its questions have their card
  }
  return out;
}

function firstLine(t: string) {
  const l = t.trim().split("\n")[0];
  return l.length > 120 ? l.slice(0, 119) + "…" : l;
}

// done is an action as done: "Reading a.go" is "Read a.go", once the round is over.
function done(what: string) {
  const [w, ...rest] = what.split(" ");
  const past: Record<string, string> = { Reading: "Read", Changing: "Changed", Running: "Ran", Searching: "Searched", Finding: "Found", Looking: "Looked", Handing: "Handed", Using: "Used" };
  return [past[w] ?? w, ...rest].join(" ");
}

const icons: [RegExp, ReactNode][] = [
  [/^(Reading|Read |Looking|Looked)/, <path key="r" d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8zM14 3v5h5" />],
  [/^(Changing|Changed)/, <path key="c" d="M4 20h4l10.5-10.5a2.1 2.1 0 0 0-3-3L5 17v3z" />],
  [/^(Running|Ran )/, <path key="x" d="M5 7l5 5-5 5M12 18h7" />],
  [/^(Searching|Searched|Finding|Found)/, <path key="s" d="M11 4.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM16 16l4.5 4.5" />],
];

function Action({ s, now, past }: { s: Step; now?: boolean; past?: boolean }) {
  const icon = icons.find(([re]) => re.test(s.what))?.[1] ?? <circle cx="12" cy="12" r="3" />;
  return (
    <li className={`flex min-h-7 items-center gap-2.5 text-[13.5px] ${now ? "font-semibold" : "text-fg/80"}`}>
      <span aria-hidden="true" className={`size-1.5 flex-none rounded-full ${now ? "animate-pulse bg-accent" : "bg-transparent"}`} />
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={`flex-none ${now ? "text-accent" : "text-muted"}`}>
        {icon}
      </svg>
      <span className="min-w-0 truncate" title={s.detail || s.what}>
        {past ? done(s.what) : s.what}
      </span>
    </li>
  );
}

function Plan({ plan }: { plan: Todo[] }) {
  return (
    <ol className="m-0 flex list-none flex-col gap-1 p-0">
      {plan.map((t, i) => (
        <li key={i} className="flex items-start gap-2.5 py-0.5 text-sm">
          {t.status === "completed" ? (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true" className="mt-0.5 flex-none">
              <circle cx="12" cy="12" r="10" className="fill-good-text" />
              <path d="M7.5 12.5l3 3 6-6.5" stroke="white" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          ) : t.status === "in_progress" ? (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true" className="mt-0.5 flex-none text-accent">
              <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2.2" />
              <circle cx="12" cy="12" r="4.5" fill="currentColor" className="animate-pulse" />
            </svg>
          ) : (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true" className="mt-0.5 flex-none text-muted">
              <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" />
            </svg>
          )}
          <span className={t.status === "completed" ? "text-muted line-through decoration-muted/60" : t.status === "in_progress" ? "font-semibold" : ""}>
            {t.status === "in_progress" && t.activeForm ? t.activeForm : t.content}
          </span>
        </li>
      ))}
    </ol>
  );
}

const small = "text-xs font-semibold text-muted";

// LiveRound is the round under way, as it goes: its plan, its latest words, what it's doing.
function LiveRound({ r, total, saying }: { r: Round; total: number; saying?: string }) {
  const [all, setAll] = useState(false);
  const acts = [...r.actions].reverse();
  const shown = all ? acts : acts.slice(0, 6);
  const words = saying || r.said[r.said.length - 1]; // as it's written, then as it was
  const done = r.plan.filter((t) => t.status === "completed").length;
  return (
    <section aria-label={`Round ${r.n}, working`} className="overflow-hidden rounded-xl border border-accent/30 bg-accent-soft/20">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 border-b border-line bg-bg px-4 py-3">
        <span aria-hidden="true" className="size-2.5 animate-pulse rounded-full bg-accent" />
        <h2 className="font-display text-[15px] font-semibold">{total > 1 ? `Round ${r.n} · working` : "Working"}</h2>
        <span className="min-w-0 truncate text-[13px] text-muted">from {r.from}</span>
      </div>
      <div className="grid grid-cols-2 max-[900px]:grid-cols-1">
        <div className="flex flex-col gap-2 border-r border-line px-4 py-3.5 max-[900px]:border-r-0 max-[900px]:border-b">
          <span className={small}>{r.plan.length ? `Its plan · ${done} of ${r.plan.length} done` : "Its plan"}</span>
          {r.plan.length ? <Plan plan={r.plan} /> : <span className="text-sm text-muted">It hasn&apos;t written one: the job may be small enough not to need it.</span>}
        </div>
        <div className="flex min-w-0 flex-col gap-2.5 px-4 py-3.5">
          <span className={small}>What it&apos;s saying</span>
          {words ? (
            <div className="rounded-lg border border-line bg-bg px-3 py-2.5 text-sm">
              <MessageText text={words} />
              {saying && <span aria-hidden="true" className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse rounded-[1px] bg-accent align-[-2px]" />}
            </div>
          ) : (
            <span className="text-sm text-muted">Nothing yet.</span>
          )}
          <span className={`${small} mt-1`}>What it&apos;s doing</span>
          {acts.length ? (
            <ul className="m-0 flex list-none flex-col p-0">
              {shown.map((s, i) => (
                <Action key={acts.length - i} s={s} now={i === 0} past={i > 0} />
              ))}
            </ul>
          ) : (
            <span className="text-sm text-muted">Starting.</span>
          )}
          {acts.length > 6 && (
            <button type="button" onClick={() => setAll(!all)} className="self-start text-[13px] font-semibold text-accent-text">
              {all ? "Show less" : `+ ${acts.length - 6} earlier this round`}
            </button>
          )}
        </div>
      </div>
    </section>
  );
}

// PastRound is a round over: folded to a line, opened to its plan, words and actions.
function PastRound({ r, total, open, changes }: { r: Round; total: number; open: boolean; changes?: () => void }) {
  const mins = r.end ? Math.max(1, Math.round((new Date(r.end.at).getTime() - new Date(r.at).getTime()) / 60000)) : 0;
  return (
    <details open={open} className="group rounded-lg border border-line px-4 py-1">
      <summary className="flex min-h-10 cursor-pointer list-none items-center gap-2.5 text-sm">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none text-muted transition-transform group-open:rotate-90">
          <path d="M9 6l6 6-6 6" />
        </svg>
        <b className="flex-none font-semibold">{total > 1 ? `Round ${r.n}` : "The round"}</b>
        <span className="min-w-0 truncate text-muted">
          · from {r.from} · {r.actions.length === 1 ? "1 action" : `${r.actions.length} actions`}
          {mins ? ` · ${mins} min` : ""}
          {r.end && ` · ${r.end.what}`}
        </span>
      </summary>
      <div className="grid grid-cols-2 gap-5 pt-1.5 pb-3 pl-6 max-[900px]:grid-cols-1">
        <div className="flex flex-col gap-2">
          <span className={small}>{r.plan.length ? `Its plan · ${r.plan.filter((t) => t.status === "completed").length} of ${r.plan.length} done` : "Its plan"}</span>
          {r.plan.length ? <Plan plan={r.plan} /> : <span className="text-sm text-muted">It didn&apos;t write one.</span>}
        </div>
        <div className="flex min-w-0 flex-col gap-2.5">
          <span className={small}>What it said</span>
          {r.said.length ? (
            r.said.map((t, i) => (
              <div key={i} className="text-sm">
                <MessageText text={t} />
              </div>
            ))
          ) : r.end?.detail ? (
            <div className="text-sm">
              <MessageText text={r.end.detail} />
            </div>
          ) : (
            <span className="text-sm text-muted">Nothing kept.</span>
          )}
          {changes && (
            <button type="button" onClick={changes} className="self-start text-[13px] font-semibold text-accent-text">
              See this round&apos;s changes
            </button>
          )}
          {r.actions.length > 0 && (
            <details className="group/acts">
              <summary className="flex min-h-8 cursor-pointer list-none items-center gap-2 text-[13px] text-fg/80">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none transition-transform group-open/acts:rotate-90">
                  <path d="M9 6l6 6-6 6" />
                </svg>
                What it did · {r.actions.length === 1 ? "1 action" : `${r.actions.length} actions`}
              </summary>
              <ul className="m-0 flex list-none flex-col p-0">
                {[...r.actions].reverse().map((s, i) => (
                  <Action key={i} s={s} past />
                ))}
              </ul>
            </details>
          )}
        </div>
      </div>
    </details>
  );
}
