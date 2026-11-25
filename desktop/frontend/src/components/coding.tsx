import { useEffect, useState } from "react";
import { box, heading } from "@/components/kit";
import { ServiceLogo } from "@/components/service-logo";
import { call } from "@/lib/api";

// Settings › Coding jobs: how an agent's coding jobs run. The agent writes the brief and
// checks the work; Claude Code, on your plan, writes the code. A job follows the
// repository's own rules, never your own Claude Code setup, and asks you before anything
// its rules don't allow.

type CodingSettings = { coder: string; copy: boolean; review: boolean; minutes: number; max_steps: number };
type Coding = {
  settings: CodingSettings;
  claude: { installed: boolean; signed_in: boolean; usable: boolean };
  github: { installed: boolean; version: string; signed_in: boolean; account: string };
};

const coders = [
  { model: "claude:claude-opus-5-5", name: "Claude Code: Opus 5.5" },
  { model: "claude:claude-sonnet-5-5", name: "Claude Code: Sonnet 5.5" },
  { model: "claude:claude-haiku-5-5", name: "Claude Code: Haiku 5.5" },
];

export function CodingSection() {
  const [coding, setCoding] = useState<Coding | null>(null);
  const [error, setError] = useState<{ field: string; message: string } | null>(null);
  const [saved, setSaved] = useState("");
  useEffect(() => {
    call<Coding>("/coding").then((res) => res.ok && setCoding(res.data));
  }, []);
  async function change(field: keyof CodingSettings, value: string | boolean | number) {
    setSaved("");
    const res = await call<Coding>("/coding", { [field]: value });
    if (!res.ok) return setError({ field, message: res.error.message });
    setError(null);
    setCoding(res.data);
    setSaved(field);
  }
  if (!coding)
    return (
      <section aria-labelledby="coding" className={box}>
        <h2 id="coding" className={heading}>
          Coding jobs
        </h2>
        <span className="text-sm text-muted">Loading</span>
      </section>
    );
  const s = coding.settings;
  const gh = coding.github;
  const row = "flex flex-col gap-2 border-t border-line pt-3.5";
  const errorFor = (field: string) =>
    error?.field === field && (
      <span role="alert" className="text-[13px] text-bad-text">
        {error.message}
      </span>
    );
  const check = (field: "copy" | "review", title: string, note: string) => (
    <label className="flex cursor-pointer items-start gap-2.5 text-sm">
      <input type="checkbox" checked={s[field]} onChange={(e) => change(field, e.target.checked)} className="mt-0.5 size-[18px] flex-none accent-accent" />
      <span className="flex flex-col">
        <b className="font-semibold">{title}</b>
        <span className="text-[13px] text-muted">{note}</span>
      </span>
    </label>
  );
  const number = (field: "minutes" | "max_steps", label: string, unit?: string) => (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={`coding-${field}`} className="text-sm font-semibold">
        {label}
      </label>
      <span className="flex items-center gap-2">
        <input
          id={`coding-${field}`}
          type="number"
          defaultValue={s[field]}
          onBlur={(e) => Number(e.target.value) !== s[field] && change(field, Number(e.target.value))}
          className="min-h-11 w-24 rounded-lg border border-line-strong bg-surface px-3 text-[15px]"
        />
        {unit && <span className="text-sm text-muted">{unit}</span>}
      </span>
    </div>
  );
  return (
    <section aria-labelledby="coding" className={box}>
      <h2 id="coding" className={heading}>
        Coding jobs
      </h2>
      <p className="m-0 text-[13px] text-muted">
        An agent hands a coding job to Claude Code in one of their folders that&apos;s a git repository, answers what these rules allow, and brings you the
        rest. Ask them in the chat; they propose the job, with its brief, and it starts on your yes.
      </p>
      {!coding.claude.usable && (
        <p role="status" className="m-0 text-[13px] text-bad-text">
          Coding jobs need Claude Code signed in on this computer.
        </p>
      )}

      <div className="flex flex-wrap items-start gap-x-6 gap-y-2 border-t border-line pt-3.5">
        <div className="flex min-w-0 flex-[1_1_260px] flex-col">
          <label htmlFor="coding-coder" className="text-sm font-semibold">
            Who writes the code
          </label>
          <span className="text-[13px] text-muted">The agent writes the brief and checks the work; this does the typing, on your plan.</span>
          {errorFor("coder")}
        </div>
        <select
          id="coding-coder"
          value={s.coder}
          onChange={(e) => change("coder", e.target.value)}
          className="min-h-11 min-w-0 flex-[0_1_300px] rounded-lg border border-line-strong bg-surface px-3 text-[15px]"
        >
          {coders.map((c) => (
            <option key={c.model} value={c.model}>
              {c.name}
              {c.model === "claude:claude-opus-5-5" ? " (Tinio's choice)" : ""}
            </option>
          ))}
        </select>
      </div>

      <div className={row}>
        {check("copy", "Work on a copy, on its own branch", "A worktree beside the repository. Your checkout, and what you're in the middle of, are never touched.")}
        {check(
          "review",
          "The agent checks the work before you see it",
          "Runs the tests, reads the changes against the brief, and sends fixes back, up to 3 times. Uses some of your plan.",
        )}
      </div>

      <div className="flex flex-col gap-1 border-t border-line pt-3.5">
        <b className="text-sm font-semibold">Follows the repository&apos;s own rules</b>
        <span className="text-[13px] text-muted">
          Its <code>CLAUDE.md</code> and its <code>.claude</code> settings. Your own Claude Code setup (your CLAUDE.md, hooks, plugins, MCP servers) stays out
          of jobs, so nothing of yours answers what Tinio should ask you.
        </span>
      </div>

      <div className={row}>
        <b className="text-sm font-semibold">On its own, inside the copy</b>
        <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>Read anything, make and change files</li>
          <li>
            Run the repository&apos;s own tests, lint and build, from its <code>package.json</code>, <code>go.mod</code>, <code>Makefile</code>,{" "}
            <code>Cargo.toml</code> or <code>pyproject.toml</code>
          </li>
        </ul>
        <b className="pt-1 text-sm font-semibold">Always asks you</b>
        <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>Installing anything, or reaching the internet</li>
          <li>Anything outside the copy, and any other command</li>
          <li>Committing, and again before pushing or opening a pull request</li>
        </ul>
      </div>

      <div className="flex flex-wrap items-end gap-x-6 gap-y-3 border-t border-line pt-3.5">
        {number("minutes", "Longest a job runs", "minutes")}
        {number("max_steps", "Steps a job may take")}
        <span className="flex-[1_1_220px] pb-2.5 text-[13px] text-muted">Past either, the job stops and keeps what it did. You can carry it on.</span>
      </div>
      {errorFor("minutes")}
      {errorFor("max_steps")}

      <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2 border-t border-line pt-3.5">
        <ServiceLogo service="github" size={40} />
        <div className="flex min-w-0 flex-[1_1_300px] flex-col">
          <span className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            <b className="text-sm font-semibold">GitHub, through your gh</b>
            <span className="rounded-md bg-surface-2 px-2 py-0.5 text-xs font-semibold text-muted">
              {!gh.installed ? "Not found" : gh.signed_in ? `Signed in${gh.account ? ` as ${gh.account}` : ""}` : "Not signed in"}
            </span>
          </span>
          <span className="text-[13px] text-muted">
            {!gh.installed
              ? "Install GitHub's gh and sign in, and jobs can push and open pull requests, each time with your yes."
              : gh.signed_in
                ? "Pushing and pull requests go through the gh on this computer, each time with your yes. Tinio holds no GitHub token."
                : "Run gh auth login in a terminal, and jobs can push and open pull requests, each time with your yes."}
          </span>
        </div>
      </div>
      <span aria-live="polite" className="text-xs text-muted">
        {saved ? "Saved." : ""}
      </span>
    </section>
  );
}
