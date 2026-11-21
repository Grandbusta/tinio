"use client";

import { useEffect, useState } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type Goal, type Growth } from "@/lib/api";

// Step 10: what an agent grows towards. Their dreams lead to goals, and a goal about a skill
// to a learning plan whose stages move on only when a check agrees the work meets them.
// Beside them, their newest weekly review and the week in numbers.

const pill = "rounded-full px-2.5 py-1 text-xs font-semibold whitespace-nowrap";

function state(g: Goal): { label: string; className: string } {
  if (g.status === "proposed") return { label: "Waiting for you", className: `${pill} bg-warn-soft text-warn-text` };
  if (g.status === "done") return { label: "Done", className: `${pill} bg-fg text-bg` };
  if (g.plan) return { label: "Learning", className: `${pill} bg-accent-soft text-accent-text` };
  return { label: "Under way", className: `${pill} bg-good-soft text-good-text` };
}

function due(day: string) {
  return new Date(`${day}T12:00:00`).toLocaleDateString([], { day: "numeric", month: "short", year: "numeric" });
}

function GoalRow({ g }: { g: Goal }) {
  const s = state(g);
  const stages = g.plan?.stages ?? [];
  const done = g.status === "done" ? 100 : stages.length ? Math.round((stages.filter((x) => x.done).length * 100) / stages.length) : null;
  const facts = [g.skill && `${g.skill[0].toUpperCase()}${g.skill.slice(1)}${g.level ? `, level ${Math.round(g.level * 10) / 10} of 10` : ""}`, g.due && `by ${due(g.due)}`]
    .filter(Boolean)
    .join(" · ");
  return (
    <div className="flex flex-col gap-2.5 border-b border-line px-5 py-3.5 last:border-b-0">
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <b className="text-[15px] font-semibold">{g.text}</b>
        <span className={s.className}>{s.label}</span>
      </div>
      {done !== null && (
        <div role="progressbar" aria-label="How far" aria-valuenow={done} aria-valuemin={0} aria-valuemax={100} className="h-1.5 overflow-hidden rounded-full bg-surface-2">
          <div className="h-full bg-accent" style={{ width: `${done}%` }} />
        </div>
      )}
      {facts && <span className="text-[13px] text-muted">{facts}</span>}
      {(g.progress || g.done_when) && <span className="text-[13px] text-muted">{g.progress || `Done when: ${g.done_when}`}</span>}
      {stages.length > 0 && (
        <ol aria-label="Learning plan" className="m-0 grid list-none gap-1.5 p-0" style={{ gridTemplateColumns: `repeat(${Math.min(stages.length, 4)}, minmax(0, 1fr))` }}>
          {stages.map((st, i) => {
            const now = !st.done && i === g.plan?.stage && g.status !== "done";
            return (
              <li
                key={i}
                aria-current={now ? "step" : undefined}
                className={`rounded-md px-2 py-1.5 text-center text-xs font-semibold ${st.done ? "bg-accent-soft text-accent-text" : now ? "bg-accent text-accent-ink" : "text-muted shadow-[inset_0_0_0_1px_var(--color-line)]"}`}
              >
                {st.learn}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}

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
  const groups = g ? [...g.dreams.map((d) => ({ key: d.id, title: d.text, label: "Dream", goals: d.goals })), ...(g.other_goals.length ? [{ key: "other", title: "Other goals", label: "", goals: g.other_goals }] : [])] : [];
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Growth</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          {name ? `${name}'s` : "Their"} dreams lead to goals, and a goal about a skill to a learning plan. A stage moves on only when an independent check agrees
          the work meets it. New goals wait for your yes.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {g && (
        <div className="flex flex-wrap items-start gap-6">
          <div className="flex min-w-0 flex-[999_1_460px] flex-col gap-4">
            {groups.length === 0 && <p className="text-muted">No dreams yet.</p>}
            {groups.map((d) => (
              <section key={d.key} aria-label={d.title} className="overflow-hidden rounded-lg border border-line">
                <div className="flex flex-col gap-0.5 border-b border-line bg-accent-soft px-5 py-4">
                  {d.label && <span className="text-xs font-semibold tracking-[0.04em] text-accent-text uppercase">{d.label}</span>}
                  <h2 className="font-display text-[19px] leading-snug font-semibold tracking-[-0.02em]">{d.title}</h2>
                </div>
                {d.goals.length === 0 ? (
                  <p className="m-0 px-5 py-3.5 text-[13px] text-muted">No goals for it yet. {name} sets them in the weekly review.</p>
                ) : (
                  d.goals.map((x) => <GoalRow key={x.id} g={x} />)
                )}
              </section>
            ))}
          </div>
          <aside className="flex min-w-0 flex-[1_1_320px] flex-col gap-3">
            <article className="flex flex-col gap-3 rounded-lg bg-warn-soft px-[22px] pt-[22px] pb-5">
              <span className="text-xs font-semibold tracking-[0.04em] text-accent-text uppercase">
                {g.review ? `${name ? `${name}'s` : "Their"} weekly review · ${when(g.review.at)}` : "Weekly review"}
              </span>
              {g.review ? (
                <>
                  <b className="font-display text-[17px] font-semibold">{g.review.title}</b>
                  {g.review.text.split(/\n\s*\n/).map((para, i) => (
                    <p key={i} className="m-0 text-[15px]">{para}</p>
                  ))}
                </>
              ) : (
                <p className="m-0 text-[15px]">{name} hasn&apos;t reviewed a week yet. Each week they look back at what they made and set the next goals.</p>
              )}
            </article>
            <div className="flex flex-col gap-2 rounded-lg border border-line px-[18px] py-4 text-sm">
              <b className="font-semibold">The last seven days</b>
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
