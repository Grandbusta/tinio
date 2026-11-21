"use client";

import { useEffect, useRef, useState } from "react";
import { call, type Work } from "@/lib/api";

// A file in the chat shown as what it is, from the work it was made from: a sheet as a
// table, a calendar as its events, music you can play, a document by its first page.
// The file itself still downloads from its row. Nothing shows for a work that isn't
// there any more, or a kind with nothing to show.

type Table = { columns: string[]; rows: string[][] };
type Event = { title: string; start: string; end: string; location: string; notes: string };
type Note = { pitch: string; bar: number; beat: number; length: number; velocity: number };
type Score = { bpm: number; beats_per_bar: number; bars: number; tracks: { name: string; instrument: string; notes: Note[] }[] };

// Works are read once per page, however many rows show them.
const works = new Map<string, Promise<Work | null>>();
function workOf(agent: string, id: string) {
  const key = `${agent}/${id}`;
  if (!works.has(key)) works.set(key, call<{ work: Work }>(`/agents/${agent}/works/${id}`).then((r) => (r.ok ? r.data.work : null)));
  return works.get(key)!;
}

export function WorkPreview({ agent, work, format }: { agent: string; work: string; format: string }) {
  const [w, setW] = useState<Work | null>(null);
  useEffect(() => {
    let live = true;
    workOf(agent, work).then((x) => live && setW(x));
    return () => {
      live = false;
    };
  }, [agent, work]);
  if (!w) return null;
  if (w.data_kind === "table" && (format === "xlsx" || format === "csv")) return <TablePreview t={w.data as Table} />;
  if (w.data_kind === "events" && format === "ics") return <EventsPreview events={(w.data as { events: Event[] }).events ?? []} />;
  if (w.data_kind === "score" && format === "midi") return <ScorePlayer score={w.data as Score} />;
  if (format === "pdf" || format === "docx") return <FirstPage title={w.title} text={w.text ?? ""} />;
  return null;
}

const firstRows = 3;
const mostRows = 50;

