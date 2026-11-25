"use client";

import { Portrait, portraits, type Character as Who } from "@tinio/brand";
import { Button, ButtonLink } from "@tinio/ui";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { useAccount } from "@/lib/account";
import { call, type Agent, type ApiError, type Character } from "@/lib/api";
import { AppHeader } from "./app-header";

// Step 4: who are you to them? What they call you, who you are to them (the relations
// their character allows), and when their morning for you comes. Meeting them raises the
// agent.

const chip =
  "min-h-11 rounded-full border px-[18px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent";
const chipOn = "border-fg bg-fg font-semibold text-bg";
const chipOff = "border-line bg-surface hover:border-fg";

export function WhoYouAre({ characterKey }: { characterKey: string }) {
  const router = useRouter();
  const account = useAccount();
  const [c, setC] = useState<Character | null>(null);
  const [missing, setMissing] = useState(false);
  const [name, setName] = useState("");
  const [relation, setRelation] = useState("");
  const [morning, setMorning] = useState({ on: true, at: "07:30", days: [1, 2, 3, 4, 5] });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  useEffect(() => {
    if (!account) return;
    if (account.name) setName((n) => n || account.name);
    call<{ characters: Character[] }>("/characters").then((res) => {
      const found = res.ok ? res.data.characters.find((x) => x.key === characterKey) : undefined;
      if (!found) return setMissing(true);
      setC(found);
      setRelation((r) => r || found.relations[0]);
    });
  }, [account, characterKey]);

  async function meet(e: FormEvent) {
    e.preventDefault();
    if (!c) return;
    setBusy(true);
    setError(null);
    // They work in the owner's time zone: their mornings, reminders and check-ins are in it.
    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    const res = await call<{ agent: Agent }>("/agents", { character: c.key, owner_name: name, relation, timezone, morning });
    setBusy(false);
    if (!res.ok) return setError(res.error);
    router.push(`/agents/${res.data.agent.id}`);
  }

  if (missing) {
    return (
      <div className="min-h-dvh bg-bg text-fg">
        <AppHeader account={account} step={0} />
        <main className="mx-auto flex max-w-[1180px] flex-col gap-4 px-6 py-16 max-[560px]:px-4">
          <h1 className="font-display text-3xl font-medium">We don&apos;t have that character</h1>
          <ButtonLink href="/raise" className="self-start">Pick someone else</ButtonLink>
        </main>
      </div>
    );
  }

  const field = (f: string) => (error?.code === "invalid" && error.data?.field === f ? error.message : "");

  return (
    <div className="min-h-dvh bg-bg text-fg">
      <AppHeader account={account} step={1} picked={c?.name} />
      {c && (
        <main className="mx-auto flex max-w-[1180px] flex-wrap items-start gap-12 px-6 pt-12 pb-16 max-[560px]:px-4 max-[560px]:pt-8">
          <form onSubmit={meet} noValidate className="flex min-w-0 flex-[999_1_520px] flex-col gap-7">
            <div className="flex flex-col gap-2.5">
              <h1 className="font-display text-[44px] leading-[1.12] font-medium tracking-[-0.02em] text-balance max-[560px]:text-[32px]">
                Who are you to {c.name}?
              </h1>
              <p className="max-w-[54ch] text-[17px] text-muted">
                {c.name} works for you, and talks to you the way people do with someone they know well. This shapes how they speak to you.
              </p>
            </div>

            <div className="flex flex-col gap-2">
              <label htmlFor="name" className="font-semibold">What should {c.name} call you?</label>
              <input
                id="name"
                type="text"
                autoComplete="given-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                aria-invalid={!!field("owner_name")}
                aria-describedby={field("owner_name") ? "name-error" : undefined}
                className={`min-h-12 w-full rounded-lg border bg-surface px-3.5 text-base text-fg focus:outline-2 focus:outline-offset-1 focus:outline-accent ${field("owner_name") ? "border-bad" : "border-line-strong"}`}
              />
              {field("owner_name") && <span id="name-error" role="alert" className="text-[13px] text-bad-text">{field("owner_name")}</span>}
            </div>

            <fieldset className="flex flex-col gap-2.5">
              <legend className="mb-2.5 font-semibold">You are their...</legend>
              <div className="flex flex-wrap gap-2">
                {c.relations.map((r) => (
                  <button key={r} type="button" aria-pressed={r === relation} onClick={() => setRelation(r)} className={`${chip} ${r === relation ? chipOn : chipOff}`}>
                    {r[0].toUpperCase() + r.slice(1)}
                  </button>
                ))}
              </div>
              {field("relation") && <span role="alert" className="text-[13px] text-bad-text">{field("relation")}</span>}
            </fieldset>

            <fieldset className="flex flex-col gap-3">
              <legend className="mb-1 font-semibold">When should {c.name} bring you your morning?</legend>
              <span className="text-sm text-muted">Each morning, your few things for the day, with what {c.name} has got ready. The first one is a question: they won&apos;t guess at your day.</span>
              <div className="flex flex-wrap items-end gap-3">
                <label className="flex flex-col gap-1 text-[13px] font-semibold">
                  At
                  <input type="time" value={morning.at} onChange={(e) => e.target.value && setMorning({ ...morning, at: e.target.value })} className="min-h-12 rounded-lg border border-line-strong bg-surface px-3 text-base font-normal" />
                </label>
                <div role="group" aria-label="Days" className="flex flex-wrap gap-1.5">
                  {([[1, "Mon"], [2, "Tue"], [3, "Wed"], [4, "Thu"], [5, "Fri"], [6, "Sat"], [0, "Sun"]] as const).map(([d, short]) => {
                    const on = morning.days.includes(d);
                    return (
                      <button
                        key={d}
                        type="button"
                        aria-pressed={on}
                        onClick={() => setMorning({ ...morning, days: on ? morning.days.filter((x) => x !== d) : [...morning.days, d] })}
                        className={`min-h-12 w-12 rounded-[10px] text-sm font-semibold ${on ? "bg-fg text-bg" : "bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]"}`}
                      >
                        {short}
                      </button>
                    );
                  })}
                </div>
              </div>
              {field("morning") && <span role="alert" className="text-[13px] text-bad-text">{field("morning")}</span>}
            </fieldset>

            <div className="rounded-lg border border-warn-text/20 bg-warn-soft px-4 py-3.5 text-sm text-warn-text">
              You&apos;ll talk with {c.name} right here, on this computer. They think with what you chose in Settings, Claude Code or your OpenAI key, within the week&apos;s cap you set on You.
            </div>

            {error && error.code !== "invalid" && <p role="alert" className="text-sm text-bad-text">{error.message}</p>}
    
            <div className="flex flex-wrap items-center gap-3">
              <Button kind="primary" type="submit" disabled={busy || !name.trim()} className="min-h-[52px] text-[15px] disabled:cursor-not-allowed disabled:opacity-60">
                {busy ? "Getting ready…" : `Meet ${c.name}`}
              </Button>
              <ButtonLink href="/raise" className="min-h-[52px] text-[15px]">Back</ButtonLink>
            </div>
          </form>

          <aside aria-label={c.name} className="min-w-0 flex-[1_1_340px] overflow-hidden rounded-lg border border-line">
            <div className="grid h-[120px] place-items-center border-b border-line bg-accent-soft">
              {c.key in portraits && <Portrait who={c.key as Who} size={84} />}
            </div>
            <div className="flex flex-col gap-3 p-5">
              <h2 className="font-display text-2xl font-semibold">{c.name}</h2>
              <span className="text-sm text-muted">{c.about}</span>
              {c.personality.length > 0 && <p className="text-sm">{c.personality.slice(0, 2).join(" ")}</p>}
              <div className="flex flex-col gap-1.5 border-t border-line pt-3">
                <span className="text-xs font-semibold tracking-[0.04em] text-muted uppercase">To you they&apos;ll be</span>
                <span className="text-sm">
                  {c.name}, whose {relation} is {name.trim() || "you"}.
                </span>
              </div>
              <span className="text-[13px] text-muted">{c.name} always says plainly that they&apos;re an AI.</span>
            </div>
          </aside>
        </main>
      )}
    </div>
  );
}
