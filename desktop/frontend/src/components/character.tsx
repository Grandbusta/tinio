"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type CharacterDay, type CharacterSheet } from "@/lib/api";

// Step 11: who an agent is. Their story, personality, routine and day are yours to change;
// each change is a version you can go back from, and reaches them within a minute. What
// they make and their dreams are shown here; dreams change through their weekly review,
// with your yes.

type Draft = { story: string[]; personality: string[]; routine: string[]; day: CharacterDay };

const days = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const field =
  "min-h-11 w-full min-w-0 rounded-lg border border-line-strong bg-surface px-3.5 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent";
const button = "inline-flex min-h-10 items-center justify-center rounded-full px-4 text-[13px] font-semibold disabled:opacity-60";

function draftOf(c: CharacterSheet): Draft {
  return { story: [...c.story], personality: [...c.personality], routine: [...c.routine], day: { ...c.day, check_ins: [...c.day.check_ins] } };
}

function versionName(source: string) {
  switch (source) {
    case "edit":
      return "Changed by you";
    case "rollback":
      return "Gone back to an earlier version";
    case "import":
      return "Imported";
    default:
      return "Raised";
  }
}

function Lines({ label, hint, lines, set }: { label: string; hint: string; lines: string[]; set: (ls: string[]) => void }) {
  const id = label.toLowerCase().replaceAll(" ", "-");
  return (
    <fieldset className="m-0 flex flex-col gap-2 border-0 p-0">
      <legend className="mb-2 p-0 text-sm font-semibold">{label}</legend>
      <span className="-mt-1 text-[13px] text-muted">{hint}</span>
      {lines.map((l, i) => (
        <div key={i} className="flex items-center gap-2">
          <label htmlFor={`${id}-${i}`} className="sr-only">{`${label}, line ${i + 1}`}</label>
          <textarea
            id={`${id}-${i}`}
            rows={1}
            className={`${field} resize-none py-2.5 leading-6 [field-sizing:content]`}
            value={l}
            maxLength={400}
            onChange={(e) => set(lines.map((x, j) => (j === i ? e.target.value.replaceAll("\n", " ") : x)))}
          />
          <button
            type="button"
            aria-label={`Remove line ${i + 1}`}
            onClick={() => set(lines.filter((_, j) => j !== i))}
            className="grid size-11 flex-none place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M6 6l12 12" />
              <path d="M18 6L6 18" />
            </svg>
          </button>
        </div>
      ))}
      {lines.length < 40 && (
        <button type="button" onClick={() => set([...lines, ""])} className={`${button} self-start border border-line-strong hover:border-fg`}>
          Add a line
        </button>
      )}
    </fieldset>
  );
}

