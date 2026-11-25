import { Button } from "@tinio/ui";
import { useState } from "react";
import { MessageText } from "@/components/message-text";
import { ServiceLogo } from "@/components/service-logo";
import { call, type ChatRow } from "@/lib/api";

// Coding jobs in the chat: one card per job that updates as it goes (not a stream of
// steps), a card for each thing the job's rules don't allow (Allow once, Allow for this
// job, Not this), and a strip at the top of the chat with the jobs going on. Replying to
// a job's card tells the job, not the agent.

const lift = "shadow-[0_1px_0.5px_rgba(11,20,26,0.13)]";
const card = `relative flex w-[78%] max-w-full flex-col gap-3 self-start rounded-lg bg-surface px-4 pt-3.5 pb-3.5 ${lift} max-[560px]:w-full`;

type Status = NonNullable<NonNullable<ChatRow["payload"]>["status"]>;

const pills: Record<Status, { label: string; tone: string }> = {
  working: { label: "Working", tone: "bg-accent-soft text-accent-text" },
  waiting: { label: "Needs you", tone: "bg-warn-soft text-fg" },
  ready: { label: "Ready", tone: "bg-good-soft text-good-text" },
  stopped: { label: "Stopped", tone: "bg-surface-2 text-muted" },
  failed: { label: "Didn't work", tone: "bg-bad-soft text-bad-text" },
  cancelled: { label: "Stopped", tone: "bg-surface-2 text-muted" },
};

function Pill({ status }: { status: Status }) {
  const p = pills[status] ?? pills.working;
  return <span className={`flex-none rounded-full px-2.5 py-0.5 text-xs font-semibold ${p.tone}`}>{p.label}</span>;
}

const money = (usd: number) => (usd < 0.01 ? "under a cent" : `$${usd.toFixed(2)}`);

// JobCard is a job as it goes: what it's doing, the files it has changed, and how it
// ended. Stop ends it; what it did is kept.
export function JobCard({ row, agent, name }: { row: ChatRow; agent: string; name: string }) {
  const p = row.payload ?? {};
  const status = (p.status ?? "working") as Status;
  const going = status === "working" || status === "waiting";
  const [stopping, setStopping] = useState(false);
  const files = p.files ?? [];
  return (
    <div className={card}>
      <div className="flex items-start gap-3">
        <ServiceLogo service="claude" size={36} />
        <div className="flex min-w-0 flex-1 flex-col">
          <b className="text-[15px] leading-snug">{row.text.replace(/^Coding job: /, "").replace(/ · \w+$/, "")}</b>
          <span className="text-[12.5px] text-muted">
            {[p.folder, p.branch, "Claude Code", p.model].filter(Boolean).join(" · ")}
          </span>
        </div>
        <Pill status={status} />
      </div>
      <span className="flex items-center gap-2 text-sm text-fg/85">
        {going && <span aria-hidden="true" className="size-2 flex-none animate-pulse rounded-full bg-accent" />}
        {p.now}
      </span>
      {p.summary && !going && (
        <div className="rounded-md bg-surface-2 px-3 py-2.5 text-sm">
          <MessageText text={p.summary} />
        </div>
      )}
      <span className="flex flex-wrap gap-x-4 gap-y-1 border-t border-line pt-2.5 text-[12.5px] text-muted">
        <span>
          <b className="text-fg">{files.length}</b> {files.length === 1 ? "file" : "files"} changed
        </span>
        {!!p.steps && <span>{p.steps} steps</span>}
        {!!p.cost_usd && <span>{money(p.cost_usd)} of model time</span>}
        {!!p.asking && <span className="font-semibold text-fg">{p.asking === 1 ? "A question for you below" : `${p.asking} questions for you below`}</span>}
      </span>
      {files.length > 0 && (
        <details className="text-[13px]">
          <summary className="cursor-pointer font-semibold text-accent-text">The files</summary>
          <ul className="m-0 mt-1.5 flex list-none flex-col gap-0.5 p-0 font-mono text-[12.5px]">
            {files.map((f) => (
              <li key={f}>{f}</li>
            ))}
          </ul>
        </details>
      )}
      <div className="flex flex-wrap items-center gap-2">
        {going && (
          <Button
            type="button"
            disabled={stopping}
            onClick={async () => {
              setStopping(true);
              await call(`/agents/${agent}/jobs/${p.job}/cancel`, {});
              setStopping(false);
            }}
          >
            {stopping ? "Stopping" : "Stop the job"}
          </Button>
        )}
        <span className="text-xs text-muted">
          {going ? `Reply to this card to tell the job something. ${name} sees it through.` : "Reply to this card to carry the job on."}
        </span>
      </div>
    </div>
  );
}

