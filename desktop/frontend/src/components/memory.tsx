"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type Fact } from "@/lib/api";

// Step 14: what an agent remembers, and where each thing came from. Before they answer they
// recall only what fits; Mind shows that. Put right anything they have wrong (from their
// next reply on), or have them forget it for good.

const week = 7 * 24 * 60 * 60 * 1000;

function origin(f: Fact) {
  switch (f.from) {
    case "message":
      return f.about === "you" ? "You told them" : "They decided it, talking with you";
    case "reminder":
    case "check_in":
    case "delivery":
      return "Talking with you";
    case "observation":
      return "From what they watch";
    case "action":
      return "From a tool they used";
    default:
      return "From their work for you";
  }
}

function Row({ f, onCorrect, onForget }: { f: Fact; onCorrect: (f: Fact, text: string) => Promise<boolean>; onForget: (f: Fact) => Promise<void> }) {
  const [mode, setMode] = useState<"read" | "edit" | "forget">("read");
  const [draft, setDraft] = useState(f.text);
  const [busy, setBusy] = useState(false);
  const link = "min-h-9 text-[13px] font-semibold text-accent-text hover:underline";
  const button = "inline-flex min-h-9 items-center rounded-full px-3.5 text-[13px] font-semibold disabled:opacity-60";
  return (
    <li className={`flex flex-col gap-2 rounded-lg border px-4 py-3 ${mode === "edit" ? "border-accent bg-accent-soft" : "border-line"}`}>
      {mode === "edit" ? (
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            if (await onCorrect(f, draft)) setMode("read");
            setBusy(false);
          }}
          className="flex flex-col gap-2"
        >
          <label htmlFor={`fix-${f.id}`} className="text-[13px] text-muted">Put it the way it is</label>
          <input
            id={`fix-${f.id}`}
            value={draft}
            maxLength={300}
            autoFocus
            onChange={(e) => setDraft(e.target.value)}
            className="min-h-10 rounded-lg border border-line-strong bg-surface px-3 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent"
          />
          <div className="flex flex-wrap items-center gap-2">
            <button type="submit" disabled={busy || !draft.trim()} className={`${button} bg-accent text-accent-ink hover:bg-accent-hover`}>
              Save
            </button>
            <button type="button" onClick={() => setMode("read")} className={`${button} border border-line-strong hover:border-fg`}>
              Cancel
            </button>
            <span className="text-xs text-muted">They&apos;ll use your words from their next reply.</span>
          </div>
        </form>
      ) : (
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
            <span className="text-[15px]">{f.text}</span>
            <span className="text-xs text-muted">
              {origin(f)} · {when(f.at)}
              {f.areas.length > 0 && f.areas[0] !== "other" ? ` · ${f.areas.join(", ")}` : ""}
            </span>
          </div>
          {mode === "forget" ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[13px] text-muted">Forget it for good?</span>
              <button
                type="button"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  await onForget(f);
                  setBusy(false);
                }}
                className={`${button} bg-fg text-bg`}
              >
                Forget
              </button>
              <button type="button" onClick={() => setMode("read")} className={`${button} border border-line-strong hover:border-fg`}>
                Keep
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-4">
              <button type="button" onClick={() => (setDraft(f.text), setMode("edit"))} className={link}>
                Correct
              </button>
              <button type="button" onClick={() => setMode("forget")} className={`${link} text-muted`}>
                Forget
              </button>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

export function MemoryPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [facts, setFacts] = useState<Fact[] | null>(null);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<Fact["about"]>("you");
  const [q, setQ] = useState("");

  useEffect(() => {
    if (!account) return;
    call<{ facts: Fact[] }>(`/agents/${id}/memory`).then((res) => (res.ok ? setFacts(res.data.facts) : setError(res.error.message)));
  }, [account, id]);

  async function correct(f: Fact, text: string) {
    setError("");
    const res = await call<{ text: string }>(`/agents/${id}/facts/${f.id}`, { text });
    if (!res.ok) {
      setError(res.error.message);
      return false;
    }
    setFacts((all) => all && all.map((x) => (x.id === f.id ? { ...x, text: res.data.text, at: new Date().toISOString() } : x)));
    return true;
  }

  async function forget(f: Fact) {
    setError("");
    const res = await call(`/agents/${id}/facts/${f.id}/forget`, {});
    if (!res.ok) return setError(res.error.message);
    setFacts((all) => all && all.filter((x) => x.id !== f.id));
  }

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = agent?.name ?? "They";
  const tabs: { key: Fact["about"]; label: string; note: string }[] = [
    { key: "you", label: "About you", note: "From your chats" },
    { key: "self", label: `${agent ? `${agent.name}'s` : "Their"} character`, note: "Their background, and what they've said of themselves" },
  ];
  const needle = q.trim().toLowerCase();
  const shown = (facts ?? []).filter((f) => f.about === tab && (!needle || f.text.toLowerCase().includes(needle)));
  const recent = (facts ?? []).filter((f) => Date.now() - new Date(f.at).getTime() < week).slice(0, 5);
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Memory</h1>
        <p className="m-0 max-w-[66ch] text-muted">
          What {name} remembers, and where each thing came from. Before they answer they recall only what fits; you can watch that in{" "}
          <Link href={`/agents/${id}/mind`}>Mind</Link>. Put right anything they have wrong, or have them forget it.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      <div className="flex flex-wrap items-start gap-6">
        <div className="flex min-w-0 flex-[3_1_540px] flex-col gap-4">
          <div role="tablist" aria-label={`What ${name} remembers`} className="flex gap-6 border-b border-line">
            {tabs.map((t) => (
              <button
                key={t.key}
                type="button"
                role="tab"
                aria-selected={tab === t.key}
                onClick={() => (setTab(t.key), setQ(""))}
                className={`-mb-px min-h-11 border-b-2 px-0.5 text-[14.5px] ${tab === t.key ? "border-accent font-semibold text-fg" : "border-transparent font-medium text-muted hover:text-fg"}`}
              >
                {t.label} <span className="text-[12.5px] font-medium text-muted">{(facts ?? []).filter((f) => f.about === t.key).length}</span>
              </button>
            ))}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2.5">
            <label htmlFor="q" className="sr-only">Search what {name} remembers</label>
            <input
              id="q"
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search"
              className="min-h-10 max-w-[420px] min-w-0 flex-[1_1_280px] rounded-lg border border-line-strong bg-surface px-3 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent"
            />
            <span className="text-[13px] text-muted">{tabs.find((t) => t.key === tab)?.note}</span>
          </div>
          {facts && shown.length === 0 && (
            <p className="m-0 text-sm text-muted">{needle ? `Nothing ${name} remembers matches "${q}".` : "Nothing yet."}</p>
          )}
          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {shown.map((f) => (
              <Row key={f.id} f={f} onCorrect={correct} onForget={forget} />
            ))}
          </ul>
        </div>
        <aside className="flex max-w-[320px] min-w-0 flex-[1_1_260px] flex-col gap-3 max-[900px]:max-w-none">
          <div className="flex flex-col gap-2.5 rounded-lg border border-line px-[18px] py-4">
            <h2 className="font-display text-base font-semibold tracking-[-0.02em]">Learnt this week</h2>
            {recent.length === 0 && <span className="text-[13px] text-muted">Nothing new this week.</span>}
            {recent.map((f) => (
              <div key={f.id} className="flex flex-col gap-px border-b border-surface-2 pb-2 last:border-b-0 last:pb-0">
                <span className="text-[13.5px]">{f.text}</span>
                <span className="text-[11.5px] text-muted">
                  {f.about === "you" ? "About you" : "Their character"} · {when(f.at)}
                </span>
              </div>
            ))}
          </div>
          <div className="flex flex-col gap-2 rounded-lg bg-accent-soft px-[18px] py-4 text-[13px]">
            <h2 className="font-display text-base font-semibold tracking-[-0.02em]">What stays private</h2>
            <span>What {name} knows about you is used only in your talks with them, and never shown to anyone else.</span>
            <span>Keys and passwords you give them are sealed, and never shown here or anywhere else.</span>
          </div>
        </aside>
      </div>
    </div>
  );
}
