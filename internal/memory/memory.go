// Package memory holds what the agent remembers: conversations, facts, the
// people it knows, and what it means to do.
package memory

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Exchange is one message and the agent's reply ("" if none was recorded).
// Message is "" when the agent spoke first.
type Exchange struct {
	At      time.Time
	Message string
	Reply   string
	Topic   string // "" for none
}

// Topic is something an agent and the people they talk with have going, as the
// agent keeps it. Summary is the turns up to FoldedAt in a few lines and
// Status where it stands, both as of SummedAt; Settled is what was decided
// in it, oldest first.
type Topic struct {
	ID        string
	Person    string // whom it's with, by their key
	Title     string // "" until it's named
	Summary   string
	Status    string
	Settled   []string
	Quiet     bool // finished or silent a while; open again once it's brought up
	StartedAt time.Time
	LastAt    time.Time
	FoldedAt  time.Time // zero: nothing folded yet
	SummedAt  time.Time // zero: never summed up
	LastSaid  string    // the newest message in it; filled only by Store.Topics
	// Embedding is of its title and summary, by EmbeddedWith's model; filled
	// only by Store.OldTopics, and nil until it's first needed.
	Embedding    []float32
	EmbeddedWith string
}

const (
	KnownFromBirth = "born" // known from the start, like a parent; never overwritten by a claim
	KnownByMeeting = "met"
	KnownByTelling = "told"
)

const CircleStranger = "stranger"

type Person struct {
	Key       string // e.g. "telegram:123"
	Name      string
	Relation  string // e.g. "Busta is Daniel's father."; "" if not known
	Circle    string // family, friend, school, ... or CircleStranger
	Source    string // KnownFromBirth, KnownByMeeting or KnownByTelling
	FirstSeen time.Time
	UpdatedAt time.Time
}

const SubjectSelf = "self"

type Fact struct {
	ID        ulid.ULID
	PerceptID ulid.ULID
	Subject   string // SubjectSelf, or the person's key (e.g. "telegram:123")
	Text      string // as said; pronouns are resolved to names when used
	At        time.Time
	Areas     []string // e.g. ["home", "family"]; empty if not labelled
}

// Intention is something the agent means to do later: for a person, a
// reminder or a promised task; for itself (SubjectSelf), an activity of its day.
type Intention struct {
	ID        ulid.ULID
	Subject   string    // SubjectSelf, or who it is for, e.g. "telegram:123"
	Kind      string    // IntentionRemind, IntentionTask or IntentionActivity
	PerceptID ulid.ULID // zero if the agent planned it itself
	ProjectID ulid.ULID
	Text      string
	Ability   string // e.g. "read_book"; "" if it only writes
	Args      map[string]string
	DueAt     time.Time
	Status    string // IntentionPending, IntentionAwaiting, IntentionDone, IntentionMissed or IntentionCancelled
	Result    string // e.g. the title of what it made; "" until done
	Attempts  int    // tries that failed
	CreatedAt time.Time
	DoneAt    time.Time
}

const (
	IntentionRemind   = "remind"
	IntentionTask     = "task"
	IntentionActivity = "activity"
)

const (
	IntentionPending   = "pending"
	IntentionDone      = "done"
	IntentionMissed    = "missed"   // could not be done in time
	IntentionAwaiting  = "awaiting" // waiting for the owner's approval (the autonomy gate)
	IntentionCancelled = "cancelled"
)