function TablePreview({ t }: { t: Table }) {
  const [all, setAll] = useState(false);
  if (!t?.columns?.length) return null;
  const rows = t.rows.slice(0, all ? mostRows : firstRows);
  return (
    <div className="-mx-4 overflow-x-auto border-y border-line">
      <table className="w-full border-collapse text-[13.5px]">
        <thead>
          <tr>
            {t.columns.map((c, i) => (
              <th key={i} scope="col" className="border-b border-line bg-surface-2 px-3 py-2 text-left text-xs font-semibold tracking-[0.03em] text-muted uppercase">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              {t.columns.map((_, j) => (
                <td key={j} className="border-b border-line px-3 py-2 align-top">
                  {r[j] ?? ""}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {t.rows.length > firstRows && (
        <button type="button" onClick={() => setAll(!all)} className="min-h-10 px-3 text-[13px] font-semibold text-accent-text">
          {all ? "Show fewer" : t.rows.length > mostRows ? `Show the first ${mostRows} of ${t.rows.length} rows` : `Show all ${t.rows.length} rows`}
        </button>
      )}
    </div>
  );
}

const firstEvents = 3;

function EventsPreview({ events }: { events: Event[] }) {
  if (!events.length) return null;
  return (
    <ul className="m-0 flex list-none flex-col gap-2 p-0">
      {events.slice(0, firstEvents).map((e, i) => {
        const at = new Date(e.start.length === 10 ? `${e.start}T00:00` : e.start);
        const timed = e.start.length > 10;
        return (
          <li key={i} className="flex items-center gap-3">
            <span aria-hidden="true" className="w-11 flex-none overflow-hidden rounded-md border border-[#E3CF8D] text-center leading-tight">
              <span className="block bg-[#7A5516] py-0.5 text-[10px] font-bold tracking-[0.06em] text-white uppercase">{at.toLocaleDateString([], { month: "short" })}</span>
              <b className="block bg-[#FCEFC0] py-0.5 text-[17px] text-[#3B2A0C]">{at.getDate()}</b>
            </span>
            <span className="flex min-w-0 flex-col leading-snug">
              <b className="truncate text-sm">{e.title}</b>
              <span className="truncate text-[12.5px] text-muted">
                {at.toLocaleDateString([], { weekday: "long" })}
                {timed && `, ${at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`}
                {e.location && ` · ${e.location}`}
              </span>
            </span>
          </li>
        );
      })}
      {events.length > firstEvents && <li className="text-[12.5px] text-muted">And {events.length - firstEvents} more.</li>}
    </ul>
  );
}

// FirstPage is a document's first page, in small: its title and the start of its words.
function FirstPage({ title, text }: { title: string; text: string }) {
  const lines = text
    .split("\n")
    .map((l) => l.replace(/^[#>*\-\s\d.]+/, "").replace(/[*_`]/g, "").trim())
    .filter((l) => l && l.toLowerCase() !== title.toLowerCase())
    .slice(0, 3)
    .join(" ");
  return (
    <div className="flex items-center gap-3.5">
      <div aria-hidden="true" className="flex h-[104px] w-20 flex-none flex-col gap-1.5 rounded-md border border-line bg-white px-2 py-2.5 shadow-[0_1px_3px_rgba(14,15,18,0.12)]">
        <span className="h-1.5 w-[70%] rounded-sm bg-[#0B0F13]" />
        {[90, 80, 86, 60, 88, 74].map((w, i) => (
          <span key={i} className="h-1 rounded-sm bg-[#CFD5DC]" style={{ width: `${w}%` }} />
        ))}
      </div>
      <p className="m-0 line-clamp-4 text-[13px] text-muted">{lines}</p>
    </div>
  );
}

const steps: Record<string, number> = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 };

// midiNumber is a note name's MIDI number ("C4" is 60); null for a drum.
function midiNumber(pitch: string) {
  const m = /^([A-Ga-g])([#b]?)(-?\d)$/.exec(pitch.trim());
  if (!m) return null;
  return 12 * (Number(m[3]) + 1) + steps[m[1].toUpperCase()] + (m[2] === "#" ? 1 : m[2] === "b" ? -1 : 0);
}

// ScorePlayer plays a score in the browser with simple sounds (a soft tone for each
// note, a thump or a tick for drums): enough to hear the idea. The MIDI file is the
// real thing.
function ScorePlayer({ score }: { score: Score }) {
  const [playing, setPlaying] = useState(false);
  const [at, setAt] = useState(0);
  const ctx = useRef<AudioContext | null>(null);
  const started = useRef(0);
  const frame = useRef(0);
  const beat = 60 / (score?.bpm || 100);
  const length = (score?.bars || 1) * (score?.beats_per_bar || 4) * beat;
  const notes = (score?.tracks ?? []).flatMap((t) =>
    t.notes.map((n) => ({ ...n, at: ((n.bar - 1) * (score.beats_per_bar || 4) + (n.beat - 1)) * beat, drum: t.instrument === "drums" || midiNumber(n.pitch) === null })),
  );
  const pitched = notes.map((n) => midiNumber(n.pitch)).filter((n): n is number => n !== null);
  const low = Math.min(...pitched, 60);
  const high = Math.max(...pitched, 72);

  useEffect(() => () => stop(), []);

  function stop() {
    cancelAnimationFrame(frame.current);
    ctx.current?.close();
    ctx.current = null;
    setPlaying(false);
  }

  function play() {
    const ac = new AudioContext();
    ctx.current = ac;
    const t0 = ac.currentTime + 0.05;
    for (const n of notes) {
      const when = t0 + n.at;
      const gain = ac.createGain();
      const loud = Math.max(0.05, Math.min(1, n.velocity / 127)) * 0.25;
      gain.connect(ac.destination);
      if (n.drum) {
        const osc = ac.createOscillator();
        osc.frequency.setValueAtTime(/kick/i.test(n.pitch) ? 120 : 900, when);
        osc.frequency.exponentialRampToValueAtTime(/kick/i.test(n.pitch) ? 40 : 300, when + 0.08);
        gain.gain.setValueAtTime(loud, when);
        gain.gain.exponentialRampToValueAtTime(0.001, when + 0.12);
        osc.connect(gain);
        osc.start(when);
        osc.stop(when + 0.13);
        continue;
      }
      const osc = ac.createOscillator();
      osc.type = "triangle";
      osc.frequency.value = 440 * 2 ** (((midiNumber(n.pitch) ?? 69) - 69) / 12);
      const end = when + Math.max(0.08, n.length * beat);
      gain.gain.setValueAtTime(0, when);
      gain.gain.linearRampToValueAtTime(loud, when + 0.01);
      gain.gain.setTargetAtTime(0, end - 0.05, 0.04);
      osc.connect(gain);
      osc.start(when);
      osc.stop(end + 0.2);
    }
    started.current = t0;
    setPlaying(true);
    const tick = () => {
      const t = ac.currentTime - started.current;
      if (t >= length) return stop();
      setAt(Math.max(0, t));
      frame.current = requestAnimationFrame(tick);
    };
    frame.current = requestAnimationFrame(tick);
  }

  if (!notes.length) return null;
  const clock = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex items-center gap-3">
        <button
          type="button"
          aria-label={playing ? "Stop" : "Play"}
          onClick={() => (playing ? stop() : play())}
          className="grid size-11 flex-none place-items-center rounded-full bg-accent text-accent-ink hover:bg-accent-hover"
        >
          {playing ? (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="1.5" /></svg>
          ) : (
            <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.5v13l11-6.5z" /></svg>
          )}
        </button>
        <span className="text-[13px] text-muted">
          {clock(playing ? at : 0)} of {clock(length)} · {Math.round(score.bpm)} bpm
        </span>
      </div>
      <div aria-hidden="true" className="relative h-14 overflow-hidden rounded-lg bg-[#FAF3D6]">
        {notes.map((n, i) => {
          const m = midiNumber(n.pitch);
          const y = n.drum ? 85 : 80 - ((m! - low) / Math.max(1, high - low)) * 70;
          return (
            <span
              key={i}
              className="absolute h-1.5 rounded-full bg-[#3B372E]"
              style={{ left: `${(n.at / length) * 100}%`, top: `${y}%`, width: `${Math.max(0.8, ((n.length * beat) / length) * 100)}%`, opacity: n.drum ? 0.45 : 1 }}
            />
          );
        })}
        {playing && <span className="absolute inset-y-0 w-0.5 bg-accent" style={{ left: `${(at / length) * 100}%` }} />}
      </div>
    </div>
  );
}
