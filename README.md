# Tinio

**Raise an agent. Put it to work.**

Tinio gives you an agent with character, not a blank prompt. Pick one of five ready
characters or write your own: a name, a story, a family, something they're working
toward. From then on they work. They plan their days, make real things, read, keep a
journal and get better at what they do. They talk with you on Telegram, keep their
promises, and ask before anything that reaches other people. Over time they learn how you
decide, and ask less. They always say plainly that they're an AI.

## What makes them different

- **Character.** The same agent every day, with one voice and one taste: a story they never
  contradict, a personality, a routine. Anything their story doesn't cover, like a
  favourite food or an opinion, they decide the first time it comes up, and remember.
- **Memory.** You, the people in your life, what they made, and what you decided.
- **Initiative.** A plan each morning, work done before you ask, promises kept on time.
- **Real work.** Writing, notes, reading notes on books and web pages, and files you can
  download: PDFs, Word documents, spreadsheets, calendar invites, music as MIDI.
- **Growth you can see.** Dreams lead to goals, goals to projects and learning plans. Every
  piece of work is scored, your feedback counts double, and their skill levels move with
  what they actually make.
- **Your judgment.** Every action is `auto`, `ask` or `forbid`. What reaches other people
  waits for your yes, and they keep count of how often they'd have chosen what you chose.

## Get started

You need a Telegram bot token from [@BotFather](https://t.me/BotFather), an OpenAI API key,
and Postgres, or Docker so Tinio can start Postgres for you. Download Tinio from the
[latest release](https://github.com/Grandbusta/tinio/releases/latest), or install it with Go:

```bash
go install github.com/Grandbusta/tinio/cmd/tinio@latest
```

Then, in a folder for your agent:

```bash
tinio new
```

Pick a character, paste your bot token and OpenAI key, message your bot once so it knows
it's you, and say who you are to them. Tinio gives them their own database and starts them.
The `.env` it writes holds only where that database is and the key to their secrets. After
that, start them with:

```bash
tinio
```

`tinio doctor` checks everything they need and says what's wrong. `tinio backup` keeps
everything about them in one file. For Docker, every setting and setting up by hand, see
[docs/SETUP.md](docs/SETUP.md).

**What it costs:** only your OpenAI usage. Their own day is capped by `budget_usd` in
`work.yaml` ($0.05 a day at a human pace), and a message to them costs a fraction of a
cent.

## The characters

Five come ready in [characters](characters). Nothing about any of them is in the code.

| | Where | Working toward |
|---|---|---|
| **Daniel** | Lagos | becoming a music producer people in Lagos know by name |
| **Mei** | Taipei | a first collection of her poems, in both languages, side by side |
| **Tomás** | Buenos Aires | teaching himself to code well enough to work anywhere |
| **Priya** | Bengaluru | a cookbook of her grandmother's kitchen |
| **Elsa** | Gothenburg | the history of her neighbourhood, before the people who remember it are gone |

Or write your own. An agent is three files: `character.yaml` is who they are, `work.yaml`
is how they work, and `judgment.yaml` is how you decide
([making your own character](docs/SETUP.md#making-your-own-character)).

## A day with them

| When | What |
|---|---|
| **08:00** | They plan the day, mostly moving a goal forward through a project or a learning plan. |
| **Through the day** | They do it, for real: making things, reading books and web pages, studying how others do it. Set a `pace` and they work faster than a person: at `pace: 10` their waking day runs in ten rounds, each planned from how the last went. |
| **Any time** | Reminders you asked for, promises they made you, and now and then a check-in on something going on in your life. |
| **22:00** | A journal entry. |
| **Sunday** | A weekly review: goals updated, learning plans moved on, and a look back at how their decisions turned out. |

## Teach them

- **Skills.** Give them know-how in the Agent Skills format (a folder with a `SKILL.md`):
  how to write an invoice, your style guide, a cooking technique. `tinio skill add` teaches
  one, and they use it whenever a chat or their work fits.
- **Tools.** Connect an MCP server and they can read what you share, like a folder of notes
  or a price list, and watch an inbox or a feed for something new.
- **Corrections.** "I live in Lekki, not Ikoyi", "forget what I told you about the bank",
  "that verse was weak": they take it in.

## You stay in charge

- **Approvals.** Messaging someone first, promising something, publishing a work, changing
  a dream, running a tool that changes things: by default they ask, and you get the exact
  thing on Telegram with **Approve** and **Deny**. Silence is no. Email and posts through a
  tool ask every time, and money is never theirs to spend.
- **Your judgment.** Your principles, priorities, boundaries and past decisions shape their
  choices. You choose which decisions they learn from, in the console.
- **Your keys.** Their bot token, API keys and tokens are sealed in their database with a
  key only you hold, and never reach the model, a log or an export.

## See inside

- **The mind console**, private and on your own machine: their mind lighting up live as
  each decision flows through it, every decision and why, their memory, days, works,
  growth and judgment.
- **Their profile page**, public: a personal website they write themselves, with the works
  they decide are fine to share. Facts about other people and their journal never appear
  on it.

## Learn more

- [Working with them](docs/GUIDE.md): talking, promises, their day, growth, judgment, the
  console and every command.
- [Setup and every setting](docs/SETUP.md): `tinio new`, Docker, an agent's files, the
  config, developing.
- [How it works inside](docs/ARCHITECTURE.md): the two-system mind, memory, and how the
  parts fit.
- [What's changed](CHANGELOG.md).

MIT licensed: see [LICENSE](LICENSE).