type Project struct {
	ID        ulid.ULID
	Title     string
	Kind      string // music, writing, reading, learning, ...
	Status    string // ProjectActive, ProjectDone or ProjectDropped
	Notes     string // where it got to, so the next session continues from there
	SourceID  ulid.ULID
	Position  int      // in bytes of its text
	Source    *Reading // set only when loaded with the project
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (p Project) PercentRead() int {
	if p.Source == nil || p.Source.Chars <= 0 {
		return 0
	}
	return min(100, p.Position*100/p.Source.Chars)
}

const (
	ReadBook    = "book"    // a public-domain book, read over many days
	ReadArticle = "article" // a Wikipedia article, read at once
)

// Reading is a book or article, without its text.
type Reading struct {
	Kind   string // ReadBook or ReadArticle
	Title  string
	Author string
	Chars  int // in bytes
}

type Source struct {
	ID ulid.ULID
	Reading
	URL       string
	Text      string
	FetchedAt time.Time
}

const (
	ProjectActive  = "active"
	ProjectDone    = "done"
	ProjectDropped = "dropped"
)

type Work struct {
	ID          ulid.ULID
	IntentionID ulid.ULID
	ProjectID   ulid.ULID
	Kind        string // journal, lyrics, beat_idea, notes, ...
	Title       string
	Text        string
	CreatedAt   time.Time
	// Data is what it carries beside its words, defined by the file made from
	// it (render.DataFormat), like a beat's score; nil if none.
	Data     json.RawMessage
	DataKind string
	// Loaded only by WorksFor. Embedding is nil until embedded; MadeFor is
	// whom it was made for (SubjectSelf for itself), "" if not for an intention.
	Embedding    []float32
	EmbeddedWith string
	MadeFor      string
}

// Carries is whether w carries data of kind; every work carries its words ("").
func (w Work) Carries(kind string) bool {
	return kind == "" || kind == w.DataKind && len(w.Data) > 0
}

// Action is a tool that changes things, proposed by the agent. It runs only
// once approved: by the gate (auto) or by the owner (ask).
type Action struct {
	ID         ulid.ULID
	Tool       string // "server.tool"
	Class      string // the autonomy class, e.g. "write"
	Args       string // its arguments as JSON, checked when proposed
	Why        string
	Status     string // ActionProposed, ActionApproved, ActionDone, ActionFailed, ActionDenied
	Result     string // what the tool said, or why it failed or didn't run
	PerceptID  ulid.ULID
	ProposedAt time.Time
	DoneAt     time.Time
}

const (
	ActionProposed = "proposed" // waiting for the owner
	ActionApproved = "approved" // runs on the next tick
	ActionDone     = "done"
	ActionFailed   = "failed"
	ActionDenied   = "denied" // the owner said no, or didn't answer
)

// Watched is one thing a watched tool returned: its ID tells it apart from
// what was seen before.
type Watched struct {
	ID   string
	Text string
	// Whole is a result that isn't a list: a new one replaces the last, so
	// what changed is what matters.
	Whole bool
}

// KindAction marks an approval as one for an Action (its Ref).
const KindAction = "action"

type Approval struct {
	ID        ulid.ULID
	Class     string // the action class, e.g. "reach_out"
	Person    string
	What      string
	Text      string    // reach_out: the message it would send; a dream: its words; an action: its arguments
	Kind      string    // reach_out: the percept kind, e.g. "reminder"; a dream: add, reword or drop
	Why       string    // why it asks; "" if it didn't say
	Ref       ulid.ULID // the intention or work it is about
	Status    string    // ApprovalPending, ApprovalApproved, ApprovalDenied or ApprovalExpired
	AskedAt   time.Time
	SentAt    time.Time // when the owner was asked; zero until then
	DecidedAt time.Time
	Message   json.RawMessage // where the request was sent, to update it after; nil until sent
	Topic     string          // the topic it came up in; "" for none
}

// Request is a request to the owner part by part, for a channel that shows it
// as a card; Text is all of it in words, for one that doesn't.
type Request struct {
	Text  string
	Title string // e.g. "Add a dream?"
	// Quote is the exact words it would send, add or use; "" if none.
	Quote string
	Where string
	When  string
	Undo  string
	Final bool // it can't be undone
	Why   string
	// Expires is when no answer becomes no; zero: never.
	Expires time.Time
	// Editable: the owner may change Quote before saying yes (their version is what's used).
	Editable bool
}

const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalDenied   = "denied"
	ApprovalExpired  = "expired" // not answered in time: denied
)

