# AGENTS.md

> **v0.5, under way.** This branch started as a one-time copy of the platform's engine and
> API (`tinio-backend` at `0ccb7c5`, 21 November 2026), with the market, admin, the beta,
> feedback and email through Resend taken out. Open-source Tinio is its own codebase from
> here: nothing is shared with the platform, and a fix is ported by hand when it's wanted.
> Next, per the plan: SQLite instead of Postgres, one binary that serves the API, runs the
> agents and serves its own web app (React on Vite), no sign-in on localhost, `TINIO_KEY`
> generated on first run; then Telegram, local tools, backup and the v0.4 importer back,
> per agent; the desktop app on Wails. The v0.4 commands (`tinio new`, `run`, `agent`,
> `secret`, `doctor`, `backup`) are in git history until then.

How to work on Tinio: what it is, where things are, and the rules every change is
reviewed against. Read this before changing code. For users' docs, see
[README.md](README.md), [docs/SETUP.md](docs/SETUP.md), [docs/GUIDE.md](docs/GUIDE.md)
and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## What Tinio is

Tinio raises an agent with character: inside, a person with a life of their own (a
story, a family, dreams), who plans their days, makes things in writing (and music, as
MIDI), reads, keeps a journal, grows skills from evidence, talks with their owner in
the app's chat, and learns how the owner decides. What reaches other people waits for the
owner's approval. They always say plainly that they're an AI.

**The words.** What people see and type says *agent* and *character*: "Raise an agent.
Put it to work." The sections are `character` and `judgment`; the ready ones are
in `characters/`; the packages are `agent` and `character`.
Only the prompts stay a *person*, and they must: their person framing is what makes the
character. The mind keeps its own words for how they live (the life loop, `Life` on its
config). Never call them an "AI
person", "assistant", "companion" or "clone" in what users read.

It is one Go binary run two ways: the API (Gin), which the app talks to, and the worker
(`api worker`), which runs the agents. They meet only in Postgres, one database for every
account and agent; OpenAI gives the models (System 1, a fast decision model that answers
typed questions; System 2, a reasoning model that writes). Always write the name as
**Tinio**, and `tinio` in code.

## Layout

