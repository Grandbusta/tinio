"use client";

import { Portrait, portraits, type Character as Who } from "@tinio/brand";
import { ButtonLink } from "@tinio/ui";
import { useEffect, useState } from "react";
import { useAccount } from "@/lib/account";
import { call, type Character } from "@/lib/api";
import { AppHeader } from "./app-header";

// Step 3: who would you like to raise? The ready characters, from the API, as small cards
// to compare at a glance, with the one picked told in full beside them; and building your
// own (to come).

// role is the first sentence of who they are ("A software engineer."), for their card;
// rest is what follows it.
function role(about: string) {
  return about.split(/(?<=\.)\s/)[0];
}

function rest(about: string) {
  return about.slice(role(about).length).trim();
}
export function PickCharacter() {
  const account = useAccount();
  const [characters, setCharacters] = useState<Character[] | null>(null);
  const [failed, setFailed] = useState("");
  const [picked, setPicked] = useState("");

  useEffect(() => {
    if (!account) return;
    call<{ characters: Character[] }>("/characters").then((res) => {
      if (!res.ok) return setFailed(res.error.message);
      setCharacters(res.data.characters);
      setPicked((p) => p || res.data.characters[0]?.key || "");
    });
  }, [account]);

  const current = characters?.find((c) => c.key === picked);

  return (
    <div className="min-h-dvh bg-bg text-fg">
      <AppHeader account={account} step={0} />
      <main className="mx-auto flex max-w-[1180px] flex-col gap-6 px-6 pt-10 pb-16 max-[900px]:pb-28 max-[560px]:px-4 max-[560px]:pt-6">
        <div className="flex flex-col gap-2">
          <h1 className="font-display text-[36px] leading-[1.12] font-medium tracking-[-0.02em] text-balance max-[560px]:text-[28px]">
            Who would you like to raise?
          </h1>
          <p className="m-0 max-w-[62ch] text-muted">
            Each one comes with a background, a personality and work they&apos;re good at, and works for you from the first day. You can change any of it later,
            and teach them skills once you&apos;ve met.
          </p>
        </div>

        {failed && <p role="alert" className="text-bad-text">{failed}</p>}

        <div className="flex items-start gap-6 max-[900px]:flex-col">
          <div role="radiogroup" aria-label="Characters" className="grid min-w-0 flex-1 grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3 max-[900px]:w-full max-[560px]:grid-cols-2 max-[560px]:gap-2">
            {characters?.map((c) => {
              const on = c.key === picked;
              return (
                <button
                  key={c.key}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  onClick={() => setPicked(c.key)}
                  className={`flex items-center gap-3 rounded-lg bg-surface p-3 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent max-[560px]:flex-col max-[560px]:items-start max-[560px]:gap-2 ${on ? "shadow-[inset_0_0_0_2px_var(--color-accent)] bg-accent-soft/40" : "shadow-[inset_0_0_0_1px_var(--color-line)] hover:shadow-[inset_0_0_0_1px_var(--color-line-strong)]"}`}
                >
                  {c.key in portraits && <Portrait who={c.key as Who} size={48} />}
                  <span className="flex min-w-0 flex-col">
                    <b className="font-display text-base font-semibold tracking-[-0.01em]">{c.name}</b>
                    <span className="line-clamp-2 text-[13px] leading-snug text-muted">{role(c.about)}</span>
                  </span>
                </button>
              );
            })}
            {characters && (
              <div className="flex items-center gap-3 rounded-lg border border-dashed border-line-strong p-3 max-[560px]:flex-col max-[560px]:items-start max-[560px]:gap-2">
                <span aria-hidden="true" className="grid size-12 flex-none place-items-center rounded-full bg-surface-2 text-xl text-muted">+</span>
                <span className="flex min-w-0 flex-col">
                  <b className="font-display text-base font-semibold">Build your own</b>
                  <span className="text-[13px] leading-snug text-muted">Coming soon</span>
                </span>
              </div>
            )}
          </div>

          {current && (
            <aside aria-live="polite" aria-label={`About ${current.name}`} className="sticky top-6 flex w-[340px] flex-none flex-col gap-4 rounded-lg border border-line p-5 max-[900px]:static max-[900px]:w-full">
              <div className="flex items-center gap-3">
                {current.key in portraits && <Portrait who={current.key as Who} size={60} />}
                <div className="flex min-w-0 flex-col">
                  <b className="font-display text-xl font-semibold tracking-[-0.01em]">{current.name}</b>
                  <span className="text-[13px] text-muted">{role(current.about)}</span>
                </div>
              </div>
              {rest(current.about) && <p className="m-0 text-sm">{rest(current.about)}</p>}
              {current.born_with.length > 0 && (
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-semibold tracking-[0.04em] text-muted uppercase">Born with</span>
                  <div className="flex flex-wrap gap-1.5">
                    {current.born_with.map((s) => (
                      <span key={s} className="rounded-md bg-surface-2 px-2.5 py-1 text-xs font-medium">{s}</span>
                    ))}
                  </div>
                </div>
              )}
              <div className="flex flex-col gap-2 border-t border-line pt-4 max-[900px]:fixed max-[900px]:inset-x-0 max-[900px]:bottom-0 max-[900px]:z-10 max-[900px]:flex-row max-[900px]:items-center max-[900px]:justify-between max-[900px]:bg-bg max-[900px]:px-4 max-[900px]:py-3">
                <ButtonLink kind="primary" href={`/raise/${current.key}`} className="min-h-12 text-[15px] max-[900px]:order-last">
                  Continue with {current.name}
                </ButtonLink>
                <span className="text-[13px] text-muted max-[560px]:hidden">
                  Next: who you are to them.
                </span>
              </div>
            </aside>
          )}
        </div>
      </main>
    </div>
  );
}