export function CharacterPage({ id }: { id: string }) {
  const { account, agent, missing, refresh } = useFrame();
  const [c, setC] = useState<CharacterSheet | null>(null);
  const [d, setD] = useState<Draft | null>(null);
  const [tab, setTab] = useState<"character" | "work">("character");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [busy, setBusy] = useState(false);

  function take(sheet: CharacterSheet) {
    setC(sheet);
    setD(draftOf(sheet));
  }

  useEffect(() => {
    if (!account) return;
    call<CharacterSheet>(`/agents/${id}/character`).then((res) => (res.ok ? take(res.data) : setError(res.error.message)));
  }, [account, id]);

  async function save(e?: FormEvent) {
    e?.preventDefault();
    if (!d || busy) return;
    setBusy(true);
    setError("");
    setSaved("");
    const res = await call<CharacterSheet>(`/agents/${id}/character`, d);
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    take(res.data);
    setSaved(`Saved. ${c?.name ?? "They"} will have it within a minute.`);
    refresh();
  }

  async function goBack(version: string) {
    setBusy(true);
    setError("");
    setSaved("");
    const res = await call<CharacterSheet>(`/agents/${id}/character/versions/${version}`, {});
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    take(res.data);
    setSaved(`Gone back. ${res.data.name} will have it within a minute.`);
    refresh();
  }

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = c?.name ?? agent?.name ?? "";
  const changed = c && d && JSON.stringify(draftOf(c)) !== JSON.stringify(d);
  const [reviewDay, reviewTime] = (d?.day.review_at ?? "").split(" ");
  const setDay = (day: Partial<CharacterDay>) => setD((x) => x && { ...x, day: { ...x.day, ...day } });
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{name ? `${name}'s character` : "Character"}</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          {name ? `Who ${name} is, and how they work.` : "Who they are, and how they work."} They never contradict their story; anything it doesn&apos;t cover they
          decide the first time it comes up, and remember. A change reaches them within a minute.
        </p>
      </div>
      <div role="group" aria-label="Sections" className="flex flex-wrap gap-1 border-b border-line pb-2.5">
        {(
          [
            ["character", "Character"],
            ["work", name ? `How ${name} works` : "How they work"],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            aria-pressed={tab === key}
            onClick={() => setTab(key)}
            className={`min-h-10 rounded-full px-3.5 text-sm ${tab === key ? "bg-surface-2 font-semibold text-fg" : "font-medium text-muted hover:text-fg"}`}
          >
            {label}
          </button>
        ))}
        <Link href={`/agents/${id}/judgment`} className="inline-flex min-h-10 items-center rounded-full px-3.5 text-sm font-medium text-muted hover:text-fg">
          How you decide
        </Link>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {saved && <p role="status" className="text-sm text-good-text">{saved}</p>}
      {c && d && (
        <div className="flex flex-wrap items-start gap-7">
          <form onSubmit={save} className="flex min-w-0 flex-[999_1_460px] flex-col gap-6">
            {tab === "character" ? (
              <>
                <Lines label={`${name}'s story`} hint="One fact a line, in the third person, with their name." lines={d.story} set={(story) => setD({ ...d, story })} />
                <Lines label="Personality" hint="How they come across, a trait a line." lines={d.personality} set={(personality) => setD({ ...d, personality })} />
                {c.makes.length > 0 && (
                  <div className="flex flex-col gap-2">
                    <span className="text-sm font-semibold">What {name} makes</span>
                    <div className="flex flex-wrap gap-2">
                      {c.makes.map((m) => (
                        <span key={m.kind} title={m.about} className="rounded-full bg-accent-soft px-3 py-1.5 text-[13px] font-semibold text-accent-text first-letter:uppercase">
                          {m.kind}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
                {c.dreams.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-sm font-semibold">What {name} is working toward</span>
                    <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-[15px]">
                      {c.dreams.map((x) => (
                        <li key={x}>{x}</li>
                      ))}
                    </ul>
                    <span className="text-[13px] text-muted">
                      Dreams change in their weekly review, and a change asks you first. See them in <Link href={`/agents/${id}/growth`}>Growth</Link>.
                    </span>
                  </div>
                )}
              </>
            ) : (
              <>
                <div className="grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-4">
                  <label className="flex flex-col gap-1.5 text-sm font-semibold">
                    Plans their day at
                    <input type="time" className={field} value={d.day.plan_at} onChange={(e) => setDay({ plan_at: e.target.value })} />
                  </label>
                  <label className="flex flex-col gap-1.5 text-sm font-semibold">
                    Writes their journal at
                    <input type="time" className={field} value={d.day.journal_at} onChange={(e) => setDay({ journal_at: e.target.value })} />
                  </label>
                  <fieldset className="m-0 flex flex-col gap-1.5 border-0 p-0">
                    <legend className="mb-1.5 p-0 text-sm font-semibold">Reviews their week on</legend>
                    <div className="flex gap-2">
                      <label htmlFor="review-day" className="sr-only">Day</label>
                      <select
                        id="review-day"
                        className={`${field.replace("w-full ", "")} w-28 flex-none`}
                        value={reviewDay ?? ""}
                        onChange={(e) => setDay({ review_at: e.target.value ? `${e.target.value} ${reviewTime || "20:00"}` : "" })}
                      >
                        <option value="">Tinio&apos;s default</option>
                        {days.map((x) => (
                          <option key={x} value={x}>{x}</option>
                        ))}
                      </select>
                      <label htmlFor="review-time" className="sr-only">Time</label>
                      <input
                        id="review-time"
                        type="time"
                        className={field}
                        disabled={!reviewDay}
                        value={reviewTime ?? ""}
                        onChange={(e) => setDay({ review_at: `${reviewDay} ${e.target.value}` })}
                      />
                    </div>
                  </fieldset>
                </div>
                <span className="-mt-3 text-[13px] text-muted">Times are {name}&apos;s own{c.timezone ? `, in ${c.timezone.replaceAll("_", " ")}` : ""}. Left empty, Tinio&apos;s defaults.</span>
                <Lines label="When they check in with you" hint="A time a line, like 18:30. None: they don't check in unprompted." lines={d.day.check_ins} set={(check_ins) => setDay({ check_ins })} />
                <Lines label="Their routine" hint="What a day of theirs looks like, a habit a line." lines={d.routine} set={(routine) => setD({ ...d, routine })} />
              </>
            )}
            <div className="flex gap-2.5">
              <button type="submit" disabled={busy || !changed} className={`${button} bg-accent text-accent-ink hover:bg-accent-hover`}>
                Save
              </button>
              <button type="button" disabled={busy || !changed} onClick={() => setD(draftOf(c))} className={`${button} border border-line-strong hover:border-fg`}>
                Cancel
              </button>
            </div>
          </form>
          <aside aria-labelledby="history" className="flex min-w-0 flex-[1_1_280px] flex-col gap-3 rounded-lg border border-line px-5 py-[18px]">
            <h2 id="history" className="font-display text-base font-semibold tracking-[-0.02em]">History</h2>
            <ol className="m-0 flex list-none flex-col p-0">
              {c.versions.map((v, i) => (
                <li key={v.id} className="flex items-center justify-between gap-2.5 border-t border-line py-2.5">
                  <div className="flex min-w-0 flex-col">
                    <span className="text-sm font-semibold">{versionName(v.source)}</span>
                    <span className="text-xs text-muted">{when(v.at)}</span>
                  </div>
                  {i === 0 ? (
                    <span className="rounded-full bg-good-soft px-2.5 py-[3px] text-xs font-semibold text-good-text">In use</span>
                  ) : (
                    <button type="button" disabled={busy} onClick={() => goBack(v.id)} className={`${button} min-h-8 border border-line-strong px-3 text-xs hover:border-fg`}>
                      Go back
                    </button>
                  )}
                </li>
              ))}
            </ol>
          </aside>
        </div>
      )}
    </div>
  );
}
