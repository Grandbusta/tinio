import Link from "next/link";
import { useEffect, useState } from "react";
import { useFrame } from "@/components/app-frame";
import { KeyForm } from "@/components/first-run";
import { box, heading } from "@/components/kit";
import { inTheApp, ThisApp } from "@/components/this-app";
import { Appearance } from "@/components/usage";
import { call } from "@/lib/api";

// Settings: your keys, the models your agents use for each of their jobs, the app
// itself, and how it looks. Every model listed is one Tinio knows the price of, so the
// week's cap holds whichever you pick.

type ModelOption = { model: string; input: number; output: number };
type Job = { job: string; model: string; from: "app" | "environment" | "default"; default: string; models: ModelOption[] };

const jobs: Record<string, { title: string; note: string; env: string }> = {
  reply: { title: "Replies", note: "Everything they say to you. Most of what a week costs.", env: "OPENAI_MODEL" },
  decision: { title: "Decisions", note: "The quick calls before a reply. Only one model makes them for now.", env: "OPENAI_DECISION_MODEL" },
  day: {
    title: "Their own day",
    note: "Planning, learning, what they read, their weekly review. It runs every day, so a light model keeps it to about a cent.",
    env: "OPENAI_DAY_MODEL",
  },
  light: { title: "Light mode", note: "Replies once the week's cap is reached.", env: "OPENAI_LIGHT_MODEL" },
  work: { title: "Works", note: "What they make: writing, documents, music.", env: "OPENAI_WORK_MODEL" },
};

const price = (m: ModelOption) => `$${m.input} in, $${m.output} out`;

export function SettingsPage() {
  const { refresh } = useFrame();
  return (
    <div className="mx-auto flex w-full max-w-[880px] flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-4">
      <div className="flex flex-col gap-1">
        <Link href="/agents" className="mb-2 inline-flex min-h-11 items-center gap-1.5 self-start text-sm font-semibold text-accent-text min-[901px]:hidden">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M15 6l-6 6 6 6" />
          </svg>
          Chats
        </Link>
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Settings</h1>
        <span className="text-muted">Everything here stays on this computer.</span>
      </div>

      <section aria-labelledby="keys" className={box}>
        <h2 id="keys" className={heading}>
          Keys
        </h2>
        <p className="m-0 text-[13px] text-muted">
          The keys for the services your agents use: the only thing you bring. Each is sealed with a key Tinio made on this computer, and only ever shown
          masked.
        </p>
        <KeyForm onSaved={refresh} />
      </section>

      <ModelsSection />

      {inTheApp() && <ThisApp />}

      <section aria-labelledby="looks" className={box}>
        <h2 id="looks" className={heading}>
          How it looks
        </h2>
        <Appearance />
      </section>
    </div>
  );
}

function ModelsSection() {
  const [list, setList] = useState<Job[] | null>(null);
  const [error, setError] = useState<{ job: string; message: string } | null>(null);
  const [saving, setSaving] = useState("");
  const [saved, setSaved] = useState("");
  useEffect(() => {
    call<{ jobs: Job[] }>("/models").then((res) => res.ok && setList(res.data.jobs));
  }, []);
  async function choose(job: string, model: string) {
    setSaving(job);
    setSaved("");
    const res = await call<{ jobs: Job[] }>("/models", { job, model });
    setSaving("");
    if (!res.ok) return setError({ job, message: res.error.message });
    setError(null);
    setList(res.data.jobs);
    setSaved(job);
  }
  return (
    <section aria-labelledby="models" className={box}>
      <h2 id="models" className={heading}>
        Models
      </h2>
      <p className="m-0 text-[13px] text-muted">
        Which of OpenAI&apos;s models does each of their jobs. Prices are per million tokens, paid to OpenAI on your key. A change starts your agents again on
        it, in a moment.
      </p>
      {!list && <span className="text-sm text-muted">Loading</span>}
      <ul className="m-0 flex list-none flex-col p-0">
        {list?.map((j) => {
          const about = jobs[j.job] ?? { title: j.job, note: "", env: "" };
          const fixed = j.from === "environment" || j.models.length < 2;
          const id = `model-${j.job}`;
          return (
            <li key={j.job} className="flex flex-wrap items-start gap-x-6 gap-y-2 border-t border-line py-3.5 first:border-t-0">
              <div className="flex min-w-0 flex-[1_1_260px] flex-col gap-0.5">
                <label htmlFor={id} className="text-sm font-semibold">
                  {about.title}
                </label>
                <span id={`${id}-note`} className="text-[13px] text-muted">
                  {about.note}
                  {j.from === "environment" && (
                    <>
                      {" "}
                      Set by <code>{about.env}</code> where Tinio runs: change it there.
                    </>
                  )}
                </span>
                {error?.job === j.job && (
                  <span role="alert" className="text-[13px] text-bad-text">
                    {error.message}
                  </span>
                )}
              </div>
              <div className="flex min-w-0 flex-[0_1_300px] flex-col gap-1">
                <select
                  id={id}
                  aria-describedby={`${id}-note`}
                  value={j.from === "default" ? "" : j.model}
                  disabled={fixed || saving === j.job}
                  onChange={(e) => choose(j.job, e.target.value)}
                  className="min-h-11 w-full rounded-lg border border-line-strong bg-surface px-3 text-[15px] disabled:opacity-70"
                >
                  {j.from === "environment" ? (
                    <option value={j.model}>{j.model}</option>
                  ) : (
                    <>
                      <option value="">{j.default ? `${j.default} (Tinio's choice)` : "Same as replies (Tinio's choice)"}</option>
                      {j.models.map((m) => (
                        <option key={m.model} value={m.model}>
                          {m.model} · {price(m)}
                        </option>
                      ))}
                    </>
                  )}
                </select>
                <span aria-live="polite" className="text-xs text-muted">
                  {saving === j.job ? "Saving" : saved === j.job ? "Saved. Your agents are starting again on it." : ""}
                </span>
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
