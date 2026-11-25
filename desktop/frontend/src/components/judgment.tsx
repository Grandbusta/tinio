"use client";

import { useEffect, useState, type FormEvent } from "react";
import { Back, useFrame, when } from "@/components/app-frame";
import { call, type Judgment, type Principle } from "@/lib/api";

// Step 12: what an agent has learnt about how you make calls. They use only the principles
// you've kept here; ones their weekly look back draws from your calls wait for you. Below,
// the calls you answered and what they'd guessed.

const button = "inline-flex min-h-9 items-center justify-center rounded-lg px-3.5 text-[13px] font-semibold disabled:opacity-60";

function origin(p: Principle, name: string) {
  const from =
    p.source === "owner" ? "you wrote it" : p.source === "seed" ? `${name} was raised with it` : `from your calls${p.calls ? ` (${p.calls})` : ""}, in ${name}'s weekly look back`;
  return `${p.status === "approved" ? "Kept" : "Proposed"} ${when(p.at)} · ${from}`;
}

const kindLabel: Record<Principle["kind"], string> = { principle: "", priority: "What matters most", boundary: "Never" };

export function JudgmentPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [j, setJ] = useState<Judgment | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const [removed, setRemoved] = useState<Record<string, boolean>>({}); // removed here, still shown; a kept one can be undone
  const [text, setText] = useState("");
  const [adding, setAdding] = useState(false);

  useEffect(() => {
    if (!account) return;
    call<Judgment>(`/agents/${id}/judgment`).then((res) => (res.ok ? setJ(res.data) : setError(res.error.message)));
  }, [account, id]);

  async function review(p: Principle, keep: boolean) {
    if (busy[p.id]) return;
    setBusy((b) => ({ ...b, [p.id]: true }));
    setError("");
    const res = await call(`/agents/${id}/principles/${p.id}`, { keep });
    setBusy((b) => ({ ...b, [p.id]: false }));
    if (!res.ok) return setError(res.error.message);
    setRemoved((r) => ({ ...r, [p.id]: !keep }));
    if (keep) setJ((x) => x && { ...x, principles: x.principles.map((y) => (y.id === p.id ? { ...y, status: "approved" } : y)) });
  }

  async function add(e: FormEvent) {
    e.preventDefault();
    const said = text.trim();
    if (!said || adding) return;
    setAdding(true);
    setError("");
    const res = await call<{ principle: Principle }>(`/agents/${id}/principles`, { text: said });
    setAdding(false);
    if (!res.ok) return setError(res.error.message);
    setText("");
    setJ((x) => x && { ...x, principles: [...x.principles, res.data.principle] });
  }

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = agent?.name ?? "They";
  const trend = j ? (j.asked.this_week < j.asked.last_week ? `down from ${j.asked.last_week}` : j.asked.this_week > j.asked.last_week ? `up from ${j.asked.last_week}` : "") : "";
  return (
    <div className="flex max-w-[960px] flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">How you decide</h1>
        <p className="m-0 max-w-[64ch] text-muted">
          What {name} has learnt about how you make calls. {name} uses only the principles you&apos;ve kept here. Before a call, {name} guesses what you&apos;d
          pick; when you pick differently, they ask what tipped it.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {j && (
        <>
          <div className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3">
            <div className="flex flex-col gap-1 rounded-lg border border-line px-[18px] py-4">
              <span className="text-[13px] text-muted">{name} guessed your call</span>
              <b className="font-display text-[26px] font-semibold">{j.guessed.of ? `${j.guessed.right} of the last ${j.guessed.of}` : "No calls yet"}</b>
            </div>
            <div className="flex flex-col gap-1 rounded-lg border border-line px-[18px] py-4">
              <span className="text-[13px] text-muted">Calls {name} asked you this week</span>
              <b className="font-display text-[26px] font-semibold">
                {j.asked.this_week} {trend && <span className="text-[15px] font-medium text-muted">{trend}</span>}
              </b>
            </div>
          </div>

          <section aria-labelledby="principles" className="flex flex-col gap-2.5">
            <h2 id="principles" className="font-display text-lg font-semibold tracking-[-0.02em]">Your principles</h2>
            {j.principles.length === 0 && <p className="m-0 text-sm text-muted">None yet. Add one, or {name} will propose some from the calls you answer.</p>}
            {j.principles.map((p) => (
              <div key={p.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-line px-[18px] py-3.5">
                <div className={`flex min-w-0 flex-[1_1_320px] flex-col gap-0.5 ${removed[p.id] ? "text-muted line-through" : ""}`}>
                  {kindLabel[p.kind] && <span className="text-xs font-semibold tracking-[0.04em] text-accent-text uppercase">{kindLabel[p.kind]}</span>}
                  <span className="text-[15px]">{p.text}</span>
                  <span className="text-xs text-muted no-underline">{origin(p, name)}</span>
                </div>
                <div className="flex gap-2">
                  {removed[p.id] && p.status === "proposed" ? (
                    <span className="text-xs font-semibold text-muted">Removed</span>
                  ) : removed[p.id] ? (
                    <button type="button" disabled={busy[p.id]} onClick={() => review(p, true)} className={`${button} border border-line-strong hover:border-fg`}>
                      Undo
                    </button>
                  ) : (
                    <>
                      {p.status === "proposed" && (
                        <button type="button" disabled={busy[p.id]} onClick={() => review(p, true)} className={`${button} bg-accent text-accent-ink hover:bg-accent-hover`}>
                          Keep
                        </button>
                      )}
                      <button type="button" disabled={busy[p.id]} onClick={() => review(p, false)} className={`${button} border border-line-strong hover:border-fg`}>
                        Remove
                      </button>
                    </>
                  )}
                </div>
              </div>
            ))}
            <form onSubmit={add} className="flex flex-wrap gap-2">
              <label htmlFor="principle" className="sr-only">A new principle</label>
              <input
                id="principle"
                value={text}
                maxLength={300}
                onChange={(e) => setText(e.target.value)}
                placeholder={'Add one in your own words, like "Regulars come before new customers"'}
                className="min-h-[42px] min-w-0 flex-[1_1_280px] rounded-lg border border-line-strong bg-surface px-3 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent"
              />
              <button type="submit" disabled={adding || !text.trim()} className={`${button} min-h-[42px] border border-line-strong hover:border-fg`}>
                Add
              </button>
            </form>
          </section>

          <section aria-labelledby="calls" className="flex flex-col gap-2.5">
            <h2 id="calls" className="font-display text-lg font-semibold tracking-[-0.02em]">Calls you made</h2>
            {j.calls.length === 0 && <p className="m-0 text-sm text-muted">None yet. When {name} asks what you&apos;d do, your answers show here.</p>}
            {j.calls.map((c) => (
              <div key={c.id} className="flex flex-col gap-1.5 rounded-lg border border-line px-[18px] py-3.5">
                <div className="flex flex-wrap justify-between gap-2.5">
                  <b className="min-w-0 text-[15px] font-semibold whitespace-pre-wrap">{c.situation}</b>
                  <span className="text-xs text-muted">{when(c.at)}</span>
                </div>
                <span className="text-sm">
                  You {c.request ? "said" : "chose"}: <b className="font-semibold">{c.chose}</b> · {name} had guessed {c.chose === c.guessed ? "the same" : c.guessed || "nothing"}
                </span>
                {c.why && <span className="text-sm text-muted">Why: &ldquo;{c.why}&rdquo;</span>}
              </div>
            ))}
          </section>
        </>
      )}
    </div>
  );
}
