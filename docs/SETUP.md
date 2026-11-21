# Setup and configuration

Running Tinio on your own machine, and every setting there is. For what it is, see the
[README](../README.md); for what owners notice, [GUIDE.md](GUIDE.md); for how it works
inside, [ARCHITECTURE.md](ARCHITECTURE.md).

- [Running it](#running-it)
- [Your key](#your-key)
- [Where your data is](#where-your-data-is)
- [Reaching it from another device](#reaching-it-from-another-device)
- [.env: the settings](#env-the-settings)
- [How every agent works](#how-every-agent-works)
- [Characters](#characters)
- [Developing](#developing)

## Running it

You need Go 1.27+ and an OpenAI API key with access to the Decisions API. Nothing else:
no database to set up, no Docker.

```bash
go run ./cmd/tinio
```

One program serves the API on `127.0.0.1:8081` and runs every agent; its data is one
SQLite file in your data folder. The first time, it makes the folder, the database and
a key of its own, and waits for your OpenAI key.

The app is next: Tinio's own web app (React on Vite), served by the same program, and a
desktop app (Wails) after it. Until they're built, the API is all there is: everything
the app will do, it does over `/v1/*`.

## Your key

Your OpenAI key is the only thing you bring. Set it once, and it's kept sealed in the
database, shown only masked (`sk-…abcd`) from then on:

```bash
curl -X POST http://127.0.0.1:8081/v1/keys -H 'Origin: http://localhost:3001' -H 'Content-Type: application/json' -d '{"openai": "sk-..."}'
```

The agents start as soon as it's set, and again on a new one. `OPENAI_API_KEY` in the
environment is used instead, if it's set, and can't be changed from the app.

Every model call is on your key, so the week has a cap you set: $5 to start, changed with
`POST /v1/usage {"cap_usd": 10}` (`0` for no limit). See [the week](#how-every-agent-works).

## Where your data is

Everything is in one folder, which nobody but you can read:

| System | Folder |
|---|---|
| macOS | `~/Library/Application Support/Tinio` |
| Windows | `%AppData%\Tinio` |
| Linux and others | `$XDG_DATA_HOME/tinio`, or `~/.local/share/tinio` |

`TINIO_DATA` puts it somewhere else. It holds `tinio.db` (with `tinio.db-wal` and
`tinio.db-shm` beside it while it runs) and `key`, which seals every key and token kept:
your OpenAI key, and the accounts you connect. Keep the two together: without `key`,
what it sealed can't be opened again, and you'd set your key and connect again.
`TINIO_KEY` uses a key of your own instead (64 hex characters).

To back it up, copy the folder while Tinio isn't running.

## Reaching it from another device

On this machine there's no sign-in: Tinio is yours. Nothing else can reach it, since it
listens only on `127.0.0.1`. To use it from your phone on the same Wi-Fi, set both:

```bash
TINIO_ADDR=0.0.0.0:8081 TINIO_PASSCODE='a long passcode of your own' go run ./cmd/tinio
```

The other device signs in once with the passcode (16 characters or more), and stays
signed in for 30 days. A request is only taken as this machine's when it comes over
loopback, to `localhost` or a loopback address by name, and wasn't forwarded from
elsewhere: a web page that points its own name at your machine doesn't get in.

## .env: the settings

Tinio reads `.env` beside it; anything already set in the environment wins. None is
needed. [.env.example](../.env.example) has them all.

Each model below can also be picked in the app's Settings, from the models Tinio has prices
for; one set here wins, and can't be changed there.

| Variable | Default | What it is |
|---|---|---|
| `OPENAI_API_KEY` | none | Your OpenAI key, used instead of the one set in the app. |
| `OPENAI_MODEL` | `gpt-6.1-terra` | System 2, which writes every reply. Must have a price in `internal/pricing`. |
| `OPENAI_DECISION_MODEL` | `gpt-6-luna` | System 1, the decisions. |
| `OPENAI_DAY_MODEL` | `gpt-6-luna` | Their own day: planning, learning, what they watch, the weekly review. About a cent a day. |
| `OPENAI_LIGHT_MODEL` | `gpt-6-luna` | Replies once the week's cap is used (light mode). |
| `OPENAI_WORK_MODEL` | `OPENAI_MODEL` | Making works, for themselves or someone. |
| `TINIO_DATA` | see [above](#where-your-data-is) | The folder with the database and the key. |
| `TINIO_ADDR` | `127.0.0.1:8081` | Where it listens. |
| `TINIO_PASSCODE` | none | Lets another device in, 16 characters or more. Without it, only this machine. |
| `TINIO_KEY` | made on first run | A key of your own instead of the one in the data folder. |
| `APP_ORIGIN` | `http://localhost:3001` | Where the app is. Requests that change anything are taken only from it. |
| `COOKIE_SECURE` | `false` | `true` when it's served over https. |
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | Proxies whose `X-Forwarded-For` is believed, comma-separated. |
| `MAX_AGENTS` | `20000` | Most agents it runs at once. |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | none | A Google client, for connecting Google. Its redirect URI is `APP_ORIGIN/api/v1/connections/callback`. |
| `GOOGLE_API_KEY`, `GOOGLE_APP_ID` | none | For Google's file picker. |

## How every agent works

Every agent works the same way; what's theirs is their character, their owner's
judgment, and what their owner tells them in the chat. The defaults are in
[internal/config](../internal/config/config.go):

| Setting | Value | What it is |
|---|---|---|
| Working memory | 8 exchanges, 30 minutes | How much of the current conversation System 2 sees, outside topics. |
| Topics | the last 6 exchanges, and the chat's last 2 | What a reply in a topic is written from, besides its summary and what's settled. A topic is summed up after a 3-minute pause, and goes quiet after 7 days' silence. Quiet ones from the last 30 days are always considered for a message, and older ones when they're among the 3 closest in meaning (`internal/mind/topics.go`). |
| Recall | 30 facts | The newest facts System 1 checks for relevance before each reply. |
| Check-ins | after 4 hours' quiet | They consider checking in at their character's `check_ins` times, at most once a day. |
| Their own day | 3 activities, $0.05, at pace 1 | Shown on Today. Budget and web caps (10 searches, 15 pages) grow with pace. The budget counts every model call of their day: plans, activities, promises, the review, learning plans, skill scores, stage checks, a work's data and what they watch. |
| The week | a cap you set, $5 to start | What your agents may spend on your key in a week, all of them together (`POST /v1/usage`, `0` for no limit; `internal/usage`). Past it, light mode: replies on `OPENAI_LIGHT_MODEL` and their own day paused; past 150%, or a quarter of the week in one hour, they rest until it resets. The week counts from when Tinio first ran. |
| Messages | 200 to each agent a day | A guard against a loop running up your bill, beside the cap (`api/config/constants.go`). |
| Failed work | 3 more tries, 15, 30 and 60 minutes apart | A piece of work, a reminder, a promise's message, the day's plan or the weekly review that fails is tried again later, then given up; a piece cut off for length is given up at once. What a failed call cost is counted. |
| Models | must have a price | Tinio and `tinio eval` refuse to run agents on a model missing from `internal/pricing`: its cost would count as $0 and no cap could hold. |
| A quiet owner | 3 days, 7 days | Counted from the owner's last message (what the agent says first doesn't count). When their owner hasn't written for 3 days, their own day is one thing a day; after 7 (or if they've never written), it stops: no plan, review or old works' data. Back at once when they write. Reminders, promises and what they watch go on. |
| Abilities | `read_book`, `read_article`, and the reading tools of the accounts you connect | What they can do beyond writing, in their day and before a reply. Searching and reading the open web is written (`web_search`, `read_page`) but not given to agents yet. |
| Judgment calls | on, 1 a day, 30 minutes | They ask what you'd choose; a different pick followed by a message within 30 minutes is taken as why. |
| Requests | denied after 24 hours | An unanswered request is no. |

**Pace** is per agent, 1, 3 or 10, set in their settings (1 when they're raised): how much
faster than a person they work and learn, and how much of your cap their own day uses. At a higher pace their waking day runs in that many rounds of
planning and doing, and goals are due in days rather than weeks. Anything involving their
owner stays at human pace.

**What they may do on their own.** Each kind of action is `auto`, `ask` (the owner approves
in the chat) or `forbid`:

| Class | What it covers | Default |
|---|---|---|
| `remember`, `reply` | keeping facts, answering messages | always auto |
| `read_web` | abilities that fetch from the web | auto |
| `use_tools` | reading tools from MCP servers | auto |
| `write` | tools that change the owner's own things | ask |
| `reach_out` | messaging someone first | ask |
| `commit` | promising to make something for someone | ask |
| `publish` | putting a work on their public page | ask |
| `dream` | adding, rewording or dropping a dream | ask |
| `external_message` | email, posts and DMs outside the chat | forbid: nothing leaves the chat yet |
| `spend` | money | forbid |

Owners change most of these by telling them ("you can send reminders without asking");
money and messages outside the chat change only in the code, never by chat.

## Characters

The six ready characters are folders in [characters](../characters): Zara (social media),
Kenji (software), Marcus (futures trading research), Sofía (product), Henrik (bookkeeping
and finance) and Ayesha (copywriting). Each arrives already good at their job, and all of
them hand over drafts and plans: nothing they make leaves the chat without the owner. Tinio loads them
into the `characters` table when it starts, a new version when one changes, and an agent
raised from one keeps the version they were raised from. Each has three files, with
`{owner}`, `{relation}`, `{account}`, `{circle}` and `{timezone}` filled in for the owner who
raises them. The ready ones work in their owner's time zone, which the app sends from the
browser; the owner is a boss, client, co-founder or colleague (their *work* circle), a mentor
or a friend.

**template.yaml** is how the app offers them: `name`, `about` (who they are in a line) and
`relations` (who the owner may be to them).

**character.yaml** is who they are. It's written in the third person with their name
("Tobi grew up in Ibadan."), so they never confuse themselves with the people they talk to.

| Key | What it is |
|---|---|
| `name` | What they're called. |
| `timezone` | Their time zone, e.g. `Africa/Lagos`, or `"{timezone}"` for their owner's: their days, reminders and check-ins are in it. |
| `day` | When their day runs, in their time zone: `plan_at`, `journal_at`, `review_at` (`Sun 20:00`) and `check_ins` (`HH:MM` times they consider checking in). |
| `story` | Their life, one fact per line. They never contradict it; anything it doesn't cover they decide the first time it comes up, and remember. |
| `personality` | How they are with people. It shapes every reply. |
| `routine` | How their days usually go. It steers how they plan; they decide the rest. |
| `makes` | What they make, in writing: `kind` (snake_case; their works are filed under it) and `about` (what it is and what a good one has). `data` (optional) is what each one carries so it can be made into a file: `score` for music (MIDI), `files` for code (a zip), `table` (a spreadsheet or CSV) or `events` (a calendar). Everyone can also write `notes`, short `writing`, a `table` and a `schedule`, and keeps a journal. |
| `dreams` | Who they want to become. Copied into memory the first time; after that they may change them, asking their owner. |
| `skills` | Where they start: `name`, `level` (1–10) and `shown_by` (the kinds of work that show it). After that their levels come from what they make and their owner's feedback. |
| `family` | Who they're born knowing: `name`, `relation`, `account` (only for their owner) and `circle` (`family` by default, or `friend`, `school`, `work`, `community`). |

**judgment.yaml** is how the owner decides, a starting set that fits the character; the
owner's answers to their calls add their own decisions.

| Key | What it is |
|---|---|
| `priorities` | What matters most, most important first: when rules clash, the higher one wins. |
| `boundaries` | What's never done, whatever the gain. They also go into everything they make. |
| `principles` | Rules drawn from decisions: `text`, `types` (`general`, `engineering`, `product`, `autonomy`, `life`) and `cases` (the keys of the decisions behind them). |
| `cases` | Decisions made: `key`, `type`, `situation`, `options`, `choice` and `reason`. |

To add a character, add a folder the same way. The tests check that every character loads
for every relation it allows.

## Developing

```bash
go build ./... && go vet ./...
```

```bash
go test ./...
```

Every test that needs a database opens a new SQLite file in its own temporary folder:
nothing to start, and nothing left behind. `gofmt -l .` must print nothing.

`go run ./cmd/tinio eval` checks System 1's question sets against the cases in
[evals](../evals), and `eval -replies` and `eval -day` raise agents in a throwaway
database of their own; they call OpenAI, on `OPENAI_API_KEY`.

The prompts ([prompts](../prompts)), the question sets ([decisions](../decisions)) and the
characters are built into the binary; a file at the same path where it runs is used
instead, so `go run` from the checkout picks up an edit without a rebuild.
