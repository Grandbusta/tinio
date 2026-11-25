"use client";

import { useEffect, useState } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type ReadAnswer as Answer, type Chain, type Decision, type Mind } from "@/lib/api";

// Step 13: how an agent thinks. Every decision is kept: what System 1 asked itself and how
// sure it was, what System 2 wrote, how long each took and what it cost. Open one to see
// its whole chain, and say whether a read was right (labels score System 1 in replays; they
// change nothing the agent does).

const tierName: Record<Decision["tier"], string> = { system1: "System 1 · fast", system2: "System 2 · slow", reflex: "Reflex · no model" };
const tierRing: Record<Decision["tier"], string> = {
  system1: "shadow-[inset_3px_0_0_#2fb8d4]",
  system2: "shadow-[inset_3px_0_0_#9466f0]",
  reflex: "shadow-[inset_3px_0_0_#8a94a6]",
};

function about(d: Decision) {
  switch (d.kind) {
    case "message":
      return "Your message";
    case "hello":
      return "You opened the chat";
    case "approval":
      return "Your answer to a request";
    case "call":
      return "Your answer to a call";
    case "reminder":
      return "A reminder";
    case "check_in":
      return "Checking in";
    case "delivery":
      return "A promise kept";
    case "observation":
      return "Something they watch";
    case "action":
      return "A tool they used";
    case "":
    case "activity":
      return "A draft for you";
    default:
      return d.kind.replaceAll("_", " ");
  }
}

function money(usd: number) {
  return usd >= 0.01 ? `$${usd.toFixed(2)}` : usd > 0 ? `$${usd.toFixed(4)}` : "$0";
}

