"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type Growth } from "@/lib/api";

// Helping you: how an agent gets better at their work for you, only ever from you. What
// they've learned about how you like things (from your edits, your tastes, and their own
// work for you), the skills you've taught them, and the week in numbers.

const from: Record<Growth["learned"][number]["from"], string> = {
  owner: "From your edits",
  taste: "You said so",
  self: "From their work for you",
  others: "From others' work",
};

function of(n: number, total: number) {
  return total ? `${n} of ${total}` : "None";
}

export function GrowthPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [g, setG] = useState<Growth | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!account) return;
    call<Growth>(`/agents/${id}/growth`).then((res) => (res.ok ? setG(res.data) : setError(res.error.message)));
  }, [account, id]);

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = agent?.name ?? "";
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Getting better at helping you</h1>
        <p className="m-0 max-w-[66ch] text-muted">
          {name || "They"} gets better only from you: what you teach, your thumbs on their work, the drafts you change, and how you decide. Nothing here is for
          themselves.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {g && (
        <div className="flex flex-wrap items-start gap-6">
          <div className="flex min-w-0 flex-[999_1_460px] flex-col gap-4">
            <section aria-labelledby="learned" className="rounded-lg border border-line">
              <div className="flex items-center justify-between gap-3 border-b border-line px-5 py-3.5">
                <h2 id="learned" className="font-display text-[17px] font-semibold tracking-[-0.02em]">What {name || "they"} has learned about how you like things</h2>
                <Link href={`/agents/${id}/judgment`} className="text-[13px] font-semibold text-accent-text">How you decide</Link>
              </div>
              {g.learned.length === 0 ? (
                <p className="m-0 px-5 py-4 text-[15px] text-muted">Nothing yet. Change a draft, give a thumbs down with a note, or just say how you like it in the chat.</p>
              ) : (
                <ul className="m-0 list-none p-0">
                  {g.learned.map((l, i) => (
                    <li key={i} className="flex flex-col gap-0.5 border-b border-line px-5 py-3.5 last:border-b-0">
                      <b className="font-semibold">{l.text}</b>
                      <span className="text-[13px] text-muted">
                        {from[l.from]}
                        {l.skill && ` · ${l.skill}`} · {when(l.at)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </section>

            <section aria-labelledby="skills" className="rounded-lg border border-line">
              <div className="flex items-center justify-between gap-3 border-b border-line px-5 py-3.5">
                <h2 id="skills" className="font-display text-[17px] font-semibold tracking-[-0.02em]">Skills</h2>
                <Link href={`/agents/${id}/skills`} className="text-[13px] font-semibold text-accent-text">Teach a skill</Link>
              </div>
              {g.skills.length === 0 ? (
                <p className="m-0 px-5 py-4 text-[15px] text-muted">No skills yet.</p>
              ) : (
                g.skills.map((s) => (
                  <div key={s.name} className="flex flex-col gap-2 border-b border-line px-5 py-3.5 last:border-b-0">
                    <div className="flex items-center justify-between gap-2.5">
                      <b className="font-semibold first-letter:uppercase">{s.name}</b>
                      <span className="text-[13px] text-muted">{Math.round(s.level * 10) / 10} of 10</span>
                    </div>
                    <div aria-hidden="true" className="h-1.5 overflow-hidden rounded-full bg-surface-2">
                      <div className="h-full bg-accent" style={{ width: `${Math.min(100, s.level * 10)}%` }} />
                    </div>
                  </div>
                ))
              )}
            </section>
          </div>

          <aside className="flex min-w-0 flex-[1_1_300px] flex-col gap-3">
            <div className="flex flex-col gap-2 rounded-lg border border-line px-[18px] py-4 text-sm">
              <b className="font-semibold">This week, for you</b>
              <div className="flex justify-between gap-3"><span className="text-muted">Morning things done</span><span>{of(g.week.done, g.week.things)}</span></div>
              <div className="flex justify-between gap-3"><span className="text-muted">Works made</span><span>{g.week.made}</span></div>
              <div className="flex justify-between gap-3"><span className="text-muted">Requests you approved</span><span>{of(g.week.approved, g.week.decided)}</span></div>
              <div className="flex justify-between gap-3"><span className="text-muted">Your calls {name || "they"} guessed</span><span>{of(g.week.guessed, g.week.answered)}</span></div>
            </div>
          </aside>
        </div>
      )}
    </div>
  );
}
