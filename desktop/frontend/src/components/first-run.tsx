import { Mark, Wordmark } from "@tinio/brand";
import Link from "next/link";
import { useEffect, useState } from "react";
import { useFrame } from "@/components/app-frame";
import { ServiceLogo } from "@/components/service-logo";
import { call } from "@/lib/api";

// Where your agents think is the only thing you bring: Claude Code signed in on this
// computer, on your own plan, or your OpenAI key. The key is kept sealed with Tinio's own
// key, made on this computer the first time it runs, and only ever shown masked; one set
// in the environment (OPENAI_API_KEY) is used instead.

export type Keys = { openai: { set: boolean; shown?: string; from?: "app" | "environment" } };

export type CLI = { id: "claude" | "opencode" | "codex"; name: string; installed: boolean; version: string; signed_in: boolean; account: string; plan: string; usable: boolean };
export type Sources = { source: "openai" | "claude"; key: boolean; clis: CLI[] };

// ready is whether your agents have something to think with: Claude Code signed in when
// they think with it, your key otherwise.
export function ready(s: Sources) {
  if (s.source === "claude") return s.clis.some((c) => c.id === "claude" && c.usable);
  return s.key;
}

// useSources is where your agents can think, and where they do; null until it's known.
export function useSources(): [Sources | null, () => void] {
  const [sources, setSources] = useState<Sources | null>(null);
  const [asked, setAsked] = useState(0);
  useEffect(() => {
    let wait = 500;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ask = () =>
      call<Sources>(asked ? "/sources?again=1" : "/sources").then((res) => {
        if (res.ok) return setSources(res.data);
        timer = setTimeout(ask, wait); // Tinio is still starting
        wait = Math.min(wait * 2, 5000);
      });
    ask();
    return () => clearTimeout(timer);
  }, [asked]);
  return [sources, () => setAsked((n) => n + 1)];
}

const plans: Record<string, string> = { max: "Max plan", pro: "Pro plan", team: "Team plan", enterprise: "Enterprise plan" };

// cliStatus is what's known of a CLI, in a line.
export function cliStatus(c: CLI) {
  if (!c.installed) return c.id === "codex" ? "Not on this computer. Install it and sign in with ChatGPT." : "Not on this computer.";
  const parts = [`Version ${c.version}`];
  if (c.id !== "claude") parts.push("Tinio can't think with it yet");
  else if (!c.signed_in) parts.push("not signed in: run claude in a terminal and sign in");
  else {
    if (c.account) parts.push(c.account);
    if (c.plan) parts.push(plans[c.plan] ?? c.plan);
  }
  return parts.join(" · ");
}

// useKeys is whether your key is set; null until it's known.
export function useKeys(): [Keys | null, () => void] {
  const [keys, setKeys] = useState<Keys | null>(null);
  const [asked, setAsked] = useState(0);
  useEffect(() => {
    let wait = 500;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ask = () =>
      call<Keys>("/keys").then((res) => {
        if (res.ok) return setKeys(res.data);
        timer = setTimeout(ask, wait); // Tinio is still starting
        wait = Math.min(wait * 2, 5000);
      });
    ask();
    return () => clearTimeout(timer);
  }, [asked]);
  return [keys, () => setAsked((n) => n + 1)];
}

