# Tinio

**Raise an agent. Put it to work.**

Tinio gives you agents with character, not blank prompts, running on your own machine.
Raise one from a ready character: a name, a voice, a short background, a job they're
already good at. From then on they work for you, and only for you: they have no life of
their own. Each morning they send you your few things for the day, with drafts already
made. They talk with you in one chat, keep their promises, make real things, and ask
before anything that reaches other people. Over time they learn how you like things done
and how you decide, and ask less. They always say plainly that they're an AI.

Everything stays on your machine, in one file, except the calls to the models, on your
own key.

> **v0.5 is under way on this branch:** agents work only for you, with a morning each day. The engine and API run as one program on SQLite, and the
> desktop app is where you raise your agents and talk to them
> ([desktop/README](desktop/README.md)). The latest release, v0.4, runs one agent on
> Telegram.

## What makes them different

- **Character.** The same agent every day, with one voice and one taste: a short
  background they never contradict (where they're from, what they know) and a
  personality. It's how they talk and work, not a life they lead.
- **Memory.** You, the people in your life, what they made, what you decided, and every
  topic you have going: pick one up months later and they know where it stood.
- **Initiative.** Your morning: yesterday in a line, up to three things for today, drafts
  made before you wake, one question if something's missing. Promises kept on time.
- **Real work.** Writing, notes, reading notes on books and web pages, and files you can
  download: PDFs, Word documents, spreadsheets, calendar invites, music as MIDI.
- **Better at helping you.** Every piece of work is scored, your thumbs and edits count
  double, and each week they look back at what you liked and changed: the lessons they
  keep and their skill levels move with what they actually make for you.
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
reply costs a fraction of a cent, and a morning with its drafts about a cent.

## The characters

Six come ready in [characters](characters), each already good at a job, working in your
time zone. Nothing about any of them is in the code.

| | What they do |
|---|---|
| **Marcus** | Personal assistant: your diary, your inbox and the loose ends, from email drafts and meeting briefs to trip plans. |
| **Kenji** | Software: working code, design docs and honest code reviews. |
| **Zara** | Social media: posts, content calendars and campaigns that sound like you. |
| **Ayesha** | Content writing: articles, newsletters and pages that get read. |
| **Sofía** | Customer support: replies your customers are glad to get, help articles, and what they keep asking. |
| **Henrik** | Bookkeeping and finance: budgets, cash-flow forecasts and clear books. |

All of them hand you drafts and plans: nothing they make leaves the chat without you.

## A day with them

| When | What |
|---|---|
| **Before you wake** | They read your yesterday (your chat, topics, promises, requests waiting, your calendar if it's connected) and draft what they can for today. |
| **Your morning** | One note at your time (07:30 on weekdays unless you change it): yesterday in a line or two, up to three things for today, each with where it came from, the drafts after it. Tick them off on Your day. |
| **Any time** | What you ask for, reminders, and promises they made you, kept on time. |
| **Sunday** | A look back at their work for you: what you liked and changed, the lessons they keep, and how you decide, for you to approve. |

## Teach them

- **Skills.** Give them know-how in the Agent Skills format (a folder with a `SKILL.md`):
  how to write an invoice, your style guide, a technique. They use it whenever a chat or
  their work fits.
- **Connections.** Connect Google or Atlassian and they can read your calendar, a file you
  pick or an issue, and propose changes that wait for your yes.
- **Corrections.** "I live in Lekki, not Ikoyi", "forget what I told you about the bank",
  "that draft was weak": they take it in.

## You stay in charge

- **Approvals.** Messaging someone first, promising something, using a tool that changes
  things: by default they ask, with the exact thing, where, when and
  whether it can be undone, and you can change it before you say yes. Silence is no. Money
  is never theirs to spend.
- **Your judgment.** Your principles, priorities, boundaries and past decisions shape their
  choices, and you choose which decisions they learn from.
- **Your keys.** Your OpenAI key and the accounts you connect are sealed with a key only
  your machine holds, and never reach the model, a log or the screen.

## Learn more

- [Working with them](docs/GUIDE.md): talking and topics, promises, your mornings, how
  they get better at helping you, judgment and what they may do.
- [Setup and every setting](docs/SETUP.md): running it, your key, your data, the settings,
  developing.
- [How it works inside](docs/ARCHITECTURE.md): the two-system mind, memory, and how the
  parts fit.
- [What's changed](CHANGELOG.md).

MIT licensed: see [LICENSE](LICENSE).
