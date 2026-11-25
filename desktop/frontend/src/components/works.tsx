"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import Markdown from "react-markdown";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import { Back, useFrame, when } from "@/components/app-frame";
import { FileIcon } from "@/components/file-icon";
import { call, type Work } from "@/lib/api";

// Step 9: what an agent has made for you, newest first, and each piece to read and download.

// What each format is called on its button.
const formatNames: Record<string, string> = {
  pdf: "PDF",
  docx: "Word",
  xlsx: "Spreadsheet",
  csv: "CSV",
  ics: "Calendar",
  midi: "MIDI",
  zip: "Zip",
};

function Downloads({ id, w }: { id: string; w: Work }) {
  return (
    <>
      {w.formats.map((f) => (
        <a
          key={f}
          href={`/api/agents/${id}/works/${w.id}/files/${f}`}
          download
          className="inline-flex min-h-9 items-center gap-1.5 rounded-full border border-line-strong pr-3 pl-2 text-xs font-semibold text-fg hover:border-fg"
        >
          <FileIcon file={f} size={14} />
          {formatNames[f] ?? f.toUpperCase()}
        </a>
      ))}
    </>
  );
}

// SendToMe has the agent send a work into your chat, as a file in its best format.
function SendToMe({ id, w }: { id: string; w: Work }) {
  const [state, setState] = useState<"idle" | "sending" | "sent" | "failed">("idle");
  async function send() {
    setState("sending");
    const res = await call(`/agents/${id}/works/${w.id}/send`, {});
    setState(res.ok ? "sent" : "failed");
  }
  if (state === "sent") {
    return (
      <Link href={`/agents/${id}`} className="inline-flex min-h-9 items-center px-1 text-xs font-semibold text-good-text">
        Sent to your chat
      </Link>
    );
  }
  return (
    <button
      type="button"
      disabled={state === "sending"}
      onClick={send}
      className="inline-flex min-h-9 items-center rounded-full border border-line-strong px-3 text-xs font-semibold text-fg hover:border-fg disabled:opacity-60"
    >
      {state === "failed" ? "Couldn't send. Try again" : "Send to me"}
    </button>
  );
}

// Words is a work's text as they wrote it, in Markdown: headings, lists, tables and
// emphasis shown, every line break kept (much of it is plain text, like lyrics), and a
// first line that repeats the title left out. Raw HTML is never rendered: what they write
// is shown, not run.
function Words({ title, text }: { title: string; text: string }) {
  let body = text.trim();
  const [first, ...rest] = body.split("\n");
  if (first.replace(/^[\s*#"']+|[\s*#"']+$/g, "").toLowerCase() === title.trim().toLowerCase()) body = rest.join("\n").trim();
  return (
    <div className="work-words">
      <Markdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        components={{ a: ({ node: _, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer nofollow" /> }}
      >
        {body}
      </Markdown>
    </div>
  );
}

function made(w: Work, name: string) {
  return `${when(w.at)} · ${w.for_you ? "made for you" : `${name}'s own`}`;
}

export function WorksPage({ id }: { id: string }) {
  const { account, agent, missing } = useFrame();
  const [works, setWorks] = useState<Work[] | null>(null);
  const [error, setError] = useState("");
  const [kind, setKind] = useState("");

  useEffect(() => {
    if (!account) return;
    call<{ works: Work[] }>(`/agents/${id}/works`).then((res) => (res.ok ? setWorks(res.data.works) : setError(res.error.message)));
  }, [account, id]);

  if (missing) {
    return <p role="alert" className="p-10 text-bad-text">We can&apos;t find that agent among yours.</p>;
  }
  const name = agent?.name ?? "";
  const kinds = [...new Set((works ?? []).map((w) => w.kind))];
  const shown = (works ?? []).filter((w) => !kind || w.kind === kind);
  return (
    <div className="flex flex-col gap-5 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Back id={id} />
      <div className="flex flex-col gap-1.5">
        <h1 className="font-display text-[34px] leading-[1.12] font-medium tracking-[-0.02em]">Works</h1>
        <p className="m-0 max-w-[62ch] text-muted">
          Everything {name} has made, newest first. Read any of it, download it as a file, or have {name} send it to you.
        </p>
      </div>
      {error && <p role="alert" className="text-sm text-bad-text">{error}</p>}
      {kinds.length > 1 && (
        <div role="group" aria-label="Show" className="flex flex-wrap gap-2">
          {["", ...kinds].map((k) => (
            <button
              key={k || "all"}
              type="button"
              aria-pressed={kind === k}
              onClick={() => setKind(k)}
              className={`min-h-10 rounded-full px-4 text-sm first-letter:uppercase ${kind === k ? "bg-fg font-semibold text-bg" : "font-medium text-fg shadow-[inset_0_0_0_1px_var(--color-line)] hover:shadow-[inset_0_0_0_1px_var(--color-fg)]"}`}
            >
              {k || "All"}
            </button>
          ))}
        </div>
      )}
      {works && works.length === 0 && <p className="text-muted">Nothing yet. What {name} makes, for themselves or for you, will be here.</p>}
      <div className="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-3.5">
        {shown.map((w) => (
          <article key={w.id} className="flex flex-col gap-2.5 rounded-lg border border-line p-[18px]">
            <span className="text-xs font-semibold tracking-[0.04em] text-muted uppercase">{w.kind}</span>
            <h2 className="font-display text-lg leading-snug font-semibold tracking-[-0.02em]">
              <Link href={`/agents/${id}/works/${w.id}`} className="text-fg hover:text-accent-text">{w.title}</Link>
            </h2>
            <p className="m-0 line-clamp-3 flex-grow text-sm text-muted">{w.peek}</p>
            <span className="text-xs text-muted">{made(w, name)}</span>
            <div className="flex flex-wrap gap-1.5 border-t border-line pt-2.5">
              <Link href={`/agents/${id}/works/${w.id}`} className="inline-flex min-h-9 items-center rounded-full bg-accent-soft px-3 text-xs font-semibold text-accent-text">
                Read
              </Link>
              <Downloads id={id} w={w} />
              <SendToMe id={id} w={w} />
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}

export function WorkPage({ id, work }: { id: string; work: string }) {
  const { account, agent } = useFrame();
  const [w, setW] = useState<Work | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!account) return;
    call<{ work: Work }>(`/agents/${id}/works/${encodeURIComponent(work)}`).then((res) => (res.ok ? setW(res.data.work) : setError(res.error.message)));
  }, [account, id, work]);

  const name = agent?.name ?? "";
  return (
    <div className="flex max-w-[760px] flex-col gap-5 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-3">
      <Link href={`/agents/${id}/works`} className="-ml-2 inline-flex min-h-11 items-center gap-1 self-start rounded-full px-2 text-sm font-semibold text-accent-text">
        ‹ All of {name ? `${name}'s` : "their"} works
      </Link>
      {error && <p role="alert" className="text-bad-text">{error}</p>}
      {w && (
        <article className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-semibold tracking-[0.04em] text-muted uppercase">{w.kind}</span>
            <h1 className="font-display text-[30px] leading-[1.15] font-medium tracking-[-0.02em]">{w.title}</h1>
            <span className="text-sm text-muted">{made(w, name)}</span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            <Downloads id={id} w={w} />
            <SendToMe id={id} w={w} />
          </div>
          <Words title={w.title} text={w.text ?? ""} />
        </article>
      )}
    </div>
  );
}