// Morning is what the agent sends their owner one morning: what it read of
// their yesterday and the things it chose for their today. Day is the date,
// YYYY-MM-DD, in the owner's time zone; SentAt is zero until the note is sent.
type Morning struct {
	Day        string
	PreparedAt time.Time
	SentAt     time.Time
	Opening    string // the greeting and yesterday, in a line or two
	Question   string // "" when nothing was missing
	Items      []MorningItem
}

// MorningItem is one thing for the owner's day. Intention is the draft the
// agent set out to make for it ("" for none), Work what came of it ("" until
// made). Source is where it came from, as the owner would say it.
type MorningItem struct {
	ID        string `json:"id"`
	What      string `json:"what"`
	How       string `json:"how"`
	Source    string `json:"source"`
	Intention string `json:"intention,omitempty"`
	Work      string `json:"work,omitempty"`
	Done      bool   `json:"done"`
}

// Label is what a System 1 answer should have been, from the owner.
type Label struct {
	DecisionID ulid.ULID
	Question   string
	Value      string // a choice, or "true" / "false" for a yes/no question
	Source     string // LabelFromChat, LabelFromConsole or LabelFromApp
	At         time.Time
}

const (
	LabelFromChat    = "chat"
	LabelFromConsole = "console"
	LabelFromApp     = "app" // the owner, on the app's Mind page
)

type Skill struct {
	Name      string
	Level     float64 // 1 to 10, from recent evidence
	ShownBy   []string
	UpdatedAt time.Time
}

type Evidence struct {
	ID     ulid.ULID
	Skill  string
	WorkID ulid.ULID // zero for feedback on no one work
	Score  float64   // 1 to 10
	Source string    // EvidenceSystem1, EvidenceOwner or EvidenceLife
	Note   string
	At     time.Time
}

const (
	EvidenceSystem1 = "system1"
	EvidenceOwner   = "owner"
	EvidenceLife    = "life" // the level its life story starts it at: a first score, outweighed as real evidence comes
)

type Lesson struct {
	ID        ulid.ULID
	Skill     string
	Text      string
	Source    string // LessonFromOwner, LessonFromSelf or LessonFromOthers
	WorkID    ulid.ULID
	At        time.Time
	RetiredAt time.Time // zero while active
}

const (
	LessonFromOwner  = "owner"
	LessonFromSelf   = "self"
	LessonFromOthers = "others"
)

type Principle struct {
	ID        ulid.ULID
	Text      string
	Kind      string // PrincipleRule, PrincipleRank or PrincipleLimit
	Rank      int    // a priority's place, 1 first; 0 for the others
	Types     []string
	Status    string   // JudgmentProposed, JudgmentApproved or JudgmentRejected
	Source    string   // JudgmentFromSeed or JudgmentFromReview
	Seed      string   // its text in the seed file; empty for others
	Cases     []string // keys of the cases behind it
	CreatedAt time.Time
	UpdatedAt time.Time
}

type JudgmentCase struct {
	ID        ulid.ULID
	Key       string // the seed file's key; the ID for others
	Type      string // general, engineering, product, autonomy or life
	Situation string
	Options   []string
	Choice    string
	Reason    string // the owner's own words; empty when they gave none
	Status    string
	Source    string
	Outcome   string // set when it came from a call that was looked back on
	At        time.Time
	UpdatedAt time.Time
	// Embedding is nil until embedded; EmbeddedWith names the model.
	Embedding    []float32
	EmbeddedWith string
}

// Text is what is embedded.
func (c JudgmentCase) Text() string {
	t := strings.TrimSuffix(c.Situation, ".") + "."
	if len(c.Options) > 0 {
		t += " Options: " + strings.Join(c.Options, "; ") + "."
	}
	t += " Chose: " + strings.TrimSuffix(c.Choice, ".") + "."
	if c.Reason != "" {
		t += " Why: " + c.Reason
	}
	return t
}

// Taste is something its owner likes or doesn't in its work, beyond how good
// it is: "likes hooks built on one repeated word".
type Taste struct {
	ID        ulid.ULID
	Kind      string // e.g. "beat_idea"; "" any
	Likes     bool
	Text      string
	WorkID    ulid.ULID
	Source    string // TasteFromTelegram or TasteFromConsole
	At        time.Time
	RetiredAt time.Time // zero while true
}

