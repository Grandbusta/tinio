"use client";

import Link from "next/link";
import { useEffect, useState, type DragEvent } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type AgentSkill, type Skills } from "@/lib/api";

// An agent's skills, as on the canvas: each on the ladder (Apprentice, then Pro once their
// scored work holds the standard), and the know-how their owner imported in the Agent
// Skills format. Certified, with a skill's tests, and the market are the platform's.

const rungs = ["Apprentice", "Pro"] as const;

function Ladder({ s }: { s: AgentSkill }) {
  const at = s.rung === "pro" ? 1 : 0;
  return (
    <ol aria-label="Ladder" className="m-0 grid flex-[1_1_300px] list-none grid-cols-2 gap-1.5 p-0">
      {rungs.map((r, i) => (
        <li
          key={r}
          aria-current={i === at ? "step" : undefined}
          className={`rounded-full px-2.5 py-2 text-center text-[13px] font-semibold ${
            i === at ? "bg-accent text-accent-ink" : i < at ? "bg-accent-soft text-accent-text" : "font-medium text-muted shadow-[inset_0_0_0_1px_var(--color-line)]"
          }`}
        >
          {r}
        </li>
      ))}
    </ol>
  );
}

function words(s: string) {
  return s.replaceAll("_", " ");
}

export function SkillsPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [data, setData] = useState<Skills | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");

  useEffect(() => {
    if (!account) return;
    call<Skills>(`/agents/${id}/skills`).then((res) => (res.ok ? setData(res.data) : setError(res.error.message)));
  }, [account, id]);

  async function remove(name: string) {
    setBusy(name);
    setError("");
    const res = await call(`/agents/${id}/skills/${encodeURIComponent(name)}/remove`, {});
    setBusy("");
    if (!res.ok) return setError(res.error.message);
    setData((d) => d && { ...d, know_how: d.know_how.filter((k) => k.name !== name) });
  }

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = agent?.name ?? "They";
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1.5">
          <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{agent ? `${agent.name}'s skills` : "Skills"}</h1>
          <p className="m-0 max-w-[62ch] text-muted">
            Each skill climbs the same ladder: Apprentice, then Pro when {name}&apos;s scored work holds the standard. Teach them more by importing a
            skill you already use.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Link href={`/agents/${id}/skills/import`} className="inline-flex min-h-10 items-center rounded-full bg-accent px-4 text-[13px] font-semibold text-accent-ink no-underline hover:bg-accent-hover hover:text-accent-ink">
            Import a skill
          </Link>
        </div>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}

      {data && (
        <>
          <section aria-labelledby="ladder" className="flex flex-col gap-3">
            <h2 id="ladder" className="sr-only">On the ladder</h2>
            {data.skills.length === 0 && <p className="m-0 text-sm text-muted">{name}&apos;s skills show here once they&apos;re born, within a minute of raising them.</p>}
            {data.skills.map((s) => (
              <article key={s.name} className="flex flex-wrap items-center gap-5 rounded-lg border border-line px-5 py-4">
                <div className="flex min-w-0 flex-[1_1_240px] flex-col gap-1">
                  <h3 className="font-display text-lg font-semibold tracking-[-0.02em] first-letter:uppercase">{s.name}</h3>
                  <span className="text-[13px] text-muted">
                    {s.born ? "Born with it · in their character" : "Grown · from their goals"}
                    {s.shown_by.length > 0 && ` · shown by ${s.shown_by.map(words).join(", ")}`}
                  </span>
                </div>
                <Ladder s={s} />
                <div className="flex min-w-0 flex-[1_1_220px] flex-col gap-1">
                  <span className="text-sm">
                    Level {s.level} of 10{s.scores.length > 0 && ` · last scored ${s.scores.join(", ")}`}
                  </span>
                  <span className="text-[13px] text-muted">
                    {s.rung === "pro" ? "At the top of the ladder: their scored work holds the standard." : "Scored work at 6 or above makes them a Pro."}
                  </span>
                </div>
              </article>
            ))}
          </section>

          <section aria-labelledby="knowhow" className="flex flex-col gap-2.5">
            <div className="flex flex-col gap-1">
              <h2 id="knowhow" className="font-display text-xl font-semibold tracking-[-0.02em]">Know-how</h2>
              <span className="text-sm text-muted">
                Instructions in the Agent Skills format (a SKILL.md), imported from any app that uses it. {name}{" "}
                reads them when a task fits; they don&apos;t climb the ladder on their own.
              </span>
            </div>
            {data.know_how.length === 0 && (
              <p className="m-0 text-sm text-muted">
                None yet. <Link href={`/agents/${id}/skills/import`}>Import one</Link> you already use.
              </p>
            )}
            {data.know_how.map((k) => (
              <article key={k.name} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-line px-[18px] py-3.5">
                <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
                  <b className="text-[15px] font-semibold">{k.title}</b>
                  <span className="text-sm">{k.description}</span>
                  <span className="text-xs text-muted">
                    {k.name} · imported {when(k.added_at)} · {k.files === 1 ? "SKILL.md" : `SKILL.md and ${k.files - 1} more file${k.files > 2 ? "s" : ""}`}
                  </span>
                </div>
                <button
                  type="button"
                  disabled={busy === k.name}
                  onClick={() => remove(k.name)}
                  className="min-h-9 rounded-full border border-line-strong px-3.5 text-[13px] font-semibold hover:border-fg disabled:opacity-60"
                >
                  Remove
                </button>
              </article>
            ))}
          </section>
        </>
      )}
    </div>
  );
}

