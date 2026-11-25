import Link from "next/link";
import { Button } from "@tinio/ui";
import { useState } from "react";
import { early } from "@/components/app-frame";
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
  return <span className={`flex-none rounded-md px-2.5 py-0.5 text-xs font-semibold ${p.tone}`}>{p.label}</span>;
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
      {p.verdict && !going && (
        <div className="flex flex-col gap-0.5 rounded-md bg-accent-soft px-3 py-2.5 text-sm">
          <b className="text-[12.5px] text-accent-text">{name} checked it</b>
          <MessageText text={p.verdict} />
        </div>
      )}
      {p.summary && !going && (
        <details className="text-[13px]">
          <summary className="cursor-pointer font-semibold text-accent-text">What Claude Code said</summary>
          <div className="mt-1.5 rounded-md bg-surface-2 px-3 py-2.5 text-sm">
            <MessageText text={p.summary} />
          </div>
        </details>
      )}
      <span className="flex flex-wrap gap-x-4 gap-y-1 border-t border-line pt-2.5 text-[12.5px] text-muted">
        <span>
          <b className="text-fg">{files.length}</b> {files.length === 1 ? "file" : "files"} changed
        </span>
        {!!p.steps && <span>{p.steps} steps</span>}
        {!!p.cost_usd && <span>{money(p.cost_usd)} of model time</span>}
        {!!p.commit && <span>Committed {p.commit}</span>}
        {!!p.pr && (
          <a href={p.pr} target="_blank" rel="noreferrer" className="font-semibold text-accent-text">
            Pull request
          </a>
        )}
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
        <Link
          href={`/agents/${agent}?job=${p.job}`}
          {...early(`/agents/${agent}/jobs/${p.job}`)}
          className="inline-flex min-h-10 items-center rounded-lg border border-line-strong px-4 text-[13px] font-semibold text-fg no-underline hover:border-fg"
        >
          {going ? "Open the job" : "Look at the changes"}
        </Link>
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
  docked?: boolean; // the one waiting on you, above the message box
  more?: number; // how many more are waiting after it
};

// JobAskCard is something the job's rules don't allow, asked of you. Waiting, it's
// docked above the message box (where the chat can't scroll it away): exactly what it
// would do, why it's asked, and Allow once, Allow for this job (the same again isn't
// asked) or Not this, with what you'd like instead. In the chat it's one quiet line: where
// it was asked, and once answered, what you said, opening to the whole command.
export function JobAskCard({ row, agent, tapped, busy, failed, answer, docked, more = 0 }: AskProps) {
  const p = row.payload ?? {};
  const open = !row.settled_at && !tapped;
  const [noting, setNoting] = useState(false);
  const [note, setNote] = useState("");
  const path = `/agents/${agent}/jobs/${p.job}/asks/${p.ask}`;
  const ended = row.settled_at ? row.text.split("\n\n").pop() : tapped;
  const choosing = p.tool === "AskUserQuestion" && !!p.questions?.length; // its own question, with choices
  const verb = choosing ? "ask you something" : (verbs[p.tool ?? ""] ?? `Use ${p.tool}`).toLowerCase();
  const command = choosing ? (
    <span className="block text-[13.5px]">{p.questions!.map((q) => q.question).join(" ")}</span>
  ) : (
    <code className="block max-h-40 overflow-y-auto rounded-md border border-line bg-surface-2 px-3 py-2 font-mono text-[12.5px] whitespace-pre-wrap text-fg [overflow-wrap:anywhere]">
      {p.detail}
    </code>
  );

  if (!docked) {
    if (open)
      return (
        <span className="flex max-w-full items-center gap-2 self-start py-1 text-[13px] text-muted">
          <span aria-hidden="true" className="size-2 flex-none animate-pulse rounded-full bg-warn-text" />
          {choosing ? "Claude Code asked you something" : `Claude Code asked to ${verb}`}: your answer is below the chat.
        </span>
      );
    const no = (ended ?? "").startsWith("You said no");
    return (
      <details className="group max-w-[78%] self-start text-[13px] max-[560px]:max-w-full">
        <summary className="flex min-h-8 cursor-pointer list-none items-center gap-2 text-muted hover:text-fg">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none transition-transform group-open:rotate-90">
            <path d="M9 6l6 6-6 6" />
          </svg>
          <span className={`flex-none font-semibold ${no ? "text-bad-text" : "text-good-text"}`}>
            {no ? "Said no" : (ended ?? "Answered").replace(/^✅\s*/, "").replace(/\.$/, "")}
          </span>
          <span className="min-w-0 truncate font-mono text-[12px]">{p.detail}</span>
        </summary>
        <div className="mt-1.5 flex flex-col gap-1.5 pl-5">
          {command}
          {p.why && <span className="text-muted">Asked because {p.why}.</span>}
          {no && ended && <span>{ended}</span>}
        </div>
      </details>
    );
  }

  if (choosing) return <Choices row={row} path={path} questions={p.questions!} busy={busy} failed={failed} answer={answer} more={more} />;

  return (
    <div className="flex w-full basis-full flex-col gap-2.5 rounded-xl border border-line bg-surface px-4 py-3.5 shadow-lg">
      <span className="flex items-center gap-2">
        <ServiceLogo service="claude" size={22} />
        <b className="text-[14px]">Claude Code wants to {verb}</b>
        {more > 0 && <span className="ml-auto text-xs text-muted">{more === 1 ? "1 more after this" : `${more} more after this`}</span>}
      </span>
      {command}
      {p.why && <span className="text-[12.5px] text-muted">Asked because {p.why}.</span>}
      {!noting && (
        <div className="flex flex-wrap gap-2">
          <Button kind="primary" type="button" disabled={busy} onClick={() => answer(row, { answer: "once" }, "✅ Allowed once.", path)}>
            Allow once
          </Button>
          <Button type="button" disabled={busy} onClick={() => answer(row, { answer: "job" }, "✅ Allowed for this job.", path)}>
            Allow for this job
          </Button>
          <Button type="button" disabled={busy} onClick={() => setNoting(true)}>
            Not this
          </Button>
        </div>
      )}
      {noting && (
        <div className="flex flex-col gap-2">
          <label htmlFor={`note-${row.id}`} className="text-[13px] font-semibold">
            It stops here. What should it do instead?
          </label>
          <span className="-mt-1 text-[12.5px] text-muted">It carries on with what you write. Left empty, it waits for you to say.</span>
          <input
            id={`note-${row.id}`}
            autoFocus
            value={note}
            onChange={(e) => setNote(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                answer(row, { answer: "no", note }, "You said no.", path);
              }
            }}
            placeholder="Use the money helpers we already have."
            maxLength={500}
            className="min-h-10 rounded-lg border border-line-strong bg-surface px-3 text-sm"
          />
          <div className="flex flex-wrap gap-2">
            <Button kind="primary" type="button" disabled={busy} onClick={() => answer(row, { answer: "no", note }, "You said no.", path)}>
              Say no
            </Button>
            <Button type="button" onClick={() => setNoting(false)}>
              Back
            </Button>
          </div>
        </div>
      )}
      {failed && (
        <span role="alert" className="text-[13px] text-bad-text">
          {failed}
        </span>
      )}
    </div>
  );
}