const (
	TasteFromTelegram = "telegram"
	TasteFromConsole  = "console"
)

const (
	PrincipleRule  = "principle"
	PrincipleRank  = "priority" // what matters most, in order: the higher wins when rules clash
	PrincipleLimit = "boundary" // never, whatever the gain
)

const (
	JudgmentProposed = "proposed" // waiting for the owner's review; not used
	JudgmentApproved = "approved"
	JudgmentRejected = "rejected"

	JudgmentFromSeed   = "seed"   // the owner's judgment file
	JudgmentFromReview = "review" // drawn by its weekly review from where the owner chose differently
	JudgmentFromOwner  = "owner"  // written by the owner, in the app
)

// Call is a decision where it predicted what its owner would choose. The
// owner's answer, when they give one, says whether they agree.
type Call struct {
	ID          ulid.ULID
	Type        string // CallFocus, CallStart, ...
	Mode        string // CallAfter or CallBefore
	Situation   string
	Options     []string
	Pick        int
	Why         string
	Confidence  float64 // how sure it is the owner would agree, 0 to 1; 0 unknown
	Stakes      string  // StakesLow, StakesMedium or StakesHigh; "" unknown
	Reversible  bool
	Ref         ulid.ULID
	Status      string // CallLogged, CallAsked, CallAnswered or CallExpired
	Waiting     string // CallWaitingReason or CallWaitingAnswer: what the owner's next message is; "" nothing
	WaitingAt   time.Time
	OwnerPick   int // an index into Options; -1 their own answer (OwnerChoice)
	OwnerChoice string
	OwnerReason string
	Agreed      *bool // nil until answered
	AnsweredBy  string
	Message     json.RawMessage
	CaseID      ulid.ULID
	CreatedAt   time.Time
	AskedAt     time.Time
	AnsweredAt  time.Time
	// Looked back on later; empty until then.
	Outcome     string // OutcomeWell, OutcomeMixed or OutcomeBadly
	Verdict     string // VerdictGood, VerdictLucky, VerdictUnlucky or VerdictBad
	OutcomeNote string
	OutcomeBy   string // OutcomeBySelf or OutcomeByOwner
	OutcomeAt   time.Time
}

const (
	StakesLow    = "low"    // little lost if it's wrong: an afternoon, a draft
	StakesMedium = "medium" // days of work, a goal, someone's expectations
	StakesHigh   = "high"   // money, reputation, a relationship, something public
)

// StakesLevels are lowest first.
var StakesLevels = []string{StakesLow, StakesMedium, StakesHigh}

func (c Call) AtStake() float64 {
	w := map[string]float64{StakesLow: 1, StakesMedium: 2, StakesHigh: 3}[c.Stakes]
	if w == 0 {
		w = 2
	}
	if !c.Reversible {
		w *= 2
	}
	return w
}

const (
	OutcomeWell  = "went_well"
	OutcomeMixed = "mixed"
	OutcomeBadly = "went_badly"
)

const (
	VerdictGood    = "good_call" // a good decision, and it went well
	VerdictLucky   = "lucky"     // it went well despite the decision
	VerdictUnlucky = "unlucky"   // a good decision that went badly anyway
	VerdictBad     = "bad_call"  // it went badly because of the decision

	OutcomeBySelf  = "self"
	OutcomeByOwner = "owner" // its owner, which its review never overrides
)

var (
	Outcomes = []string{OutcomeWell, OutcomeMixed, OutcomeBadly}
	Verdicts = []string{VerdictGood, VerdictLucky, VerdictUnlucky, VerdictBad}
)

const ()

const (
	CallAfter  = "after"  // it acts on its pick; the owner grades it later
	CallBefore = "before" // it waits for the owner's answer; unanswered, it goes with its pick

	CallLogged   = "logged" // made, not asked: graded in the console, if ever
	CallAsked    = "asked"
	CallAnswered = "answered"
	CallExpired  = "expired"

	CallWaitingReason = "reason" // the owner chose differently: their next message says why
	CallWaitingAnswer = "answer" // the owner chose something else: their next message says what, and why

	AnsweredOnTelegram = "telegram"
	AnsweredInConsole  = "console"
	AnsweredByApproval = "approval" // answering an approval: a yes or no to what it wanted
)

