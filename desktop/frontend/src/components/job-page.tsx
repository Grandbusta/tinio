import Link from "next/link";
import { Button } from "@tinio/ui";
import { useCallback, useEffect, useState } from "react";
import { useFrame } from "@/components/app-frame";
import { MessageText } from "@/components/message-text";
import { ServiceLogo } from "@/components/service-logo";
import { call } from "@/lib/api";

// A coding job, opened from its card: what happened, told by who did it; its changes, by
// file and by round, where a tapped line takes a note that goes back with "Send it back";
// and the decision: commit it (on its branch, on this computer), send it back, or go back
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
type Page = { job: Job; steps: Step[]; changes: Change[]; github: { installed: boolean; signed_in: boolean; account: string }; origin: boolean };
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
  const out: { n: number; sign: "+" | "-" | " " | "@"; text: string }[] = [];
  let oldN = 0;
  let newN = 0;
  for (const l of patch.split("\n")) {
    const h = /^@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(l);
    if (h) {
      oldN = Number(h[1]);
      newN = Number(h[2]);
      out.push({ n: 0, sign: "@", text: l });
    } else if (l.startsWith("+")) out.push({ n: newN++, sign: "+", text: l.slice(1) });
    else if (l.startsWith("-")) out.push({ n: oldN++, sign: "-", text: l.slice(1) });
    else if (l.startsWith("\\")) continue;
    else {
      out.push({ n: newN, sign: " ", text: l.slice(1) });
      oldN++;
      newN++;
    }
  }
  return out;
}

