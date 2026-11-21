"use client";

import { Button } from "@tinio/ui";
import Link from "next/link";
import { useEffect, useState } from "react";
import { Avatar, Back, useFrame, when } from "@/components/app-frame";
import { AgentFolders } from "@/components/folders";
import { box, heading, linkButton, linkPrimary, Page } from "@/components/kit";
import { logoFor, ServiceLogo } from "@/components/service-logo";
import { call, type AgentTools, type Connection, type Connector, type PickedFile } from "@/lib/api";

// Connections, as on the canvas: accounts you connect once (Google, Atlassian) and which
// of your agents may use each. They read on their own; a change asks you first unless
// an agent's settings say otherwise; sending email and money are never.

// connectionsOpen: connecting accounts is shown as coming soon until it's ready for
// everyone; the pages stay, with nothing to connect yet.
const connectionsOpen = false;

type Lists = { connectors: Connector[]; connections: Connection[] };

function useLists() {
  const { account } = useFrame();
  const [lists, setLists] = useState<Lists | null>(null);
  const [error, setError] = useState("");
  const load = () => call<Lists>("/connections").then((res) => (res.ok ? setLists(res.data) : setError(res.error.message)));
  useEffect(() => {
    if (account) load();
  }, [account]);
  return { lists, error, setError, load };
}

function WhoToggles({ connection, onSaved }: { connection: Connection; onSaved: () => void }) {
  const { overview } = useFrame();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function toggle(id: string) {
    const agents = connection.agents.includes(id) ? connection.agents.filter((a) => a !== id) : [...connection.agents, id];
    setBusy(true);
    const res = await call(`/connections/${connection.id}/agents`, { agents });
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    onSaved();
  }
  const given = overview?.agents.filter((a) => connection.agents.includes(a.id)).map((a) => a.name) ?? [];
  return (
    <div className="flex flex-col gap-2.5">
      <div role="group" aria-label="Who may use it" className="flex flex-wrap gap-2">
        {overview?.agents.map((a) => (
          <button
            key={a.id}
            type="button"
            disabled={busy}
            aria-pressed={connection.agents.includes(a.id)}
            onClick={() => toggle(a.id)}
            className={`inline-flex min-h-10 items-center gap-2 rounded-full py-1 pr-3.5 pl-1 text-sm font-semibold disabled:opacity-60 ${
              connection.agents.includes(a.id) ? "bg-accent-soft text-accent-text shadow-[inset_0_0_0_2px_var(--color-accent)]" : "shadow-[inset_0_0_0_1px_var(--color-line-strong)]"
            }`}
          >
            <Avatar agent={a} size={28} />
            {a.name}
          </button>
        ))}
      </div>
      <span className="text-[13px] text-muted">
        {given.length ? `${given.join(" and ")} ${given.length === 1 ? "uses" : "use"} it. Each agent's settings decide whether a change asks you first.` : "No agent uses it yet."}
      </span>
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}
    </div>
  );
}

