"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { Back, useFrame } from "@/components/app-frame";
import { call, type AgentSettings, type Verdict } from "@/lib/api";

// An agent's own settings, from About: what they may do on their own, how fast they work,
// where you talk, and their data. A change is saved at once and reaches them within a
// minute. Your own settings are on You.

const classes: Record<string, { label: string; note: string }> = {
  read_web: { label: "Look things up on the web", note: "Searches and pages they read for their work. It happens mid-task, so it can't wait to ask." },
  dream: { label: "Change one of their dreams", note: "From their weekly review." },
  commit: { label: "Promise something to someone else", note: "A date, a delivery, a reply. Anything for you is always fine." },
  publish: { label: "Post publicly", note: "Their public page, once it's here." },
  reach_out: { label: "Message other people first", note: "Reminders and check-ins to anyone but you." },
  write: { label: "Change things with tools you connect", note: "Files, docs, a calendar, once tools come with Connect." },
  external_message: { label: "Email or message outside this chat", note: "Nothing leaves the chat yet, so never, for now." },
  spend: { label: "Spend money", note: "Never. It can't be changed." },
};

const verdictName: Record<Verdict, string> = { auto: "On their own", ask: "Asks you", forbid: "Never" };

const paceNames: [number, string, string][] = [
  [1, "Like a person", "One day of theirs is one of yours. The calmest, and uses the least of your week."],
  [3, "Quicker", "Three rounds of work a day. They learn faster and use more of your week."],
  [10, "Fast", "Ten rounds a day: what takes a person four weeks takes them about three days."],
];

const box = "flex flex-col gap-3.5 rounded-lg border border-line px-[22px] py-5";
const h2 = "font-display text-lg font-semibold tracking-[-0.02em]";

