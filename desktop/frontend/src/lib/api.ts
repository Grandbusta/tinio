// Calls to Tinio's API, at /api inside the app's window (the desktop app hands them to
// the API in the same program). Every answer has one shape: data on success,
// or an error with a code to act on and a message for people.

export type ApiError = { code: string; message: string; data?: Record<string, unknown> };
export type Answer<T> = { ok: true; data: T } | { ok: false; error: ApiError };

export type Account = { id: string; email: string; name: string; created_at: string };

export type Character = {
  key: string;
  name: string;
  about: string;
  personality: string[];
  toward: string;
  born_with: string[];
  relations: string[];
};

export type Agent = { id: string; character: string; name: string; owner_name: string; relation: string; pace: number; created_at: string; paused: boolean };

// A request in parts, for its card; without a title it's an older one, shown as words.
// editable: you may change quote before saying yes, and your version is what's used.
export type Request = {
  title?: string;
  quote?: string;
  where?: string;
  when?: string;
  undo?: string;
  final?: boolean;
  why?: string;
  expires?: string;
  editable?: boolean;
};

// One row of an agent's chat. The owner's taps (approve, pick) and their first hello are
// rows too, never shown. A settled ask or choice comes again with its new text: rows are
// kept by id.
export type ChatRow = {
  id: string;
  author: "owner" | "agent";
  kind: "message" | "ask" | "choice" | "file" | "approve" | "pick" | "hello" | "react";
  text: string;
  payload?: {
    approval?: string;
    call?: string;
    options?: number;
    pick?: number;
    file?: string;
    name?: string;
    size?: number;
    work?: string; // a file's work, and what it was made as
    format?: string;
    answers?: string[]; // a reply's answers to tap; several may be picked with many
    many?: boolean;
    read?: string[]; // what was read to write a reply
    good?: boolean; // a thumb on work: how it landed, and what was off
    note?: string;
  } & Request;
  at: string;
  settled_at?: string;
  topic?: string; // the topic it's in, as the agent sorted it or you chose; none for small talk
  reply_to?: string; // the row it replies to, when you chose one
};

// Something you and an agent have going, as the agent keeps it. Its summary and status are
// written once the talk pauses; settled is what was decided in it, oldest first.
export type Topic = {
  id: string;
  title: string; // "" until the agent names it
  summary: string;
  status: string;
  settled: string[];
  quiet: boolean;
  started_at: string;
  last_at: string;
  waiting: number; // its asks and calls not answered yet
  things: { id: string; kind: ChatRow["kind"]; text: string; payload?: ChatRow["payload"]; at: string; settled: boolean }[];
};

const offline: ApiError = { code: "offline", message: "Tinio isn't answering. Try again in a moment, or restart the app." };

// early holds reads started before they were asked for (prefetch), by path, for a moment.
// Once taken it serves only a second more (React asks twice on a page's first render in
// development), so a later read is always fresh.
const early = new Map<string, { at: number; taken?: number; answer: Promise<Answer<unknown>> }>();
const earlyFor = 10_000;
const takenFor = 1_000;

// prefetch starts a read a page will make, so it's on its way while you point at the link
// to it; the page's own call takes it, once, if it's fresh.
export function prefetch(path: string) {
  const e = early.get(path);
  if (e && Date.now() - e.at < earlyFor) return;
  early.set(path, { at: Date.now(), answer: read(path) });
}

export async function call<T>(path: string, body?: unknown): Promise<Answer<T>> {
  if (body === undefined) {
    const e = early.get(path);
    const now = Date.now();
    if (e && now - e.at < earlyFor && (e.taken === undefined || now - e.taken < takenFor)) {
      e.taken ??= now;
      return e.answer as Promise<Answer<T>>;
    }
    early.delete(path);
    return read<T>(path);
  }
  return send<T>(path, body);
}

function read<T>(path: string): Promise<Answer<T>> {
  return send<T>(path, undefined);
}

