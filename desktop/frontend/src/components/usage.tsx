"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useFrame } from "@/components/app-frame";
import { useTheme, type Theme } from "@/components/theme";
import { call, type Usage } from "@/lib/api";

// The week your agents share, on your own key: what it may cost (the cap you set),
// how much is used and by whom, and what happens as it fills.

export function resets(at: string, short = false) {
  return new Date(at).toLocaleString("en-GB", { weekday: short ? "short" : "long", hour: "2-digit", minute: "2-digit", hour12: false });
}

function names(u: Usage) {
  const ns = u.agents.map((a) => a.name);
  return ns.length <= 1 ? (ns[0] ?? "Your agent") : `${ns.slice(0, -1).join(", ")} and ${ns[ns.length - 1]}`;
}

const dollars = (n: number) => `$${n.toLocaleString("en-GB", { minimumFractionDigits: n % 1 ? 2 : 0, maximumFractionDigits: 2 })}`;

// The meter's one line under your agents' list.
export function MeterLine() {
  const { usage } = useFrame();
  if (!usage) return null;
  const text =
    usage.state === "light"
      ? `Light mode until ${resets(usage.resets_at, true)}`
      : usage.state === "resting"
        ? `Resting until ${resets(usage.resets_at, true)}`
        : usage.cap_usd > 0
          ? `${Math.round(usage.used)}% of this week's cap`
          : `${dollars(usage.spent_usd)} this week`;
  return (
    <Link href="/you" className="flex flex-col gap-1.5 border-t border-line px-4 py-3 text-fg no-underline hover:bg-surface-2">
      <span className="flex justify-between gap-2 text-[13px]">
        <span>{text}</span>
        <span className="font-semibold text-accent-text">Usage</span>
      </span>
      {usage.cap_usd > 0 && <Bar usage={usage} thin />}
    </Link>
  );
}

function Bar({ usage, thin }: { usage: Usage; thin?: boolean }) {
  const pct = usage.state === "normal" ? Math.min(usage.used, 100) : 100;
  const fill = usage.state === "resting" ? "bg-muted" : usage.state === "normal" && pct >= 90 ? "bg-warn-text" : "bg-accent";
  return (
    <span
      role="meter"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      aria-label={`${Math.round(usage.used)}% of this week used`}
      className={`relative block overflow-hidden rounded-full bg-surface-2 ${thin ? "h-1.5" : "h-3"}`}
    >
      <span className={`absolute inset-y-0 left-0 ${fill}`} style={{ width: `${pct}%` }} />
    </span>
  );
}

type Level = "warn75" | "warn90" | "light" | "resting";

function levelOf(u: Usage): Level | null {
  if (u.state === "resting") return "resting";
  if (u.state === "light") return "light";
  if (u.used >= 90) return "warn90";
  if (u.used >= 75) return "warn75";
  return null;
}