type Checked = { name: string; title: string; written: string; description: string; files: string[] };

// SkillImportPage takes a SKILL.md or a zipped skill folder, shows what's in it, and keeps
// it as the agent's know-how on the owner's word.
export function SkillImportPage({ id }: { id: string }) {
  const { agent } = useFrame();
  const [file, setFile] = useState<File | null>(null);
  const [checked, setChecked] = useState<Checked | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState("");
  const name = agent?.name ?? "They";

  function form(f: File) {
    const fd = new FormData();
    fd.append("file", f);
    return fd;
  }

  async function check(f: File) {
    setFile(f);
    setChecked(null);
    setDone("");
    setError("");
    setBusy(true);
    const res = await call<Checked>(`/agents/${id}/skills/import?check=1`, form(f));
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    setChecked(res.data);
  }

  async function keep() {
    if (!file) return;
    setBusy(true);
    setError("");
    const res = await call(`/agents/${id}/skills/import`, form(file));
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    setDone(checked?.title ?? file.name);
    setChecked(null);
    setFile(null);
  }

  return (
    <div className="flex max-w-[820px] flex-col gap-5 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Link href={`/agents/${id}/skills`} className="-ml-2 inline-flex min-h-11 items-center self-start rounded-full px-2 text-sm font-semibold text-accent-text">
        ‹ {agent ? `${agent.name}'s skills` : "Their skills"}
      </Link>
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Import a skill</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          Bring know-how you already have: a skill in the Agent Skills format, a SKILL.md with a name and a description, alone or in a zipped folder with
          the files it points to. {name} reads it when a task fits.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}

      {!checked && !done && <SkillDrop busy={busy} onFile={check} />}

      {checked && file && (
        <section className="flex flex-col gap-3.5 rounded-lg border border-line px-[22px] py-5">
          <div className="flex flex-wrap items-baseline justify-between gap-2.5">
            <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">{checked.title}</h2>
            <span className="text-[13px] text-muted">
              {file.name} · {checked.files.length} file{checked.files.length === 1 ? "" : "s"} · {Math.max(1, Math.round(file.size / 1024))} KB
            </span>
          </div>
          <span className="-mt-2 text-[13px] text-muted">
            {checked.written
              ? `Its SKILL.md calls it "${checked.written}"; we'll call it ${checked.name}, the way skills are named, so ${name} can ask for it by name.`
              : `Named ${checked.name}. A skill with the same name is replaced.`}
          </span>
          <p className="m-0 text-[15px]">{checked.description}</p>
          <div className="flex flex-col gap-1">
            <span className="text-xs font-semibold tracking-[0.04em] text-muted uppercase">Files</span>
            <ul className="m-0 list-disc pl-[18px] text-sm">
              {checked.files.slice(0, 12).map((f) => (
                <li key={f}>{f}</li>
              ))}
              {checked.files.length > 12 && <li>and {checked.files.length - 12} more</li>}
            </ul>
          </div>
          <div className="flex flex-col gap-1 rounded-lg border border-warn-text/20 bg-warn-soft px-3.5 py-3 text-sm">
            <b>Import only skills you trust</b>
            <span>
              {name} reads a skill like instructions from you. It can&apos;t give them permissions: what they may do stays as you set it in their settings.
              Scripts in it are never run, only read.
            </span>
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="button" disabled={busy} onClick={keep} className="min-h-11 rounded-full bg-accent px-5 text-sm font-semibold text-accent-ink hover:bg-accent-hover disabled:opacity-60">
              {busy ? "Importing…" : "Import it"}
            </button>
            <button type="button" disabled={busy} onClick={() => (setChecked(null), setFile(null))} className="min-h-11 rounded-full border border-line-strong px-5 text-sm font-semibold hover:border-fg">
              Choose another
            </button>
          </div>
        </section>
      )}

      {done && (
        <section role="status" className="flex flex-col gap-2.5 rounded-lg border border-good-text/20 bg-good-soft px-[22px] py-5">
          <b className="text-base text-good-text">Imported {done}</b>
          <span className="text-sm text-good-text">{name} has it within a minute, and reads it when a task fits.</span>
          <div className="flex flex-wrap gap-2">
            <Link href={`/agents/${id}/skills`} className="inline-flex min-h-10 items-center rounded-full border border-line-strong bg-bg px-4 text-[13px] font-semibold text-fg no-underline hover:border-fg">
              Back to their skills
            </Link>
            <button type="button" onClick={() => setDone("")} className="min-h-10 rounded-full px-4 text-[13px] font-semibold text-accent-text">
              Import another
            </button>
          </div>
        </section>
      )}

      <section className="flex flex-col gap-1.5 text-sm text-muted">
        <b className="text-fg">What a skill looks like</b>
        <span>
          A folder with a SKILL.md that starts with its name and a description of when to use it, then the instructions. Other files it mentions,
          like examples, come along in the zip.
        </span>
      </section>
    </div>
  );
}