const verbs: Record<string, string> = {
  Bash: "Run a command",
  WebFetch: "Read a web page",
  WebSearch: "Search the web",
  Edit: "Change a file outside its copy",
  Write: "Write a file outside its copy",
  MultiEdit: "Change a file outside its copy",
};

type AskProps = {
  row: ChatRow;
  agent: string;
  name: string;
  tapped?: string;
  busy?: boolean;
  failed?: string;
  answer: (row: ChatRow, body: Record<string, unknown>, said: string, path?: string) => void;
};

// JobAskCard is something the job's rules don't allow, asked of you: exactly what it
// would do, and why it's asked. Allow once, Allow for this job (the same again isn't
// asked), or Not this, with what you'd like instead.
export function JobAskCard({ row, agent, tapped, busy, failed, answer }: AskProps) {
  const p = row.payload ?? {};
  const open = !row.settled_at && !tapped;
  const [noting, setNoting] = useState(false);
  const [note, setNote] = useState("");
  const path = `/agents/${agent}/jobs/${p.job}/asks/${p.ask}`;
  const ended = row.settled_at ? row.text.split("\n\n").pop() : tapped;
  return (
    <div className={`${card} border border-warn-text/40`}>
      <span className="flex items-center gap-2">
        <ServiceLogo service="claude" size={24} />
        <b className="text-[14px]">Claude Code wants to {(verbs[p.tool ?? ""] ?? `use ${p.tool}`).toLowerCase()}</b>
      </span>
      <code className="rounded-md bg-fg px-3 py-2.5 font-mono text-[13px] break-all whitespace-pre-wrap text-bg">{p.detail}</code>
      {p.why && <span className="text-[13px] text-muted">Asked because {p.why}.</span>}
      {open && !noting && (
        <div className="flex flex-wrap gap-2">
          <Button type="button" disabled={busy} onClick={() => answer(row, { answer: "once" }, "✅ Allowed once.", path)}>
            Allow once
          </Button>
          <Button type="button" disabled={busy} onClick={() => answer(row, { answer: "job" }, "✅ Allowed for this job.", path)}>
            Allow for this job
          </Button>
          <Button kind="primary" type="button" disabled={busy} onClick={() => setNoting(true)}>
            Not this
          </Button>
        </div>
      )}
      {open && noting && (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            answer(row, { answer: "no", note }, "You said no.", path);
          }}
        >
          <label htmlFor={`note-${row.id}`} className="text-[13px] font-semibold">
            What should it do instead? (it reads this)
          </label>
          <input
            id={`note-${row.id}`}
            autoFocus
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Use the money helpers we already have."
            maxLength={500}
            className="min-h-10 rounded-md border border-line-strong bg-surface px-3 text-sm"
          />
          <div className="flex flex-wrap gap-2">
            <Button kind="primary" type="submit" disabled={busy}>
              Say no
            </Button>
            <Button type="button" onClick={() => setNoting(false)}>
              Back
            </Button>
          </div>
        </form>
      )}
      {ended && <span className="text-[13px] font-semibold">{ended}</span>}
      {failed && (
        <span role="alert" className="text-[13px] text-bad-text">
          {failed}
        </span>
      )}
    </div>
  );
}

// JobsStrip is the jobs going on, or done today, at the top of the chat: each opens
// its card.
export function JobsStrip({ rows }: { rows: ChatRow[] }) {
  const day = Date.now() - 24 * 3600_000;
  const jobs = rows.filter((r) => {
    if (r.kind !== "job") return false;
    const s = r.payload?.status;
    return s === "working" || s === "waiting" || ((s === "ready" || s === "stopped") && new Date(r.at).getTime() > day);
  });
  if (jobs.length === 0) return null;
  return (
    <nav aria-label="Coding jobs" className="flex gap-2 overflow-x-auto border-t border-line px-4 py-2 max-[900px]:px-2">
      {jobs
        .slice()
        .reverse()
        .map((r) => (
          <button
            key={r.id}
            type="button"
            onClick={() => document.getElementById(`row-${r.id}`)?.scrollIntoView({ behavior: "smooth", block: "center" })}
            className="flex min-h-11 max-w-[280px] flex-none items-center gap-2 rounded-xl border border-line bg-surface px-2.5 py-1 text-left hover:border-line-strong"
          >
            <ServiceLogo service="claude" size={26} />
            <span className="flex min-w-0 flex-col">
              <b className="truncate text-[13px]">{r.text.replace(/^Coding job: /, "").replace(/ · \w+$/, "")}</b>
              <span className="truncate text-[11.5px] text-muted">{r.payload?.folder}</span>
            </span>
            <Pill status={(r.payload?.status ?? "working") as Status} />
          </button>
        ))}
    </nav>
  );
}
