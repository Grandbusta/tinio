// The wordmark: "tinio", lowercase Manrope ExtraBold, set tight, in the same colour as the
// mark beside it.
export function Wordmark({ className = "" }: { className?: string }) {
  return <span className={`font-display font-extrabold tracking-[-0.045em] ${className}`}>tinio</span>;
}
