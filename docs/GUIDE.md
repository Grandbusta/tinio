# Working with them

How you work with your agent day to day, in their chat: talking, what they do on their
own, how they grow, and how they learn your judgment. Setup and every setting is in
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
  (each agent's chat and their own day), when it resets, and about how many messages are
  left at your usual rate. At the cap they're in light mode: they still reply, more simply,
  and their own day pauses until the reset. Well past it (half as much again, or a quarter
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
writes the reply as them, from their life story and personality, the recent conversation,
and the facts System 1 picked as relevant. Every message gets a reply in their own words,
and you see it appear as they write it.

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

While replying, System 2 also returns what to remember (facts about you, and things they
decided about themselves), who you are to them ("I'm your cousin"), reminders, promises,
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
  couple of minutes.
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
  GarageBand or any DAW, and a beat they ask you to review comes with its `.mid`.
- **Their own work:** they see what they've made lately, and the piece you mean in full,
  so they talk about it as it is and never make up its details.
- **Check-ins:** at the `check_ins` times in their character (in your time zone), they may ask about something
  you mentioned in the last 30 days that's still going on, like an interview. At most once
  a day, only after the conversation has been quiet for 4 hours, never about the same thing
  twice.

## Their day

**Today**, from their chat's header or the column beside it, shows their day as it stands, in
their time: what they planned and how each thing went (done, now, later, waiting for you,
missed), what they made, and how their newest journal entry begins.

At `plan_at` (08:00) they plan a few things to do, steered by `routine`: mostly what they
think will help you, from what they know about you and your work, and the rest growing
themselves in ways that make them more useful to you, moving a goal forward through a
project. What they make for you is kept as made for you: it shows as yours and never reaches
their public page. At a faster pace (3 or 10, in their settings) they work faster than a
person: at pace 10 their waking day runs in ten rounds, planned a few at a time and building
on how the last ones went, and uses more of your cap
([pace](SETUP.md#how-every-agent-works)). Through the day they do them, for real, in
writing: the things `makes` lists, notes, reading notes on a book chapter or an article.
What they make is kept as a work; what they did becomes facts about themselves, so they can
tell you about it. If you're chatting when something is due, it waits 30 minutes; past
their daily budget, the rest is missed. At `journal_at` (22:00) they write a journal entry,
unless they made nothing and talked with no one all day: then there's nothing to write.

When something they set out to do fails (a piece of work, a reminder, a promised message),
they try again later, three more times, then let it go.

**Works**, from the panel about them (or a title on Today), is everything they've made for
themselves or for you, newest first: read any of it (headings, lists and tables shown as
they're meant, every line break kept), download it as a PDF, a Word file or whatever else it
can be made into, or have them send it into your chat with **Send to me**, as the file it's
best made into. Their journal stays theirs.

**Growth**, from the panel about them, is who they want to become: each dream with the goals
that serve it, a goal waiting for your yes marked so, a goal about a skill with its level and
its learning plan's stages, their newest weekly review as they wrote it, and the last seven
days counted (works made, requests you approved, your calls they guessed).

**Character**, from the panel about them, is who they are and how they work: their story a
line at a time, their personality, their day (when they plan, write their journal and review
their week, and when they check in with you) and their routine, all yours to change. What
they make and their dreams are shown; dreams change in their weekly review, with your yes.
Each change is a version you can go back from, and reaches them within a minute: at their
next tick they rest and wake at once with it, keeping what they had in hand.

**How you decide**, from the same panel, is what they've learnt about how you make calls:
the principles they may use (only the ones you keep), those their weekly review proposed
from your calls (Keep or Remove), and your own, added in a sentence; then the calls you
answered, what they'd guessed, and why you chose as you did.

**Mind** shows how they think: today's decisions counted, and every decision, newest first.
Open one to see its whole chain: what System 1 asked itself and how sure it was, what
System 2 wrote, and how long each step took and what it cost. Say whether a read was right,
or what it was; that's kept to score System 1 in replays, and changes nothing they do.

**Memory** is what they remember about you and about their own life, and where each thing
came from. Correct one in your words (used from their next reply), or have them forget it
for good.

When you're away, they slow down: after 3 days without a word from you, they do one thing
a day, and say why in their plan; after a week, their own day stops until you write, and
then it's back that day. Your reminders and promises still arrive on time.

## Connections and tools

Every agent can look things up on Wikipedia, in a reply to you or in their own day, and
read public-domain books from Project Gutenberg a passage a day; their **Tools** page, from
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

## Who they want to become

- **Dreams** (years) lead to **goals** (a "done when" and a due date), and goals to
  **projects**. Their character starts them with dreams and skills; after that
  they're theirs. Ask them to learn something ("add sales to your goals") and they take it
  on as their own goal, with a new skill and a learning plan.
- **Skills grow from evidence.** System 1 scores each work that shows a skill, 1 to 10
  ([decisions/skill_level.yaml](../decisions/skill_level.yaml)); your feedback in chat ("that
  verse was weak") counts double. A skill's level is the average of its last 10 scores.
- **Lessons:** specific advice on doing a skill well ("cut the hook to six words"), from
  your corrections, their own mistakes, and studying how others do it. Each skill's newest
  lessons go into every practice. At most once a day they may send you something they made
  and ask what you think.
- **Learning plans:** a goal about a skill gets a plan, the way a self-taught learner makes
  one: three to five stages, each with what to learn, a practice piece, a "done when", and
  resources to study (books and articles). Their days follow the current stage.
- **The weekly review** (`review_at`, Sunday 20:00, part of their own day): they look back at the week, update
  each goal, set new ones (chosen, like their days, to make them more useful to you), move
  learning plans on, retire absorbed lessons, and may want to change a dream (which asks you
  first). The review is private.

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
| `publish` | putting a work on their public page | ask |
| `dream` | adding, rewording or dropping a dream | ask |
| `external_message` | email, posts and DMs outside the chat | forbid: nothing leaves the chat yet |
| `spend` | money | forbid |

Anything only for you (a reminder you asked for, a promise to you) never needs your OK.
When they ask, you get a card in the chat: what they want to do, the exact words, where it
goes, when, whether it can be undone, why they ask (under "Why this?"), and when no answer
becomes no. **Approve** or **Deny**; or, when it has words to change (a message, a dream),
**Change it**: edit them and approve your version, which is what they use. Unanswered after
24 hours, it's denied. "Needs you" in your agents' list shows who's waiting. You can also change permissions by
telling them ("you can send reminders without asking"); money and messages outside the
chat never change by chat, nor in the app.

**Settings**, from the panel about them, is that agent's own: each class above as "On their
own", "Asks you" or "Never" (the ones Tinio fixes shown, not changed), their pace (1, 3 or
10), their quiet hours (no check-ins, requests or calls then; they wait until it ends, while
reminders and promises you asked for still come), pausing them (they stop working and
hearing anything; what you send is read when you resume them), and deleting them, for good,
once you type their name. A change reaches them within a minute.

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
- **How it turned out.** A few days after a call, their Sunday review looks back at what
  actually happened and judges the decision apart from the luck: a *good call*, *lucky*
  (it went well despite the decision), *unlucky* (a good decision that went badly anyway)
  or a *bad call*. Your past decisions then carry how they turned out, and bad calls and lucky breaks can become
  new principles, proposed for you to approve.
- **Calls.** When they decide something that's your kind of call, they note the options,
  their pick, why, and how sure they are you'd agree:

  | Call | Where | What happens |
  |---|---|---|
  | `focus`: what to work on today | their day plan | they act on their pick; you grade it |
  | `start`: something new, or keep going | their day plan | they act on their pick; you grade it |
  | `new_goal`, `drop_goal` | their weekly review | it waits for your answer; unanswered in 24h, they go with their pick |
  | `publish`, `commit`, `reach_out`, `dream` | approvals | your yes or no counts as agreeing or not |

  Each call also says what's at stake (low, medium or high) and whether it can be undone.
  In the chat you get a button per option and **Something else**:
  every goal call, and one day-plan call a day: the one that puts the most at stake and
  they're least sure of (the others are kept, not asked). Pick
  differently and they ask what tipped it; your next message is your reason. Every answer
  becomes one of your decisions. Their weekly review turns
  disagreements into new principles, proposed for you to approve.

## Corrections

- **Wrong facts:** "I live in Lekki, not Ikoyi" replaces the fact. Anyone can correct what
  they know about them; only you can correct what they know about themselves.
- **Forgetting:** "forget what I told you about the bank" removes it for good.
- **Misreads:** "that's not what I meant, I was asking for help" labels System 1's read of
  your last message.

## Not here yet

From v0.4, coming back per agent: chatting with them on Telegram, their public profile
page, tools from MCP servers you run yourself, and backing up, restoring and exporting an
agent (until then, copy the data folder while Tinio isn't running). Searching and reading
the open web comes later too. And the app itself: the web app, then the desktop app.
