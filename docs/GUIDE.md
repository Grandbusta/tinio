# Working with them

How you work with your agent day to day, in their chat: talking, their morning for you,
how they get better at helping you, and how they learn your judgment. An agent works for
you: they have a character and a voice, but no life of their own. Setup and every setting is in
[SETUP.md](SETUP.md); how it works inside is in [ARCHITECTURE.md](ARCHITECTURE.md).

Tinio's own app (a web app served by the same program, then a desktop app) is planned, not
built yet. The pages below are what it will show; the API on `/v1/*` already does all of
it.

The first time you open their chat they say hello. After that they rest while nothing is
going on, at no cost, and wake when you write or when something they promised is due.

## Your key, your cap, your machine

- **Your OpenAI key** is the only thing you bring. Set it once in the app; it's kept sealed
  in your data folder's database and only ever shown masked. Until it's set, your agents
  wait; set a new one and they start again on it. `OPENAI_API_KEY` in the environment is
  used instead, if it's set ([your key](SETUP.md#your-key)).
- **The week's cap.** Every model call is on your key, so you set what a week may cost:
  $5 to start, any amount you like, or no limit. You see how much is used, what used it
  (each agent's chat and their work on their own: mornings, drafts, what they watch), when
  it resets, and about how many messages are left at your usual rate. At the cap they're in
  light mode: they still reply, more simply, and mornings come without drafts until the
  reset. Well past it (half as much again, or a quarter
  of the week in one hour) they rest, and what you send is read at the reset. Asks and calls
  waiting for your answer, and reminders and promises already made, are never cut.
- **No sign-in on this machine.** Tinio is yours, and listens only on this machine. To use
  it from your phone on the same Wi-Fi, set a passcode of 16 characters or more
  (`TINIO_PASSCODE`); the phone signs in with it once, for 30 days
  ([another device](SETUP.md#reaching-it-from-another-device)).

You can raise as many agents as you like, and send each of them 200 messages a day.

## Talking

Every text message is read by **System 1**, a fast decision model (the
[OpenAI Decisions API](https://developers.openai.com/api/docs/guides/decisions)), which
answers typed questions with calibrated confidence: what you're doing (sharing something,
asking about them, asking what they can do, asking about yourself, asking for help, asking
to be reminded, small talk), whether you're answering what they just asked, and which part
of whose life it's about. Then **System 2**, a reasoning model (the OpenAI Responses API),
writes the reply as them, from their background and personality, the recent conversation,
and the facts System 1 picked as relevant. Every message gets a reply in their own words,
and you see it appear as they write it.

### What the chat shows

- **Formatting where it helps.** A short reply is plain text. A plan, steps or options can
  come as a list, with the one thing that matters in bold and links you can open; code comes
  coloured by its language, with a button to copy it.
- **Work as what it is.** A file they send shows what's in it: a sheet as a table (its first
  rows, then all of them), a calendar as its events, music you can play right there, a
  document by its first page. It still downloads as its file.
- **Answers to tap.** When they ask something with a few likely answers ("Simplified or
  traditional?"), the answers show under their message: tap one, or pick several and send.
  Typing your own works as ever.
- **What they read.** When they looked something up to answer (your calendar, a Wikipedia
  article), it's named under the reply.
- **Thumbs on what they made.** Up counts toward the skills it shows; down asks what was off,
  in a line, and they learn from it. A message's menu also copies it.
- **Search.** The magnifier in the header finds words anywhere in the chat and steps through
  each match.

### Topics

It's one chat, however much you have going. They sort what you say into topics as you talk
(a gift guide, the winter menu), and a reply in one is written from that topic: its last
few messages, however old, their notes on where it stands, and what you've settled in it.
So picking something up three days later works, and "six posts, not ten" isn't forgotten.
Your other topics stay out of it: a reply knows only their names. Small talk belongs to no
topic.

- A small label in the chat shows where the subject changes. Tap it, or a chip under the
  header, to see one topic only; **Everything** brings the whole chat back. What you send
  while one is shown stays in it.
- **+** starts a topic: your next message starts it, and they name it. Just saying "new
  thing: the spring newsletter" works too.
- **Reply** to any message to say "I mean this one"; your reply goes in that message's topic.
- If they sorted something wrong, a message's menu moves it to another topic, a new one or
  none. Your message takes their answer along.
- **About › Topics** lists what's open, each with where it stands, what you settled and its
  files and asks; you can rename one, merge two or close one. Closing deletes nothing, and
  a topic wakes when you bring it up, however long ago it went quiet: an old one is found by
  what it was about. One goes quiet by itself after a week without a word,
  or once its work is done.

While replying, System 2 also returns what to remember (facts about you and your work),
who you are to them ("I'm your cousin"), reminders, promises,
corrections, changes to their permissions, and your feedback on their work. They never use
em dashes.

## Reminders, promises and check-ins

- **Reminders:** "remind me tomorrow at 9 to call the bank". At that time they message you,
  in their own words. If you don't say when, they ask. Times are in their own time zone,
  which is yours: you give it when you raise them.
- **Promises:** "can you write me a verse about Sunday mornings tonight?" If they agree,
  they promise a time, make it up to 30 minutes ahead (with anything you added since, like
  "make it funny"), and send it on time with a few words of their own. They promise only
  what they can do in writing, at most three open promises per person, and nothing to
  strangers. A promise not kept within 24 hours is missed. Ask for something they promised
  sooner or later ("can you send it now?") and they move it; "now" arrives within a
  couple of minutes. You can change what it comes as too ("now, as a calendar").
- **Files:** ask for something they already made as a file ("send me that outreach as a
  PDF") and it comes right away in the chat, with their reply beside it, to download.
  Something new ("a plan I can print, tonight") is promised like anything else and sent as
  that file at its time.

  | File | Made from | Ask for |
  |---|---|---|
  | PDF | the words, laid out on A4 | anything to print or pass on |
  | Word (.docx) | the words, in Word's styles | a document you'll edit |
  | Spreadsheet (.xlsx) and CSV | a table (the built-in `table` kind) | a list, a comparison, a budget |
  | Calendar (.ics) | dated events (the built-in `schedule` kind) | a plan to add to your calendar; times are local |
  | MIDI (.mid) | a score (music with `data: score`) | a beat to open in GarageBand |
  | Zip | files (a kind with `data: files`, like Kenji's code) | code to unzip and run |
- **MIDI:** music (a craft with `data: score`, in a character you build) is written out note by
  note as well as in words. Ask for a beat as MIDI and it comes as a `.mid` that opens in
  GarageBand or any DAW.
- **What they made:** they see what they've made lately, and the piece you mean in full,
  so they talk about it as it is and never make up its details.
- **Check-ins:** off: their morning is how they write to you first each day.

## Your mornings

Each morning, at your time (07:30 on weekdays unless you change it, in your time zone),
each agent sends one note in their chat, for their own job:

- **Yesterday in a line or two:** what got done, what's still waiting.
- **Your few things for today:** up to three, fewer when there's less, never filler. Each
  says how to do it well, and comes from something they know: a topic, a promise, a
  reminder, a request still waiting, your thumbs, yesterday's morning, your calendar if it's
  connected. A thing with nowhere it came from is never sent.
- **What's ready:** 45 minutes before your time they read your yesterday and make up to two
  drafts for today's things (a note, a post, a plan, one of the things they make). Each
  draft follows the note as a file. Nothing is sent, posted or booked: a draft is only made
  for you.
- **One question,** only if something they'd need is missing. With little to go on (a new
  agent), the note is just the question: they won't guess at your day.

Quiet hours move it to when they end. In light mode (the week's cap reached) it comes
without drafts. After 3 days without a word from you it comes without drafts; after a week
there's none, until you write. It comes in the chat only: there's no email on this computer.

**Your day**, from the rail (or the panel about an agent), shows every agent's things from
this morning: tick one done (or tell them in the chat), open its draft, see where it came
from and what's waiting on you. Tomorrow's morning starts from what's left.

**Mornings**, in each agent's settings, sets theirs: on or off, the time and the days. You
also choose it when you raise them.

When something they set out to do fails (a draft, a reminder, a promised
message, the morning), they try again later, three more times, then let it go.

**Works**, from the panel about them (or a draft on Your day), is everything they've made
for you, newest first: read any of it (headings, lists and tables shown as
they're meant, every line break kept), download it as a PDF, a Word file or whatever else it
can be made into, or have them send it into your chat with **Send to me**, as the file it's
best made into.

**Helping you**, from the panel about them, is how they're getting better at helping you:
what they've learned about how you like things (lessons from your edits and their own
work, and your tastes, newest first), their skills and levels, and the last seven days
counted (works made, requests you approved, your calls they guessed, the things in their
mornings and how many you ticked done).

**Character**, from the panel about them, is who they are: their background a line at a
time (where they're from and what they know, for their voice) and their personality, both
yours to change. What they make is shown. Each change is a version you can go back from,
and reaches them within a minute: at their next tick they rest and wake at once with it,
keeping what they had in hand.

**How you decide**, from the same panel, is what they've learnt about how you make calls:
the principles they may use (only the ones you keep), those their weekly look back proposed
from your calls (Keep or Remove), and your own, added in a sentence; then the calls you
answered, what they'd guessed, and why you chose as you did.

**Mind** shows how they think: today's decisions counted, and every decision, newest first.
Open one to see its whole chain: what System 1 asked itself and how sure it was, what
System 2 wrote, and how long each step took and what it cost. Say whether a read was right,
or what it was; that's kept to score System 1 in replays, and changes nothing they do.

**Memory** is what they remember about you, the people in your life and their own
character, and where each thing came from. Correct one in your words (used from their next reply), or have them forget it
for good.

Your reminders and promises arrive on time however long you're away.

## Connections and tools

Every agent can look things up on Wikipedia, in a reply to you or while making something
for you, and read public-domain books from Project Gutenberg; their **Tools** page, from
About, lists what they have. Searching the open web comes later.

**Connect** is the accounts you connect once for your agents: **Google** (your calendar,
and the Drive files you pick) and **Atlassian** (Jira and Confluence). Google needs a Google
client of your own first (`GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, and for the file
picker `GOOGLE_API_KEY` and `GOOGLE_APP_ID`; see [the settings](SETUP.md#env-the-settings)).
You choose what they may do before you sign in at the service itself (Tinio never sees your
password), then which of your agents may use it; an agent's **Tools** page lets them use a
connection you already have, or connects one for them. Its tokens are sealed with your
machine's key.

They read on their own: your events, the files you picked, issues and pages. Anything that
changes something (adding an event, making a doc, creating or editing an issue, a comment)
is only proposed, and asks you first unless you set `write` to "On their own" in their
settings; editing something that's there already always asks. Nothing
invites, shares, deletes, sends email or touches money. Ask "what's on tomorrow?" and they
look before they answer, in the same reply (it takes a few seconds longer). Whatever they
then suggest changing asks you first, even if their settings let them change things on
their own: what they read can't make them act unasked. If a service signs Tinio out, the
connection shows "Needs you: sign in again" and their tools pause until you do.

## Their skills

**Skills**, from the panel about them, is what they can do and how well. Each skill is on a
ladder: Apprentice, then Pro once their scored work holds a level of 6 out of 10. The ones
they were born with come first, with their latest scores.

**Import a skill** brings know-how you already use, in the Agent Skills format: a SKILL.md (a
name and a description, then the instructions), alone or as a zipped folder with the files it
points to. You see what's in it before it's kept, under its title (its first heading, or its
name in words). Small slips in the SKILL.md are forgiven: a description with a colon in it
still reads, and a name like "Brand Voice" becomes brand-voice, the way skills are named.
They read it when a task fits, like instructions from you, so import only what you trust: it
gives them no permissions, and its scripts are never run. Remove takes it away again; what
they made with it stays.

## Getting better at helping you

- **Skills** start where their character puts them; you can teach more from the Market.
- **Skills grow from evidence.** System 1 scores each work that shows a skill, 1 to 10
  ([decisions/skill_level.yaml](../decisions/skill_level.yaml)); your feedback in chat ("that
  verse was weak") counts double. A skill's level is the average of its last 10 scores.
- **Lessons:** specific advice on doing your work the way you like it ("keep the posts under
  50 words"), from your corrections, their own mistakes, and what they read. The newest go
  into everything they make for you. Your feedback ties to the newest thing they made.
- **The weekly look back** (Sunday 20:00, your time): they look back at their week of work
  for you, keep or retire lessons, and draw rules from where you chose differently, for you
  to approve. Nothing is written for themselves.

## What they may do on their own

Every action belongs to a class, and each class is done on their own (`auto`), asked of you
first (`ask`), or never done (`forbid`):

| Class | What it covers | Default |
|---|---|---|
| `remember`, `reply` | keeping facts, answering messages | always auto |
| `read_web` | abilities that fetch from the web | auto |
| `write` | tools you connect that change your own things | ask |
| `reach_out` | messaging someone first | ask |
| `commit` | promising to make something for someone | ask |
| `publish` | posting publicly, through a tool | ask |
| `external_message` | email, posts and DMs outside the chat | forbid: nothing leaves the chat yet |
| `spend` | money | forbid |

Anything only for you (a reminder you asked for, a promise to you) never needs your OK.
When they ask, you get a card in the chat: what they want to do, the exact words, where it
goes, when, whether it can be undone, why they ask (under "Why this?"), and when no answer
becomes no. **Approve** or **Deny**; or, when it has words to change (a message),
**Change it**: edit them and approve your version, which is what they use. Unanswered after
24 hours, it's denied. "Needs you" in your agents' list shows who's waiting. You can also change permissions by
telling them ("you can send reminders without asking"); money and messages outside the
chat never change by chat, nor in the app.

**Settings**, from the panel about them, is that agent's own: each class above as "On their
own", "Asks you" or "Never" (the ones Tinio fixes shown, not changed), their morning (on or
off, the time, the days), their quiet hours (no morning, requests or calls then; they wait
until it ends, while reminders and promises you asked for still come), pausing them (they
stop working and hearing anything; what you send is read when you resume them), and
deleting them, for good, once you type their name. A change reaches them within a minute.

## Learning your judgment

They learn how you decide, so their choices become more like yours:

- **Your judgment.** Their character comes with a starting judgment that fits them
  ([what's in it](SETUP.md#characters)): past decisions (the situation, the options, what
  was chosen and **why**) and the principles behind them, used from the start. Your
  answers to their calls add your own.
- **Priorities and boundaries.** Rules clash ("finish what you started" against "rest is
  part of the work"), so `priorities` says what matters most to you, in order: the higher
  one wins. `boundaries` are what you never do, whatever the gain; they also go into
  everything they make.
- **Like this one.** The past decisions of yours they see are the ones most like what
  they're deciding, by meaning ("start a new song or finish the beat?" finds "three
  unfinished projects: start a fourth?"), not just the newest (OpenAI embeddings, a
  fraction of a cent a day).
- **Your taste.** Beyond how good their work is, what you like and don't: "I love when a
  hook repeats one word", "not into long intros". They pick it up when you comment in
  the chat. It goes into everything they make, beside what makes it good.
- **Calls.** Every request you answer (a message to someone, a promise to someone, a post)
  is a call: your yes or no counts as agreeing with them or not, and becomes one of your
  decisions. Answer differently from what they expected and your next message within 30
  minutes is taken as why. Their weekly look back turns disagreements into new principles,
  proposed for you to approve.

## Corrections

- **Wrong facts:** "I live in Lekki, not Ikoyi" replaces the fact. Anyone can correct what
  they know about them; only you can correct what they know about themselves.
- **Forgetting:** "forget what I told you about the bank" removes it for good.
- **Misreads:** "that's not what I meant, I was asking for help" labels System 1's read of
  your last message.

## Not here yet

From v0.4, coming back per agent: chatting with them on Telegram, tools from MCP servers
you run yourself, and backing up, restoring and exporting an agent (until then, copy the data folder while Tinio isn't running). Searching and reading
the open web comes later too. And the app itself: the web app, then the desktop app.