// KeyForm shows your OpenAI key, masked, and sets or changes it.
export function KeyForm({ onSaved, first }: { onSaved?: () => void; first?: boolean }) {
  const [keys, reload] = useKeys();
  const [value, setValue] = useState("");
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  if (!keys) return <span className="text-sm text-muted">Loading</span>;
  const k = keys.openai;
  if (k.from === "environment")
    return (
      <p className="m-0 text-sm">
        <span className="font-mono">{k.shown}</span>, from <code>OPENAI_API_KEY</code> where Tinio runs. To change it, change it there.
      </p>
    );
  if (k.set && !editing)
    return (
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-sm">{k.shown}</span>
        <button type="button" onClick={() => setEditing(true)} className="min-h-10 rounded-lg border border-line-strong px-4 text-[13px] font-semibold hover:border-fg">
          Change
        </button>
      </div>
    );
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={async (e) => {
        e.preventDefault();
        setSaving(true);
        const res = await call("/keys", { openai: value });
        setSaving(false);
        if (!res.ok) return setError(res.error.message);
        setValue("");
        setError("");
        setEditing(false);
        reload();
        onSaved?.();
      }}
    >
      <label htmlFor="openai-key" className="text-sm font-semibold">
        OpenAI key
      </label>
      <div className="flex flex-wrap gap-2">
        <input
          id="openai-key"
          type="password"
          autoComplete="off"
          spellCheck={false}
          autoFocus={first}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="sk-..."
          aria-invalid={!!error}
          aria-describedby="openai-key-note"
          className="min-h-11 min-w-0 flex-[1_1_320px] rounded-lg border border-line-strong bg-surface px-3 font-mono text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent"
        />
        <button
          type="submit"
          disabled={saving || value.trim() === ""}
          className="min-h-11 rounded-lg bg-accent px-5 text-sm font-semibold text-accent-ink hover:bg-accent-hover disabled:opacity-50"
        >
          {saving ? "Saving" : first ? "Continue" : "Save"}
        </button>
        {editing && (
          <button type="button" onClick={() => setEditing(false)} className="min-h-11 rounded-lg px-4 text-sm font-semibold text-muted hover:text-fg">
            Cancel
          </button>
        )}
      </div>
      <span id="openai-key-note" className={`text-[13px] ${error ? "text-bad-text" : "text-muted"}`}>
        {error || "Kept sealed on this computer, and sent only to OpenAI. Your agents think with it, and you pay OpenAI for what they use."}
      </span>
    </form>
  );
}

// FirstRun is the first thing Tinio asks: where your agents think. Claude Code, when
// it's signed in on this computer, comes first: no key, on your own plan. Your OpenAI key
// otherwise. It can wait (skip): you can look around and raise an agent, but nobody
// replies until then.
export function FirstRun({ sources, onDone, onSkip, onAgain }: { sources: Sources; onDone: () => void; onSkip: () => void; onAgain: () => void }) {
  const claude = sources.clis.find((c) => c.id === "claude");
  const [pick, setPick] = useState<"claude" | "key">(claude?.usable ? "claude" : "key");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  async function useClaude() {
    setSaving(true);
    const res = await call<Sources>("/sources", { source: "claude" });
    setSaving(false);
    if (!res.ok) return setError(res.error.message);
    onDone();
  }
  const option = (id: "claude" | "key", logo: string, name: string, pill: string, status: string, off: boolean) => (
    <button
      type="button"
      aria-pressed={pick === id}
      disabled={off}
      onClick={() => setPick(id)}
      className="flex w-full items-start gap-3.5 rounded-xl border border-line bg-surface px-[18px] py-4 text-left hover:border-line-strong disabled:cursor-default disabled:bg-surface-2 aria-pressed:border-2 aria-pressed:border-accent aria-pressed:px-[17px] aria-pressed:py-[15px]"
    >
      <ServiceLogo service={logo} size={44} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <b className="text-base">{name}</b>
          <span className="rounded-md bg-surface-2 px-2 py-0.5 text-xs font-semibold text-muted">{pill}</span>
        </span>
        <span className="text-[13.5px] text-muted">{status}</span>
      </span>
    </button>
  );
  return (
    <div className="flex min-h-dvh flex-col bg-bg text-fg">
      <div className="h-8 flex-none [--wails-draggable:drag]" />
      <main className="mx-auto flex w-full max-w-[600px] flex-1 flex-col justify-center gap-6 px-6 pb-16">
        <div className="flex items-center gap-2.5 text-accent-text">
          <Mark size={32} title="" aria-hidden="true" />
          <Wordmark className="text-[24px]" />
        </div>
        <div className="flex flex-col gap-2">
          <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Where your agents think</h1>
          <p className="m-0 text-[15px] text-muted">
            Agents that work for you, on this computer. Everything they remember stays here. Pick what they think with: it does everything, from talking with
            you to your mornings and what they make.
          </p>
        </div>
        <div role="group" aria-label="Where your agents think" className="flex flex-col gap-2.5">
          {claude &&
            option(
              "claude",
              "claude",
              "Claude Code",
              claude.usable ? "Signed in" : claude.installed ? "Not signed in" : "Not on this computer",
              claude.usable ? `${cliStatus(claude)}. Sonnet for talking, Opus 5.5 for what they make, Haiku for quick jobs. On your plan.` : cliStatus(claude),
              !claude.usable,
            )}
          {option("key", "openai", "An OpenAI key", sources.key ? "Set" : "Pay per message", "A key from platform.openai.com. You pay OpenAI for what they use, with a weekly cap.", false)}
        </div>
        {claude && !claude.usable && (
          <button type="button" onClick={onAgain} className="self-start text-[13px] font-semibold text-accent-text">
            I've signed in to Claude Code: look again
          </button>
        )}
        {pick === "key" ? (
          <section className="flex flex-col gap-3 rounded-lg border border-line px-[22px] py-5">
            <KeyForm first onSaved={onDone} />
            <a href="https://platform.openai.com/api-keys" target="_blank" rel="noreferrer" className="self-start text-[13px] font-semibold text-accent-text">
              Get a key from OpenAI
            </a>
          </section>
        ) : (
          <div className="flex flex-col gap-2">
            <p className="m-0 rounded-lg bg-surface-2 px-4 py-3 text-[13.5px] text-muted">
              Tinio runs the Claude Code you installed, as you, on this computer. It never sees your sign-in, and it uses your plan's allowance, which your own
              work in Claude Code shares.
            </p>
            {error && (
              <span role="alert" className="text-[13px] text-bad-text">
                {error}
              </span>
            )}
          </div>
        )}
        <div className="flex flex-wrap items-center justify-between gap-3">
          {pick === "claude" ? (
            <button
              type="button"
              onClick={useClaude}
              disabled={saving}
              className="min-h-11 rounded-lg bg-accent px-5 text-sm font-semibold text-accent-ink hover:bg-accent-hover disabled:opacity-50"
            >
              {saving ? "Starting" : "Use Claude Code"}
            </button>
          ) : (
            <span />
          )}
          <button type="button" onClick={onSkip} className="min-h-11 rounded-lg px-4 text-sm font-semibold text-muted hover:bg-surface-2 hover:text-fg">
            Skip for now
          </button>
        </div>
      </main>
    </div>
  );
}