function took(ms: number) {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms} ms`;
}

function words(s: string) {
  return s.replaceAll("_", " ");
}

// The answer in words, and how sure: a choice and its confidence, a yes or no and its chance.
function answered(a: Answer): { text: string; sure?: number } {
  if (a.refused) return { text: "Wouldn't say" };
  if (a.type === "predicate") {
    const p = a.probability ?? 0;
    return { text: p >= 0.5 ? "Yes" : "No", sure: p >= 0.5 ? p : 1 - p };
  }
  if (a.type === "score") return { text: `Level ${Math.round(((a.score ?? 0) + 1) * 10) / 10}`, sure: a.confidence };
  return { text: words(a.choice ?? ""), sure: a.confidence };
}

function Step({ d, q, onLabel }: { d: Decision; q: Chain["questions"]; onLabel: (d: Decision, question: string, value: string) => Promise<void> }) {
  const [wrong, setWrong] = useState<string | null>(null);
  const meta = [d.set, d.provider.replace(/^openai\//, ""), took(d.latency_ms), money(d.cost_usd)].filter(Boolean).join(" · ");
  const answers = Object.entries(d.answers ?? {});
  return (
    <li className="flex flex-col gap-2 rounded-lg border border-line p-3.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className={`rounded-md bg-surface-2 py-0.5 pr-2.5 pl-2 text-[11.5px] font-semibold ${tierRing[d.tier]}`}>{tierName[d.tier]}</span>
        <span className="text-xs text-muted">{meta}</span>
      </div>
      {answers.map(([name, a]) => {
        const said = answered(a);
        const info = q[name];
        const label = d.labels?.[name];
        return (
          <div key={name} className="flex flex-col gap-1">
            <span className="text-[13px] text-muted">{info?.instructions ?? words(name)}</span>
            <span className="text-sm font-semibold">{said.text}</span>
            {said.sure !== undefined && (
              <span className="flex items-center gap-2 text-xs text-muted">
                <span aria-hidden="true" className="h-1.5 w-28 overflow-hidden rounded-full bg-surface-2">
                  <span className={`block h-full ${said.sure >= 0.75 ? "bg-good" : said.sure >= 0.5 ? "bg-accent" : "bg-line-strong"}`} style={{ width: `${Math.round(said.sure * 100)}%` }} />
                </span>
                {Math.round(said.sure * 100)}% sure
              </span>
            )}
            {info?.choices && a.type === "choice" && !a.refused && (
              <div className="flex flex-wrap items-center gap-1.5 text-xs">
                {label ? (
                  <span className="font-semibold text-good-text">{label === a.choice ? "You said this read was right" : `You said it was: ${words(label)}`}</span>
                ) : wrong === name ? (
                  <>
                    <span className="text-muted">What was it?</span>
                    {info.choices
                      .filter((c) => c.value !== a.choice)
                      .map((c) => (
                        <button key={c.value} type="button" title={c.description} onClick={() => onLabel(d, name, c.value)} className="min-h-8 rounded-lg border border-line-strong px-2.5 hover:border-fg">
                          {words(c.value)}
                        </button>
                      ))}
                  </>
                ) : (
                  <>
                    <span className="text-muted">Was this read right?</span>
                    <button type="button" onClick={() => onLabel(d, name, a.choice ?? "")} className="min-h-8 rounded-lg border border-line-strong px-2.5 font-semibold hover:border-fg">
                      Right
                    </button>
                    <button type="button" onClick={() => setWrong(name)} className="min-h-8 rounded-lg border border-line-strong px-2.5 font-semibold hover:border-fg">
                      Wrong
                    </button>
                  </>
                )}
              </div>
            )}
          </div>
        );
      })}
      {d.tier !== "system1" && <span className="text-sm font-semibold">{words(d.action)}</span>}
      {d.reply && <p className="m-0 border-l-2 border-line pl-3 text-sm whitespace-pre-wrap">{d.reply}</p>}
      {d.outcome?.detail && <span className="text-xs text-muted">{d.outcome.detail}</span>}
    </li>
  );
}

export function MindPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [m, setM] = useState<Mind | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [chain, setChain] = useState<Chain | null>(null);

  useEffect(() => {
    if (!account) return;
    call<Mind>(`/agents/${id}/mind`).then((res) => {
      if (!res.ok) return setError(res.error.message);
      setM(res.data);
      const first = res.data.decisions.find((d) => !d.parent_id);
      if (first) setOpen(first.id);
    });
  }, [account, id]);

  useEffect(() => {
    if (!open) return;
    setChain(null);
    call<Chain>(`/agents/${id}/mind/${open}`).then((res) => (res.ok ? setChain(res.data) : setError(res.error.message)));
  }, [id, open]);

  async function label(d: Decision, question: string, value: string) {
    const res = await call(`/agents/${id}/labels`, { decision: d.id, question, value });
    if (!res.ok) return setError(res.error.message);
    setChain((c) => c && { ...c, chain: c.chain.map((x) => (x.id === d.id ? { ...x, labels: { ...x.labels, [question]: value } } : x)) });
  }

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = agent?.name ?? "They";
  const roots = (m?.decisions ?? []).filter((d) => !d.parent_id);
  const costOf = (root: Decision) => (m?.decisions ?? []).filter((d) => d.id === root.id || d.parent_id === root.id).reduce((n, d) => n + d.cost_usd, 0);
  const s1 = m?.today.by_tier.system1 ?? 0;
  const shown = chain?.chain[0];
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Mind</h1>
        <p className="m-0 max-w-[64ch] text-muted">
          How {name} thinks. Every decision is kept: what {name} asked themselves and how sure they were, what they wrote, how long it took and what it cost. Open
          one to see its whole chain.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {m && (
        <>
          <div className="grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-3">
            {[
              { label: "Decisions today", value: String(m.today.decisions), sub: `${s1} System 1 · ${m.today.by_tier.system2 ?? 0} System 2 · ${m.today.by_tier.reflex ?? 0} reflexes` },
              { label: "Unsure reads", value: s1 ? `${Math.round((m.today.escalations * 100) / s1)}%` : "None", sub: "of System 1's reads, so System 2 was told" },
              { label: "Average step", value: m.today.decisions ? took(m.today.avg_latency_ms) : "None", sub: "a decision, today" },
              { label: "Cost today", value: money(m.today.cost_usd), sub: "every model call today" },
            ].map((x) => (
              <div key={x.label} className="flex flex-col gap-1 rounded-lg border border-line px-[18px] py-4">
                <span className="text-[13px] text-muted">{x.label}</span>
                <b className="font-display text-[26px] font-semibold">{x.value}</b>
                <span className="text-xs text-muted">{x.sub}</span>
              </div>
            ))}
          </div>
          <div className="flex flex-wrap items-start gap-6">
            <section aria-labelledby="feed" className="flex min-w-0 flex-[1_1_320px] flex-col gap-2">
              <h2 id="feed" className="font-display text-lg font-semibold tracking-[-0.02em]">Decisions</h2>
              {roots.length === 0 && <p className="m-0 text-sm text-muted">Nothing yet. {name}&apos;s decisions show here as they happen.</p>}
              <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
                {roots.map((d) => (
                  <li key={d.id}>
                    <button
                      type="button"
                      aria-pressed={open === d.id}
                      onClick={() => setOpen(d.id)}
                      className={`flex w-full flex-col gap-0.5 rounded-lg border px-3.5 py-2.5 text-left ${open === d.id ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong"}`}
                    >
                      <span className="flex justify-between gap-2 text-xs text-muted">
                        <span>
                          {when(d.at)} · {about(d)}
                        </span>
                        <span>{money(costOf(d))}</span>
                      </span>
                      <span className="line-clamp-2 text-sm font-semibold">{d.text || words(d.action)}</span>
                      <span className="text-xs text-muted">
                        {tierName[d.tier]} · {words(d.action)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
            <section aria-labelledby="chain" aria-live="polite" className="flex min-w-0 flex-[2_1_440px] flex-col gap-2">
              <h2 id="chain" className="font-display text-lg font-semibold tracking-[-0.02em]">Decision chain</h2>
              {shown && (
                <>
                  <span className="text-[13px] text-muted">
                    {when(shown.at)} · {about(shown)} · took {took(chain.chain.reduce((n, d) => Math.max(n, d.latency_ms), 0))} ·{" "}
                    {money(chain.chain.reduce((n, d) => n + d.cost_usd, 0))}
                  </span>
                  {shown.text && <p className="m-0 rounded-lg bg-surface-2 px-3.5 py-2.5 text-sm whitespace-pre-wrap">{shown.text}</p>}
                  <ol className="m-0 flex list-none flex-col gap-2 p-0">
                    {chain.chain.map((d) => (
                      <Step key={d.id} d={d} q={chain.questions} onLabel={label} />
                    ))}
                  </ol>
                </>
              )}
            </section>
          </div>
        </>
      )}
    </div>
  );
}
