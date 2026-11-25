import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { Avatar, useFrame } from "@/components/app-frame";
import { box, heading } from "@/components/kit";
import { inTheApp } from "@/components/this-app";
import { call, type Verdict } from "@/lib/api";

// Folders on this computer the owner adds, for the agents they choose: Read, or Read and
// make files (each change asked first, unless an agent's settings say otherwise). What's
// enforced is in Tinio's Go (internal/folders); this is where they're added and given.

export type Folder = {
  id: string;
  name: string;
  path: string;
  write: boolean;
  agents: string[];
  files: number;
  skipped: number;
  more: boolean;
  missing: boolean;
};

type Peek = { name: string; path: string; files: number; skipped: number; more: boolean; home: boolean };

export function useFolders() {
  const [folders, setFolders] = useState<Folder[] | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    call<{ folders: Folder[] }>("/folders").then((res) => (res.ok ? setFolders(res.data.folders) : setError(res.error.message)));
  }, []);
  useEffect(load, [load]);
  // A change answers with every folder as it is now.
  const change = useCallback(async (path: string, body: unknown) => {
    const res = await call<{ folders: Folder[]; folder?: string }>(path, body);
    if (!res.ok) {
      setError(res.error.message);
      return null;
    }
    setError("");
    setFolders(res.data.folders);
    return res.data;
  }, []);
  return { folders, error, setError, change, load };
}

export function FolderGlyph({ size = 24, className = "" }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" aria-hidden="true" className={className}>
      <path d="M3 7.5A1.5 1.5 0 0 1 4.5 6H9l2 2h8.5A1.5 1.5 0 0 1 21 9.5v8a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z" />
    </svg>
  );
}

function FolderTile({ size = 44 }: { size?: number }) {
  return (
    <span aria-hidden="true" style={{ width: size, height: size }} className="grid flex-none place-items-center rounded-[10px] bg-accent-soft text-accent-text">
      <FolderGlyph size={Math.round(size * 0.55)} />
    </span>
  );
}

function counted(f: { files: number; skipped: number; more: boolean }) {
  const files = `${f.more ? "More than " : ""}${f.files.toLocaleString("en-GB")} ${f.files === 1 ? "file" : "files"}`;
  return f.skipped ? `${files} · ${f.skipped.toLocaleString("en-GB")} never read: hidden files, keys and shortcuts` : files;
}

const seg = "inline-flex flex-wrap gap-1 self-start rounded-xl border border-line bg-surface-2 p-1";
const segBtn = (on: boolean) => `min-h-9 rounded-lg px-3 text-[13px] font-semibold ${on ? "bg-bg text-accent-text shadow-sm" : "text-muted hover:text-fg"}`;
const whoBtn = (on: boolean) =>
  `inline-flex min-h-10 items-center gap-2 rounded-lg py-1 pr-3.5 pl-1 text-sm font-semibold ${on ? "border-2 border-accent bg-accent-soft/50 text-accent-text" : "border border-line bg-bg text-fg"}`;
const btn = "inline-flex min-h-10 items-center justify-center gap-2 rounded-lg border border-line-strong bg-bg px-4 text-[13px] font-semibold text-fg hover:border-fg disabled:opacity-50";
const primary = "inline-flex min-h-10 items-center justify-center gap-2 rounded-lg bg-accent px-4 text-[13px] font-semibold text-accent-ink no-underline hover:bg-accent-hover hover:text-accent-ink disabled:opacity-50";