// skipped is whether you put the key off, kept in this window; never set, it's asked.
const skipKey = "key-skipped";
export function keySkipped() {
  try {
    return localStorage.getItem(skipKey) === "1";
  } catch {
    return false;
  }
}
export function skipKeyForNow() {
  try {
    localStorage.setItem(skipKey, "1");
  } catch {}
}

// NoKey says, where you'd wait for a reply, that nobody can reply until they have
// something to think with, and takes you to Settings for it. Shown only while they don't.
export function NoKey({ name, compact }: { name?: string; compact?: boolean }) {
  const { keySet } = useFrame();
  if (keySet !== false) return null;
  const who = name ?? "Your agents";
  const box = (
    <div role="status" className="flex flex-wrap items-center gap-3 rounded-lg border border-line bg-warn-soft px-4 py-3">
      <div className="flex min-w-0 flex-[1_1_240px] flex-col gap-0.5">
        <b className="text-sm font-semibold">{name ? `${name} can't reply yet` : "Nobody can reply yet"}</b>
        <span className="text-[13px] text-fg/80">
          Choose where {name ? `${name} thinks` : `${who.toLowerCase()} think`} in Settings: Claude Code or your OpenAI key
          {name ? `. Then ${name} will say hello.` : ". Then they'll say hello."}
        </span>
      </div>
      <Link
        href="/settings"
        className="inline-flex min-h-9 items-center rounded-lg bg-accent px-3.5 text-[13px] font-semibold text-accent-ink no-underline hover:bg-accent-hover"
      >
        Choose in Settings
      </Link>
    </div>
  );
  if (compact) return <div className="border-b border-line p-3">{box}</div>;
  return <div className="w-full max-w-[760px] px-10 pt-4 max-[900px]:px-3 max-[900px]:pt-2">{box}</div>;
}
