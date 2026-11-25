"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Avatar, Back, useFrame } from "@/components/app-frame";
import { call, type MorningItem, type OwnerDay } from "@/lib/api";

// Your day: each agent's morning, once they've sent it. The things they chose for you,
// each with how, where it came from and what they drafted for it, ticked off as you go;
// what's waiting on you. With id, one agent's part, reached from the panel about them.

export function TodayPage({ id }: { id?: string }) {
  const { account, overview, missing } = useFrame();
  const [days, setDays] = useState<OwnerDay[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!account) return;
    if (id) {
      call<{ today: OwnerDay }>(`/agents/${id}/today`).then((res) => (res.ok ? setDays([res.data.today]) : setError(res.error.message)));
    } else {
      call<{ days: OwnerDay[] }>("/today").then((res) => (res.ok ? setDays(res.data.days) : setError(res.error.message)));
    }
  }, [account, id]);

  if (id && missing) {
    return (
      <div className="grid flex-1 place-items-center px-6">
        <p role="alert" className="text-bad-text">We can&apos;t find that agent among yours.</p>
      </div>
    );
  }

  async function tick(day: OwnerDay, item: MorningItem) {
    const res = await call<{ today: OwnerDay }>(`/agents/${day.agent}/today/items/${item.id}`, { done: !item.done });
    if (!res.ok) return setError(res.error.message);
    setDays((ds) => ds?.map((d) => (d.agent === day.agent ? res.data.today : d)) ?? null);
  }

  const items = days?.flatMap((d) => d.items) ?? [];
  const done = items.filter((i) => i.done).length;
  const waiting = days?.reduce((n, d) => n + d.waiting, 0) ?? 0;
  const sent = days?.filter((d) => d.sent) ?? [];
  const coming = days?.filter((d) => !d.sent && d.next) ?? [];

  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      {id && <Back id={id} />}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] text-muted">{days?.[0]?.date}{sent.length > 0 && " · from your agents' mornings"}</span>
          <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Your day</h1>
        </div>
        {items.length > 0 && <span className="text-sm text-muted">{done} of {items.length} done</span>}
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}

      {days && (
        <div className="flex flex-wrap items-start gap-6">
          <div className="flex min-w-0 flex-[999_1_460px] flex-col gap-4">
            {sent.map((d) => {
              const agent = overview?.agents.find((a) => a.id === d.agent);
              return (
                <section key={d.agent} aria-labelledby={`from-${d.agent}`} className="rounded-lg border border-line">
                  <div className="flex items-center gap-3 border-b border-line px-5 py-3.5">
                    {agent && <Avatar agent={agent} size={32} />}
                    <div className="flex min-w-0 flex-auto flex-col">
                      <h2 id={`from-${d.agent}`} className="font-display text-[17px] font-semibold tracking-[-0.02em]">From {d.name}</h2>
                      <span className="text-[12.5px] text-muted">Sent at {d.sent}</span>
                    </div>
                    <Link href={`/agents/${d.agent}`} className="text-[13px] font-semibold text-accent-text">Read the note</Link>
                  </div>
                  {d.items.length === 0 ? (
                    <p className="m-0 px-5 py-4 text-[15px] text-muted">{d.question || `Nothing from ${d.name} for today.`}</p>
                  ) : (
                    <ol className="m-0 list-none p-0">
                      {d.items.map((it) => (
                        <li key={it.id} className="flex gap-3.5 border-b border-line px-5 py-4 last:border-b-0">
                          <button
                            type="button"
                            aria-pressed={it.done}
                            aria-label={it.done ? `Mark "${it.what}" not done` : `Mark "${it.what}" done`}
                            onClick={() => tick(d, it)}
                            className="-my-2.5 -ml-2.5 grid size-11 flex-none place-items-center rounded-full hover:bg-surface-2"
                          >
                            <span className={`grid size-[22px] place-items-center rounded-md ${it.done ? "bg-accent" : "border-2 border-line-strong"}`}>
                              {it.done && (
                                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                  <path d="M5 12l5 5 9-10" />
                                </svg>
                              )}
                            </span>
                          </button>
                          <div className="flex min-w-0 flex-auto flex-col gap-1.5">
                            <span className={`font-semibold ${it.done ? "text-muted line-through" : ""}`}>{it.what}</span>
                            {it.how && <span className="text-sm text-muted">{it.how}</span>}
                            <div className="flex flex-wrap items-center gap-2">
                              {it.work && (
                                <Link href={`/agents/${d.agent}/works/${it.work.id}`} className="inline-flex min-h-8 items-center gap-1.5 rounded-full bg-accent-soft px-3 text-[13px] font-semibold text-accent-text hover:brightness-95">
                                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                                    <path d="M14 3v5h5" />
                                  </svg>
                                  {it.work.title}
                                </Link>
                              )}
                              <span className="text-xs text-muted first-letter:uppercase">{it.source}</span>
                            </div>
                          </div>
                          {it.done && <span className="self-start rounded-full bg-good-soft px-2.5 py-1 text-xs font-semibold whitespace-nowrap text-good-text">Done</span>}
                        </li>
                      ))}
                    </ol>
                  )}
                </section>
              );
            })}
            {sent.length === 0 && (
              <p className="m-0 rounded-lg border border-line px-5 py-4 text-[15px] text-muted">
                {coming.length > 0
                  ? `Your morning comes at ${coming.map((d) => `${d.next} from ${d.name}`).join(", ")}: your few things for today, with what they've got ready.`
                  : "Nothing for today. Mornings are off for your agents; turn them on in each agent's Mornings."}
              </p>
            )}
            {sent.length > 0 && (
              <p className="m-0 max-w-[70ch] text-[13px] text-muted">
                Tick what&apos;s done, or tell them in the chat (&ldquo;the tasting&apos;s booked&rdquo;). Tomorrow&apos;s morning starts from what&apos;s left.
              </p>
            )}
          </div>

          <div className="flex min-w-0 flex-[1_1_300px] flex-col gap-4">
            {waiting > 0 && (
              <section aria-labelledby="waiting" className="flex flex-col gap-3 rounded-lg border border-line px-5 py-[18px]">
                <div className="flex items-center justify-between gap-2.5">
                  <h2 id="waiting" className="font-display text-base font-semibold tracking-[-0.02em]">Waiting on you</h2>
                  <span className="rounded-full bg-accent px-2.5 py-0.5 text-xs font-bold text-accent-ink">{waiting}</span>
                </div>
                {days
                  .filter((d) => d.waiting > 0)
                  .map((d) => (
                    <Link key={d.agent} href={`/agents/${d.agent}`} className="text-sm font-semibold text-accent-text">
                      {d.waiting === 1 ? `1 from ${d.name}` : `${d.waiting} from ${d.name}`}
                    </Link>
                  ))}
              </section>
            )}
            {coming.length > 0 && sent.length > 0 && (
              <section className="flex flex-col gap-1.5 rounded-lg bg-surface-2 px-[18px] py-4">
                <b className="text-sm">Still to come</b>
                <span className="text-sm text-muted">{coming.map((d) => `${d.name} at ${d.next}`).join(", ")}</span>
              </section>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
