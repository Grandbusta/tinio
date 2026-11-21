"use client";

import { Avatar, useFrame, when } from "@/components/app-frame";

// Your agents, with no chat open: beside the list, who needs you and what they've done
// lately. With none yet, the frame sends you to raise one.

export function AllAgents() {
  const { account, overview } = useFrame();
  const agents = overview?.agents ?? [];
  const waitingOn = agents.filter((a) => a.waiting > 0);
  const nameOf = (id: string) => agents.find((a) => a.id === id)?.name ?? "";
  const who = account?.name || agents[0]?.owner_name || "";
  return (
    <div className="flex flex-col gap-6 px-10 pt-8 pb-12">
      <div className="flex flex-col gap-1.5">
        <span className="text-sm text-muted">
          {greeting()}
          {who && `, ${who}`}
        </span>
        <h2 className="font-display text-4xl font-medium tracking-[-0.02em]">Your agents</h2>
      </div>
      {overview && (
        <>
          <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
            <div className="flex flex-col gap-1 rounded-lg border border-line px-[18px] py-4">
              <span className="text-[13px] text-muted">Needs you</span>
              <b className="font-display text-[26px] font-semibold">
                {waitingOn.length === 0 ? "Nobody" : waitingOn.length === 1 ? "1 agent" : `${waitingOn.length} agents`}
              </b>
              <span className="text-[13px] text-muted">
                {waitingOn.length === 0 ? "Nothing waits for your answer." : waitingOn.map((a) => `${a.name} ${a.waiting}`).join(" · ")}
              </span>
            </div>
            <div className="flex flex-col gap-1 rounded-lg border border-line px-[18px] py-4">
              <span className="text-[13px] text-muted">Your agents</span>
              <b className="font-display text-[26px] font-semibold">{agents.length}</b>
              <span className="truncate text-[13px] text-muted">{agents.map((a) => a.name).join(" · ")}</span>
            </div>
          </div>

          <section aria-labelledby="lately" className="rounded-lg border border-line">
            <h3 id="lately" className="border-b border-line px-[18px] py-3.5 font-display text-[17px] font-semibold tracking-[-0.02em]">
              Lately, across your agents
            </h3>
            {overview.lately.length === 0 ? (
              <p className="m-0 px-[18px] py-3.5 text-sm text-muted">Nothing made or asked in the last two days.</p>
            ) : (
              <ul className="m-0 list-none p-0">
                {overview.lately.map((l, i) => {
                  const a = agents.find((x) => x.id === l.agent);
                  return (
                    <li key={i} className="flex items-center gap-3 border-b border-line px-[18px] py-3 last:border-b-0">
                      {a && <Avatar agent={a} size={32} />}
                      <span className="min-w-0 flex-auto text-sm">
                        {l.kind === "made" ? `${nameOf(l.agent)} made “${l.text}”.` : `${nameOf(l.agent)} asked: ${l.text}`}
                      </span>
                      <span className="text-xs whitespace-nowrap text-muted">{when(l.at)}</span>
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
          <span className="text-sm text-muted">Pick an agent on the left to talk with them. Anything waiting on you shows in their chat.</span>
        </>
      )}
    </div>
  );
}

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}