// FoldersSection is Folders in Settings: every folder, what agents may do in it and
// who may use it; adding and removing.
export function FoldersSection() {
  const { overview } = useFrame();
  const agents = overview?.agents ?? [];
  const { folders, error, change } = useFolders();
  const [asking, setAsking] = useState("");
  return (
    <section id="folders" aria-labelledby="folders-h" className={box}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-[1_1_420px] flex-col gap-1">
          <h2 id="folders-h" className={heading}>Folders</h2>
          <span className="text-[13px] text-muted">
            Folders on this computer your agents may use, and nothing outside them. They read on their own; making or changing a file asks you first. Nothing is
            ever deleted.
          </span>
        </div>
        <Link href="/settings/folders/add" className={primary}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
            <path d="M12 5v14" />
            <path d="M5 12h14" />
          </svg>
          Add a folder
        </Link>
      </div>
      {error && <p role="alert" className="m-0 text-[13px] text-bad-text">{error}</p>}
      {folders?.length === 0 && (
        <div className="flex flex-col items-center gap-2.5 rounded-lg border border-dashed border-line-strong px-5 py-7 text-center">
          <FolderGlyph size={40} className="text-muted" />
          <b className="text-[15px]">No folders yet</b>
          <span className="max-w-[52ch] text-[13px] text-muted">
            Add the folder where you keep your work, and choose which agents may use it. They&apos;ll read what a task needs, and draft new files there for your yes.
          </span>
        </div>
      )}
      {folders?.map((f) => (
        <article key={f.id} className="overflow-hidden rounded-lg border border-line">
          <div className="flex flex-wrap items-center gap-3 border-b border-line bg-surface-2 px-[18px] py-3.5">
            <FolderTile />
            <div className="flex min-w-0 flex-[1_1_260px] flex-col">
              <b className="text-[15px]">{f.name}</b>
              <span className="font-mono text-xs break-all text-muted">{f.path}</span>
              <span className={`text-xs ${f.missing ? "text-bad-text" : "text-muted"}`}>{f.missing ? "This folder isn't there any more: moved, renamed or deleted." : counted(f)}</span>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {inTheApp() && !f.missing && (
                <button type="button" className={btn} onClick={() => call("/app/show-folder", { id: f.id })}>
                  Show in Finder
                </button>
              )}
              <button type="button" className={btn} onClick={() => setAsking(f.id)}>
                Remove
              </button>
            </div>
          </div>
          {asking === f.id && (
            <div role="alert" className="flex flex-wrap items-center justify-between gap-2.5 border-b border-line bg-bad-soft px-[18px] py-3 text-sm">
              <span>Your agents stop using {f.name}. The folder and its files stay just as they are.</span>
              <span className="flex gap-2">
                <button type="button" className="inline-flex min-h-10 items-center rounded-lg bg-bad px-4 text-[13px] font-semibold text-white" onClick={() => change(`/folders/${f.id}/remove`, {})}>
                  Remove it
                </button>
                <button type="button" className={btn} onClick={() => setAsking("")}>
                  Cancel
                </button>
              </span>
            </div>
          )}
          <div className="flex flex-col gap-3.5 px-[18px] py-3.5">
            <div className="flex flex-wrap items-center justify-between gap-2.5">
              <div className="flex min-w-0 flex-[1_1_260px] flex-col">
                <b className="text-sm font-semibold">What they may do</b>
                <span className="text-[13px] text-muted">
                  {f.write ? "List, search and read what's in it, and make or change text files, each with your yes." : "List, search and read what's in it. Nothing in it changes."}
                </span>
              </div>
              <div role="group" aria-label={`What they may do in ${f.name}`} className={seg}>
                <button type="button" aria-pressed={!f.write} className={segBtn(!f.write)} onClick={() => change(`/folders/${f.id}`, { write: false })}>
                  Read
                </button>
                <button type="button" aria-pressed={f.write} className={segBtn(f.write)} onClick={() => change(`/folders/${f.id}`, { write: true })}>
                  Read and make files
                </button>
              </div>
            </div>
            <div className="flex flex-col gap-2 border-t border-line pt-3">
              <b className="text-sm font-semibold">Who may use it</b>
              <div role="group" aria-label={`Who may use ${f.name}`} className="flex flex-wrap gap-2">
                {agents.map((a) => {
                  const on = f.agents.includes(a.id);
                  return (
                    <button
                      key={a.id}
                      type="button"
                      aria-pressed={on}
                      className={whoBtn(on)}
                      onClick={() => change(`/folders/${f.id}`, { agents: on ? f.agents.filter((x) => x !== a.id) : [...f.agents, a.id] })}
                    >
                      <Avatar agent={a} size={30} />
                      {a.name}
                    </button>
                  );
                })}
              </div>
              {f.agents.length === 0 && <span className="text-xs text-muted">No agent uses it. It stays here until you remove it.</span>}
            </div>
          </div>
        </article>
      ))}
      <details className="border-t border-line pt-3 text-sm">
        <summary className="cursor-pointer font-semibold text-accent-text">What they never see, even in a folder you add</summary>
        <ul className="mt-2 mb-0 flex flex-col gap-1 pl-[18px] text-fg/80">
          <li>Keys and passwords: .env files, .ssh, keys, keychains and password stores</li>
          <li>Hidden files and folders, browser profiles, and Tinio&apos;s own data</li>
          <li>Anything outside the folder, even through a shortcut that points out of it</li>
        </ul>
        <span className="mt-2 block text-muted">A file&apos;s words are material for their work, never instructions to them.</span>
      </details>
    </section>
  );
}

