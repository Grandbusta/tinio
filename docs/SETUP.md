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
| `OPENAI_DAY_MODEL` | `gpt-6-luna` | Their work on the clock: preparing your morning, the weekly look back, what they watch. A morning costs under a tenth of a cent. |
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
| Check-ins | off | Their morning is how they write to you first each day. |
| Mornings | weekdays at 07:30, your time | Set per agent, in their settings or when you raise them (on or off, the time, the days). It starts 45 minutes before: up to 3 things for the day, each from something it knows, and up to 2 drafts made for them; a draft still being made holds the note back 30 minutes at most, and a morning more than 4 hours late is skipped. Quiet hours move it to when they end. It comes in the chat: there's no email copy on this computer. |
| A day's work | $0.05 | The most a day of work on the clock may cost: the morning, its drafts, promises, the look back, skill scores, a work's data and what they watch. Web caps: 10 searches, 15 pages. |
| Weekly look back | Sunday 20:00 | What it learned about how you like things (lessons kept or retired) and rules drawn from where you chose differently, for you to approve. Nothing is written for itself. |
| The week | a cap you set, $5 to start | What your agents may spend on your key in a week, all of them together (`POST /v1/usage`, `0` for no limit; `internal/usage`). Past it, light mode: replies on `OPENAI_LIGHT_MODEL`, mornings without drafts, no watching or look back; past 150%, or a quarter of the week in one hour, they rest until it resets. The week counts from when Tinio first ran. |
| Messages | 200 to each agent a day | A guard against a loop running up your bill, beside the cap (`api/config/constants.go`). |
| Failed work | 3 more tries, 15, 30 and 60 minutes apart | A draft, a reminder, a promise's message, the morning or the weekly look back that fails is tried again later, then given up; a piece cut off for length is given up at once. What a failed call cost is counted. |
| Models | must have a price | Tinio and `tinio eval` refuse to run agents on a model missing from `internal/pricing`: its cost would count as $0 and no cap could hold. |
| A quiet owner | 3 days, 7 days | Counted from the owner's last message (what the agent says first doesn't count). When their owner hasn't written for 3 days, their morning comes without drafts; after 7 (or if they've never written), there's no morning, look back or old works' data. Back at once when they write. Reminders, promises and what they watch go on. |
| Abilities | `read_book`, `read_article`, and the reading tools of the accounts you connect | What they can use while making something for you, and before a reply, beyond writing. Searching and reading the open web is written (`web_search`, `read_page`) but not given to agents yet. |
| Judgment calls | from requests | Every request you answer is a call: a yes or no that teaches them how you decide. A different answer followed by a message within 30 minutes is taken as why. |
| Requests | denied after 24 hours | An unanswered request is no. |

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
| `publish` | posting publicly, through a tool | ask |
| `external_message` | email, posts and DMs outside the chat | forbid: nothing leaves the chat yet |
| `spend` | money | forbid |

Owners change most of these by telling them ("you can send reminders without asking");
money and messages outside the chat change only in the code, never by chat.

## Characters

The six ready characters are folders in [characters](../characters): Marcus (personal
assistant), Kenji (software), Zara (social media), Ayesha (content writing), Sofía
(customer support) and Henrik (bookkeeping and finance). Each arrives already good at their job, and all of
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
| `timezone` | Their time zone, e.g. `Africa/Lagos`, or `"{timezone}"` for their owner's: their mornings, reminders and check-ins are in it. |
| `story` | Their background, one fact per line: where they're from and what they know from their work, for their voice and know-how. They never contradict it. It isn't a life they live: they work for their owner. |
| `personality` | How they are with people. It shapes every reply. |
| `makes` | What they make, in writing: `kind` (snake_case; their works are filed under it) and `about` (what it is and what a good one has). `data` (optional) is what each one carries so it can be made into a file: `score` for music (MIDI), `files` for code (a zip), `table` (a spreadsheet or CSV) or `events` (a calendar). Everyone can also write `notes`, short `writing`, a `table` and a `schedule`. |
| `skills` | Where they start: `name`, `level` (1–10) and `shown_by` (the kinds of work that show it). After that their levels come from what they make and their owner's feedback. |
| `family` | Who they know from the start: their owner (`name`, `relation`, `account`, `circle`). |

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
[evals](../evals), and `eval -replies` raises agents in a throwaway
database of its own; both call OpenAI, on `OPENAI_API_KEY`.

The prompts ([prompts](../prompts)), the question sets ([decisions](../decisions)) and the
characters are built into the binary; a file at the same path where it runs is used
instead, so `go run` from the checkout picks up an edit without a rebuild.
