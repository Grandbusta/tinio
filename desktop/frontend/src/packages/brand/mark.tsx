import type { SVGProps } from "react";

// The mark: one of the characters as a pixel bust, on an 8 by 8 grid. Always one colour
// (it draws in currentColor); the eyes are holes, never filled in.
const MARK = "M43.4 4 Q46 4 46 6.6 L46 11 L50.4 11 Q53 11 53 13.6 L53 36.4 Q53 39 50.4 39 L46 39 L46 46 L57.4 46 Q60 46 60 48.6 L60 57.4 Q60 60 57.4 60 L6.6 60 Q4 60 4 57.4 L4 48.6 Q4 46 6.6 46 L18 46 L18 39 L13.6 39 Q11 39 11 36.4 L11 13.6 Q11 11 13.6 11 L18 11 L18 6.6 Q18 4 20.6 4 Z M46 18 L39 18 L39 25 L46 25 Z M18 18 L18 25 L25 25 L25 18 Z";

// The same bust on whole pixels of a 16 by 16 grid, sharp at favicon sizes.
const SMALL = "M11.5 0 Q12 0 12 0.5 L12 2 L13.5 2 Q14 2 14 2.5 L14 9.5 Q14 10 13.5 10 L12 10 L12 12 L15.5 12 Q16 12 16 12.5 L16 15.5 Q16 16 15.5 16 L0.5 16 Q0 16 0 15.5 L0 12.5 Q0 12 0.5 12 L4 12 L4 10 L2.5 10 Q2 10 2 9.5 L2 2.5 Q2 2 2.5 2 L4 2 L4 0.5 Q4 0 4.5 0 Z M12 4 L10 4 L10 6 L12 6 Z M4 4 L4 6 L6 6 L6 4 Z";

export function Mark({ size = 32, title = "Tinio", ...props }: { size?: number; title?: string } & SVGProps<SVGSVGElement>) {
  const small = size <= 24;
  return (
    <svg width={size} height={size} viewBox={small ? "0 0 16 16" : "0 0 64 64"} role="img" aria-label={title} {...props}>
      <path fill="currentColor" fillRule="evenodd" d={small ? SMALL : MARK} />
    </svg>
  );
}
