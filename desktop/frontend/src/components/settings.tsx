import Link from "next/link";
import { useEffect, useState } from "react";
import { useFrame } from "@/components/app-frame";
import { CodingSection } from "@/components/coding";
import { FoldersSection } from "@/components/folders";
import { cliStatus, KeyForm, ready, type Sources, useSources } from "@/components/first-run";
import { ServiceLogo } from "@/components/service-logo";
import { box, heading } from "@/components/kit";
import { inTheApp, ThisApp } from "@/components/this-app";
import { Appearance } from "@/components/usage";
import { call } from "@/lib/api";

// Settings: where your agents think (Claude Code or your OpenAI key), the model for each
// of their jobs, the folders on this computer they may use, the app itself, and how it
// looks. Every OpenAI model listed is one Tinio knows the price of, so the week's cap
// holds; Claude Code reports what each call would cost, and counts the same way.

type ModelOption = { model: string; name: string; source: "openai" | "claude"; input: number; output: number };
type Job = { job: string; model: string; from: "app" | "environment" | "default"; default: string; models: ModelOption[] };

const jobs: Record<string, { title: string; note: string; env: string }> = {
  reply: { title: "Replies", note: "Everything they say to you. Most of what a week costs.", env: "OPENAI_MODEL" },
  decision: { title: "Decisions", note: "The quick calls before a reply. A small model keeps replies quick.", env: "OPENAI_DECISION_MODEL" },
  day: {
    title: "Your mornings",
    note: "Working out your morning, what they watch for you, and their weekly look back. It runs every day, so a small model keeps it light.",
    env: "OPENAI_DAY_MODEL",
  },
  light: { title: "Light mode", note: "Replies once the week's cap is reached.", env: "OPENAI_LIGHT_MODEL" },
  work: { title: "Works", note: "What they make: writing, documents, music.", env: "OPENAI_WORK_MODEL" },
};

// nameOf is a model's name as shown: "Sonnet 5.5" for "claude:claude-sonnet-5-5".
const nameOf = (j: Job, model: string) => {
  const m = j.models.find((o) => o.model === model);
  return m ? (m.source === "claude" ? `Claude Code: ${m.name}` : m.name) : model;
};

const price = (m: ModelOption) => (m.source === "claude" ? "on your plan" : `$${m.input} in, $${m.output} out`);

