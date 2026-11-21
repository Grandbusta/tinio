"use client";

import Link from "next/link";
import type { ReactNode } from "react";

// The pieces pages share: link styles, a box, a heading, a full-width page.
export const linkButton =
  "inline-flex min-h-11 items-center justify-center rounded-full border border-line-strong bg-bg px-5 text-sm font-semibold text-fg no-underline hover:border-fg hover:text-fg";
export const linkPrimary =
  "inline-flex min-h-11 items-center justify-center rounded-full bg-accent px-5 text-sm font-semibold text-accent-ink no-underline hover:bg-accent-hover hover:text-accent-ink";
export const field = "min-h-11 w-full rounded-lg border border-line-strong bg-surface px-3 text-[15px] font-normal";
export const box = "flex flex-col gap-3 rounded-lg border border-line px-[22px] py-5";
export const heading = "font-display text-lg font-semibold tracking-[-0.02em]";

export function chip(on: boolean) {
  return `min-h-9 rounded-full px-3.5 text-[13px] font-semibold ${on ? "bg-fg text-bg" : "text-fg shadow-[inset_0_0_0_1px_var(--color-line-strong)]"}`;
}

// Page is a full-width page (Connections): on a phone, with a way back to the chats.
export function Page({ children, wide }: { children: ReactNode; wide?: boolean }) {
  return (
    <div className={`mx-auto flex w-full ${wide ? "max-w-[1120px]" : "max-w-[880px]"} flex-col gap-6 px-10 pt-8 pb-12 max-[900px]:px-4 max-[900px]:pt-4`}>
      <Link href="/agents" className="-mb-4 inline-flex min-h-11 items-center gap-1.5 self-start text-sm font-semibold text-accent-text min-[901px]:hidden">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M15 6l-6 6 6 6" />
        </svg>
        Chats
      </Link>
      {children}
    </div>
  );
}

export function BackTo({ href, children }: { href: string; children: ReactNode }) {
  return (
    <Link href={href} className="-mb-2 -ml-2 inline-flex min-h-11 items-center self-start rounded-full px-2 text-sm font-semibold text-accent-text no-underline hover:bg-accent-soft">
      ‹ {children}
    </Link>
  );
}
