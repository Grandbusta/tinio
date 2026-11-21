# Changelog

## Unreleased (v0.5, under way)

**Many agents, on your own machine**
- One program, `tinio`: the API and every agent in one process, listening only on this
  machine. Everything is in one SQLite file in your data folder (Application Support on
  macOS, `%AppData%` on Windows, `~/.local/share/tinio` elsewhere): no Postgres, no Docker.
- No sign-in on this machine; another device on your network gets in with
  `TINIO_PASSCODE`. Tinio makes its key on first run and keeps it in the data folder.
- You bring only your OpenAI key: set it once through the API, kept sealed and shown only
  masked; the agents start on it, and again when it changes. The models have defaults.
- The week is a spending cap you set ($5 to start, or no limit), with light mode past it.
  No plans, no limit on agents, and every pace yours to choose.
- The engine is a one-time copy of the platform's: topics, looking before replying,
  Wikipedia and books in a reply, connections (Google, Atlassian), request cards you can
  change, quiet hours and pausing, skills with checks, and the six job-ready characters.
  The Skills market, admin, the beta, feedback and email are the platform's alone.

**Still to come before v0.5.0:** Tinio's own web app, served by the same program; Telegram,
local tools, backup and moving from v0.4, per agent; the desktop app.

**Not here until they come back:** the v0.4 commands (`tinio new`, `agent`, `secret`,
`skill`, `doctor`, `backup`, `restore`), Telegram, the public profile page, and the five
life characters (in git history).

## Cheaper, never released as v0.4.1 (in v0.5)

**Their own day costs less**
- `daily_life.model`: a model of its own for their day (planning, their own activities,
  learning, the weekly review, what they watch). Replies and what they make for someone
  keep System 2's.
- `daily_life.service_tier: flex`: OpenAI's cheaper, slower tier for their day, falling
  back to the standard tier when flex has no room.
- At a faster pace one plan covers up to three rounds, and no new plan is made while what
  they planned is still to do.
- No journal on a day they made nothing and talked with no one.
- Past works and the last journal are quoted up to 1,500 characters in their day's notes.

**Replies cost less**
- System 2's instructions stay the same from call to call: the people they know, their
  dreams, skill levels and goals moved to the note after the conversation, so OpenAI's
  prompt cache holds. Each request carries a cache key, and cached tokens are logged.
- A test holds every System 2 instruction to its recorded size.

**Embeddings**
- Switching the embedding model no longer spoils what was saved: what the old model
  embedded is embedded again as it comes up.

## v0.4.0: agents with character

**Characters**
- The five ready agents are characters, in `characters/`: Daniel, Mei, Tomás, Priya and
  Elsa. Their stories show where they are in life instead of giving an age.
- An agent is three files: `character.yaml` is who they are, `work.yaml` is how they work,
  and `judgment.yaml` is how you decide. `tinio agent edit | export | import | history |
  rollback` works on them.

**A new look**
- Ink, a deep blue chosen for trust, with slate greys and no gradients.
- A new mark, a one-colour pixel bust, with a favicon for the console and the profile
  page, an app icon and new banners.
- A rewritten README.

**Upgrading**
- `tinio self` is `tinio agent` now. `self` still works for this release.
- An export or an old `self/` folder with `life.yaml` and `living.yaml` still imports,
  as `character.yaml` and `work.yaml`. Their database is brought up to date on the first
  start.

## v0.3.1: doctor, backup and restore

- **`tinio doctor`** checks everything a person needs to run, one line each, and says
  what's wrong:
  - the database;
  - that `TINIO_KEY` opens their secrets;
  - the config with its prompts and question sets;
  - that their OpenAI key reaches each model;
  - the bot token, and that each owner has pressed Start;
  - each tool server.

  It changes nothing but bringing the database up to date. In Docker:
  `docker exec <name> /tinio doctor`.
- **`tinio backup [file]`** writes everything about them to one file, readable only by
  you. It's taken at one moment, so they keep running while it's made. Their secrets are
  in it sealed: keep `TINIO_KEY` safe too, apart from the backup.
- **`tinio restore <file>`** brings them back into a new, empty database. It never writes
  over anyone. A backup from an older Tinio is brought up to date once it's loaded.

## v0.3.0: a person is their own database

A person now lives entirely in their own database. Outside it, all they need is two
lines: `DATABASE_URL` says where they are, and `TINIO_KEY` unlocks their keys. Tinio
is also one binary now, with releases you can download.

**A person is their own database**
- Their life, how they live, your judgment, their skills and their keys are all kept in
  their database. Two people never share a key, a database or an OpenAI account.
- Each part of them is versioned: `tinio self edit life` opens it in your editor and
  refuses an edit that doesn't read. `tinio self history` and `tinio self rollback`
  take you back. `tinio self export` and `tinio self import` move them in and out as files.