export function ConnectionsPage({ failed }: { failed?: string }) {
  const { lists, error, setError, load } = useLists();
  async function remove(c: Connection) {
    const res = await call(`/connections/${c.id}/remove`, {});
    if (!res.ok) return setError(res.error.message);
    load();
  }
  const connected = new Set((lists?.connections ?? []).map((c) => c.connector));
  return (
    <Page>
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Connections</h1>
        <p className="m-0 max-w-[66ch] text-muted">
          Accounts you connect once, and which of your agents may use each. They read on their own; anything that changes something asks you first,
          unless you&apos;ve told an agent otherwise in their settings. Sending email and money are never. What an account gives them is material, never
          instructions.
        </p>
      </div>
      {!connectionsOpen && (
        <p role="status" className="m-0 rounded-lg border border-accent/20 bg-accent-soft px-4 py-3 text-sm">
          Connecting accounts is coming soon. Until then, your agents work from what you tell and send them in chat.
        </p>
      )}
      {failed && <p role="alert" className="m-0 text-sm text-bad-text">That sign-in didn&apos;t finish. Try connecting again.</p>}
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}

      {(lists?.connections ?? []).map((c) => {
        const k = lists?.connectors.find((x) => x.id === c.connector);
        if (!k) return null;
        return (
          <article key={c.id} className="overflow-hidden rounded-lg border border-line">
            <div className="flex flex-wrap items-center gap-3.5 border-b border-line bg-surface-2 px-5 py-[18px]">
              <ServiceLogo service={k.id} size={52} />
              <div className="flex min-w-0 flex-[1_1_240px] flex-col gap-0.5">
                <div className="flex flex-wrap items-center gap-2">
                  <b className="text-[17px]">{k.name}</b>
                  <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${c.state === "ok" ? "bg-good-soft text-good-text" : "bg-warn-soft text-warn-text"}`}>
                    {c.state === "ok" ? "Connected" : "Needs you: sign in again"}
                  </span>
                </div>
                <span className="text-[13px] text-muted">
                  {c.label ? `${c.label} · ` : ""}connected {when(c.created_at)}
                </span>
              </div>
              <div className="flex flex-wrap gap-2">
                <Link href={`/connections/${k.id}`} className={c.state === "ok" ? linkButton : linkPrimary}>
                  {c.state === "ok" ? "Change access" : "Sign in again"}
                </Link>
                <Button type="button" onClick={() => remove(c)} className="text-bad-text">
                  Disconnect
                </Button>
              </div>
            </div>
            {c.state === "needs_you" && (
              <div className="border-b border-warn-text/20 bg-warn-soft px-5 py-3 text-sm">
                {k.name} signed Tinio out of this account. Until you sign in again, your agents can&apos;t use it; nothing else stops.
              </div>
            )}
            <div className="flex flex-col px-5 py-1.5">
              {k.parts.map((p) => {
                const on = c.access.parts.includes(p.id);
                return (
                  <div key={p.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-line py-3 first:border-t-0">
                    <div className="flex flex-[0_0_220px] items-center gap-3">
                      <ServiceLogo service={logoFor(p.id)} size={36} />
                      <b className="text-[15px]">{p.title}</b>
                    </div>
                    <span className="min-w-0 flex-[1_1_300px] text-sm text-muted">
                      {p.note}
                      {p.id === "drive" && on && c.access.files && c.access.files.length > 0 && ` Picked: ${c.access.files.map((f) => f.name).join(", ")}.`}
                    </span>
                    <span
                      className={`flex-none rounded-full px-2.5 py-0.5 text-xs font-semibold ${p.later ? "bg-surface-2 text-muted" : on ? "bg-good-soft text-good-text" : "bg-surface-2 text-muted"}`}
                    >
                      {p.later ? "Later" : on ? "On" : "Off"}
                    </span>
                  </div>
                );
              })}
            </div>
            <div className="flex flex-col gap-2.5 border-t border-line px-5 pt-3.5 pb-[18px]">
              <span className="text-sm font-semibold">Who may use it</span>
              <WhoToggles connection={c} onSaved={load} />
            </div>
          </article>
        );
      })}

      {lists && (
        <section className="flex flex-col gap-3">
          <h2 className={heading}>{!connectionsOpen ? "What you'll connect" : lists.connections.length ? "More to connect" : "Connect an account"}</h2>
          <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
            {lists.connectors
              .filter((k) => !connected.has(k.id))
              .map((k) => (
                <div key={k.id} className="flex flex-col gap-2.5 rounded-lg border border-line p-4">
                  <ServiceLogo service={k.id} size={44} />
                  <b className="text-[15px]">{k.name}</b>
                  <span className="flex-grow text-[13px] text-muted">{k.about}</span>
                  {!connectionsOpen ? (
                    <span className="self-start rounded-full bg-surface-2 px-2.5 py-1 text-xs font-semibold text-muted">Coming soon</span>
                  ) : k.ready ? (
                    <Link href={`/connections/${k.id}`} className={`${linkButton} self-start`}>
                      Connect
                    </Link>
                  ) : (
                    <span className="text-[13px] font-semibold text-muted">Not set up on this Tinio yet</span>
                  )}
                </div>
              ))}
            {["Notion", "Slack", "Telegram"].map((name) => (
              <div key={name} className="flex flex-col gap-2.5 rounded-lg border border-line p-4">
                <ServiceLogo service={name.toLowerCase()} size={44} />
                <b className="text-[15px]">{name}</b>
                <span className="text-[13px] font-semibold text-muted">Coming later</span>
              </div>
            ))}
          </div>
        </section>
      )}
    </Page>
  );
}

declare global {
  interface Window {
    gapi?: { load: (what: string, done: () => void) => void };
    google?: { picker: any }; // Google's picker library, untyped
  }
}

// pickFiles opens Google's own file picker in the owner's browser, with a fresh token
// for this app's files only: the files picked are the only ones agents may read.
async function pickFiles(connection: string): Promise<PickedFile[] | string> {
  const res = await call<{ token: string; api_key: string; app_id: string }>(`/connections/${connection}/picker`);
  if (!res.ok) return res.error.message;
  if (!window.gapi) {
    await new Promise<void>((done, fail) => {
      const s = document.createElement("script");
      s.src = "https://apis.google.com/js/api.js";
      s.onload = () => done();
      s.onerror = () => fail(new Error("Google's picker didn't load"));
      document.head.appendChild(s);
    }).catch(() => undefined);
  }
  if (!window.gapi) return "Google's file picker didn't load. Try again.";
  await new Promise<void>((done) => window.gapi!.load("picker", done));
  const p = window.google!.picker;
  return new Promise((done) => {
    new p.PickerBuilder()
      .addView(new p.DocsView().setIncludeFolders(false).setSelectFolderEnabled(false))
      .enableFeature(p.Feature.MULTISELECT_ENABLED)
      .setOAuthToken(res.data.token)
      .setDeveloperKey(res.data.api_key)
      .setAppId(res.data.app_id)
      .setCallback((d: { action: string; docs?: { id: string; name: string; mimeType: string }[] }) => {
        if (d.action === p.Action.PICKED) done((d.docs ?? []).map((x) => ({ id: x.id, name: x.name, mime: x.mimeType })));
        if (d.action === p.Action.CANCEL) done([]);
      })
      .build()
      .setVisible(true);
  });
}

export function ConnectPage({ connector, connected, forId, failed }: { connector: string; connected?: string; forId?: string; failed?: string }) {
  const { lists, error, setError, load } = useLists();
  const { overview } = useFrame();
  const k = lists?.connectors.find((x) => x.id === connector);
  const conn = lists?.connections.find((c) => c.connector === connector);
  const [parts, setParts] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const from = overview?.agents.find((a) => a.id === forId);

  useEffect(() => {
    if (k && parts === null) setParts(conn?.access.parts ?? k.parts.filter((p) => p.default && !p.later).map((p) => p.id));
  }, [k, conn, parts]);

  async function signIn() {
    setBusy(true);
    setError("");
    const res = await call<{ url: string }>("/connections/start", { connector, parts, agent: forId ?? "" });
    if (!res.ok) {
      setBusy(false);
      return setError(res.error.message);
    }
    window.location.assign(res.data.url);
  }

  async function pick() {
    if (!conn) return;
    setBusy(true);
    setError("");
    const picked = await pickFiles(conn.id);
    if (typeof picked === "string") {
      setBusy(false);
      return setError(picked);
    }
    if (picked.length) {
      const all = [...(conn.access.files ?? []), ...picked.filter((f) => !conn.access.files?.some((x) => x.id === f.id))];
      const res = await call(`/connections/${conn.id}/files`, { files: all });
      if (!res.ok) setError(res.error.message);
    }
    setBusy(false);
    load();
  }

  async function unpick(id: string) {
    if (!conn) return;
    const res = await call(`/connections/${conn.id}/files`, { files: (conn.access.files ?? []).filter((f) => f.id !== id) });
    if (!res.ok) return setError(res.error.message);
    load();
  }

  if (!connectionsOpen) {
    return (
      <Page>
        <BackLink />
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{k ? `Connect ${k.name}` : "Connect"}</h1>
        <p role="status" className="m-0 max-w-[62ch] text-muted">Connecting accounts is coming soon.</p>
      </Page>
    );
  }
  const step = connected && conn ? (done ? "done" : "who") : "what";
  const steps = ["1 · What they may do", `2 · Sign in with ${k?.name ?? "it"}`, "3 · Who may use it"];
  const at = step === "what" ? 0 : 2;
  return (
    <Page>
      {from ? <Back id={from.id} /> : <BackLink />}
      <div aria-hidden="true" className="flex items-center gap-3.5">
        <ServiceLogo service="tinio" size={56} />
        <svg width="36" height="20" viewBox="0 0 36 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-muted">
          <path d="M3 6h28l-5-4" />
          <path d="M33 14H5l5 4" />
        </svg>
        <ServiceLogo service={connector} size={56} />
      </div>
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{k ? `Connect ${k.name}` : "Connect"}</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          Connect it once for your account, then choose which agents may use it.{from && ` Opened from ${from.name}, so they're chosen.`}
        </p>
      </div>
      <ol aria-label="Steps" className="m-0 flex list-none flex-wrap gap-2 p-0 text-[13px] font-semibold">
        {steps.map((label, i) => (
          <li
            key={label}
            aria-current={i === at ? "step" : undefined}
            className={`rounded-full px-3 py-1.5 ${i === at && step !== "done" ? "bg-accent-soft text-accent-text" : i < at || step === "done" ? "bg-good-soft text-good-text" : "text-muted shadow-[inset_0_0_0_1px_var(--color-line)]"}`}
          >
            {label}
          </li>
        ))}
      </ol>
      {failed && <p role="alert" className="m-0 text-sm text-bad-text">{failed === "declined" ? "The sign-in was cancelled, so nothing was connected." : "That sign-in didn't finish. Try again."}</p>}
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}

      {k && step === "what" && parts && (
        <section className={box}>
          <h2 className={heading}>What they may do</h2>
          <span className="-mt-1.5 text-sm text-muted">
            {k.name} asks you to allow only what you choose here. Anything that changes something still asks you first, unless you change that in an
            agent&apos;s settings.
          </span>
          {k.parts.map((p) => {
            const on = parts.includes(p.id);
            return (
              <button
                key={p.id}
                type="button"
                aria-pressed={on}
                disabled={p.later}
                onClick={() => setParts(on ? parts.filter((x) => x !== p.id) : [...parts, p.id])}
                className={`flex w-full items-start gap-3 rounded-lg px-4 py-3.5 text-left disabled:cursor-default disabled:bg-surface-2 ${
                  on ? "bg-accent-soft/50 shadow-[inset_0_0_0_2px_var(--color-accent)]" : "shadow-[inset_0_0_0_1px_var(--color-line)]"
                }`}
              >
                <span
                  aria-hidden="true"
                  className={`mt-px grid size-[22px] flex-none place-items-center rounded-md text-[13px] font-bold ${p.later ? "bg-line text-muted" : on ? "bg-accent text-accent-ink" : "shadow-[inset_0_0_0_2px_var(--color-line-strong)]"}`}
                >
                  {on ? "✓" : ""}
                </span>
                <ServiceLogo service={logoFor(p.id)} size={40} />
                <span className="flex min-w-0 flex-col gap-0.5">
                  <b className="text-[15px]">{p.title}</b>
                  <span className="text-[13.5px] text-muted">{p.note}</span>
                </span>
              </button>
            );
          })}
          <div>
            <Button kind="primary" type="button" disabled={busy || !k.ready || parts.length === 0} onClick={signIn}>
              {busy ? "Opening…" : k.ready ? `Next: sign in with ${k.name}` : "Not set up on this Tinio yet"}
            </Button>
          </div>
          <span className="text-[13px] text-muted">{k.name} opens its own sign-in. Tinio never sees your password.</span>
        </section>
      )}

      {k && conn && step === "who" && (
        <section className={box}>
          <h2 className={heading}>Who may use it</h2>
          <span className="-mt-1.5 text-sm text-muted">
            Connected{conn.label ? ` as ${conn.label}` : ""}. Only the agents you choose see it; you can change this on Connections any time.
          </span>
          <WhoToggles connection={conn} onSaved={load} />
          {connector === "google" && conn.access.parts.includes("drive") && (
            <div className="flex flex-col gap-2.5 border-t border-line pt-3.5">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <b className="text-[15px]">Files they may read</b>
                <Button type="button" disabled={busy} onClick={pick} className="min-h-9 px-3.5 text-[13px]">
                  {busy ? "Opening…" : "Pick files in Google Drive"}
                </Button>
              </div>
              {(conn.access.files ?? []).map((f) => (
                <div key={f.id} className="flex items-center justify-between gap-2.5 rounded-lg bg-surface-2 px-3 py-2 text-sm">
                  <span className="flex items-center gap-2.5">
                    <ServiceLogo service={f.mime.includes("document") ? "docs" : "drive"} size={28} />
                    {f.name}
                  </span>
                  <button type="button" onClick={() => unpick(f.id)} className="text-[13px] font-semibold text-accent-text">
                    Remove
                  </button>
                </div>
              ))}
              <span className="text-[13px] text-muted">Only the files you pick here, and the docs they make in a &quot;Tinio&quot; folder. Nothing else in your Drive.</span>
            </div>
          )}
          <div>
            <Button kind="primary" type="button" onClick={() => setDone(true)}>
              Done
            </Button>
          </div>
        </section>
      )}

      {k && step === "done" && (
        <section role="status" className="flex flex-col gap-2.5 rounded-lg border border-good-text/20 bg-good-soft px-[22px] py-5">
          <b className="text-base text-good-text">{k.name} is connected</b>
          <span className="text-sm text-good-text">The agents you chose have it within a minute.</span>
          <div className="flex flex-wrap gap-2">
            {from && (
              <Link href={`/agents/${from.id}/tools`} className={linkButton}>
                Back to {from.name}&apos;s tools
              </Link>
            )}
            <Link href="/connections" className={linkButton}>
              All connections
            </Link>
          </div>
        </section>
      )}
    </Page>
  );
}

function BackLink() {
  return (
    <Link href="/connections" className="-mb-2 -ml-2 inline-flex min-h-11 items-center self-start rounded-full px-2 text-sm font-semibold text-accent-text no-underline hover:bg-accent-soft">
      ‹ Connections
    </Link>
  );
}

const verdictWord: Record<string, string> = { auto: "On their own", ask: "Asks you", forbid: "Never" };

const icon = (d: string) => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d={d} />
  </svg>
);

// What every agent has, whatever you connect. web: it reads from the web (Wikipedia,
// Gutenberg), so their settings' "Look things up on the web" turns it on or off. Web
// search isn't given to agents yet.
const builtIn = [
  { title: "Wikipedia", note: "Look a topic up and read the article.", icon: icon("M4 5h16M4 12h16M4 19h10"), web: true },
  { title: "Books", note: "Read public-domain classics from Project Gutenberg, a passage a day, in their own time.", icon: icon("M4 5a2 2 0 0 1 2-2h12v18H6a2 2 0 0 1-2-2zM8 3v18"), web: true },
  { title: "Make files", note: "PDF, Word, spreadsheets, calendar files and music, sent to you in the chat.", icon: icon("M6 3h8l4 4v14H6zM14 3v4h4"), web: false },
  { title: "Their journal, reminders and promises", note: "Kept for them, and for you to see.", icon: icon("M5 4h14v16H5zM9 8h6M9 12h6"), web: false },
];

export function AgentToolsPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [t, setT] = useState<AgentTools | null>(null);
  const [error, setError] = useState("");
  const load = () => call<AgentTools>(`/agents/${id}/tools`).then((res) => (res.ok ? setT(res.data) : setError(res.error.message)));
  useEffect(() => {
    if (account) load();
  }, [account, id]);

  async function use(connection: string, on: boolean) {
    const res = await call(`/agents/${id}/tools/${connection}`, { use: on });
    if (!res.ok) return setError(res.error.message);
    load();
  }

  if (missing) return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  const name = agent?.name ?? "They";
  const pill = (v: string) => `flex-none rounded-full px-2.5 py-0.5 text-xs font-semibold ${v === "auto" ? "bg-good-soft text-good-text" : v === "ask" ? "bg-warn-soft text-warn-text" : "bg-surface-2 text-muted"}`;
  return (
    <div className="flex max-w-[860px] flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">{agent ? `${agent.name}'s tools` : "Tools"}</h1>
        <p className="m-0 max-w-[60ch] text-muted">What {name} can use beyond your chat. They read on their own; a change asks you first, as their settings say.</p>
      </div>
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}

      {t && (
        <>
          <AgentFolders id={id} name={name} changes={t.changes} />
          <section className="flex flex-col gap-2.5">
            <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">From your connections</h2>
            {t.given.map((c) => (
              <article key={c.id} className="flex flex-col gap-2.5 rounded-lg border border-line px-[18px] py-4">
                <div className="flex flex-wrap items-center justify-between gap-2.5">
                  <div className="flex items-center gap-3">
                    <ServiceLogo service={c.connector} size={44} />
                    <div className="flex flex-col gap-0.5">
                      <b className="text-base">{c.name}</b>
                      <span className="text-[13px] text-muted">
                        {c.label || "Connected"}
                        {c.state === "needs_you" && " · needs you to sign in again"}
                      </span>
                    </div>
                  </div>
                  <Button type="button" onClick={() => use(c.id, false)} className="min-h-10 px-4 text-[13px]">
                    Stop {name} using it
                  </Button>
                </div>
                {c.parts.map((p) => (
                  <div key={p.id} className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 border-t border-line pt-2.5">
                    <span className="flex min-w-0 flex-[1_1_320px] items-center gap-3 text-sm">
                      <ServiceLogo service={logoFor(p.id)} size={32} />
                      <span>
                        <b className="font-semibold">{p.title}.</b> <span className="text-muted">{p.note}</span>
                      </span>
                    </span>
                    <span className={pill(p.id === "calendar" ? "auto" : t.changes)}>
                      {p.id === "calendar"
                        ? "Reads on their own"
                        : p.id === "calendar_add"
                          ? `Changes: ${verdictWord[t.changes].toLowerCase()}`
                          : `Reads on their own · changes: ${verdictWord[t.changes].toLowerCase()}`}
                    </span>
                  </div>
                ))}
              </article>
            ))}
            {t.offer.map((c) => (
              <article key={c.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-accent/20 bg-accent-soft/40 px-[18px] py-4">
                <ServiceLogo service={c.connector} size={44} />
                <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
                  <b className="text-base">You&apos;ve connected {c.name}</b>
                  <span className="text-sm">
                    {c.label && `${c.label}. `}Let {name} use it too, for what you allowed: {c.parts.map((p) => p.title).join("; ")}.
                  </span>
                </div>
                <Button kind="primary" type="button" onClick={() => use(c.id, true)}>
                  Let {name} use it
                </Button>
              </article>
            ))}
            {t.given.length === 0 && t.offer.length === 0 && (
              <article className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed border-line-strong px-[18px] py-4">
                <div className="flex gap-1.5" aria-hidden="true">
                  <ServiceLogo service="calendar" size={36} />
                  <ServiceLogo service="drive" size={36} />
                  <ServiceLogo service="atlassian" size={36} />
                </div>
                <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
                  <b className="text-base">Nothing connected yet</b>
                  <span className="text-sm">
                    Soon you&apos;ll connect Google so {name} can plan around your calendar and work from your files, or Atlassian for Jira and Confluence.
                  </span>
                </div>
                {connectionsOpen ? (
                  <div className="flex flex-wrap gap-2">
                    <Link href={`/connections/google?for=${id}`} className={linkPrimary}>
                      Connect Google
                    </Link>
                    <Link href={`/connections/atlassian?for=${id}`} className={linkButton}>
                      Connect Atlassian
                    </Link>
                  </div>
                ) : (
                  <span className="self-start rounded-full bg-surface-2 px-2.5 py-1 text-xs font-semibold text-muted">Coming soon</span>
                )}
              </article>
            )}
            <Link href="/connections" className="self-start text-sm font-semibold">
              All your connections ›
            </Link>
          </section>

          {t.lately.length > 0 && (
            <section className="flex flex-col gap-2">
              <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">Lately</h2>
              {t.lately.map((l) => (
                <div key={l.at + l.what} className="flex flex-wrap justify-between gap-x-3 gap-y-1 border-b border-line py-2 text-sm">
                  <span className="min-w-0 [overflow-wrap:anywhere]">{l.what}</span>
                  <span className="text-muted">{when(l.at)}</span>
                </div>
              ))}
            </section>
          )}

          <section className="flex flex-col gap-2.5">
            <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">Built in</h2>
            {builtIn.map((b) => (
              <div key={b.title} className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 rounded-lg border border-line px-[18px] py-3">
                <span className="grid size-9 flex-none place-items-center rounded-lg bg-accent-soft text-accent-text" aria-hidden="true">
                  {b.icon}
                </span>
                <span className="min-w-0 flex-[1_1_300px] text-sm">
                  <b className="font-semibold">{b.title}.</b> <span className="text-muted">{b.note}</span>
                </span>
                {b.web && <span className={pill(t.web === "forbid" ? "forbid" : "auto")}>{t.web === "forbid" ? "Off in their settings" : "On their own"}</span>}
              </div>
            ))}
          </section>

          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-lg bg-surface-2 px-4 py-3.5 text-sm">
            <span>
              <b>Change things with tools you connect:</b> {verdictWord[t.changes].toLowerCase()}
            </span>
            <Link href={`/agents/${id}/settings`} className="font-semibold">
              Change in their settings
            </Link>
          </div>
        </>
      )}
    </div>
  );
}