// UsageBanner is Tinio's note at the top of a chat as the week fills; the warnings can be
// put away until the next level or week.
export function UsageBanner() {
  const { usage } = useFrame();
  const level = usage && levelOf(usage);
  const key = level && usage ? `usage-banner:${level}:${usage.resets_at}` : "";
  const [hidden, setHidden] = useState(true);
  useEffect(() => {
    try {
      setHidden(!!key && localStorage.getItem(key) === "1");
    } catch {
      setHidden(false);
    }
  }, [key]);
  if (!usage || !level || hidden) return null;
  const at = resets(usage.resets_at);
  const who = names(usage);
  const many = usage.agents.length > 1;
  const b = {
    warn75: { box: "bg-accent-soft", title: "75% of this week's cap used", body: `Your week resets ${at}.` },
    warn90: {
      box: "bg-warn-soft",
      title: "90% of this week's cap used",
      body: `At 100%, ${who} ${many ? "reply" : "replies"} more simply and mornings come without drafts until ${at}.`,
    },
    light: {
      box: "bg-warn-soft",
      title: `Light mode until ${at}`,
      body: `You've reached this week's cap. ${who} still ${many ? "reply" : "replies"}, more simply, and mornings come without drafts until the reset. Asks, calls, reminders and promises carry on as usual.`,
    },
    resting: {
      box: "bg-surface-2",
      title: `Resting until ${at}`,
      body: `This week went well past its cap, so ${who} ${many ? "are" : "is"} resting until it resets. Asks and calls waiting for you, and reminders and promises already made, still go through.`,
    },
  }[level];
  const dismissible = level === "warn75" || level === "warn90";
  return (
    <div className="w-full max-w-[760px] px-10 pt-4 max-[900px]:px-3 max-[900px]:pt-2">
      <div role="status" className={`flex flex-wrap items-start gap-3 rounded-lg border border-line px-4 py-3 ${b.box}`}>
        <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
          <b className="text-sm font-semibold">{b.title}</b>
          <span className="text-[13px] text-fg/80">{b.body}</span>
        </div>
        <div className="flex items-center gap-2">
          <Link href="/you" className="inline-flex min-h-9 items-center rounded-lg border border-line-strong bg-bg px-3.5 text-[13px] font-semibold text-fg no-underline hover:border-fg">
            {level === "light" || level === "resting" ? "Change the cap" : "See usage"}
          </Link>
          {dismissible && (
            <button
              type="button"
              aria-label="Put this away"
              onClick={() => {
                setHidden(true);
                try {
                  localStorage.setItem(key, "1");
                } catch {}
              }}
              className="grid size-9 place-items-center rounded-full text-muted hover:bg-bg hover:text-fg"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                <path d="M6 6l12 12" />
                <path d="M18 6L6 18" />
              </svg>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

// Appearance is light or dark in this browser, or the device's choice.
export function Appearance() {
  const { theme, choose } = useTheme();
  const options: [Theme, string][] = [
    ["system", "Like your device"],
    ["light", "Light"],
    ["dark", "Dark"],
  ];
  return (
    <div className="flex flex-col gap-2">
      <span id="appearance" className="text-sm font-semibold">Appearance</span>
      <div role="group" aria-labelledby="appearance" className="flex flex-wrap gap-1 self-start rounded-xl border border-line bg-surface-2 p-1">
        {options.map(([t, label]) => (
          <button
            key={t}
            type="button"
            aria-pressed={theme === t}
            onClick={() => choose(t)}
            className={`min-h-9 rounded-lg px-3.5 text-[13px] font-semibold ${theme === t ? "bg-bg text-accent-text shadow-sm" : "text-muted hover:text-fg"}`}
          >
            {label}
          </button>
        ))}
      </div>
    </div>
  );
}

// YouPage is you: the week your agents share and its cap, and what used it. Your
// keys and models are in Settings.
export function YouPage() {
  const { usage, refresh } = useFrame();
  const steps = usage && [
    { at: "0 to 75%", title: "As usual", text: "Full replies, and mornings with drafts made before you wake." },
    { at: "75%, 90%", title: "A note from Tinio", text: "At the top of your chats." },
    { at: "100%", title: "Light mode", text: "They still reply, more simply, and mornings come without drafts until the reset." },
    { at: "About 150%", title: "Resting", text: "They rest until the reset. What you send is read then." },
    { at: "Always", title: "Never cut", text: "Asks and calls waiting for your answer, and reminders and promises already made." },
  ];
  const now = !usage ? -1 : usage.state === "resting" ? 3 : usage.state === "light" ? 2 : usage.used >= 75 ? 1 : 0;
  const colours = ["bg-accent", "bg-accent/55", "bg-ink-400", "bg-ink-300", "bg-slate-400"];
  return (
    <div className="mx-auto flex w-full max-w-[880px] flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-4">
      <div className="flex flex-col gap-1">
        <Link href="/agents" className="mb-2 inline-flex min-h-11 items-center gap-1.5 self-start text-sm font-semibold text-accent-text min-[901px]:hidden">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M15 6l-6 6 6 6" />
          </svg>
          Chats
        </Link>
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">You</h1>
        <span className="text-muted">
          This week&apos;s spending on your key. Your key and models are in{" "}
          <Link href="/settings" className="font-semibold text-accent-text">
            Settings
          </Link>
          .
        </span>
      </div>

      {usage && steps && (
        <>
          <section aria-labelledby="week" className="flex flex-col gap-3.5 rounded-lg border border-line px-[22px] py-5">
            <div className="flex items-center justify-between gap-3">
              <h2 id="week" className="font-display text-lg font-semibold tracking-[-0.02em]">This week</h2>
              <span
                className={`rounded-md px-2.5 py-1 text-xs font-semibold ${usage.state === "normal" ? "bg-accent-soft text-accent-text" : usage.state === "light" ? "bg-warn-soft text-warn-text" : "bg-surface-2 text-fg"}`}
              >
                {usage.state === "normal" ? "As usual" : usage.state === "light" ? "Light mode" : "Resting"}
              </span>
            </div>
            <div className="flex items-baseline gap-2">
              <b className="font-display text-[40px] leading-none font-semibold">{dollars(usage.spent_usd)}</b>
              <span className="text-muted">{usage.cap_usd > 0 ? `of ${dollars(usage.cap_usd)} this week` : "this week, with no cap"}</span>
            </div>
            {usage.cap_usd > 0 && <Bar usage={usage} />}
            <div className="flex flex-wrap justify-between gap-2 text-[13px] text-muted">
              <span>Resets {resets(usage.resets_at)}</span>
              {usage.cap_usd > 0 && (
                <span>
                  {usage.state === "light"
                    ? "Simpler replies until the reset"
                    : usage.state === "resting"
                      ? "Back when the week resets"
                      : `About ${usage.messages_left.toLocaleString("en-GB")} messages left at your usual rate`}
                </span>
              )}
            </div>
            <CapForm usage={usage} onSaved={refresh} />
          </section>

          {usage.agents.length > 0 && usage.cap_usd > 0 && (
            <section aria-labelledby="used" className="flex flex-col gap-3.5 rounded-lg border border-line px-[22px] py-5">
              <h2 id="used" className="font-display text-lg font-semibold tracking-[-0.02em]">What used this week</h2>
              <span aria-hidden="true" className="flex h-3 overflow-hidden rounded-full bg-surface-2">
                {usage.agents.map((a, i) => (
                  <span key={a.id} className={colours[i % colours.length]} style={{ width: `${Math.min(a.chat + a.work, 100)}%` }} />
                ))}
              </span>
              <ul className="m-0 flex list-none flex-col p-0">
                {usage.agents.map((a, i) => (
                  <li key={a.id} className="flex items-center gap-3 border-t border-line py-2.5 first:border-t-0">
                    <span aria-hidden="true" className={`size-2.5 flex-none rounded-[3px] ${colours[i % colours.length]}`} />
                    <span className="flex min-w-0 flex-auto flex-col">
                      <b className="text-sm font-semibold">{a.name}</b>
                      <span className="text-xs text-muted">
                        Chat {Math.round(a.chat)}% · mornings and drafts {Math.round(a.work)}%
                      </span>
                    </span>
                    <b className="text-sm font-semibold">{Math.round(a.chat + a.work)}%</b>
                  </li>
                ))}
              </ul>
              {usage.agents.length > 1 && <span className="text-xs text-muted">Your agents share the week.</span>}
            </section>
          )}

          {usage.cap_usd > 0 && (
            <section aria-labelledby="fills" className="flex flex-col gap-1 rounded-lg border border-line px-[22px] py-5">
              <h2 id="fills" className="mb-2 font-display text-lg font-semibold tracking-[-0.02em]">As the week fills</h2>
              <ol className="m-0 flex list-none flex-col p-0">
                {steps.map((s, i) => (
                  <li key={s.title} className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-line py-3 first:border-t-0">
                    <span className={`w-24 flex-none font-display text-sm font-bold ${i === now ? "text-accent-text" : "text-muted"}`}>{s.at}</span>
                    <span className="flex min-w-0 flex-[1_1_240px] flex-col">
                      <b className="text-sm font-semibold">{s.title}</b>
                      <span className="text-[13px] text-muted">{s.text}</span>
                    </span>
                    {i === now && <span className="rounded-md bg-accent-soft px-2.5 py-0.5 text-xs font-semibold text-accent-text">Now</span>}
                  </li>
                ))}
              </ol>
            </section>
          )}
        </>
      )}
    </div>
  );
}

// CapForm sets what a week may cost on your key: dollars, or 0 for no limit.
function CapForm({ usage, onSaved }: { usage: Usage; onSaved: () => void }) {
  const [cap, setCap] = useState(String(usage.cap_usd));
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  useEffect(() => setCap(String(usage.cap_usd)), [usage.cap_usd]);
  const changed = Number(cap) !== usage.cap_usd;
  return (
    <form
      className="flex flex-col gap-2 border-t border-line pt-3.5"
      onSubmit={async (e) => {
        e.preventDefault();
        setSaving(true);
        const res = await call<Usage>("/usage", { cap_usd: Number(cap) });
        setSaving(false);
        if (!res.ok) return setError(res.error.message);
        setError("");
        onSaved();
      }}
    >
      <label htmlFor="cap" className="text-sm font-semibold">A week may cost up to</label>
      <div className="flex flex-wrap items-center gap-2">
        <span className="flex min-h-11 items-center rounded-lg border border-line-strong bg-surface pl-3 focus-within:outline-2 focus-within:outline-offset-1 focus-within:outline-accent">
          <span aria-hidden="true" className="text-muted">$</span>
          <input
            id="cap"
            inputMode="decimal"
            value={cap}
            onChange={(e) => setCap(e.target.value.replace(/[^0-9.]/g, ""))}
            aria-invalid={!!error}
            aria-describedby="cap-note"
            className="min-h-11 w-28 bg-transparent px-1.5 text-[15px] outline-none"
          />
        </span>
        <button
          type="submit"
          disabled={!changed || saving || cap === ""}
          className="min-h-11 rounded-lg bg-accent px-5 text-sm font-semibold text-accent-ink hover:bg-accent-hover disabled:opacity-50"
        >
          {saving ? "Saving" : "Save"}
        </button>
      </div>
      <span id="cap-note" className={`text-[13px] ${error ? "text-bad-text" : "text-muted"}`}>
        {error || "Paid to OpenAI on your key. 0 means no limit. A day of chatting and a morning is often a few cents."}
      </span>
    </form>
  );
}