// SkillDrop takes a SKILL.md or a zipped skill folder, dropped on it or chosen. With
// file, it shows what was chosen, to choose another.
export function SkillDrop({ busy, file, onFile }: { busy: boolean; file?: File | null; onFile: (f: File) => void }) {
  const [over, setOver] = useState(false);
  function drop(e: DragEvent) {
    e.preventDefault();
    setOver(false);
    const f = e.dataTransfer.files[0];
    if (f) onFile(f);
  }
  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={drop}
      className={`flex flex-col items-center gap-3 rounded-xl border-2 border-dashed px-6 py-10 text-center ${over ? "border-accent bg-accent-soft/40" : "border-line-strong"}`}
    >
      <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className="text-muted">
        <path d="M12 15V3" />
        <path d="M7 8l5-5 5 5" />
        <path d="M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4" />
      </svg>
      {file ? (
        <>
          <b className="text-base">{file.name}</b>
          <span className="text-sm text-muted">{Math.max(1, Math.round(file.size / 1024))} KB · ready to read</span>
        </>
      ) : (
        <>
          <b className="text-base">Drop a SKILL.md or a .zip here</b>
          <span className="text-sm text-muted">Up to 100 files and 10 MB.</span>
        </>
      )}
      <label
        className={`inline-flex min-h-11 cursor-pointer items-center rounded-full px-5 text-sm font-semibold ${
          file ? "border border-line-strong text-fg hover:border-fg" : "bg-accent text-accent-ink hover:bg-accent-hover"
        }`}
      >
        {busy ? "Reading it…" : file ? "Choose another" : "Choose a file"}
        <input
          type="file"
          accept=".md,.zip,text/markdown,application/zip"
          className="sr-only"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) onFile(f);
            e.target.value = "";
          }}
        />
      </label>
    </div>
  );
}
