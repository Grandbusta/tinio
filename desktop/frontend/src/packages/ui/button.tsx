import type { AnchorHTMLAttributes, ButtonHTMLAttributes } from "react";

// One primary action per view; everything else is secondary.
type Kind = "primary" | "secondary";

const base =
  "inline-flex min-h-11 items-center justify-center gap-2 rounded-full px-5 text-sm font-semibold " +
  "transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent " +
  "disabled:cursor-not-allowed disabled:opacity-50";
const kinds: Record<Kind, string> = {
  primary: "bg-accent text-accent-ink hover:bg-accent-hover",
  secondary: "border border-line-strong text-fg hover:border-fg",
};

export function Button({ kind = "secondary", className = "", ...props }: { kind?: Kind } & ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button className={`${base} ${kinds[kind]} ${className}`} {...props} />;
}

export function ButtonLink({ kind = "secondary", className = "", ...props }: { kind?: Kind } & AnchorHTMLAttributes<HTMLAnchorElement>) {
  return <a className={`${base} ${kinds[kind]} ${className}`} {...props} />;
}
