import { useEffect, useState } from "react";
import { call } from "@/lib/api";

// This app, on You, in the desktop app only: whether Tinio opens when you log in, what
// closing its window does, and quitting. Agents live their days only while Tinio runs.

type AppSettings = { open_at_login: boolean; system: string; notifications: boolean };

// Read when You is shown, once main.tsx has the runtime.
export const inTheApp = () => typeof window !== "undefined" && "runtime" in window;

export function ThisApp() {
  const [s, setS] = useState<AppSettings | null>(null);
  const [error, setError] = useState("");
  const [quitting, setQuitting] = useState(false);
  useEffect(() => {
    call<AppSettings>("/app").then((res) => res.ok && setS(res.data));
  }, []);
  if (!s) return null;
  const mac = s.system === "darwin";
  const closing = mac
    ? "Closing the window keeps Tinio running: click Tinio in the Dock to come back. Quit it from the menu, or here."
    : "Closing the window keeps Tinio running, down in the taskbar. Quit it here.";
  return (
    <section aria-labelledby="this-app" className="flex flex-col gap-3.5 rounded-lg border border-line px-[22px] py-5">
      <h2 id="this-app" className="font-display text-lg font-semibold tracking-[-0.02em]">This app</h2>
      <p className="m-0 text-[13px] text-muted">Your agents live their days while Tinio runs: they plan, make things and keep their promises. {closing}</p>
      <label className="flex items-start gap-3">
        <input
          type="checkbox"
          checked={s.open_at_login}
          onChange={async (e) => {
            const res = await call<AppSettings>("/app", { open_at_login: e.target.checked });
            if (!res.ok) return setError(res.error.message);
            setError("");
            setS(res.data);
          }}
          className="mt-0.5 size-5 flex-none accent-[var(--accent)]"
        />
        <span className="flex flex-col">
          <b className="text-sm font-semibold">Open Tinio when you log in</b>
          <span className="text-[13px] text-muted">It opens out of the way, so they carry on without you remembering to open it.</span>
        </span>
      </label>
      {error && <span role="alert" className="text-[13px] text-bad-text">{error}</span>}
      {!s.notifications && <span className="text-[13px] text-muted">Notifications aren&apos;t available here, so what they say waits in their chat.</span>}
      <button
        type="button"
        disabled={quitting}
        onClick={async () => {
          setQuitting(true);
          await call("/app/quit", {});
        }}
        className="min-h-10 self-start rounded-full border border-line-strong px-4 text-[13px] font-semibold hover:border-fg disabled:opacity-60"
      >
        {quitting ? "Quitting" : "Quit Tinio"}
      </button>
    </section>
  );
}
