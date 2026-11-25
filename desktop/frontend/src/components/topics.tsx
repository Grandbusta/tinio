"use client";

import Link from "next/link";
import { Button } from "@tinio/ui";
import { useEffect, useState, type FormEvent } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { FileIcon } from "@/components/file-icon";
import { field, linkPrimary } from "@/components/kit";
import { call, type Topic } from "@/lib/api";

// What you and an agent have going, as they keep it, from About: each topic with where it
// stands, what you settled, and its files and asks. You never make one by hand here; you
// can rename one, merge two, or close one. Closing deletes nothing.

export function TopicsPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [topics, setTopics] = useState<Topic[] | null>(null);
  const [open, setOpen] = useState("");
  const [error, setError] = useState("");
  const load = () =>
    call<{ topics: Topic[] }>(`/agents/${id}/topics`).then((res) => {
      if (!res.ok) return setError(res.error.message);
      setTopics(res.data.topics);
      setOpen((o) => o || (res.data.topics.find((t) => !t.quiet)?.id ?? ""));
    });
  useEffect(() => {
    if (account) load();
  }, [account, id]);

  async function act(path: string, body: Record<string, unknown> = {}) {
    setError("");
    const res = await call(`/agents/${id}/topics/${path}`, body);
    if (!res.ok) return setError(res.error.message);
    load();
  }

  if (missing) return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  const name = agent?.name ?? "your agent";
  const live = (topics ?? []).filter((t) => !t.quiet);
  const quiet = (topics ?? []).filter((t) => t.quiet);
  return (
    <div className="flex max-w-[720px] flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Topics</h1>
        <p className="m-0 max-w-[60ch] text-muted">
          What you and {name} have going, as {name} keeps it. You never make one: {name} sorts the chat as you talk, and you can put it right.
        </p>
      </div>
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}

      {topics && (
        <>
          <section className="flex flex-col gap-2.5">
            <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">Open</h2>
            {live.length === 0 && <p className="m-0 text-sm text-muted">Nothing yet. Start one with + in the chat, or just tell {name} about something new.</p>}
            {live.map((t) => (
              <TopicCard key={t.id} agent={id} topic={t} others={live.filter((x) => x.id !== t.id)} open={open === t.id} toggle={() => setOpen(open === t.id ? "" : t.id)} act={act} />
            ))}
          </section>

          {quiet.length > 0 && (
            <section className="flex flex-col gap-2">
              <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">Quiet</h2>
              <p className="m-0 text-sm text-muted">Finished or gone silent. Bring one up in the chat and {name} picks it up where it stood.</p>
              <div className="flex flex-col border-t border-line">
                {quiet.map((t) => (
                  <Link key={t.id} href={`/agents/${id}?topic=${t.id}`} className="flex min-h-[52px] items-center gap-3 border-b border-line px-1 py-1.5 text-fg no-underline hover:bg-surface-2">
                    <span className="flex min-w-0 flex-1 flex-col">
                      <b className="text-[14.5px] font-semibold">{t.title || "New topic"}</b>
                      <span className="truncate text-[13px] text-muted">{t.status || `Last word ${when(t.last_at)}`}</span>
                    </span>
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="flex-none text-muted">
                      <path d="M9 6l6 6-6 6" />
                    </svg>
                  </Link>
                ))}
              </div>
            </section>
          )}

          <p className="m-0 text-[12.5px] text-muted">
            Small talk belongs to no topic. A topic goes quiet after a week without a word, or once its work is done. Closing one deletes nothing: the messages stay in the chat.
          </p>
        </>
      )}
    </div>
  );
}

const cap = "text-xs font-bold tracking-[0.05em] text-muted uppercase";