export function SettingsPage({ id }: { id: string }) {
  const { account, agent, missing, refresh } = useFrame();
  const router = useRouter();
  const [s, setS] = useState<AgentSettings | null>(null);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [confirm, setConfirm] = useState("");
  const [quiet, setQuiet] = useState({ from: "", until: "" });

  useEffect(() => {
    if (!account) return;
    call<AgentSettings>(`/agents/${id}/settings`).then((res) => {
      if (!res.ok) return setError(res.error.message);
      setS(res.data);
      setQuiet(res.data.quiet);
    });
  }, [account, id]);

  const name = agent?.name ?? "They";

  async function save(change: { classes?: Record<string, Verdict>; pace?: number; quiet?: { from: string; until: string } }) {
    setBusy(true);
    setError("");
    setSaved("");
    const res = await call<AgentSettings>(`/agents/${id}/settings`, change);
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    setS(res.data);
    setQuiet(res.data.quiet);
    setSaved(`Saved. ${name} will have it within a minute.`);
  }

  async function pause(paused: boolean) {
    setBusy(true);
    setError("");
    setSaved("");
    const res = await call<AgentSettings>(`/agents/${id}/pause`, { paused });
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    setS(res.data);
    setSaved(paused ? `${name} is paused.` : `${name} is back, and reads what you sent meanwhile.`);
    refresh();
  }

  async function remove(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const res = await call(`/agents/${id}/delete`, { name: confirm });
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    refresh();
    router.replace("/agents");
  }

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{agent ? `${agent.name}'s settings` : "Settings"}</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          What {name} may do on their own, and how they work. Only you change these, here: never {name}, and never by chat for what can&apos;t be.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {saved && <p role="status" className="text-sm text-good-text">{saved}</p>}

      {s && (
        <div className="flex max-w-[880px] flex-col gap-5">
          <section aria-labelledby="may-do" className={box}>
            <div className="flex flex-col gap-1">
              <h2 id="may-do" className={h2}>What {name} may do</h2>
              <span className="text-sm text-muted">Anything only for you they do on their own. &ldquo;Asks you&rdquo; waits for your yes; silence is no.</span>
            </div>
            {s.classes.map((c) => {
              const info = classes[c.class] ?? { label: c.class.replaceAll("_", " "), note: "" };
              return (
                <div key={c.class} className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-3">
                  <div className="flex min-w-0 flex-[1_1_260px] flex-col">
                    <b className="text-[15px] font-semibold">{info.label}</b>
                    <span className="text-[13px] text-muted">{info.note}</span>
                  </div>
                  {c.choices.length === 0 ? (
                    <span className="rounded-full bg-surface-2 px-3 py-1.5 text-[13px] font-semibold text-muted">{verdictName[c.verdict]}</span>
                  ) : (
                    <div role="group" aria-label={info.label} className="flex flex-wrap gap-1 rounded-full border border-line bg-surface-2 p-1">
                      {c.choices.map((v) => (
                        <button
                          key={v}
                          type="button"
                          disabled={busy}
                          aria-pressed={c.verdict === v}
                          onClick={() => c.verdict !== v && save({ classes: { [c.class]: v } })}
                          className={`min-h-9 rounded-full px-3 text-[13px] font-semibold disabled:cursor-wait ${c.verdict === v ? "bg-bg text-accent-text shadow-sm" : "text-muted hover:text-fg"}`}
                        >
                          {verdictName[v]}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </section>

          <section aria-labelledby="pace" className={box}>
            <h2 id="pace" className={h2}>Their pace</h2>
            <div className="flex flex-wrap gap-2">
              {paceNames.map(([p, label]) => {
                const allowed = s.paces.includes(p);
                return (
                  <button
                    key={p}
                    type="button"
                    disabled={busy || !allowed}
                    aria-pressed={s.pace === p}
                    onClick={() => s.pace !== p && save({ pace: p })}
                    className={`min-h-11 rounded-full px-4 text-sm disabled:cursor-not-allowed ${s.pace === p ? "bg-fg font-semibold text-bg" : allowed ? "font-medium text-fg shadow-[inset_0_0_0_1px_var(--color-line-strong)] hover:shadow-[inset_0_0_0_1px_var(--color-fg)]" : "font-medium text-muted/70 shadow-[inset_0_0_0_1px_var(--color-line)]"}`}
                  >
                    {label}
                  </button>
                );
              })}
            </div>
            <span className="text-[13px] text-muted">{paceNames.find(([p]) => p === s.pace)?.[2]}</span>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                save({ quiet });
              }}
              className="flex flex-col gap-2 border-t border-line pt-3"
            >
              <b className="text-sm font-semibold">Quiet hours</b>
              <span className="text-[13px] text-muted">
                No check-ins, requests or calls from {name} in these hours, in their time zone; they wait until it ends. Reminders and promises you asked for still come.
              </span>
              <div className="flex flex-wrap items-end gap-3">
                <label className="flex flex-col gap-1 text-[13px] font-semibold">
                  From
                  <input type="time" value={quiet.from} onChange={(e) => setQuiet({ ...quiet, from: e.target.value })} className="min-h-11 rounded-lg border border-line-strong bg-surface px-3 text-[15px] font-normal" />
                </label>
                <label className="flex flex-col gap-1 text-[13px] font-semibold">
                  Until
                  <input type="time" value={quiet.until} onChange={(e) => setQuiet({ ...quiet, until: e.target.value })} className="min-h-11 rounded-lg border border-line-strong bg-surface px-3 text-[15px] font-normal" />
                </label>
                <button
                  type="submit"
                  disabled={busy || (quiet.from === s.quiet.from && quiet.until === s.quiet.until) || !quiet.from !== !quiet.until}
                  className="min-h-11 rounded-full bg-accent px-4 text-[13px] font-semibold text-accent-ink hover:bg-accent-hover disabled:opacity-50"
                >
                  Save
                </button>
                {s.quiet.from && (
                  <button type="button" disabled={busy} onClick={() => save({ quiet: { from: "", until: "" } })} className="min-h-11 rounded-full border border-line-strong px-4 text-[13px] font-semibold hover:border-fg">
                    No quiet hours
                  </button>
                )}
              </div>
            </form>
          </section>

          <section aria-labelledby="talk" className={box}>
            <h2 id="talk" className={h2}>Where you talk</h2>
            <div className="flex items-center justify-between gap-3 border-t border-line pt-3">
              <div className="flex flex-col">
                <b className="text-[15px] font-semibold">In your browser</b>
                <span className="text-[13px] text-muted">Always on, on any device.</span>
              </div>
              <span className="rounded-full bg-good-soft px-2.5 py-1 text-xs font-semibold text-good-text">On</span>
            </div>
            <div className="flex items-center justify-between gap-3 border-t border-line pt-3">
              <div className="flex flex-col">
                <b className="text-[15px] font-semibold">Telegram</b>
                <span className="text-[13px] text-muted">{name} gets their own bot. Coming soon.</span>
              </div>
              <span className="rounded-full px-2.5 py-1 text-xs font-semibold text-muted shadow-[inset_0_0_0_1px_var(--color-line)]">Soon</span>
            </div>
          </section>

          <section aria-labelledby="data" className={box}>
            <h2 id="data" className={h2}>{name}&apos;s data</h2>
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-3">
              <div className="flex min-w-0 flex-[1_1_300px] flex-col">
                <b className="text-[15px] font-semibold">Export {name}</b>
                <span className="text-[13px] text-muted">One file with everything about {name}, to keep or to run yourself with the open-source Tinio.</span>
              </div>
              <span className="text-xs font-semibold text-muted">Coming soon</span>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-3">
              <div className="flex min-w-0 flex-[1_1_300px] flex-col">
                <b className="text-[15px] font-semibold">{s.paused ? `${name} is paused` : `Pause ${name}`}</b>
                <span className="text-[13px] text-muted">
                  {s.paused
                    ? `They aren't working or talking. Resume them and they read what you sent meanwhile.`
                    : `${name} stops working and talking, and remembers everything when you resume. Asks and reminders wait too.`}
                </span>
              </div>
              <button
                type="button"
                disabled={busy}
                onClick={() => pause(!s.paused)}
                className={`min-h-10 rounded-full px-4 text-[13px] font-semibold ${s.paused ? "bg-accent text-accent-ink hover:bg-accent-hover" : "border border-line-strong hover:border-fg"}`}
              >
                {s.paused ? "Resume" : "Pause"}
              </button>
            </div>
            <div className="flex flex-col gap-3 border-t border-line pt-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex min-w-0 flex-[1_1_300px] flex-col">
                  <b className="text-[15px] font-semibold">Delete {name}</b>
                  <span className="text-[13px] text-muted">Their memory, what they made, their chat and their files go for good. It can&apos;t be undone.</span>
                </div>
                {!deleting && (
                  <button type="button" onClick={() => setDeleting(true)} className="min-h-10 rounded-full border border-bad px-4 text-[13px] font-semibold text-bad-text hover:bg-bad-soft">
                    Delete
                  </button>
                )}
              </div>
              {deleting && (
                <form onSubmit={remove} className="flex flex-col gap-2 rounded-lg bg-bad-soft p-4">
                  <label htmlFor="confirm" className="text-sm font-semibold text-bad-text">Type {name} to delete them</label>
                  <div className="flex flex-wrap gap-2">
                    <input
                      id="confirm"
                      autoComplete="off"
                      value={confirm}
                      onChange={(e) => setConfirm(e.target.value)}
                      className="min-h-11 min-w-0 flex-[1_1_220px] rounded-lg border border-line-strong bg-surface px-3.5 text-[15px] focus:outline-2 focus:outline-offset-1 focus:outline-accent"
                    />
                    <button
                      type="submit"
                      disabled={busy || confirm.trim().toLowerCase() !== name.toLowerCase()}
                      className="min-h-11 rounded-full bg-bad px-4 text-[13px] font-semibold text-white disabled:opacity-50"
                    >
                      Delete {name} for good
                    </button>
                    <button type="button" onClick={() => (setDeleting(false), setConfirm(""))} className="min-h-11 rounded-full border border-line-strong px-4 text-[13px] font-semibold hover:border-fg">
                      Cancel
                    </button>
                  </div>
                </form>
              )}
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