export function SettingsPage() {
  const { refresh } = useFrame();
  const [sources, reload] = useSources();
  const changed = () => {
    reload();
    refresh();
  };
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

      <ThinkSection sources={sources} onChange={changed} />

      <ModelsSection sources={sources} onChange={refresh} />

      <CodingSection />

      <FoldersSection />

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

// ThinkSection is where they can think: Claude Code if it's on this computer, and your
// OpenAI key. Using one starts every job on its default there.
function ThinkSection({ sources, onChange }: { sources: Sources | null; onChange: () => void }) {
  const [error, setError] = useState("");
  const [switching, setSwitching] = useState("");
  async function use(source: "claude" | "openai") {
    setSwitching(source);
    const res = await call<Sources>("/sources", { source });
    setSwitching("");
    if (!res.ok) return setError(res.error.message);
    setError("");
    onChange();
  }
  const row = (logo: string, name: string, status: string, end: React.ReactNode, below?: React.ReactNode) => (
    <li key={logo} className="flex flex-col gap-3 border-t border-line py-3.5 first:border-t-0">
      <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2">
        <ServiceLogo service={logo} size={40} />
        <div className="flex min-w-0 flex-[1_1_260px] flex-col">
          <b className="text-[15px]">{name}</b>
          <span className="text-[13px] text-muted">{status}</span>
        </div>
        {end}
      </div>
      {below}
    </li>
  );
  const inUse = <span className="px-2 text-[13px] font-semibold text-accent-text">In use</span>;
  const useButton = (source: "claude" | "openai", label: string) => (
    <button
      type="button"
      disabled={!!switching}
      onClick={() => use(source)}
      className="min-h-10 rounded-full border border-line-strong px-4 text-[13px] font-semibold hover:border-fg disabled:opacity-50"
    >
      {switching === source ? "Starting" : label}
    </button>
  );
  return (
    <section aria-labelledby="think" className={box}>
      <h2 id="think" className={heading}>
        Where they think
      </h2>
      <p className="m-0 text-[13px] text-muted">
        What Tinio found on this computer, and your key. Tinio asks each CLI whether it's signed in, and never reads its sign-in. Using one moves every job
        below to its default there.
      </p>
      {!sources && <span className="text-sm text-muted">Loading</span>}
      {sources && (
        <ul className="m-0 flex list-none flex-col p-0">
          {sources.clis.map((c) =>
            row(
              c.id,
              c.name,
              cliStatus(c),
              c.id === "claude" && c.usable ? (sources.source === "claude" ? inUse : useButton("claude", "Use Claude Code")) : null,
            ),
          )}
          {row(
            "openai",
            "OpenAI key",
            sources.key ? "Set: pay OpenAI per message, with a weekly cap" : "Not set: pay OpenAI per message, with a weekly cap",
            sources.source === "openai" ? (sources.key ? inUse : null) : sources.key ? useButton("openai", "Use your key") : null,
            <div className="pl-[54px] max-[600px]:pl-0">
              <KeyForm onSaved={onChange} />
            </div>,
          )}
        </ul>
      )}
      {sources && !ready(sources) && (
        <p role="status" className="m-0 text-[13px] text-bad-text">
          {sources.source === "claude" ? "Claude Code isn't signed in, so nobody can reply." : "No key yet, so nobody can reply."}
        </p>
      )}
      {error && (
        <span role="alert" className="text-[13px] text-bad-text">
          {error}
        </span>
      )}
    </section>
  );
}

function ModelsSection({ sources, onChange }: { sources: Sources | null; onChange: () => void }) {
  const [list, setList] = useState<Job[] | null>(null);
  const [error, setError] = useState<{ job: string; message: string } | null>(null);
  const [saving, setSaving] = useState("");
  const [saved, setSaved] = useState("");
  useEffect(() => {
    call<{ jobs: Job[] }>("/models").then((res) => res.ok && setList(res.data.jobs));
  }, [sources?.source]);
  const claudeReady = !!sources?.clis.some((c) => c.id === "claude" && c.usable);
  async function choose(job: string, model: string) {
    setSaving(job);
    setSaved("");
    const res = await call<{ jobs: Job[] }>("/models", { job, model });
    setSaving("");
    if (!res.ok) return setError({ job, message: res.error.message });
    setError(null);
    setList(res.data.jobs);
    setSaved(job);
    onChange();
  }
  return (
    <section aria-labelledby="models" className={box}>
      <h2 id="models" className={heading}>
        Models
      </h2>
      <p className="m-0 text-[13px] text-muted">
        Which model does each of their jobs, from Claude Code or your key. Mixing is fine. OpenAI&apos;s prices are per million tokens; Claude Code&apos;s are on
        your plan. A change starts your agents again on it, in a moment.
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
                      <option value="">{j.default ? `${nameOf(j, j.default)} (Tinio's choice)` : "Same as replies (Tinio's choice)"}</option>
                      {(["claude", "openai"] as const).map((src) => (
                        <optgroup key={src} label={src === "claude" ? (claudeReady ? "Claude Code" : "Claude Code (sign in to use)") : sources?.key ? "OpenAI" : "OpenAI (add a key to use)"}>
                          {j.models
                            .filter((m) => m.source === src)
                            .map((m) => (
                              <option key={m.model} value={m.model} disabled={src === "claude" ? !claudeReady : !sources?.key}>
                                {m.name} · {price(m)}
                              </option>
                            ))}
                        </optgroup>
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
