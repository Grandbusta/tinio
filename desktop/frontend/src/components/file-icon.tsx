// A file's icon, as on the canvas: a page with its corner turned, tinted for its kind,
// with a mark for what it holds and its extension. The tiles stay light in dark mode,
// like paper on a desk.

type Kind = { name: string; label: string; glyph: "lines" | "grid" | "cal" | "note" | "zip"; bg: string; fold: string; ink: string };

const kinds: Record<string, Kind> = {
  pdf: { name: "PDF", label: "PDF", glyph: "lines", bg: "#FBE5E2", fold: "#E2BCB6", ink: "#8E2A22" },
  docx: { name: "Word", label: "DOCX", glyph: "lines", bg: "#E3EEFB", fold: "#B0C5E3", ink: "#1D4C8F" },
  xlsx: { name: "Sheet", label: "XLSX", glyph: "grid", bg: "#E1F1E6", fold: "#B6D4BF", ink: "#2C6A3F" },
  csv: { name: "CSV", label: "CSV", glyph: "grid", bg: "#E1F1E6", fold: "#B6D4BF", ink: "#2C6A3F" },
  ics: { name: "Calendar", label: "ICS", glyph: "cal", bg: "#FCEFC0", fold: "#E3CF8D", ink: "#7A5516" },
  mid: { name: "Music", label: "MID", glyph: "note", bg: "#FAF3D6", fold: "#DDD3AF", ink: "#3B372E" },
  zip: { name: "Archive", label: "ZIP", glyph: "zip", bg: "#EEF1F5", fold: "#CAD0D9", ink: "#3A414D" },
  txt: { name: "Text", label: "TXT", glyph: "lines", bg: "#EEF1F5", fold: "#CAD0D9", ink: "#3A414D" },
};

const aliases: Record<string, string> = { doc: "docx", xls: "xlsx", midi: "mid", md: "txt" };

// kindOf is the kind of a file name, an extension or a format name ("report.pdf", "pdf",
// "midi"); anything else is text.
export function kindOf(file: string): Kind {
  const ext = (file.includes(".") ? file.split(".").pop() : file)?.toLowerCase() ?? "";
  const k = kinds[aliases[ext] ?? ext] ?? kinds.txt;
  return ext === "md" ? { ...k, label: "MD" } : k;
}

export function FileIcon({ file, size = 44 }: { file: string; size?: number }) {
  const k = kindOf(file);
  return (
    <svg
      viewBox="0 0 56 72"
      width={size}
      height={Math.round((size * 72) / 56)}
      role="img"
      aria-label={`${k.name} file`}
      className="flex-none drop-shadow-[0_1px_1.5px_rgba(14,15,18,0.12)]"
    >
      <path d="M6 0H38L56 18V66A6 6 0 0 1 50 72H6A6 6 0 0 1 0 66V6A6 6 0 0 1 6 0Z" fill={k.bg} />
      <path d="M38 0V12A6 6 0 0 0 44 18H56Z" fill={k.fold} />
      {k.glyph === "lines" && (
        <g fill={k.ink}>
          <rect x="11" y="20" width="26" height="3.6" rx="1.8" />
          <rect x="11" y="27.5" width="32" height="3.6" rx="1.8" />
          <rect x="11" y="35" width="21" height="3.6" rx="1.8" />
        </g>
      )}
      {k.glyph === "grid" && (
        <g stroke={k.ink} fill="none">
          <rect x="12" y="19" width="32" height="24" rx="2.5" strokeWidth="2.6" />
          <path d="M12 27H44M12 35H44M22.7 19V43M33.3 19V43" strokeWidth="2.2" />
        </g>
      )}
      {k.glyph === "cal" && (
        <g fill={k.ink}>
          <rect x="12" y="20" width="32" height="23" rx="3" fill="none" stroke={k.ink} strokeWidth="2.6" />
          <path d="M13 21.5H43V27H13Z" />
          <rect x="17" y="32.5" width="5" height="4" rx="1" />
          <rect x="25.5" y="32.5" width="5" height="4" rx="1" />
          <rect x="34" y="32.5" width="5" height="4" rx="1" />
        </g>
      )}
      {k.glyph === "note" && (
        <g fill={k.ink}>
          <path d="M27.5 18V38" stroke={k.ink} strokeWidth="3" strokeLinecap="round" />
          <ellipse cx="23" cy="38.5" rx="5.8" ry="4.6" />
          <path d="M27.5 18C29 23 36 23 35 30C34.5 27 31.5 25.5 27.5 26Z" />
        </g>
      )}
      {k.glyph === "zip" && (
        <g fill={k.ink}>
          <rect x="24" y="4" width="4.5" height="4" />
          <rect x="28.5" y="10" width="4.5" height="4" />
          <rect x="24" y="16" width="4.5" height="4" />
          <rect x="28.5" y="22" width="4.5" height="4" />
          <rect x="24" y="28" width="4.5" height="4" />
          <rect x="24.5" y="35" width="8" height="10" rx="2.5" />
        </g>
      )}
      {size >= 28 && (
        <text x="28" y="60" textAnchor="middle" fontFamily="Manrope, Arial, sans-serif" fontSize="10.5" fontWeight="800" letterSpacing="0.6" fill={k.ink}>
          {k.label}
        </text>
      )}
    </svg>
  );
}
