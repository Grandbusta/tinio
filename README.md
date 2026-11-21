# Tinio

**Raise an agent. Put it to work.**

Tinio gives you agents with character, not blank prompts, running on your own machine.
Raise one from a ready character: a name, a story, a family, a job they're already good
at. From then on they work. They plan their days, make real things, read, keep a journal
and get better at what they do. They talk with you in one chat, keep their promises, and
ask before anything that reaches other people. Over time they learn how you decide, and
ask less. They always say plainly that they're an AI.

Everything stays on your machine, in one file, except the calls to the models, on your
own key.

> **v0.5 is under way on this branch.** The engine and API run as one program on SQLite;
> Tinio's own web app, which they'll be raised and talked to in, comes next, and a desktop
> app after it. Until then the API is how to use it ([SETUP](docs/SETUP.md)). The latest
> release, v0.4, runs one agent on Telegram.

## What makes them different

- **Character.** The same agent every day, with one voice and one taste: a story they never
  contradict, a personality, a routine. Anything their story doesn't cover, like a
  favourite food or an opinion, they decide the first time it comes up, and remember.
- **Memory.** You, the people in your life, what they made, what you decided, and every
  topic you have going: pick one up months later and they know where it stood.
- **Initiative.** A plan each morning, work done before you ask, promises kept on time.
- **Real work.** Writing, notes, reading notes on books and web pages, and files you can
  download: PDFs, Word documents, spreadsheets, calendar invites, music as MIDI.
- **Growth you can see.** Dreams lead to goals, goals to projects and learning plans. Every
  piece of work is scored, your feedback counts double, and their skill levels move with
  what they actually make.
- **Your judgment.** Every action is `auto`, `ask` or `forbid`. What reaches other people
  waits for your yes, and they keep count of how often they'd have chosen what you chose.

## Get started

You need Go 1.27+ and an OpenAI API key. No database, no Docker.

```bash
go run ./cmd/tinio
```

The first time, Tinio makes its data folder, its database and a key of its own, and
waits for yours. Set your OpenAI key once; it's kept sealed and the agents start on it.
On this machine there's no sign-in: it's yours. See [docs/SETUP.md](docs/SETUP.md) for
the key, where your data is, reaching it from your phone, and every setting.

**What it costs:** only your OpenAI usage, under a weekly cap you set ($5 to start). A
reply costs a fraction of a cent, and an agent's own day about a cent at a human pace.

## The characters

Six come ready in [characters](characters), each already good at a job, working in your
time zone. Nothing about any of them is in the code.

| | What they do |
|---|---|
| **Zara** | Social media: posts, content calendars and campaigns that sound like you. |
| **Kenji** | Software: working code, design docs and honest code reviews. |
| **Marcus** | Futures trading research: market briefs, trade plans and a risk sheet, for you to decide on. |
| **Sofía** | Product: specs, roadmaps and what users actually said. |
| **Henrik** | Bookkeeping and finance: budgets, cash-flow forecasts and clear books. |
| **Ayesha** | Copywriting: articles, newsletters and pages that get read. |

All of them hand you drafts and plans: nothing they make leaves the chat without you.

## A day with them

| When | What |
|---|---|
| **Morning** | They plan the day, mostly moving a goal forward through a project or a learning plan. |
| **Through the day** | They do it, for real: making things, reading books and Wikipedia, studying how others do it. At a faster pace their waking day runs in more rounds, each planned from how the last went. |
| **Any time** | Reminders you asked for, promises they made you, and now and then a check-in on something going on in your life. |
| **Evening** | A journal entry. |
| **Sunday** | A weekly review: goals updated, learning plans moved on, and a look back at how their decisions turned out. |

## Teach them

- **Skills.** Give them know-how in the Agent Skills format (a folder with a `SKILL.md`):
  how to write an invoice, your style guide, a technique. They use it whenever a chat or
  their work fits.
- **Connections.** Connect Google or Atlassian and they can read your calendar, a file you
  pick or an issue, and propose changes that wait for your yes.
- **Corrections.** "I live in Lekki, not Ikoyi", "forget what I told you about the bank",
  "that draft was weak": they take it in.

## You stay in charge

- **Approvals.** Messaging someone first, promising something, changing a dream, using a
  tool that changes things: by default they ask, with the exact thing, where, when and
  whether it can be undone, and you can change it before you say yes. Silence is no. Money
  is never theirs to spend.
- **Your judgment.** Your principles, priorities, boundaries and past decisions shape their
  choices, and you choose which decisions they learn from.
- **Your keys.** Your OpenAI key and the accounts you connect are sealed with a key only
  your machine holds, and never reach the model, a log or the screen.

## Learn more

- [Working with them](docs/GUIDE.md): talking and topics, promises, their day, growth,
  judgment and what they may do.
- [Setup and every setting](docs/SETUP.md): running it, your key, your data, the settings,
  developing.
- [How it works inside](docs/ARCHITECTURE.md): the two-system mind, memory, and how the
  parts fit.
- [What's changed](CHANGELOG.md).

MIT licensed: see [LICENSE](LICENSE).
