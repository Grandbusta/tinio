"use client";

import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type CharacterSheet } from "@/lib/api";

// Step 11: who an agent is. Their background and personality are yours to change; each
// change is a version you can go back from, and reaches them within a minute. What they
// make is shown here. When they write to you is their Mornings, in Settings.

type Draft = { story: string[]; personality: string[] };
const field =
  "min-h-11 w-full min-w-0 rounded-lg border border-line-strong bg-surface px-3.5 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent";
const button = "inline-flex min-h-10 items-center justify-center rounded-lg px-4 text-[13px] font-semibold disabled:opacity-60";

function draftOf(c: CharacterSheet): Draft {
  return { story: [...c.story], personality: [...c.personality] };
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
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{name ? `${name}'s character` : "Character"}</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          {name ? `Who ${name} is: their background and personality, which shape how they talk and work for you.` : "Who they are: their background and personality."} They
          never contradict their background. A change reaches them within a minute.
        </p>
      </div>
      <div role="group" aria-label="Sections" className="flex flex-wrap gap-1 border-b border-line pb-2.5">
        <span className="inline-flex min-h-10 items-center rounded-lg bg-surface-2 px-3.5 text-sm font-semibold text-fg">Character</span>
        <Link href={`/agents/${id}/settings#mornings`} className="inline-flex min-h-10 items-center rounded-lg px-3.5 text-sm font-medium text-muted hover:text-fg">
          Mornings
        </Link>
        <Link href={`/agents/${id}/judgment`} className="inline-flex min-h-10 items-center rounded-lg px-3.5 text-sm font-medium text-muted hover:text-fg">
          How you decide
        </Link>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {saved && <p role="status" className="text-sm text-good-text">{saved}</p>}
      {c && d && (
        <div className="flex flex-wrap items-start gap-7">
          <form onSubmit={save} className="flex min-w-0 flex-[999_1_460px] flex-col gap-6">
            <>
                <Lines label={`${name}'s background`} hint="One fact a line, in the third person, with their name. It's for their voice and know-how." lines={d.story} set={(story) => setD({ ...d, story })} />
                <Lines label="Personality" hint="How they come across, a trait a line." lines={d.personality} set={(personality) => setD({ ...d, personality })} />
                {c.makes.length > 0 && (
                  <div className="flex flex-col gap-2">
                    <span className="text-sm font-semibold">What {name} makes</span>
                    <div className="flex flex-wrap gap-2">
                      {c.makes.map((m) => (
                        <span key={m.kind} title={m.about} className="rounded-md bg-accent-soft px-3 py-1.5 text-[13px] font-semibold text-accent-text first-letter:uppercase">
                          {m.kind}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
            </>
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
                    <span className="rounded-md bg-good-soft px-2.5 py-[3px] text-xs font-semibold text-good-text">In use</span>
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