export function JobPage({ agent, job }: { agent: string; job: string }) {
  const { agent: a } = useFrame();
  const [page, setPage] = useState<Page | null>(null);
  const [round, setRound] = useState(0); // 0: all of it
  const [file, setFile] = useState(0);
  const [noting, setNoting] = useState<{ file: string; line: number } | null>(null);
  const [draft, setDraft] = useState("");
  const [notes, setNotes] = useState<Note[]>([]);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [committing, setCommitting] = useState(false);
  const [message, setMessage] = useState("");
  const [pushing, setPushing] = useState(false);

  const load = useCallback(() => {
    call<Page>(`/agents/${agent}/jobs/${job}${round ? `?round=${round}` : ""}`).then((res) => {
      if (!res.ok) return setError(res.error.message);
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

  if (!page) return <div className="p-10 text-sm text-muted">{error || "Loading"}</div>;
  const j = page.job;
  const name = a?.name ?? "Your agent";
  const who = (s: Step) => ({ agent: name, cli: "Claude Code", owner: "You", tinio: "Tinio" })[s.who];
  const change = page.changes[Math.min(file, Math.max(0, page.changes.length - 1))];
  const added = page.changes.reduce((n, c) => n + c.added, 0);
  const removed = page.changes.reduce((n, c) => n + c.removed, 0);
  const finished = [...page.steps].reverse().find((s) => s.who === "cli" && (s.what === "Finished" || s.what === "Stopped"));
  const [label, tone] = pill[j.status] ?? pill.working;
  const canDecide = !going && !j.commit;

  return (
    <div className="flex min-h-full flex-col">
      <div className="flex flex-col gap-2.5 border-b border-line px-8 pt-8 pb-4 max-[900px]:px-4">
        <Link href={`/agents/${agent}`} className="-ml-2 inline-flex min-h-11 items-center gap-1 self-start px-2 text-sm font-semibold text-accent-text">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M15 6l-6 6 6 6" />
          </svg>
          Chat with {name}
        </Link>
        <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2">
          <h1 className="font-display text-[28px] leading-tight font-medium tracking-[-0.02em]">{j.title}</h1>
          <span className={`rounded-full px-2.5 py-0.5 text-[12.5px] font-semibold ${tone}`}>{j.commit && !j.pr ? "Committed" : j.pr ? "Pull request open" : label}</span>
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

      <div className="flex flex-1 flex-wrap items-start">
        <section aria-labelledby="happened" className="flex max-w-[380px] min-w-0 flex-[1_1_300px] flex-col gap-3 px-8 pt-5 pb-8 max-[900px]:px-4">
          <h2 id="happened" className="font-display text-[17px] font-semibold">
            What happened
          </h2>
          <ol className="m-0 flex list-none flex-col p-0">
            {page.steps.map((s, i) => (
              <li key={i} className="flex gap-3 pb-3.5">
                <span aria-hidden="true" className={`mt-1.5 size-2.5 flex-none rounded-full ${dot[s.who]}`} />
                <span className="flex min-w-0 flex-col">
                  <span className="text-xs font-semibold text-muted">{who(s)}</span>
                  <span className="text-sm">{s.what}</span>
                  {s.detail && s.what !== "Wrote the brief" && s.what !== "Finished" && !s.what.includes(s.detail.slice(0, 24)) && (
                    <span className="line-clamp-3 font-mono text-[12px] break-all text-muted">{s.detail}</span>
                  )}
                  {s.detail && (s.what === "Wrote the brief" || s.what === "Finished") && (
                    <details className="text-[13px]">
                      <summary className="cursor-pointer text-accent-text">Read it</summary>
                      <div className="mt-1 text-fg/85">
                        <MessageText text={s.detail} />
                      </div>
                    </details>
                  )}
                </span>
              </li>
            ))}
          </ol>
        </section>

        <section aria-labelledby="changes" className="flex min-w-0 flex-[999_1_480px] flex-col gap-3 border-l border-line px-8 pt-5 pb-8 max-[900px]:border-l-0 max-[900px]:px-4">
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
                  onClick={() => {
                    setRound(r);
                    setFile(0);
                  }}
                  className="min-h-9 rounded-full border border-line-strong px-3 text-[13px] font-semibold aria-pressed:border-fg aria-pressed:bg-fg aria-pressed:text-bg"
                >
                  {r === 0 ? "All of it" : `Round ${r}`}
                </button>
              ))}
            </div>
          )}
          {page.changes.length === 0 && <p className="m-0 text-sm text-muted">{going ? "Nothing changed yet." : "No changes."}</p>}
          {page.changes.length > 0 && (
            <div role="group" aria-label="Files changed" className="flex flex-col gap-0.5 rounded-lg border border-line p-1">
              {page.changes.map((c, i) => (
                <button
                  key={c.path}
                  type="button"
                  aria-pressed={i === file}
                  onClick={() => setFile(i)}
                  className="flex justify-between gap-2.5 rounded-md px-2.5 py-2 text-left font-mono text-[12.5px] hover:bg-surface-2 aria-pressed:bg-accent-soft"
                >
                  <span className="break-all">{c.path}</span>
                  <span className="flex-none">
                    <b className="text-good-text">+{c.added}</b> <b className="text-bad-text">−{c.removed}</b>
                  </span>
                </button>
              ))}
            </div>
          )}
          {change && (
            <div role="group" aria-label={change.path} className="overflow-hidden rounded-lg border border-line">
              <div className="border-b border-line bg-surface-2 px-3 py-2 font-mono text-[12.5px] text-fg/75">{change.path}</div>
              {lines(change.patch).map((l, i) =>
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
                      <span aria-hidden="true" className="w-11 flex-none pr-2.5 text-right text-muted">
                        {l.n}
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
          )}
          {notes.length > 0 && (
            <div className="flex flex-col gap-1.5 rounded-lg bg-accent-soft/40 px-3.5 py-3 text-[13px]">
              <b>{notes.length === 1 ? "One note" : `${notes.length} notes`} to send back</b>
              {notes.map((n, i) => (
                <span key={i} className="flex justify-between gap-2">
                  <span>
                    <code>
                      {n.file}:{n.line}
                    </code>{" "}
                    {n.text}
                  </span>
                  <button type="button" onClick={() => setNotes((ns) => ns.filter((_, k) => k !== i))} className="font-semibold text-accent-text">
                    Remove
                  </button>
                </span>
              ))}
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
        ) : (
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2.5">
            <span className="min-w-0 flex-[1_1_280px] text-sm text-fg/85">
              {going
                ? "It's working. Its card in the chat shows how it goes."
                : j.pr
                  ? "The pull request is open. Carry the job on from its card to change it."
                  : j.commit
                    ? `Committed on ${j.branch || j.base}, on this computer.`
                    : page.changes.length
                      ? "Look it over: commit it, send it back with your notes, or go back a round."
                      : "It made no changes."}
            </span>
            {canDecide && (
              <>
                <label htmlFor="back-note" className="sr-only">
                  What to change, in a word
                </label>
                <input
                  id="back-note"
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="What to change (for Send it back)"
                  className="min-h-10 min-w-0 flex-[1_1_220px] rounded-full border border-line-strong bg-surface px-4 text-sm"
                />
                <Button type="button" disabled={!!busy || (!note.trim() && notes.length === 0)} onClick={() => act("back", "back", { note, lines: notes }, () => (setNotes([]), setNote("")))}>
                  {busy === "back" ? "Sending" : "Send it back"}
                </Button>
                {j.rounds > 0 && (
                  <Button type="button" disabled={!!busy} onClick={() => act("goback", "goback", {})}>
                    Go back a round
                  </Button>
                )}
                {page.changes.length > 0 && (
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
              </>
            )}
            {j.commit && j.branch && !going && page.origin && (
              <Button kind="primary" type="button" disabled={!page.github.signed_in} onClick={() => setPushing(true)}>
                {j.pr ? "Push again" : "Push and open a pull request"}
              </Button>
            )}
            {j.commit && j.branch && !page.origin && <span className="text-xs text-muted">The repository has no origin, so there&apos;s nowhere to push it.</span>}
            {j.commit && page.origin && !page.github.signed_in && <span className="text-xs text-muted">Sign in to GitHub&apos;s gh on this computer to push.</span>}
          </div>
        )}
      </div>
    </div>
  );
}
