# minime: raise an agent, put it to work

**Raise an agent. Put it to work.** An agent with character, not a blank prompt: pick one
of five ready characters or write your own (a name, a story, a family, dreams), and from
there they work. They plan their days, make real things, read, keep a journal, and get
better at what they do. They talk with you on Telegram, keep their promises, and ask
before anything that reaches other people. Over time they learn how you decide, and ask
less. They always say plainly that they're an AI.

## 💡 What makes them different

- **Character.** The same agent every day, with a consistent voice and taste: a story they
  never contradict, a family, a personality, a routine. Anything their story doesn't cover
  (a favourite food, an opinion) they decide the first time it comes up, and remember.
- **They remember.** You, the people in your life, what they made, and what you decided.
- **They take initiative.** A plan each morning, work done before you ask, promises kept on
  time.
- **They get better, and you can see why.** Dreams lead to goals, goals to projects and
  learning plans. Every piece of work is scored, your feedback counts double, and their
  skill levels move with what they actually make.
- **They learn how you decide, and ask before anything that matters.** Every action is
  `auto`, `ask` or `forbid`. What reaches other people waits for your approval, and they
  track how often they'd have chosen what you chose.

## 🌱 Quick start

You need a Telegram bot token from [@BotFather](https://t.me/BotFather), an OpenAI API key,
and Postgres (or Docker, and it starts one for you). Download minime for your system from
the [latest release](https://github.com/Grandbusta/minime/releases/latest), or with Go:

```bash
go install github.com/Grandbusta/minime/cmd/minime@latest
```

Then, in a folder for them (their `.env` is written there):

```bash
minime new
```

Pick a character, paste your token and key, message your bot once so it knows it's you, and say
who you are to them. It puts them in their own database (`.env` only says where, and holds
the key to their secrets) and starts them. After that:

```bash
minime
```

If something's wrong, `minime doctor` says what; `minime backup` keeps everything about
them in one file. Every setting, Docker, and setting things up by hand: [docs/SETUP.md](docs/SETUP.md).

**What it costs:** only your OpenAI usage. Their own day is capped by `budget_usd` in
`work.yaml` ($0.05 a day at a human pace), and a message to them costs a fraction of
a cent. On the day the screenshots below were taken, Daniel made 100 decisions for $0.03.

## 👥 Pick a character

Five characters come ready in [characters](characters). Nothing about any of them is in the
code.

| | | Where | Working toward |
|---|---|---|---|
| **Daniel** | 18 | Lagos | becoming a music producer people in Lagos know by name |
| **Mei** | 24 | Taipei | a first collection of her poems, in both languages, side by side |
| **Tomás** | 21 | Buenos Aires | teaching himself to code well enough to work anywhere |
| **Priya** | 30 | Bengaluru | a cookbook of her grandmother's kitchen |
| **Elsa** | 67 | Gothenburg | the history of her neighbourhood, before the people who remember it are gone |

Or write your own: an agent is a folder of three files. `character.yaml` is who they are,
`work.yaml` is how they work, and `judgment.yaml` is how you decide
([making your own character](docs/SETUP.md#making-your-own-character)).

## ☀️ A day with them

| When | What |
|---|---|
| **08:00** | They plan the day, mostly moving a goal forward through a project or a learning plan. |
| **Through the day** | They do it, for real, in writing: making things, reading real books and web pages, studying how others do it. Set a `pace` and they work faster than a person: at `pace: 10` their waking day runs in ten rounds, each planned from how the last went. |
| **Any time** | Reminders you asked for, promises they made you, and now and then a check-in on something going on in your life. |
| **22:00** | A journal entry. |
| **Sunday** | A weekly review: goals updated, learning plans moved on, and a look back at how their decisions turned out. |

## 🔐 You stay in charge

- **Approvals.** Messaging someone first, promising something, publishing a work, changing
  a dream: by default they ask, and you get the exact thing on Telegram with **✅ Approve**
  and **❌ Deny**. Email and posts through a tool you connect ask every time; money is
  forbidden.
- **Your judgment.** Your principles, priorities, boundaries and past decisions shape their
  choices. You approve what they learn from, in the console.
- **Corrections.** "I live in Lekki, not Ikoyi", "forget what I told you about the bank",
  "that verse was weak": they take it in.

## 🧠 See inside

<p align="center">
  <img alt="The mind console" src="docs/images/console.png" width="49%">
  <img alt="Daniel's profile page" src="docs/images/profile.png" width="49%">
</p>

- **The mind console** (private, on localhost): their brain lighting up live as each decision
  flows through it, every decision and why, their memory, days, works, growth and judgment.
- **Their profile page** (public): a personal website they write themselves, with the works
  they decide are fine to share.

## 📖 Learn more

- [Working with them](docs/GUIDE.md): talking, promises, their day, growth, judgment, the
  console and every command.
- [Setup and every setting](docs/SETUP.md): `minime new`, Docker, the agent's files, the
  config, developing.
- [How it works inside](docs/ARCHITECTURE.md): the two-system mind, memory, the parts and
  how they fit.
- [What's changed](CHANGELOG.md).

MIT licensed: see [LICENSE](LICENSE).