// Choices is Claude Code asking you something, with the choices it offers: one question
// with one choice goes at a tap; more, or several choices, once each has an answer. A
// choice of your own words is always there.
function Choices({ row, path, questions, busy, failed, answer, more }: {
  row: ChatRow;
  path: string;
  questions: NonNullable<NonNullable<ChatRow["payload"]>["questions"]>;
  busy?: boolean;
  failed?: string;
  answer: AskProps["answer"];
  more: number;
}) {
  const [picked, setPicked] = useState<Record<string, string[]>>({});
  const [own, setOwn] = useState<Record<string, string>>({});
  const at = (q: string) => (own[q]?.trim() ? [own[q].trim()] : (picked[q] ?? []));
  const send = (choices: Record<string, string>) =>
    answer(row, { answer: "choose", choices }, `You chose: ${Object.values(choices).join("; ")}.`, path);
  const all = () => Object.fromEntries(questions.map((q) => [q.question, at(q.question).join(", ")]));
  const ready = questions.every((q) => at(q.question).length > 0);
  const atOnce = questions.length === 1 && !questions[0].multiSelect;
  const pick = (q: (typeof questions)[number], label: string) => {
    if (atOnce) return send({ [q.question]: label });
    setOwn((o) => ({ ...o, [q.question]: "" }));
    setPicked((p) => {
      const was = p[q.question] ?? [];
      if (!q.multiSelect) return { ...p, [q.question]: [label] };
      return { ...p, [q.question]: was.includes(label) ? was.filter((l) => l !== label) : [...was, label] };
    });
  };
  return (
    <div className="flex max-h-[45vh] w-full basis-full flex-col gap-3 overflow-y-auto rounded-xl border border-line bg-surface px-4 py-3.5 shadow-lg">
      <span className="flex items-center gap-2">
        <ServiceLogo service="claude" size={22} />
        <b className="text-[14px]">Claude Code asks you</b>
        {more > 0 && <span className="ml-auto text-xs text-muted">{more === 1 ? "1 more after this" : `${more} more after this`}</span>}
      </span>
      {questions.map((q, i) => (
        <fieldset key={i} className="flex flex-col gap-2">
          <legend className="mb-2 flex flex-col gap-0.5">
            {q.header && <span className="text-xs font-semibold text-muted">{q.header}</span>}
            <span className="text-[14.5px] font-semibold">{q.question}</span>
          </legend>
          <div className="flex flex-col gap-1.5">
            {q.options.map((o) => {
              const on = (picked[q.question] ?? []).includes(o.label) && !own[q.question]?.trim();
              return (
                <button
                  key={o.label}
                  type="button"
                  disabled={busy}
                  aria-pressed={atOnce ? undefined : on}
                  onClick={() => pick(q, o.label)}
                  className="flex flex-col items-start gap-0.5 rounded-lg border border-line px-3.5 py-2.5 text-left hover:border-line-strong hover:bg-surface-2 aria-pressed:border-accent aria-pressed:bg-accent-soft disabled:opacity-60"
                >
                  <b className="text-[13.5px] font-semibold">{o.label}</b>
                  {o.description && <span className="text-[13px] leading-snug text-muted">{o.description}</span>}
                </button>
              );
            })}
            <label htmlFor={`own-${row.id}-${i}`} className="sr-only">
              Something else
            </label>
            <input
              id={`own-${row.id}-${i}`}
              value={own[q.question] ?? ""}
              onChange={(e) => setOwn((o) => ({ ...o, [q.question]: e.target.value }))}
              onKeyDown={(e) => {
                if (e.key === "Enter" && ready) {
                  e.preventDefault();
                  send(all());
                }
              }}
              placeholder="Something else: say it in your words"
              maxLength={500}
              className="min-h-10 rounded-lg border border-line-strong bg-surface px-3.5 text-[13.5px]"
            />
          </div>
        </fieldset>
      ))}
      {(!atOnce || own[questions[0].question]?.trim()) && (
        <div>
          <Button kind="primary" type="button" disabled={busy || !ready} onClick={() => send(all())}>
            {busy ? "Sending" : "Send the answer"}
          </Button>
        </div>
      )}
      {failed && (
        <span role="alert" className="text-[13px] text-bad-text">
          {failed}
        </span>
      )}
    </div>
  );
}
