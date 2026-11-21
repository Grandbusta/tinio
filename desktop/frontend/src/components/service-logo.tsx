// A service's mark on a white tile, for connections and tools. Drawn simply for now; the
// launch uses each company's own logo files, as their brand guidelines ask.

const marks: Record<string, { label: string; svg: React.ReactNode }> = {
  google: {
    label: "Google",
    svg: (
      <>
        <path fill="#4285F4" d="M22.6 12.2c0-.8-.1-1.5-.2-2.2H12v4.2h5.9a5 5 0 0 1-2.2 3.3v2.7h3.6c2.1-1.9 3.3-4.8 3.3-8z" />
        <path fill="#34A853" d="M12 23c3 0 5.5-1 7.3-2.7l-3.6-2.7c-1 .7-2.2 1.1-3.7 1.1-2.9 0-5.3-1.9-6.2-4.5H2.1v2.8A11 11 0 0 0 12 23z" />
        <path fill="#FBBC05" d="M5.8 14.2a6.6 6.6 0 0 1 0-4.3V7.1H2.1a11 11 0 0 0 0 9.9l3.7-2.8z" />
        <path fill="#EA4335" d="M12 5.4c1.6 0 3.1.6 4.2 1.7l3.2-3.2A11 11 0 0 0 2.1 7.1l3.7 2.8C6.7 7.3 9.1 5.4 12 5.4z" />
      </>
    ),
  },
  calendar: {
    label: "Google Calendar",
    svg: (
      <>
        <rect x="3" y="3" width="18" height="18" rx="3" fill="#FFFFFF" stroke="#DADCE0" />
        <path d="M3 6a3 3 0 0 1 3-3h12a3 3 0 0 1 3 3v2.5H3z" fill="#4285F4" />
        <path d="M16 21l5-5v2a3 3 0 0 1-3 3z" fill="#EA4335" />
        <text x="12" y="18.2" textAnchor="middle" fontFamily="Arial, sans-serif" fontSize="8.5" fontWeight="700" fill="#4285F4">
          31
        </text>
      </>
    ),
  },
  drive: {
    label: "Google Drive",
    svg: (
      <>
        <path d="M8.3 3h7.4l6.3 10.9h-7.4z" fill="#FBBC04" />
        <path d="M8.3 3L2 13.9l3.7 6.4L12 9.4z" fill="#0F9D58" />
        <path d="M5.7 20.3h12.6l3.7-6.4H9.4z" fill="#4285F4" />
      </>
    ),
  },
  docs: {
    label: "Google Docs",
    svg: (
      <>
        <path d="M6 2.5h8.5L19 7v13a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 5 20V4a1.5 1.5 0 0 1 1-1.5z" fill="#4285F4" />
        <path d="M14.5 2.5V7H19z" fill="#A1C2FA" />
        <path d="M8 11h8M8 14h8M8 17h5" stroke="#FFFFFF" strokeWidth="1.4" strokeLinecap="round" />
      </>
    ),
  },
  gmail: {
    label: "Gmail",
    svg: (
      <>
        <rect x="2.5" y="5" width="19" height="14" rx="2" fill="#FFFFFF" />
        <path d="M2.5 7.2v10.3A1.5 1.5 0 0 0 4 19h2.6v-7.7z" fill="#4285F4" />
        <path d="M21.5 7.2v10.3A1.5 1.5 0 0 1 20 19h-2.6v-7.7z" fill="#34A853" />
        <path d="M2.5 7.2L12 14l9.5-6.8V6.5A1.5 1.5 0 0 0 19.1 5.3L12 10.4 4.9 5.3A1.5 1.5 0 0 0 2.5 6.5z" fill="#EA4335" />
      </>
    ),
  },
  tinio: {
    label: "Tinio",
    svg: (
      <>
        <rect x="1" y="1" width="22" height="22" rx="5" fill="#0F4B92" />
        <path
          fill="#FFFFFF"
          fillRule="evenodd"
          transform="translate(4.5 4.5) scale(0.234)"
          d="M43.4 4 Q46 4 46 6.6 L46 11 L50.4 11 Q53 11 53 13.6 L53 36.4 Q53 39 50.4 39 L46 39 L46 46 L57.4 46 Q60 46 60 48.6 L60 57.4 Q60 60 57.4 60 L6.6 60 Q4 60 4 57.4 L4 48.6 Q4 46 6.6 46 L18 46 L18 39 L13.6 39 Q11 39 11 36.4 L11 13.6 Q11 11 13.6 11 L18 11 L18 6.6 Q18 4 20.6 4 Z M46 18 L39 18 L39 25 L46 25 Z M18 18 L18 25 L25 25 L25 18 Z"
        />
      </>
    ),
  },
  notion: {
    label: "Notion",
    svg: (
      <>
        <rect x="3.5" y="3" width="17" height="18" rx="3" fill="#FFFFFF" stroke="#0B0F13" strokeWidth="1.6" />
        <path d="M8.5 17V7.5l7 9.5V7" fill="none" stroke="#0B0F13" strokeWidth="2" strokeLinejoin="round" />
      </>
    ),
  },
  slack: {
    label: "Slack",
    svg: (
      <>
        <rect x="7.6" y="2.5" width="3.2" height="10" rx="1.6" fill="#36C5F0" />
        <rect x="11.5" y="7.6" width="10" height="3.2" rx="1.6" fill="#2EB67D" />
        <rect x="13.2" y="11.5" width="3.2" height="10" rx="1.6" fill="#ECB22E" />
        <rect x="2.5" y="13.2" width="10" height="3.2" rx="1.6" fill="#E01E5A" />
      </>
    ),
  },
  telegram: {
    label: "Telegram",
    svg: (
      <>
        <circle cx="12" cy="12" r="10" fill="#2AABEE" />
        <path
          d="M6.4 11.7l9.9-3.9c.5-.2.9.1.8.7l-1.7 8c-.1.5-.5.6-.9.4l-2.6-1.9-1.3 1.2c-.1.1-.3.2-.5.2l.2-2.7 4.8-4.3c.2-.2 0-.3-.3-.1l-5.9 3.7-2.5-.8c-.6-.2-.6-.6.1-.8z"
          fill="#FFFFFF"
        />
      </>
    ),
  },
  atlassian: {
    label: "Atlassian",
    svg: (
      <>
        <path d="M7.4 11.3c-.3-.4-.9-.3-1.1.1L2.1 19.8c-.2.4.1.9.6.9h5.9c.3 0 .5-.2.6-.4 1.3-2.7.5-6.8-1.8-9z" fill="#2684FF" />
        <path d="M11.4 3.7c-2.4 3.7-2.2 7.8-.6 10.9l2.8 5.7c.1.3.4.4.6.4h5.9c.5 0 .8-.5.6-.9L12.5 3.7c-.2-.5-.9-.5-1.1 0z" fill="#0052CC" />
      </>
    ),
  },
};

// logoFor is the mark for a connector, or one of its parts.
export function logoFor(id: string) {
  return { calendar_add: "calendar", all: "atlassian" }[id] ?? id;
}

export function ServiceLogo({ service, size = 44 }: { service: string; size?: number }) {
  const m = marks[service];
  return (
    <span
      role="img"
      aria-label={m?.label ?? service}
      style={{ width: size, height: size, borderRadius: Math.round(size * 0.24) }}
      className="grid flex-none place-items-center bg-white shadow-[inset_0_0_0_1px_#E3E7EC,0_1px_2px_rgba(14,15,18,.06)]"
    >
      <svg viewBox="0 0 24 24" width={Math.round(size * 0.6)} height={Math.round(size * 0.6)} aria-hidden="true">
        {m?.svg ?? <text x="12" y="16" textAnchor="middle" fontSize="12" fontWeight="700" fill="#0F4B92">{service[0]?.toUpperCase()}</text>}
      </svg>
    </span>
  );
}