- Keys and tokens are sealed with `TINIO_KEY` (AES-GCM) and are never stored as plain
  text. `tinio secret list | set | unset | rekey` manages them. A value already set in
  the environment still wins.
- Skills live in the database, every file of them, under `tinio skill list | add | remove`.
  Images and other files that aren't text are described rather than read as words. A
  skill can be up to 10 MB, and its scripts are never run.
- `tinio new` writes a new person straight into their database. It moves an existing
  `.env` aside to `.env.<name>` and never takes over a database that belongs to someone else.

**One binary**
- The config, prompts, question sets and the five lives are built in, so the binary
  alone runs a person. A file on disk at the same path still wins, so you can change
  them without a rebuild.
- Download it from the release (Linux, macOS and Windows), or install it with
  `go install github.com/Grandbusta/tinio/cmd/tinio@latest`.
- The image is `ghcr.io/grandbusta/tinio:v0.3.0`, for amd64 and arm64. `:latest` follows
  the newest published release.

**Upgrading**
- Someone in `self/` moves into their database once, on their first start. If `.env` has
  no `TINIO_KEY`, one is made and written there.
- **Back up `TINIO_KEY`.** Without it, their keys can't be opened again. Keep it
  somewhere safe and apart from the database.
- After the move, `self/` is no longer read. Keep it as a backup or delete it.

## v0.2.0: launch

The first release we're announcing: create an AI person, talk with them on Telegram, and
watch them live their own life. They make real things, use the tools you connect, notice
what changes, and learn how you decide, asking you before anything that matters.

**A person of their own**
- **Five lives, ready to start:** Daniel, Mei, Tomás, Priya and Elsa. `tinio new` picks
  one, sets up `.env`, finds your Telegram ID, can start a Postgres in Docker, and starts
  them. Your own person stays private in `self/`.
- **Talking:** a fast decision model reads each message (System 1) and a reasoning model
  answers in their own words (System 2). Memory of themselves and of each person, by
  circle; corrections and forgetting from chat; names used the way people do when texting.
  A reply never waits on their day: talking and living run side by side.
- **Speaking first:** reminders, check-ins, and promises made ahead and sent on time. Ask
  for something promised sooner or later ("can you send it now?") and they move it; they
  never say something is coming at a time it isn't.
- **Their own day:** a morning plan, real work (making things, reading books and web
  pages), a journal at night, and a `pace` to live faster than a person.
- **Growth:** dreams, goals (including ones you ask them to take on), skills scored from
  their work, lessons, learning plans, and a weekly review. A learning stage moves on only
  when an independent check agrees the work meets it.

**What they make, as real files**
- They know their own work when they talk about it, and never make up its details.
- Anything they made can be sent as a **PDF** or a **Word** document; a table as a
  **spreadsheet** or **CSV**; dated events as a **calendar** file; music as **MIDI**,
  written note by note; code as a **zip**. Ask for something they already made and it
  comes right away with their reply; something new is promised and sent at its time.
- Every life can make a `table` and a `schedule`; a life's own crafts say what else
  their work carries (`data: score` for music, `data: files` for code).
- File formats are plug-ins: a new one is a type plus a line in main.

**Tools you connect (MCP)**
- **Read:** tools from any MCP server you list, local or remote, become things they can
  use in their day and in promises, up to three in a row (search, then read). A local
  server never sees your keys.
- **Act:** a tool that changes things (save a file, send an email, book) is only
  proposed. You see exactly what it would do on Telegram and approve it; destructive
  tools always ask, and so does anything prompted by what they read rather than your own
  words. Email and posts ask before every send; spending stays off until you allow it
  in your config.
- **Watch:** a read tool can be looked at on a schedule, like an inbox or a folder. What's
  new is noticed, with what changed; they may remember it, tell you, or propose an
  action.

**Skills**
- Give them know-how in the open Agent Skills format (a folder with a `SKILL.md`, the
  same as Claude's). The skill that fits what they're doing is used, by meaning; skills
  grant no permission, and their scripts never run.

**Learning your judgment, and staying in charge**
- Every action is `auto`, `ask` or `forbid`, with approvals on Telegram; silence is no.
- Principles, priorities, boundaries and past decisions; calls with stakes, predictions
  you answer with a tap, outcomes judged apart from luck, similar decisions found by
  meaning, and your taste.

**Seeing inside and running it**
- A private mind console (with their tools, actions and skills) and a public profile
  page they write themselves.
- One binary, Postgres and OpenAI; Docker without compose. `AGENTS.md` explains how the
  code is written and the safety rules every change keeps.

## v0.1.0: early version

Tagged before launch and not announced: the first version anyone could run, with the
five lives, talking on Telegram, speaking first, their own day, growth, autonomy,
learning your judgment, the mind console and the profile page.
