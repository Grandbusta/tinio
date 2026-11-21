"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Back, useFrame } from "@/components/app-frame";
import { call, type Today } from "@/lib/api";

// Step 7: an agent's own day, in the app's frame. What they planned and how each thing
// went, what they made, and a line from their journal. Reached from the panel about them.

const states: Record<Today["plan"][number]["state"], { label: string; className: string }> = {
  done: { label: "Done", className: "bg-good-soft text-good-text" },
  now: { label: "Now", className: "bg-fg text-bg" },
  later: { label: "Later", className: "shadow-[inset_0_0_0_1px_var(--color-line)] text-muted" },
  waiting: { label: "Waiting for you", className: "bg-accent-soft text-accent-text" },
  missed: { label: "Missed", className: "bg-warn-soft text-warn-text" },
};

export function TodayPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [today, setToday] = useState<Today | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!account) return;
    call<{ today: Today }>(`/agents/${id}/today`).then((res) => {
      if (res.ok) setToday(res.data.today);
      else setError(res.error.message);
    });
  }, [account, id]);

  if (missing) {
    return (
      <div className="grid flex-1 place-items-center px-6">
        <p role="alert" className="text-bad-text">We can&apos;t find that agent among yours.</p>
      </div>
    );
  }

  const name = agent?.name ?? "";
  return (
      <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
        <Back id={id} />
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-[13px] text-muted">
              {today?.date}
              {agent && ` · ${together(agent.created_at, name)}`}
            </span>
            <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Today</h1>
          </div>
          <div className="flex flex-wrap gap-2">
            {today && today.waiting > 0 && (
              <Link href={`/agents/${id}`} className="inline-flex min-h-9 items-center rounded-full bg-accent px-3.5 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover">
                {today.waiting === 1 ? "1 ask waiting" : `${today.waiting} asks waiting`}
              </Link>
            )}
          </div>
        </div>
        {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}

        {today && (
          <div className="flex flex-wrap items-start gap-6">
            <section aria-labelledby="plan" className="min-w-0 flex-[999_1_460px] rounded-lg border border-line">
              <div className="flex items-center justify-between gap-3 border-b border-line px-5 py-4">
                <h2 id="plan" className="font-display text-lg font-semibold tracking-[-0.02em]">{name}&apos;s plan</h2>
                {today.planned_at && <span className="text-[13px] text-muted">Planned at {today.planned_at}</span>}
              </div>
              {today.plan.length === 0 ? (
                <p className="px-5 py-4 text-[15px] text-muted">
                  Nothing planned yet today. {name} plans at {today.plans_at}, their time, once you&apos;ve been in touch this week.
                </p>
              ) : (
                <ol className="m-0 list-none p-0">
                  {today.plan.map((p, i) => (
                    <li key={i} className="flex gap-4 border-b border-line px-5 py-3.5">
                      <span className="w-12 flex-none pt-0.5 text-[13px] font-semibold text-muted">{p.at}</span>
                      <div className="flex min-w-0 flex-auto flex-col gap-1">
                        <span className="font-semibold">{p.what}</span>
                        {p.detail && <span className="text-sm text-muted">{p.detail}</span>}
                      </div>
                      <span className={`self-start rounded-full px-2.5 py-1 text-xs font-semibold whitespace-nowrap ${states[p.state].className}`}>
                        {states[p.state].label}
                      </span>
                    </li>
                  ))}
                </ol>
              )}
              <p className="m-0 px-5 py-3.5 text-[13px] text-muted">
                {name} plans from their goals, their skills and what you asked for. To move anything, tell {name} in the chat.
              </p>
            </section>

            <div className="flex min-w-0 flex-[1_1_300px] flex-col gap-4">
              <section aria-labelledby="made" className="flex flex-col gap-3 rounded-lg border border-line px-5 py-[18px]">
                <h2 id="made" className="font-display text-base font-semibold tracking-[-0.02em]">Made today</h2>
                {today.made.length === 0 ? (
                  <span className="text-sm text-muted">Nothing yet.</span>
                ) : (
                  today.made.map((w, i) => (
                    <Link key={i} href={`/agents/${id}/works/${w.id}`} className="flex min-w-0 flex-col rounded-md hover:bg-surface-2">
                      <b className="text-sm text-accent-text">{w.title}</b>
                      <span className="text-xs text-muted first-letter:uppercase">
                        {w.kind} · {w.at}
                      </span>
                    </Link>
                  ))
                )}
              </section>

              {today.journal && (
                <section aria-labelledby="journal" className="flex flex-col gap-1.5 rounded-lg border border-warn-text/20 bg-warn-soft px-[18px] py-4 text-muted">
                  <b id="journal" className="text-sm text-fg">From {name}&apos;s journal</b>
                  <q className="text-sm">{today.journal}</q>
                  <span className="text-xs">The journal is {name}&apos;s own; this is how the newest entry begins.</span>
                </section>
              )}
            </div>
          </div>
        )}
      </div>
  );
}

// together is how long they've been with you, like "three weeks with Mei".
function together(raised: string, name: string) {
  const days = Math.floor((Date.now() - new Date(raised).getTime()) / 86_400_000);
  if (days < 1) return `your first day with ${name}`;
  if (days < 14) return `${days === 1 ? "one day" : `${days} days`} with ${name}`;
  if (days < 60) return `${Math.floor(days / 7)} weeks with ${name}`;
  return `${Math.floor(days / 30)} months with ${name}`;
}