func CallCaseType(callType string) string {
	switch callType {
	case "publish":
		return "product"
	case "commit", "reach_out":
		return "autonomy"
	}
	return "life"
}

// Agreement is over the latest calls of one type the owner answered.
type Agreement struct {
	Type     string
	Answered int
	Agreed   int
}

const ()

// Conversation is where the agent last talked with a person, so it can
// message them first.
type Conversation struct {
	Person       string
	Source       string
	Thread       string          // e.g. the Telegram chat ID
	Payload      json.RawMessage // the channel's details from their last message, used to address a message
	LastActivity time.Time
}

// SelfVersion is one version of a section of the person (their character,
// how they work, their owner's judgment), as YAML; the newest is the one used.
type SelfVersion struct {
	ID      ulid.ULID
	Section string // SectionCharacter or SectionJudgment
	Doc     string
	Source  string // SelfImported, SelfNew, SelfEdited, SelfRolledBack
	At      time.Time
}

const (
	SectionCharacter = "character"
	SectionJudgment  = "judgment"
)

var Sections = []string{SectionCharacter, SectionJudgment}

const (
	SelfImported   = "import" // from files: their old self/ folder, or tinio agent import
	SelfNew        = "new"    // tinio new
	SelfEdited     = "edit"
	SelfRolledBack = "rollback"
)

// AgentSkill is a skill as stored: every file in it by its slash-separated
// path, SKILL.md included.
type AgentSkill struct {
	Name        string
	Description string
	Files       map[string][]byte
	AddedAt     time.Time
	// Source is where it came from: "" their owner imported it, or
	// market:<handle>@<version> taught from the market.
	Source string
}

// ChatMessage is one row of an agent's web chat, from their owner or from
// them: a message, or what Telegram's buttons and documents carry.
type ChatMessage struct {
	ID     string
	Author string // ChatOwner or ChatAgent
	Kind   string // ChatMessageKind ("" is one), ChatAsk, ChatChoice, ChatFile, ChatApprove, ChatPick, ChatHello, ChatReact, ChatJob, ChatJobAsk or ChatJobReply
	Text   string
	// Payload says which request, call or file: for ChatApprove a
	// perception.Answer, for ChatPick a perception.Pick. nil for a message.
	Payload   json.RawMessage
	At        time.Time
	SettledAt time.Time // when an ask or choice ended (its text then says how); zero until then
	Topic     string    // the topic it's in; "" for none
	ReplyTo   string    // the row it replies to, when the owner chose one; ""
}

const (
	ChatOwner = "owner"
	ChatAgent = "agent"
	// ChatSource is the percepts' source for the owner's chat messages.
	ChatSource = "web"

	ChatMessageKind = "message"
	ChatAsk         = "ask"       // the agent asks the owner's permission: Approve or Deny
	ChatChoice      = "choice"    // the agent asks the owner to pick, for their judgment
	ChatFile        = "file"      // the agent sends a file: its payload names it
	ChatApprove     = "approve"   // the owner's answer to an ask
	ChatPick        = "pick"      // the owner's pick on a choice
	ChatHello       = "hello"     // the owner opened the chat for the first time: never shown
	ChatReact       = "react"     // the owner's thumb on a work: never shown
	ChatJob         = "job"       // a coding job's card, updated as it goes: its payload is the job
	ChatJobAsk      = "job_ask"   // a coding job's question to the owner: Allow once, for the job, or not
	ChatJobReply    = "job_reply" // the owner's reply to a job: it goes to the job, never the agent
)

// HelloID is the id of an agent's hello row: the same for every agent, so
// there's only ever one, and the earliest, so it sorts first.
const HelloID = "00000000000000000000000000"

// SentFile is a file an agent sent in their chat.
type SentFile struct {
	ID          string
	Name        string
	ContentType string
	Data        []byte
	At          time.Time
}