function TopicCard({ agent, topic: t, others, open, toggle, act }: {
  agent: string;
  topic: Topic;
  others: Topic[];
  open: boolean;
  toggle: () => void;
  act: (path: string, body?: Record<string, unknown>) => Promise<void>;
}) {
  const [renaming, setRenaming] = useState(false);
  const [title, setTitle] = useState(t.title);
  const [merging, setMerging] = useState(false);
  const [into, setInto] = useState("");
  const files = t.things.filter((x) => x.kind === "file").length;
  const meta = [`Since ${when(t.started_at)}`, `last word ${when(t.last_at)}`, files && `${files} file${files === 1 ? "" : "s"}`].filter(Boolean).join(" · ");

  async function rename(e: FormEvent) {
    e.preventDefault();
    await act(t.id, { title });
    setRenaming(false);
  }

  return (
    <article className={`overflow-hidden rounded-xl border ${open ? "border-line-strong" : "border-line"}`}>
      <button type="button" aria-expanded={open} onClick={toggle} className="flex min-h-16 w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-2">
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex flex-wrap items-center gap-2">
            <b className="font-display text-base font-semibold">{t.title || "New topic"}</b>
            {t.waiting > 0 && <span className="rounded-md bg-warn-soft px-2 py-0.5 text-xs font-semibold text-warn-text">Needs you</span>}
          </span>
          {t.status && <span className="text-sm text-fg">{t.status}</span>}
          <span className="text-[12.5px] text-muted">{meta}</span>
        </span>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={`flex-none text-muted transition-transform ${open ? "rotate-90" : ""}`}>
          <path d="M9 6l6 6-6 6" />
        </svg>
      </button>

      {open && (
        <div className="flex flex-col gap-3.5 border-t border-line px-4 pt-3 pb-4">
          <div className="flex flex-col gap-1">
            <span className={cap}>Where it stands</span>
            <span className="text-sm">{t.summary || "It's written down once the talk pauses."}</span>
          </div>
          {t.settled.length > 0 && (
            <div className="flex flex-col gap-1">
              <span className={cap}>What you settled</span>
              <ul className="m-0 flex list-disc flex-col gap-0.5 pl-5 text-sm">
                {t.settled.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ul>
            </div>
          )}
          {t.things.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <span className={cap}>In this topic</span>
              {t.things.map((x) =>
                x.kind === "file" ? (
                  <a key={x.id} href={`/api/agents/${agent}/files/${x.payload?.file}`} download={x.payload?.name} className="flex min-h-11 items-center gap-3 rounded-lg bg-surface-2 px-2.5 py-1 text-fg no-underline">
                    <FileIcon file={x.payload?.name ?? ""} size={24} />
                    <span className="min-w-0 flex-1 truncate text-sm font-semibold">{x.payload?.name}</span>
                    <span className="text-[12.5px] text-muted">{when(x.at)}</span>
                  </a>
                ) : (
                  <Link key={x.id} href={`/agents/${agent}?topic=${t.id}`} className="flex min-h-11 items-center gap-3 rounded-lg bg-surface-2 px-2.5 py-1 text-fg no-underline">
                    <span className="w-10 flex-none text-[11px] font-bold tracking-[0.04em] text-muted uppercase">{x.kind === "ask" ? "Ask" : "Call"}</span>
                    <span className="min-w-0 flex-1 truncate text-sm">{x.payload?.title || x.text.split("\n")[0]}</span>
                    <span className={`flex-none rounded-md px-2 py-0.5 text-xs font-semibold ${x.settled ? "bg-surface text-muted" : "bg-warn-soft text-warn-text"}`}>{x.settled ? "Answered" : "Needs you"}</span>
                  </Link>
                ),
              )}
            </div>
          )}

          {renaming && (
            <form onSubmit={rename} className="flex flex-wrap items-center gap-2">
              <label htmlFor={`title-${t.id}`} className="sr-only">Its name</label>
              <input id={`title-${t.id}`} value={title} onChange={(e) => setTitle(e.target.value)} maxLength={60} autoFocus className={`${field} max-w-xs flex-1`} />
              <Button kind="primary" type="submit" disabled={!title.trim()}>Save</Button>
              <Button type="button" onClick={() => setRenaming(false)}>Cancel</Button>
            </form>
          )}
          {merging && (
            <div className="flex flex-wrap items-center gap-2">
              <label htmlFor={`into-${t.id}`} className="text-sm">Put it all into</label>
              <select id={`into-${t.id}`} value={into} onChange={(e) => setInto(e.target.value)} className={`${field} max-w-xs flex-1`}>
                <option value="">Pick a topic</option>
                {others.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.title || "New topic"}
                  </option>
                ))}
              </select>
              <Button kind="primary" type="button" disabled={!into} onClick={() => act(`${t.id}/merge`, { into })}>Merge</Button>
              <Button type="button" onClick={() => setMerging(false)}>Cancel</Button>
            </div>
          )}

          {!renaming && !merging && (
            <div className="flex flex-wrap items-center gap-2">
              <Link href={`/agents/${agent}?topic=${t.id}`} className={`${linkPrimary} min-h-10 px-4 text-[13px]`}>
                Open in chat
              </Link>
              <Button type="button" onClick={() => setRenaming(true)} className="min-h-10 px-4 text-[13px]">Rename</Button>
              {others.length > 0 && <Button type="button" onClick={() => setMerging(true)} className="min-h-10 px-4 text-[13px]">Merge with…</Button>}
              <button type="button" onClick={() => act(`${t.id}/close`)} className="inline-flex min-h-10 items-center rounded-lg px-4 text-[13px] font-semibold text-muted hover:bg-surface-2 hover:text-fg">Close it</button>
            </div>
          )}
        </div>
      )}
    </article>
  );
}
