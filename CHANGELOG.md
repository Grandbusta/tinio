# Changelog

## v0.5 (unreleased)

**They work for you, and only for you.** An agent has a character and a voice, a short
background and a personality, but no life of their own: no day they plan, no rounds or
pace, no journal, goals, dreams or learning plans, and no public page.
- **Your morning.** Each agent sends one note in their chat at your time (weekdays at
  07:30 unless you change it): yesterday in a line or two, up to three things for today,
  each from something they know (a topic, a promise, a request waiting, your calendar),
  the drafts they made for them 45 minutes before, and one question only if something's
  missing. Nothing is sent anywhere: a draft is only made for you. Set it per agent in
  Mornings (on or off, the time, the days), or when you raise them. Quiet hours move it;
  past the week's cap it comes without drafts; after a week without a word from you there's
  none until you write.
- **Your day**, on the rail: every agent's things from this morning, to tick done, with
  their drafts and where each came from.
- **Helping you** (once Growth): what they've learned about how you like things, their
  skills, and the week counted, the things in their mornings included.
- **The weekly look back** replaces the review: lessons kept or retired, and how you
  decide, from where you chose differently, for you to approve.
- **A new cast.** Marcus is now a personal assistant (your diary, your inbox and the loose
  ends: email drafts, meeting briefs, task lists, trip plans) and Sofía a customer support
  lead (reply drafts, help articles, a weekly support digest, saved replies). Ayesha is a
  content writer; Zara, Kenji and Henrik are as they were.

**Many agents, on your own machine**
- One program, `tinio`: the API and every agent in one process, listening only on this
  machine. Everything is in one SQLite file in your data folder (Application Support on
  macOS, `%AppData%` on Windows, `~/.local/share/tinio` elsewhere): no Postgres, no Docker.
- No sign-in on this machine; another device on your network gets in with
  `TINIO_PASSCODE`. Tinio makes its key on first run and keeps it in the data folder.
- You bring only your OpenAI key: set it once through the API, kept sealed and shown only
  masked; the agents start on it, and again when it changes. The models have defaults.
- The week is a spending cap you set ($5 to start, or no limit), with light mode past it.
  No plans and no limit on agents.
- The engine is a one-time copy of the platform's: topics, looking before replying,
  Wikipedia and books in a reply, connections (Google, Atlassian), request cards you can
  change, quiet hours and pausing, skills with checks, and the six job-ready characters.
  The Skills market, admin, the beta, feedback and email are the platform's alone.

**The desktop app** (`desktop/`, built with Wails): the agents and the API run inside it,
with no port and no sign-in. It asks where your agents think first, Claude Code or your OpenAI key (or later, in Settings: until then the chats say nobody can reply), then you raise an agent
and talk to them, with replies as they're written. Every page of the web app is there:
Your day, their works, skills, topics, character, judgment, memory, mind, Helping you,
tools and settings, and You, with your key and the week's cap. It keeps running when you close its
window (in the Dock on macOS, the taskbar elsewhere), opens at login (on by default, off
on You), tells you in a notification what an agent says while you're elsewhere, and
saves their files where you choose. Settings has where they think, the model for each of their jobs
(replies, decisions, mornings, light mode, works), the app, and how it looks.

**Claude Code thinks for them, with no key:** with Claude Code signed in on your computer,
first run offers it before a key, and your agents think with it on your own plan: replies,
decisions, mornings, light mode and works. Tinio runs the `claude` you installed in safe
mode (your CLAUDE.md, hooks, plugins and MCP servers left out), asks it whether it's signed
in, and never reads its sign-in. Defaults: Sonnet 5.5 for replies, Haiku 5.5 for decisions,
mornings and light mode, Opus 5.5 for works. Settings shows where they can think (Claude
Code, and OpenCode and Codex when found) and lets each job pick a model from Claude Code or
your key; what Claude Code says a call would cost counts toward the week's cap.