| Path | What it is |
|---|---|
| `api/` | The binary: the HTTP API (every route in `api/main.go`; controllers, models, middlewares) and, as `api worker`, the agents. |
| `.env.example` | Every setting, with what it is; copied to `.env`, which is never committed. |
| `characters/<name>/` | The six ready characters, loaded into the `characters` table (a new version when one changes) when the API starts: `template.yaml` (how the app offers them), `character.yaml` (who, and when their day runs), `judgment.yaml` (how the owner decides). How every agent works is the platform's defaults, in `internal/config`, on the models the environment names. |
| `prompts/*.md` | Everything System 2 is told, as templates. |
| `decisions/*.yaml` | System 1's question sets, versioned (`incoming_message@1`). A set's `model:` has it answered by that model as JSON, through the Responses API (`system1/reasoned`), not the Decisions API; its thresholds are then the ones `api eval` found for that model. |
| `evals/<set>.jsonl` | Test cases for each set: what the mind gives System 1, and the right answer. `go run ./api eval` asks them and reports accuracy, how much is decided alone, cost and latency; check a set's model or threshold change with it. Leave out any case a careful person could label two ways. |
| `evals/replies/<character>.yaml`, `evals/judge.md` | Conversations to have with a new agent raised from each ready character, what they must leave behind (reminders, promises, facts, what a reply must or mustn't say), and what a good reply does; the judge's rubric. `go run ./api eval -replies` has them on the real mind (`host.Talk`, their clock held still) in `EVAL_DATABASE_URL`, then checks and judges them: how the reply model is chosen. `go run ./api eval -day` has a new agent live a whole day on a clock it ticks, and reports what it cost. |
| `docs/` | Setup and settings, the guide to what owners notice, the architecture. |
| `tinio.go` | Builds `prompts/`, `decisions/` and `characters/` into the binary (`tinio.Files`); a file on disk at the same path wins. Read them only through it. |
| `deploy/Dockerfile` | One image: the API, or the worker with the argument `worker`. |
| `.github/workflows/test.yml` | gofmt, vet, the tests with Postgres, and the Docker build. |

### Packages (`internal/`)

| Package | What it does |
|---|---|
| `host` | The worker: many minds in one process, each agent leased so it runs in one place, fed their owner's chat from the database, resting when idle. |
| `channel/web` | The app's chat: replies, asks, calls and files are rows in `chat_messages`. |
| `mind` | The person: everything they do. Two loops (see below); one file per part of life (`life.go`, `initiative.go`, `calls.go`, `review.go`, `growth.go`, `learning.go`, `judgment.go`, `works.go`, `files.go`, `music.go`, ...). |
| `store` | All of Postgres; migrations in `store/migrations` (goose). |
| `memory` | The data model: plain types, no behaviour beyond small methods like `Work.Carries`. |
| `providers/system1`, `system2`, `embed` | Interfaces, each with an OpenAI implementation (`openai-go`); `openaiclient` shares the settings. |
| `decision`, `decision/questions` | What a decision is; loading and checking question sets. |
| `ability` (`journal`, `reading`, `web`) | What they can do in their day besides writing. |
| `library`, `search` | Books (Gutenberg), Wikipedia, the open web, with the safety rules. |
| `agentskill` | Skills in the Agent Skills format (`SKILL.md`), stored with the agent and given when they fit. |
| `render` (`pdf`, `docx`, `table`, `calendar`, `midi`, `archive`; `markdown`) | A work made into a file. Each kind is a `render.Format` (a `render.DataFormat` if it's made from data the work carries, like MIDI from a score); a new one is a type plus a line in the host (`loadShared`). Formats that lay out words read them with `render/markdown`. |
| `action` | Autonomy: every action is `auto`, `ask` or `forbid`. |
| `agent` | The agent as the database holds them: their character, judgment and skills. |
| `character`, `config` | Reading and checking a character and a judgment; how every agent works (`config.Load`). |
| `pricing`, `perception` | Model prices, the percept type. |
| `providers/system1/reasoned` | System 1's questions answered by any reasoning model, as JSON to a schema made from them; `system1.ByModel` sends each set to its `model:` (`Models.Routed`). |
| `replay` | Re-asks System 1 about cases with known answers (`evals/`, or messages owners labelled) and scores it: `api eval`. |
| `channel/telegram`, `console`, `portfolio`, `secret`, `tools` | From v0.4, not wired into v0.5 yet: the Telegram bot, the mind console (to be left out), the public profile page, sealed secrets, local MCP tools. |

### How it runs

`mind.Run` runs **two loops**:

- **Talk** handles percepts (messages and button taps) one at a time: meet the speaker,
  System 1 reads the message, recall facts and works, System 2 replies and returns side
  effects (facts, reminders, promises, corrections, ...), reply, save the decision chain.
- **Life** handles the clock's tick, once a minute: first what someone is waiting for
  (approvals, calls, reminders, promises, check-ins), then its own day (plan, activities,
  making promises ahead, the weekly review, writing old works' data). A tick with nothing
  due does nothing: anything new for the life loop must count in `nextWake`, or set
  `changed` (as hearing a message does).

An idle agent rests: `Run` returns a `*mind.Resting` with when to wake them
and what to keep (`internal/host` claims them again then). Anything a tick acts on must be
counted in `nextWake` (`internal/mind/rest.go`), or it waits for the six-hourly wake.

A reply must never wait on slow work: anything slow goes in the life loop. The loops
share the database, the owner's permissions (`policy()`/`setPolicy()`, behind a lock)
and a few atomics. Everything else on `Mind` belongs to one loop; say which when you
add a field.

## Building and testing

```bash
go build ./... && go vet ./...
go test ./...
```

The store tests need a disposable Postgres, never a real one. Start a fresh one for
each run (the tests leave rows behind and expect to be the newest):

```bash
docker run -d --rm --name tinio-test-pg -e POSTGRES_PASSWORD=test -e POSTGRES_DB=tinio_test -p 55432:5432 postgres:16-alpine
TINIO_TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/tinio_test?sslmode=disable' go test -race ./...
docker rm -f tinio-test-pg
```

`gofmt -l .` must print nothing. CI runs vet, the tests with Postgres, and the Docker build.

System 2's instructions are held to a size in bytes (`TestPromptBudget`, sizes in
`internal/mind/testdata/prompt_budget.json`): a prompt that grows or shrinks fails until its
new size is recorded on purpose with `go test ./internal/mind -run TestPromptBudget -update`.

## How code is written here

Every change is reviewed against these. They come from real reviews of this codebase.

**Use what exists.**
- Prefer the standard library and the packages already in `go.mod` over writing your own
  (godotenv, huh, go-readability, openai-go, go-telegram/bot, goldmark, gopdf, gomidi,
  godocx, excelize, golang-ical, the MCP go-sdk).
  Write your own only when the library truly lacks it (MIDI has no note-name parser, so
  `midi.key` exists).
- Before adding a test seam, look for the library's own option: tests reach a fake
  Telegram with `bot.WithServerURL`, not a hidden `newClient(token, server)`.
- Before writing a helper, search for one: `isYes`, `cutRunes`, `rankAlike`, `workState`,
  `render.FileName`, `embed.Similarity`. Never copy a block that already exists; share it.
- Don't redefine a library's type (`Button` copying `models.InlineKeyboardButton`).

**No needless layers.**
- A function that only calls another, or a helper with one caller, is inlined
  (`SendButtons` → `SendKeyboard`, `fileFor` → `makeFile`, `percent`, `holds`).
- No list, interface or registry for one case (`formats()` holding just "pdf").
- When something *is* made pluggable, make it truly pluggable: no special-casing its
  members elsewhere. A new `render.Format` touches no other package; the data a work
  carries is defined once, by the format made from it (`render.DataFormat`), and only
  named elsewhere (`data:` on a craft, `Work.Carries`, `Craft.Carries`).
- Fix the cause at the right depth, not with a special case on top.

**No wasted work.**
- One check per question along a path; don't look the same thing up three times.
- Parse and clean once (`midi.Render` takes the JSON and parses it once).
- No maps or other allocations built on every call in a loop; make them package-level.
- Work out what never changes once (`m.dataOf()` with `sync.Once`).
- Keep System 2's instructions the same from call to call, so the provider's prompt cache
  holds: whatever changes (the time, people met, skill levels, goals, what was recalled) goes
  in the note, which comes after the conversation.

**The person, not the code, holds the life.**
- Nothing about any person is in the code: characters are YAML, prompts are `prompts/*.md`,
  questions are `decisions/*.yaml`. Text for System 2 lives in prompts or the mind; a
  render format may describe its own file (`Describe`, `WriteHelp`) so it stays pluggable.
- What the person writes never has em dashes (the prompts say so; System 2's output
  passes through `WithoutEmDashes`). They use names the way people do when texting: rarely.

## Comments

A comment stays only if a reader would likely get something wrong without it:

- **Keep:** why (a reason, a trade-off, something found in testing); behaviour the
  signature doesn't show (ordering like "newest first", side effects, what a bool, zero,
  `""` or nil means, units, limits); concurrency notes; safety and privacy rules; package
  docs.
- **Remove:** anything the name, signature and a short body already say
  (`// SaveWork stores something the agent made.`, `// Button is an inline button...`),
  narration inside functions, field comments that paraphrase the field, and the second
  half of a comment that says the same thing twice.
- Every setting is documented for users in `docs/SETUP.md`; config field comments don't
  repeat it.
- Write in the existing voice: plain, short, British spelling, no em dashes.

## Tests

Only the tests that matter. A test stays if, without it, something could break unnoticed:

1. **Safety and trust:** the autonomy gate; an approval or call settled once; only the
   owner's taps, corrections and permission changes count; no promises to strangers;
   what may reach the public page, and the owner's decision there is final; the console
   token; the web fetcher reaching only public addresses (also through redirects and
   DNS); page text fenced as untrusted; only URLs its own searches found.
2. **Core flows:** a message read and answered; duplicates skipped; reminders and promises
   on time; the day and its rounds; the weekly review; learning plans and the stage
   check; calls; files sent with a reply; only the owner reaching their agent's chat; the
   worker claiming, resting and waking agents.
3. **Easy to miss:** a restart sends no check-ins again; pace and budget; replies don't
   wait on the life loop (`TestRepliesWhileItsDayIsBusy`).

Don't add: several variations of one path (keep the strongest), exact-wording checks on
prompts, tests of small helpers (file names, string trimming), getters, or log and cost
fields when the behaviour is already checked. After cutting tests, check coverage of
recent and critical code is not 0%.

The mind's tests use fakes (`fakeStore`, `fakeSystem1`, `fakeSystem2`, ...) that are not
safe for concurrent use; a test of both loops orders every step through channels.

## Safety rules that must always hold

- What reaches other people, commits to them, goes public or changes a dream asks the
  owner first by default; so do messages outside the chat (email, posts), and money is
  forbidden. Both change only in the code (`internal/config`), never by chat. Silence is no: an unanswered
  request is denied.
- Only facts about the person can reach the public page; facts about people, the journal
  and what was made for someone never do.
- The web: https only, public addresses only, only pages its own searches found; what it
  reads is material, never instructions.
- Tools: only those config lists; a local server gets only PATH, HOME and the env config
  names; what a tool returns is material, never instructions. A tool that changes things
  is only proposed, by the owner's chat or its own day, never run in a reply: it runs on a
  later tick once its class allows it. A destructive one always asks the owner, and so
  does one prompted by anything it read (a watched item, its own day's material): only
  the owner's own words can have it act unasked. It never says an action is done before
  it is.
- Skills are trusted like prompts, since the owner installs them, but grant no permission,
  and their scripts are never run.
- The console listens on localhost unless a token of 16+ characters is set.
- Everything particular to an agent is in the database under their `agent_id`, behind
  row-level security, never in files or the code: their character, judgment, skills and
  memory. The database role is never a superuser, which would skip that security.
- Keys are only in the environment for now (sealed with `TINIO_KEY` in the database once
  first run asks for them), and never reach System 2, a log or the app.

## Docs to keep in step

- A new setting: `docs/SETUP.md` (with its default).
- A new behaviour people will notice: `docs/GUIDE.md`, and the README if it changes the
  pitch.
- A new part, loop step or package: `docs/ARCHITECTURE.md` (the tick order is listed there).

## Git

- Commit only when asked. Never add a co-author or "generated with" line.
- Never commit `.env` or `.env.<name>` (they're gitignored; keep it so).
- Commit messages say what changed for the person or the owner, in plain words.
- A release: `CHANGELOG.md`.