// pick is a folder chosen with the computer's own folder window ("" if it was
// closed); null outside the app, where the path is typed instead.
async function pick(): Promise<{ path: string } | { error: string } | null> {
  if (!inTheApp()) return null;
  const res = await call<{ path: string }>("/app/pick-folder", {});
  return res.ok ? { path: res.data.path } : { error: res.error.message };
}

// AddFolderPage is adding a folder: pick it, then what they may do and who.
// ?for=<agent> chooses that agent to start with.
export function AddFolderPage() {
  const { overview, refresh } = useFrame();
  const agents = overview?.agents ?? [];
  const forId = new URLSearchParams(window.location.search).get("for") ?? "";
  const [peek, setPeek] = useState<Peek | null>(null);
  const [typed, setTyped] = useState("");
  const [write, setWrite] = useState(false);
  const [who, setWho] = useState<string[]>(forId ? [forId] : []);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [added, setAdded] = useState<string[] | null>(null);
  const app = inTheApp();
  useEffect(() => {
    if (!forId && agents.length === 1 && who.length === 0) setWho([agents[0].id]);
  }, [agents, forId, who.length]);

  async function look(path: string) {
    if (!path) return;
    setBusy(true);
    const res = await call<Peek>(`/folders/peek?path=${encodeURIComponent(path)}`);
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    setError("");
    setPeek(res.data);
  }
  async function choose() {
    const got = await pick();
    if (!got) return;
    if ("error" in got) return setError(got.error);
    look(got.path);
  }
  async function add() {
    if (!peek) return;
    setBusy(true);
    const res = await call<{ folders: Folder[] }>("/folders", { path: peek.path, write, agents: who });
    setBusy(false);
    if (!res.ok) return setError(res.error.message);
    setAdded(who);
    refresh();
  }
  const names = (ids: string[]) => ids.map((id) => agents.find((a) => a.id === id)?.name ?? "").filter(Boolean);
  const list = (ns: string[]) => (ns.length <= 1 ? (ns[0] ?? "") : `${ns.slice(0, -1).join(", ")} and ${ns[ns.length - 1]}`);

  return (
    <div className="mx-auto flex w-full max-w-[880px] flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-4">
      <Link href="/settings#folders" className="-ml-2 inline-flex min-h-11 items-center self-start rounded-lg px-2 text-sm font-semibold text-accent-text no-underline hover:bg-accent-soft">
        ‹ Settings
      </Link>
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Add a folder</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          {app ? "Pick a folder with your computer's own window," : "Give the folder's full path,"} then choose what your agents may do in it and who may use it. You
          can change both, or remove it, in Settings any time.
        </p>
      </div>
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}

      {added ? (
        <section role="status" className="flex flex-col gap-2.5 rounded-lg border border-good/30 bg-good-soft px-[22px] py-5">
          <b className="text-base text-good-text">{peek?.name} is added</b>
          <span className="text-sm text-good-text">
            {list(names(added))} {added.length === 1 ? "has" : "have"} it within a minute.
            {write ? " New files and changes there wait for your yes in the chat." : ""}
          </span>
          <div className="flex flex-wrap gap-2">
            <Link href="/settings#folders" className={btn}>
              Back to Settings
            </Link>
            {added[0] && (
              <Link href={`/agents/${added[0]}`} className={btn}>
                Chat with {names([added[0]])[0]}
              </Link>
            )}
          </div>
        </section>
      ) : !peek ? (
        <section className={box}>
          <h2 className={heading}>Pick it</h2>
          {app ? (
            <>
              <span className="text-sm text-fg/80">Your computer&apos;s folder window opens. Tinio gets the one folder you choose, and what&apos;s inside it.</span>
              <button type="button" className={`${primary} self-start`} disabled={busy} onClick={choose}>
                <FolderGlyph size={18} />
                Choose a folder…
              </button>
            </>
          ) : (
            <form
              className="flex flex-col gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                look(typed.trim());
              }}
            >
              <label htmlFor="folder-path" className="text-sm font-semibold">
                The folder&apos;s full path, on the computer Tinio runs on
              </label>
              <div className="flex flex-wrap gap-2">
                <input
                  id="folder-path"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  placeholder="/Users/you/Documents/Shop"
                  className="min-h-11 min-w-0 flex-[1_1_320px] rounded-lg border border-line-strong bg-surface px-3 font-mono text-sm"
                />
                <button type="submit" className={primary} disabled={busy || !typed.trim()}>
                  Look inside
                </button>
              </div>
            </form>
          )}
        </section>
      ) : (
        <section className={box}>
          <div className="flex flex-wrap items-center gap-3">
            <FolderTile size={48} />
            <div className="flex min-w-0 flex-[1_1_260px] flex-col">
              <b className="text-[17px]">{peek.name}</b>
              <span className="font-mono text-xs break-all text-muted">{peek.path}</span>
            </div>
            <button type="button" className={btn} onClick={() => (app ? choose() : setPeek(null))}>
              Pick another
            </button>
          </div>
          {peek.home && (
            <div role="alert" className="flex flex-col gap-1 rounded-lg border border-warn-text/20 bg-warn-soft px-3.5 py-3 text-sm">
              <b>That&apos;s your whole home folder</b>
              <span>Everything you keep would be theirs to read. Pick the folder a job needs, like your shop&apos;s documents.</span>
            </div>
          )}
          <span className="rounded-lg bg-surface-2 px-3.5 py-3 text-sm">{counted(peek)}</span>

          <fieldset className="m-0 flex flex-col gap-2.5 border-0 p-0">
            <legend className="mb-2.5 text-[15px] font-semibold">What they may do</legend>
            {[
              { on: !write, set: false, title: "Read", note: "List, search and read what's in it. Nothing in the folder changes." },
              { on: write, set: true, title: "Read and make files", note: "Also make new text files and change them, each with your yes first, unless an agent's settings say otherwise. Nothing is ever deleted." },
            ].map((o) => (
              <label key={o.title} className={`flex cursor-pointer items-start gap-3 rounded-lg px-4 py-3.5 ${o.on ? "border-2 border-accent bg-accent-soft/40" : "border border-line"}`}>
                <input type="radio" name="may" checked={o.on} onChange={() => setWrite(o.set)} className="mt-1 size-4 accent-[var(--accent)]" />
                <span className="flex flex-col gap-0.5">
                  <b className="text-[15px]">{o.title}</b>
                  <span className="text-[13px] text-muted">{o.note}</span>
                </span>
              </label>
            ))}
          </fieldset>

          <div className="flex flex-col gap-2.5 border-t border-line pt-3.5">
            <b className="text-[15px]">Who may use it</b>
            <div role="group" aria-label="Who may use it" className="flex flex-wrap gap-2">
              {agents.map((a) => {
                const on = who.includes(a.id);
                return (
                  <button key={a.id} type="button" aria-pressed={on} className={whoBtn(on)} onClick={() => setWho(on ? who.filter((x) => x !== a.id) : [...who, a.id])}>
                    <Avatar agent={a} size={32} />
                    {a.name}
                  </button>
                );
              })}
            </div>
            <span className="text-[13px] text-muted">{who.length ? `${list(names(who))} ${who.length === 1 ? "gets" : "get"} it within a minute.` : "Choose at least one agent. You can add the others later."}</span>
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="button" className={primary} disabled={busy || who.length === 0} onClick={add}>
              Add {peek.name}
            </button>
            <Link href="/settings#folders" className={btn}>
              Cancel
            </Link>
          </div>
        </section>
      )}
    </div>
  );
}

