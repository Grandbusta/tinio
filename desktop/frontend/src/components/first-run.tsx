import { Mark, Wordmark } from "@tinio/brand";
import { useEffect, useState } from "react";
import { call } from "@/lib/api";

// Your keys are the only thing you bring: Tinio's own key, which seals them, is made on
// this computer the first time it runs. Your OpenAI key is kept sealed and only ever
// shown masked; one set in the environment (OPENAI_API_KEY) is used instead.

export type Keys = { openai: { set: boolean; shown?: string; from?: "app" | "environment" } };

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
        <button type="button" onClick={() => setEditing(true)} className="min-h-10 rounded-full border border-line-strong px-4 text-[13px] font-semibold hover:border-fg">
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
          className="min-h-11 rounded-full bg-accent px-5 text-sm font-semibold text-accent-ink hover:bg-accent-hover disabled:opacity-50"
        >
          {saving ? "Saving" : first ? "Continue" : "Save"}
        </button>
        {editing && (
          <button type="button" onClick={() => setEditing(false)} className="min-h-11 rounded-full px-4 text-sm font-semibold text-muted hover:text-fg">
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

// FirstRun is the first thing Tinio asks: your OpenAI key. Then you raise an agent.
export function FirstRun({ onDone }: { onDone: () => void }) {
  return (
    <div className="flex min-h-dvh flex-col bg-bg text-fg">
      <div className="h-8 flex-none [--wails-draggable:drag]" />
      <main className="mx-auto flex w-full max-w-[560px] flex-1 flex-col justify-center gap-6 px-6 pb-16">
        <div className="flex items-center gap-2.5 text-accent-text">
          <Mark size={32} title="" aria-hidden="true" />
          <Wordmark className="text-[24px]" />
        </div>
        <div className="flex flex-col gap-2">
          <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Welcome to Tinio</h1>
          <p className="m-0 text-[15px] text-muted">
            Agents with lives of their own, running on this computer. Everything they remember stays here. The one thing you bring is a key for the models they
            think with.
          </p>
        </div>
        <section className="flex flex-col gap-3 rounded-lg border border-line px-[22px] py-5">
          <KeyForm first onSaved={onDone} />
          <a href="https://platform.openai.com/api-keys" target="_blank" rel="noreferrer" className="self-start text-[13px] font-semibold text-accent-text">
            Get a key from OpenAI
          </a>
        </section>
        <p className="m-0 text-[13px] text-muted">A week costs at most $5 to start with. You can change that on You.</p>
      </main>
    </div>
  );
}