async function send<T>(path: string, body: unknown): Promise<Answer<T>> {
  let res: Response;
  try {
    // A FormData (a file being uploaded) goes as it is, with the type the browser gives it.
    const form = body instanceof FormData;
    res = await fetch(`/api${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers: body === undefined || form ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : form ? body : JSON.stringify(body),
      credentials: "same-origin",
    });
  } catch {
    return { ok: false, error: offline };
  }
  const out = await res.json().catch(() => null);
  if (out?.status === "success") return { ok: true, data: out.data as T };
  if (out?.status === "error") {
    return { ok: false, error: { code: out.code, message: out.message, data: out.data } };
  }
  return { ok: false, error: offline };
}

// An agent's own day, in their time. A plan item is done, now (due, being done), later,
// waiting (for your approval) or missed (not done in time).
export type Today = {
  date: string;
  plans_at: string;
  planned_at?: string;
  plan: { at: string; what: string; detail?: string; state: "done" | "now" | "later" | "waiting" | "missed" }[];
  made: { id: string; title: string; kind: string; at: string }[];
  journal: string;
  waiting: number;
};

// The account's agents, each with the last thing said in their chat, their requests
// waiting, what they've said since you last looked, and what they're doing now and next
// ("HH:MM · what", their time); and what they made and asked lately across all of them,
// newest first.
export type Overview = {
  agents: (Agent & {
    last?: { author: "owner" | "agent"; kind: ChatRow["kind"]; text: string; at: string };
    waiting: number;
    unread: number;
    now?: string;
    next?: string;
  })[];
  lately: { agent: string; kind: "made" | "asked"; text: string; at: string }[];
};

// Something an agent made that you may see: for themselves, or for you (for_you). peek is
// in a list, text when it's read. formats are the files it can be downloaded as now.
// What an agent grows towards: their active dreams, each with the goals that serve it (a
// proposed goal waits for your answer), each goal's learning plan if it has one; their
// newest weekly review, written for themselves; and the last seven days counted.
export type Goal = {
  id: string;
  text: string;
  done_when?: string;
  due?: string; // YYYY-MM-DD
  status: "proposed" | "active" | "done";
  progress?: string; // as their last review put it
  skill?: string;
  level?: number; // the skill's, 1 to 10
  plan?: { stage: number; status: string; stages: { learn: string; done: boolean }[] };
};

export type Growth = {
  dreams: { id: string; text: string; source: "life" | "self" | "owner"; since: string; goals: Goal[] }[];
  other_goals: Goal[];
  review: { title: string; text: string; at: string } | null;
  week: { made: number; decided: number; approved: number; answered: number; guessed: number };
};

// How their owner decides, as the agent has learnt it: the principles it may use (kept)
// and those waiting for you (proposed), the calls you answered, newest first, and how
// often it guessed them.
export type Principle = {
  id: string;
  text: string;
  kind: "principle" | "priority" | "boundary";
  status: "proposed" | "approved";
  source: "seed" | "review" | "owner";
  calls: number;
  at: string;
};

export type Judgment = {
  principles: Principle[];
  calls: { id: string; situation: string; chose: string; guessed: string; why?: string; request: boolean; at: string }[];
  guessed: { right: number; of: number };
  asked: { this_week: number; last_week: number };
};

// How an agent thinks: today's decisions counted, and their newest decisions; one opens on
// its whole chain, with the wording of what System 1 asked itself.
export type ReadAnswer = {
  type: "choice" | "predicate" | "score";
  refused?: boolean;
  choice?: string;
  probability?: number;
  confidence?: number;
  score?: number;
};

export type Decision = {
  id: string;
  parent_id?: string;
  at: string;
  percept_id?: string;
  set: string;
  tier: "reflex" | "system1" | "system2";
  provider: string;
  answers: Record<string, ReadAnswer> | null;
  action: string;
  labels?: Record<string, string>;
  reply: string;
  latency_ms: number;
  cost_usd: number;
  outcome: { status: string; detail?: string } | null;
  kind: string; // what it was about: message, hello, reminder, ...; "" for their own day
  text: string;
};

export type Mind = {
  today: { decisions: number; by_tier: Record<string, number>; escalations: number; cost_usd: number; avg_latency_ms: number };
  decisions: Decision[];
};

export type Chain = {
  chain: Decision[];
  questions: Record<string, { set: string; type: string; instructions: string; choices?: { value: string; description?: string }[] }>;
};

// What an agent remembers, about you and about their own life, newest first; from is the
// kind of thing it was learnt from (message, activity, observation, ...).
export type Fact = { id: string; about: "you" | "self"; text: string; at: string; areas: string[]; from: string };

// Who an agent is: what their owner may change (story, personality, routine, day) and
// what they're shown only (what they make, their dreams), and their versions, newest first.
export type CharacterDay = { plan_at: string; journal_at: string; review_at: string; check_ins: string[] };

export type CharacterSheet = {
  name: string;
  story: string[];
  personality: string[];
  routine: string[];
  timezone: string;
  day: CharacterDay;
  makes: { kind: string; about: string }[];
  dreams: string[];
  versions: { id: string; source: "new" | "import" | "edit" | "rollback"; at: string }[];
};

export type Work = {
  id: string;
  kind: string;
  title: string;
  peek?: string;
  text?: string;
  at: string;
  for_you: boolean;
  formats: string[];
  data?: unknown; // what it carries beside its words, as data_kind says (a table, events, a score); only when one is read
  data_kind?: string;
};

// The week your agents share, on your own key: cap_usd is what it may cost (0: no limit),
// spent_usd what it has; used is the percent of the cap used (past 100 once it's over; 0
// with no cap), and the agents' shares are percents of it too.
export type Usage = {
  cap_usd: number;
  spent_usd: number;
  state: "normal" | "light" | "resting";
  used: number;
  resets_at: string;
  messages_left: number;
  agents: { id: string; name: string; chat: number; day: number }[];
};

export type Verdict = "auto" | "ask" | "forbid";

// An agent's own settings. A class with no choices is fixed by Tinio.
export type AgentSettings = {
  classes: { class: string; verdict: Verdict; choices: Verdict[] }[];
  pace: number;
  paces: number[];
  quiet: { from: string; until: string }; // HH:MM in their time zone; both "": none
  paused: boolean;
};

// An agent's skills on the ladder, and the know-how their owner imported.
export type AgentSkill = { name: string; level: number; rung: "apprentice" | "pro"; born: boolean; shown_by: string[]; scores: number[] };
export type KnowHow = { name: string; title: string; description: string; files: number; added_at: string; from: "" | "market" }; // name: its handle
export type Skills = { skills: AgentSkill[]; know_how: KnowHow[] };

// Connections: accounts an owner connects once (Google, Atlassian, ...), given to the
// agents they choose. Tokens never leave the API.
export type ConnectorPart = { id: string; title: string; note: string; default: boolean; later?: boolean };
export type Connector = { id: string; name: string; about: string; parts: ConnectorPart[]; ready: boolean }; // ready: set up on this Tinio
export type PickedFile = { id: string; name: string; mime: string };
export type Connection = {
  id: string;
  connector: string;
  label: string; // the outside account, like an email
  access: { parts: string[]; files?: PickedFile[] };
  state: "ok" | "needs_you"; // needs_you: sign in again
  agents: string[];
  created_at: string;
};
export type AgentConnection = Connection & { name: string; parts: ConnectorPart[] };
export type AgentTools = {
  given: AgentConnection[];
  offer: AgentConnection[]; // the account's, not given to this agent
  lately: { what: string; at: string }[]; // newest first
  changes: Verdict; // what a change asks, as their settings say
  comments: Verdict;
  web: Verdict; // auto: the web and Wikipedia are theirs; forbid: off in their settings
};