**Coding jobs:** an agent with a folder they may make files in that's a git repository
(Kenji, say) can hand a coding job to Claude Code. They read the code first and propose a
job with its brief; on your yes it starts at once, on a copy of the repository on its own
branch (your checkout is never touched), in the project's own topic. One card in the chat
shows it as it goes: what it's doing, the files it changed, and how it ended, with Claude
Code's summary. On its own a job reads, changes files in its copy, and runs the
repository's own test, lint and build commands and read-only looks (git status, cat);
anything else comes to you as a card: Allow once, Allow for this job (the same isn't asked
again), or Not this, with what it should do instead, which it reads. Reply to a job's card
to tell it something, or to carry it on once it's done: the agent doesn't hear it. A strip
at the top of the chat shows the jobs going on. A job follows the repository's CLAUDE.md
and .claude settings, never your own Claude Code setup; jobs running when Tinio closes are
stopped, and a reply carries them on. Settings has Coding jobs: who writes the code (Opus
5.5 to start), on a copy or not, how long a job runs, and GitHub through your own gh.
Approved tools that only start something (a job) run as soon as you say yes.

**A truer blue:** the app's blue is a royal blue now, in both modes. In dark mode buttons
are a strong blue with white text, where they were a pale blue with dark text, and links
and pills keep a lighter blue that reads well on the dark. Dark mode is darker and plainer:
near-black with neutral greys, and the rail the page's own colour. The chat's pattern is
gone: the chat sits on a plain, soft tint, and its messages stand out on their own.

**More room for the work:** the chat list is narrower (320px), and folds to a column of
faces with its button or ⌘\. Folded, a face keeps its unread dot, a ring means they need
you, and the name shows on hover; the app remembers your choice. A job or a work opened
folds it on its own, and so does a window between 901 and 1100px wide, where opening it
lays it over the page until you pick a chat. The chat keeps a reading width, centred.

**Squarer corners:** buttons, inputs and chips have 8px corners and status pills 6px,
where they were fully round; the message box and segmented controls 12px. Avatars, dots,
round icon buttons and progress bars stay round.

**The agent checks the work:** when Claude Code finishes a round, the agent checks it
before you see it. Tinio runs the repository's tests in the copy, and the agent reads the
changes against the brief, what you asked for since, and the repository's rules. Not
right: it sends it back to Claude Code saying what to fix, up to three times for each
thing you ask. Right: it's ready for you, with the agent's word on its card ("Kenji
checked it") and on its page, and the history shows each check. What you say while it's
being checked goes into the next round. What you allow for a job is kept across its
rounds. It's on by default; Settings › Coding jobs turns it off.

**A job's page:** open a job from its card to see what happened, told by who did it (the
agent, Claude Code, you), and its changes, file by file, for the whole job or one round at
a time. Tap a line to say something about it; Ask for changes takes your notes, and the line
each is about, for another round. Undo the last round (Discard the changes, after one round) puts the copy back, once you confirm. Commit it commits
on the job's branch, on this computer, as you; then, if the repository has an origin and
GitHub's gh is signed in, Push and open a pull request is a second yes.

**Folders on your computer:** add a folder in Settings, or with the + beside the message
box, and choose which agents may use it, Read or Read and make files. They list, search
and read what's in it (text, Word, Excel, PDF) on their own; making or changing a file
asks you first, with the file and what changes. Hidden files, keys, browser profiles and
anything outside the folder are never seen, nothing is deleted, and a changed file's old
version is kept.

Each release has the app for macOS (one for Intel and Apple silicon), Windows and Linux,
unsigned for now, and CI builds and tests it on every push.

**Still to come before v0.5.0:** Telegram, local tools and backup, per
agent; the app reachable from your phone; connecting accounts from the app.

**Not here until they come back:** the v0.4 commands (`tinio new`, `agent`, `secret`,
`skill`, `doctor`, `backup`, `restore`), Telegram, the public profile page, and the five
life characters (in git history).

## Cheaper, never released as v0.4.1

v0.5 later removed their own day, so only the savings on replies and embeddings carried over.

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
