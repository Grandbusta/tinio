"use client";

import { Mark, Wordmark } from "@tinio/brand";
import Link from "next/link";
import { useEffect, useState } from "react";
import { call, type Account } from "@/lib/api";

// The steps of raising an agent: picking them, then who you are to them, then saying hello.
const steps = ["Character", "You", "Say hello"];

export function AppHeader({ account, step, picked }: { account: Account | null; step?: number; picked?: string }) {
  const [hasAgents, setHasAgents] = useState(false); // raising another: there's a way back to them
  useEffect(() => {
    if (account) call<{ agents: unknown[] }>("/agents").then((res) => res.ok && setHasAgents(res.data.agents.length > 0));
  }, [account]);
  return (
    <header className="border-b border-line pt-6 [--wails-draggable:drag]">
      <div className="mx-auto flex max-w-[1180px] flex-wrap items-center justify-between gap-3 px-6 py-2.5 max-[560px]:px-4">
        <Link href="/agents" className="flex min-h-11 items-center gap-2.5 text-accent-text" aria-label="Tinio, your agents">
          <Mark size={28} title="" aria-hidden="true" />
          <Wordmark className="text-[21px]" />
        </Link>
        {step !== undefined && (
          <ol aria-label="Steps" className="flex flex-wrap gap-2 text-[13px] font-semibold max-[560px]:order-3 max-[560px]:w-full">
            {steps.map((s, i) => {
              const label = `${i + 1} · ${i === 0 && picked && step > 0 ? picked : s}`;
              const tone = i < step ? "bg-good-soft text-good-text" : i === step ? "bg-accent-soft text-accent-text" : "text-muted ring-1 ring-line ring-inset";
              return (
                <li key={s} aria-current={i === step ? "step" : undefined} className={`rounded-full px-3 py-1.5 ${tone}`}>
                  {label}
                </li>
              );
            })}
          </ol>
        )}
        {account && (
          <div className="flex items-center gap-3 text-sm">
            {hasAgents && (
              <Link href="/agents" className="font-semibold text-accent-text">
                ‹ Your agents
              </Link>
            )}
          </div>
        )}
      </div>
    </header>
  );
}
