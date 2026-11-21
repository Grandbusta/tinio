"use client";

import { Children, isValidElement, useState, type ReactNode } from "react";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";

// An agent's message, as they wrote it in Markdown: lists, bold, links and code, every
// line break kept. Code blocks are coloured by their language, on a dark block in either
// theme, with a copy button. Raw HTML is never rendered, links open in a new tab, and
// headings are only bold: a message isn't a document.
export function MessageText({ text }: { text: string }) {
  return (
    <div className="message-text">
      <Markdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]}
        components={{
          a: ({ node: _, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer nofollow" />,
          h1: Strong,
          h2: Strong,
          h3: Strong,
          h4: Strong,
          h5: Strong,
          h6: Strong,
          pre: CodeBlock,
        }}
      >
        {text}
      </Markdown>
    </div>
  );
}

// plainText is a message's words without their Markdown, for a one-line preview: code,
// emphasis, headings, quotes and list marks taken off, a link by its words.
export function plainText(md: string) {
  return md
    .replace(/```[\w+#-]*\n?/g, "")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/(\*\*|\*)(\S[^*]*?)\1/g, "$2")
    .replace(/^\s*(#{1,6}|>|[-*+]|\d+\.)\s+/gm, "")
    .replace(/\s+/g, " ")
    .trim();
}

function Strong({ children }: { children?: ReactNode }) {
  return (
    <p>
      <b>{children}</b>
    </p>
  );
}

// CodeBlock is a fenced block: its language named, its code coloured, and Copy.
function CodeBlock({ children }: { children?: ReactNode }) {
  const [copied, setCopied] = useState(false);
  const code = Children.toArray(children).find(isValidElement) as { props: { className?: string; children?: ReactNode } } | undefined;
  const language = /language-([\w+#-]+)/.exec(code?.props.className ?? "")?.[1] ?? "";
  async function copy() {
    await navigator.clipboard.writeText(textOf(code?.props.children).replace(/\n$/, ""));
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }
  return (
    <div className="code-block">
      <div className="flex items-center justify-between border-b border-white/10 bg-white/5 py-1 pr-1.5 pl-3.5">
        <span className="text-xs font-semibold tracking-[0.02em] text-[#8A919B]">{names[language] ?? (language || "Code")}</span>
        <button type="button" onClick={copy} className="inline-flex min-h-8 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold text-[#C9D1DB] hover:bg-white/10 hover:text-white">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <rect x="9" y="9" width="11" height="11" rx="2" />
            <path d="M5 15V5a2 2 0 0 1 2-2h8" />
          </svg>
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre>{children}</pre>
    </div>
  );
}

// textOf is the text in rendered children: the code, without the colouring's spans.
function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement(node)) return textOf((node.props as { children?: ReactNode }).children);
  return "";
}

const names: Record<string, string> = {
  js: "JavaScript",
  javascript: "JavaScript",
  ts: "TypeScript",
  typescript: "TypeScript",
  tsx: "TSX",
  jsx: "JSX",
  py: "Python",
  python: "Python",
  go: "Go",
  rb: "Ruby",
  ruby: "Ruby",
  sh: "Shell",
  bash: "Shell",
  shell: "Shell",
  sql: "SQL",
  json: "JSON",
  yaml: "YAML",
  yml: "YAML",
  html: "HTML",
  css: "CSS",
  java: "Java",
  kotlin: "Kotlin",
  swift: "Swift",
  rust: "Rust",
  php: "PHP",
  csharp: "C#",
  cpp: "C++",
  c: "C",
  md: "Markdown",
  markdown: "Markdown",
};
