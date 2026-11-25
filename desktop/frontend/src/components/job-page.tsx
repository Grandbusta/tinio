import Link from "next/link";
import { Button } from "@tinio/ui";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
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
type Page = { job: Job; steps: Step[]; changes: Change[]; github: { installed: boolean; signed_in: boolean; account: string }; origin: boolean; notes: string[] };
type Note = { file: string; line: number; text: string };

const pill: Record<Job["status"], [string, string]> = {
  working: ["Working", "bg-accent-soft text-accent-text"],
  waiting: ["Needs you", "bg-warn-soft text-fg"],
  ready: ["Ready for you", "bg-good-soft text-good-text"],
  stopped: ["Stopped", "bg-surface-2 text-muted"],
  failed: ["Didn't work", "bg-bad-soft text-bad-text"],
  cancelled: ["Stopped", "bg-surface-2 text-muted"],
};

const dot: Record<Step["who"], string> = { agent: "bg-accent", cli: "bg-fg", owner: "bg-warn-text", tinio: "bg-muted" };

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

// back, when it's open in the chat's place, puts the chat back.
export function JobPage({ agent, job, back }: { agent: string; job: string; back?: () => void }) {
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
  const box = useRef<HTMLTextAreaElement>(null);
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
  useEffect(() => {
    if (!going) return;
    const t = setInterval(load, 3000);
    return () => clearInterval(t);
  }, [going, load]);

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
  const who = (s: Step) => ({ agent: name, cli: "Claude Code", owner: "You", tinio: "Tinio" })[s.who];
  const added = page.changes.reduce((n, c) => n + c.added, 0);
  const removed = page.changes.reduce((n, c) => n + c.removed, 0);
  const finished = [...page.steps].reverse().find((s) => s.who === "cli" && (s.what === "Finished" || s.what === "Stopped"));
  const latest = page.steps[page.steps.length - 1];
  const checked = [...page.steps].reverse().find((s) => s.who === "agent" && s.what.startsWith("Checked it"));
  const verdict = j.status === "ready" && checked && checked.what !== "Checked it and sent it back" ? checked.detail : "";
  const [label, tone] = pill[j.status] ?? pill.working;
  const canDecide = !going && !j.commit;

  // The top of the bar: the last word on the job, by who said it.
  const step = [...page.steps].reverse().find((s) => s.who === "cli");
  const top: { agent: boolean; who: string; text: ReactNode } = going
    ? {
        agent: false,
        who: `Claude Code${j.rounds > 1 ? ` · round ${j.rounds}` : ""}`,
        text: (
          <span className="inline-flex items-center gap-2">
            <span aria-hidden="true" className="size-2 flex-none animate-pulse rounded-full bg-accent" />
            {j.status === "waiting" ? "Waiting on you: its question is in the chat" : step?.what || "Working"}
          </span>
        ),
      }
    : j.pr
      ? { agent: false, who: "Pull request open", text: <>On <code>{j.branch}</code>. Pushing again adds what&apos;s committed since.</> }
      : j.commit
        ? { agent: false, who: "Committed", text: <>On <code>{j.branch || j.base}</code>, on this computer, as you. Nothing has left it.</> }
        : verdict
          ? { agent: true, who: `${name} checked it`, text: <MessageText text={verdict} /> }
          : finished?.detail
            ? { agent: false, who: j.status === "ready" ? "Claude Code finished" : "Claude Code stopped", text: <MessageText text={finished.detail} /> }
            : { agent: false, who: "Claude Code", text: page.changes.length ? "Look it over: commit it, ask for changes, or undo it." : "It made no changes." };
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
    <div className="page-in flex min-h-full flex-col bg-bg">
      <div className="flex flex-col gap-2.5 border-b border-line px-8 pt-8 pb-4 max-[900px]:px-4">
        <Link
          href={`/agents/${agent}`}
          onClick={(e) => {
            if (!back) return;
            e.preventDefault();
            back();
          }}
          className="-ml-2 inline-flex min-h-11 items-center gap-1 self-start px-2 text-sm font-semibold text-accent-text"
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M15 6l-6 6 6 6" />
          </svg>
          Chat with {name}
        </Link>
        <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2">
          <h1 className="font-display text-[28px] leading-tight font-medium tracking-[-0.02em]">{j.title}</h1>
          <span className={`rounded-md px-2.5 py-0.5 text-[12.5px] font-semibold ${tone}`}>{j.commit && !j.pr ? "Committed" : j.pr ? "Pull request open" : label}</span>
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
          <span>{j.rounds === 1 ? "1 round" : `${j.rounds} rounds`}</span>
          {j.cost_usd > 0 && <span>${j.cost_usd.toFixed(2)} of model time, on your plan</span>}
          {j.pr && (
            <a href={j.pr} target="_blank" rel="noreferrer" className="font-semibold text-accent-text">
              The pull request
            </a>
          )}
        </div>
      </div>

      <div className="flex flex-1 flex-col">
        <details className="group border-b border-line px-8 py-2.5 max-[900px]:px-4">
          <summary className="flex min-h-9 cursor-pointer list-none items-center gap-3 text-sm">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none text-muted transition-transform group-open:rotate-90">
              <path d="M9 6l6 6-6 6" />
            </svg>
            <h2 id="happened" className="flex-none font-display text-[15px] font-semibold">
              What happened
            </h2>
            <span className="min-w-0 truncate text-[13px] text-muted">
              {page.steps.length} {page.steps.length === 1 ? "step" : "steps"}
              {latest && ` · ${who(latest)}: ${latest.what}`}
            </span>
          </summary>
          <ol className="m-0 mt-3 grid max-h-[50vh] list-none grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-x-8 overflow-y-auto p-0 pb-2">
            {page.steps.map((s, i) => (
              <li key={i} className="flex gap-3 pb-3.5">
                <span aria-hidden="true" className={`mt-1.5 size-2.5 flex-none rounded-full ${dot[s.who]}`} />
                <span className="flex min-w-0 flex-col">
                  <span className="text-xs font-semibold text-muted">{who(s)}</span>
                  <span className="text-sm [overflow-wrap:anywhere]">{s.what}</span>
                  {s.detail && (s.what === "Wrote the brief" || s.what === "Finished") ? (
                    <details className="text-[13px]">
                      <summary className="cursor-pointer text-accent-text">Read it</summary>
                      <div className="mt-1 text-fg/85">
                        <MessageText text={s.detail} />
                      </div>
                    </details>
                  ) : s.detail && s.what.startsWith("Ran ") ? (
                    <details className="text-[13px]">
                      <summary className="cursor-pointer text-accent-text">What it printed</summary>
                      <pre className="mt-1 max-h-64 overflow-auto rounded-md bg-surface-2 p-2 font-mono text-[11.5px] whitespace-pre-wrap">{s.detail}</pre>
                    </details>
                  ) : s.detail && (s.who === "agent" || s.who === "owner") && !s.what.startsWith("Kept what") ? (
                    <span className="text-[13px] text-fg/80">{s.detail}</span>
                  ) : (
                    s.detail &&
                    !s.what.startsWith("Kept what") &&
                    !s.what.includes(s.detail.slice(0, 24)) && <span className="line-clamp-3 font-mono text-[12px] break-all text-muted">{s.detail}</span>
                  )}
                </span>
              </li>
            ))}
          </ol>
        </details>
        {page.notes.length > 0 && (
          <details className="group border-b border-line px-8 py-2.5 max-[900px]:px-4">
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

        <section aria-labelledby="changes" className="flex min-w-0 flex-1 flex-col gap-3 px-8 pt-5 pb-8 max-[900px]:px-4">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h2 id="changes" className="font-display text-[17px] font-semibold">
              The changes
            </h2>
            <span className="text-[13px] text-muted">
              {page.changes.length} {page.changes.length === 1 ? "file" : "files"} · <b className="text-good-text">+{added}</b>{" "}
              <b className="text-bad-text">−{removed}</b> · tap a line to say something about it
            </span>
          </div>
          {j.rounds > 1 && (
            <div role="group" aria-label="Which round" className="flex flex-wrap gap-1.5">
              {[0, ...Array.from({ length: j.rounds }, (_, i) => i + 1)].map((r) => (
                <button
                  key={r}
                  type="button"
                  aria-pressed={round === r}
                  onClick={() => setRound(r)}
                  className="min-h-9 rounded-lg px-3 text-[13px] font-semibold text-muted hover:bg-surface-2 hover:text-fg aria-pressed:bg-line aria-pressed:text-fg"
                >
                  {r === 0 ? "All of it" : `Round ${r}`}
                </button>
              ))}
            </div>
          )}
          {page.changes.length === 0 && <p className="m-0 text-sm text-muted">{going ? "Nothing changed yet." : "No changes."}</p>}
          {page.changes.length > 0 && (
            <div className="flex items-start gap-4 max-[900px]:flex-col">
              <div
                role="group"
                aria-label="Files changed"
                className="sticky top-3 flex max-h-[calc(100dvh-220px)] w-[300px] flex-none flex-col gap-0.5 overflow-y-auto rounded-lg border border-line p-1 max-[900px]:static max-[900px]:max-h-none max-[900px]:w-full"
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
                <div role="group" aria-label={change.path} id={`file-${ci}`} className="scroll-mt-3 overflow-hidden rounded-lg border border-line">
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
                    className="sticky top-0 z-10 flex w-full items-center gap-2 border-b border-line bg-surface-2 px-3 py-2 text-left font-mono text-[12.5px]"
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

      <div className="sticky bottom-0 flex flex-col gap-3 border-t border-line bg-bg px-8 py-3.5 max-[900px]:px-4">
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
                  <span className="max-w-[72ch] text-[14.5px] leading-normal">{top.text}</span>
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
            {j.status !== "cancelled" && (
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
            )}
            {j.status !== "cancelled" && <span className="-mt-1 pl-1 text-xs text-muted">{hint}</span>}
            {j.commit && j.branch && !page.origin && <span className="text-xs text-muted">The repository has no origin, so there&apos;s nowhere to push it.</span>}
            {j.commit && page.origin && !page.github.signed_in && <span className="text-xs text-muted">Sign in to GitHub&apos;s gh on this computer to push.</span>}
          </>
        )}
      </div>
    </div>
  );
}