// AgentFolders is an agent's folders on their Tools page: what each lets them do, and
// the folders you've added that aren't theirs.
export function AgentFolders({ id, name, changes }: { id: string; name: string; changes?: Verdict }) {
  const { folders, error, change } = useFolders();
  if (!folders) return null;
  const mine = folders.filter((f) => f.agents.includes(id));
  const others = folders.filter((f) => !f.agents.includes(id));
  const pill = (v: string) => `flex-none rounded-md px-2.5 py-0.5 text-xs font-semibold ${v === "auto" ? "bg-good-soft text-good-text" : v === "ask" ? "bg-warn-soft text-warn-text" : "bg-surface-2 text-muted"}`;
  const writes = changes ?? "ask";
  return (
    <section className="flex flex-col gap-2.5">
      <h2 className="font-display text-xl font-semibold tracking-[-0.02em]">Folders on this computer</h2>
      {error && <p role="alert" className="m-0 text-sm text-bad-text">{error}</p>}
      {mine.map((f) => (
        <article key={f.id} className="flex flex-col gap-2.5 rounded-lg border border-line px-[18px] py-4">
          <div className="flex flex-wrap items-center justify-between gap-2.5">
            <div className="flex min-w-0 items-center gap-3">
              <FolderTile />
              <div className="flex min-w-0 flex-col">
                <b className="text-base">{f.name}</b>
                <span className="font-mono text-xs break-all text-muted">{f.path}</span>
              </div>
            </div>
            <button type="button" className={btn} onClick={() => change(`/agents/${id}/folders/${f.id}`, { use: false })}>
              Stop {name} using it
            </button>
          </div>
          <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 border-t border-line pt-2.5">
            <span className="min-w-0 flex-[1_1_320px] text-sm">List, search and read its files</span>
            <span className={pill("auto")}>On their own</span>
          </div>
          {f.write && (
            <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 border-t border-line pt-2.5">
              <span className="min-w-0 flex-[1_1_320px] text-sm">Make a new text file, or change one</span>
              <span className={pill(writes)}>{writes === "auto" ? "On their own" : writes === "ask" ? "Asks you" : "Never"}</span>
            </div>
          )}
          <span className="text-[13px] text-muted">
            {f.missing ? "This folder isn't there any more. " : ""}
            <Link href="/settings#folders">Change it in Settings</Link>
          </span>
        </article>
      ))}
      {others.map((f) => (
        <article key={f.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-accent/20 bg-accent-soft/40 px-[18px] py-4">
          <FolderTile />
          <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
            <b className="text-base">You&apos;ve added {f.name}</b>
            <span className="text-sm text-fg/80">
              Let {name} read what&apos;s in it{f.write ? ", and make files there with your yes" : ""}.
            </span>
          </div>
          <button type="button" className={primary} onClick={() => change(`/agents/${id}/folders/${f.id}`, { use: true })}>
            Let {name} use it
          </button>
        </article>
      ))}
      {folders.length === 0 && (
        <article className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed border-line-strong px-[18px] py-4">
          <div className="flex min-w-0 flex-[1_1_320px] flex-col gap-0.5">
            <b className="text-base">No folders yet</b>
            <span className="text-sm text-fg/80">Add a folder on this computer so {name} can work from your documents, and save what they make there.</span>
          </div>
          <Link href={`/settings/folders/add?for=${id}`} className={primary}>
            Add a folder
          </Link>
        </article>
      )}
    </section>
  );
}

// ComposerFolders is the folders above an agent's message box ("Mei can use"), and the +
// beside it: add a folder for them now, or give them one you've added. A folder added here
// is theirs only, and Read to start.
export function ComposerFolders({ id, name }: { id: string; name: string }) {
  const { folders, error, setError, change } = useFolders();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState<Folder | null>(null);
  const menu = useRef<HTMLDivElement>(null);
  const plus = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: Event) => {
      if (e instanceof KeyboardEvent ? e.key === "Escape" : !menu.current?.contains(e.target as Node) && !plus.current?.contains(e.target as Node)) {
        setOpen(false);
        if (e instanceof KeyboardEvent) plus.current?.focus();
      }
    };
    window.addEventListener("keydown", close);
    window.addEventListener("pointerdown", close);
    return () => {
      window.removeEventListener("keydown", close);
      window.removeEventListener("pointerdown", close);
    };
  }, [open]);

  const mine = (folders ?? []).filter((f) => f.agents.includes(id));
  const others = (folders ?? []).filter((f) => !f.agents.includes(id));
  async function addNew() {
    setOpen(false);
    const got = await pick();
    if (!got) {
      window.history.pushState(null, "", `/settings/folders/add?for=${id}`);
      window.dispatchEvent(new PopStateEvent("popstate"));
      return;
    }
    if ("error" in got) return setError(got.error);
    if (!got.path) return;
    const done = await change("/folders", { path: got.path, write: false, agents: [id] });
    if (done) setNote(done.folders.find((f) => f.id === done.folder) ?? null);
  }
  async function give(f: Folder) {
    setOpen(false);
    const done = await change(`/agents/${id}/folders/${f.id}`, { use: true });
    if (done) setNote(done.folders.find((x) => x.id === f.id) ?? null);
  }
  const shown = note ? (folders ?? []).find((f) => f.id === note.id) ?? note : null;

  return (
    <>
      {(mine.length > 0 || shown || error) && (
        <div className="flex basis-full flex-col gap-2">
          {shown && (
            <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-xl border border-line bg-surface px-3.5 py-2.5 text-sm">
              <FolderGlyph size={18} className="text-accent-text" />
              <span className="min-w-0 flex-[1_1_220px]">
                <b>{shown.name}</b> is {name}&apos;s now · {shown.write ? "Read and make files" : "Read"}
              </span>
              {!shown.write && (
                <button type="button" className={btn} onClick={() => change(`/folders/${shown.id}`, { write: true })}>
                  Let them make files too
                </button>
              )}
              <button type="button" aria-label="Put this away" onClick={() => setNote(null)} className="grid size-8 place-items-center rounded-full text-muted hover:bg-surface-2 hover:text-fg">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
                  <path d="M6 6l12 12" />
                  <path d="M18 6L6 18" />
                </svg>
              </button>
            </div>
          )}
          {error && <span role="alert" className="text-[13px] text-bad-text">{error}</span>}
          {mine.length > 0 && (
            <ul aria-label={`Folders ${name} can use`} className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0 text-[13px]">
              <li className="text-muted">{name} can use</li>
              {mine.map((f) => (
                <li key={f.id} className="inline-flex min-h-8 items-center gap-1.5 rounded-lg border border-line bg-surface py-0.5 pr-1 pl-2.5">
                  <FolderGlyph size={15} className="text-accent-text" />
                  <Link href="/settings#folders" className="font-semibold text-fg no-underline">
                    {f.name}
                  </Link>
                  <span className="text-muted">· {f.write ? "Read and make files" : "Read"}</span>
                  <button
                    type="button"
                    aria-label={`Stop ${name} using ${f.name}`}
                    onClick={() => change(`/agents/${id}/folders/${f.id}`, { use: false })}
                    className="grid size-6 place-items-center rounded-full text-muted hover:bg-line hover:text-fg"
                  >
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round" aria-hidden="true">
                      <path d="M6 6l12 12" />
                      <path d="M18 6L6 18" />
                    </svg>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <div className="relative flex-none">
        {open && (
          <div ref={menu} role="menu" aria-label="Folders" className="absolute bottom-14 left-0 z-20 flex w-[340px] max-w-[calc(100vw-32px)] flex-col gap-0.5 rounded-xl border border-line bg-bg p-1.5 shadow-lg">
            <button type="button" role="menuitem" onClick={addNew} className="flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left hover:bg-surface-2">
              <FolderGlyph size={20} className="mt-0.5 flex-none text-fg/80" />
              <span className="flex flex-col">
                <b className="text-sm">Add a folder…</b>
                <span className="text-[12.5px] text-muted">For {name} to use from now on, read-only to start</span>
              </span>
            </button>
            {others.length > 0 && <div className="mx-1.5 mt-1 border-t border-line px-1.5 pt-2 pb-0.5 text-xs font-semibold text-muted">Folders you&apos;ve added</div>}
            {others.map((f) => (
              <button key={f.id} type="button" role="menuitem" onClick={() => give(f)} className="flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left hover:bg-surface-2">
                <FolderGlyph size={20} className="mt-0.5 flex-none text-fg/80" />
                <span className="flex min-w-0 flex-col">
                  <b className="text-sm">{f.name}</b>
                  <span className="truncate text-[12.5px] text-muted">{f.write ? "Read and make files" : "Read"}</span>
                </span>
              </button>
            ))}
          </div>
        )}
        <button
          ref={plus}
          type="button"
          aria-label="Add a folder"
          aria-haspopup="menu"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
          className={`grid size-12 place-items-center rounded-full border ${open ? "border-fg bg-fg text-bg" : "border-line-strong bg-bg text-fg hover:border-fg"} focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent`}
        >
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
            <path d="M12 5v14" />
            <path d="M5 12h14" />
          </svg>
        </button>
      </div>
    </>
  );
}
