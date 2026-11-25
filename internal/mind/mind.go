// Package mind runs the cognitive cycle: take a percept, decide, act, record.
//
// System 1 (fast, typed answers) reads every text message and System 2 (slow
// reasoning) replies as the agent itself. Pronouns are resolved to names
// before System 1 reads anything, so "your church" and "my church" are never
// confused. A second loop on the clock's ticks lets the agent speak first
// (initiative.go) and live a day of its own (life.go).
package mind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/agentskill"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/embed"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/render"
	"github.com/Grandbusta/tinio/internal/search"
	"github.com/Grandbusta/tinio/internal/usage"
)

const nonTextSetID = "reflex.non_text@1"

// draftEvery is how often, at most, a reply being written is shown again.
const draftEvery = 150 * time.Millisecond

type Store interface {
	SavePercept(ctx context.Context, p perception.Percept) (isNew bool, err error)
	SaveDecision(ctx context.Context, d decision.Decision) error
	RecentExchanges(ctx context.Context, source, thread string, since time.Time,
		exclude ulid.ULID, limit int) ([]memory.Exchange, error)
	SaveFact(ctx context.Context, f memory.Fact) error
	RecallCandidates(ctx context.Context, subjects, areas []string, limit int) ([]memory.Fact, error)
	UnlabelledFacts(ctx context.Context, limit int) ([]memory.Fact, error)
	SetFactAreas(ctx context.Context, id ulid.ULID, areas []string) error
	Person(ctx context.Context, key string) (memory.Person, bool, error)
	KnownPeople(ctx context.Context, limit int) ([]memory.Person, error)
	SavePerson(ctx context.Context, p memory.Person) error
	AddPersonIfNew(ctx context.Context, p memory.Person) (bool, error)
	SaveIntention(ctx context.Context, i memory.Intention) error
	DueIntentions(ctx context.Context, now time.Time, limit int) ([]memory.Intention, error)
	IntentionsDue(ctx context.Context, from, to time.Time) ([]memory.Intention, error)
	FinishIntention(ctx context.Context, id ulid.ULID, status, result string, at time.Time) error
	PostponeIntention(ctx context.Context, id ulid.ULID, due time.Time) error
	SetIntentionArg(ctx context.Context, id ulid.ULID, key, value string) error
	RetryIntention(ctx context.Context, id ulid.ULID, due time.Time) error
	OpenIntentions(ctx context.Context, subject, kind string) ([]memory.Intention, error)
	AwaitIntention(ctx context.Context, id ulid.ULID) error
	ResumeIntention(ctx context.Context, id ulid.ULID, notBefore time.Time) error
	SaveApproval(ctx context.Context, a memory.Approval) error
	Approval(ctx context.Context, id ulid.ULID) (memory.Approval, bool, error)
	UnsentApprovals(ctx context.Context, limit int) ([]memory.Approval, error)
	MarkApprovalSent(ctx context.Context, id ulid.ULID, message []byte, at time.Time) error
	DecideApproval(ctx context.Context, id ulid.ULID, status, text string, at time.Time) (bool, error)
	StaleApprovals(ctx context.Context, before time.Time, limit int) ([]memory.Approval, error)
	SaveAutonomyRule(ctx context.Context, class, verdict, by string, percept ulid.ULID, at time.Time) error
	AutonomyRules(ctx context.Context) (map[string]string, error)
	DeleteFact(ctx context.Context, id ulid.ULID) (bool, error)
	SaveLabel(ctx context.Context, l memory.Label) error
	LastRead(ctx context.Context, setID, source, thread string, before time.Time) (ulid.ULID, bool, error)
	AddSkill(ctx context.Context, sk memory.Skill) error
	Skills(ctx context.Context) ([]memory.Skill, error)
	SetSkillLevel(ctx context.Context, name string, level float64, at time.Time) error
	SaveEvidence(ctx context.Context, e memory.Evidence) error
	RecentEvidence(ctx context.Context, skill string, limit int) ([]memory.Evidence, error)
	SaveLesson(ctx context.Context, l memory.Lesson) error
	Lessons(ctx context.Context, skill string, limit int) ([]memory.Lesson, error)
	RetireLesson(ctx context.Context, id ulid.ULID, at time.Time) error
	RecentFound(ctx context.Context, limit int) ([]search.Result, error)
	SeedJudgment(ctx context.Context, cases []memory.JudgmentCase, principles []memory.Principle) (int, int, error)
	Principles(ctx context.Context, status string) ([]memory.Principle, error)
	JudgmentCases(ctx context.Context, status string, types []string) ([]memory.JudgmentCase, error)
	SavePrinciple(ctx context.Context, p memory.Principle) error
	SaveCaseEmbedding(ctx context.Context, id ulid.ULID, v []float32, model string) error
	SaveTaste(ctx context.Context, t memory.Taste) error
	Tastes(ctx context.Context, limit int) ([]memory.Taste, error)
	SaveCall(ctx context.Context, c memory.Call) error
	Call(ctx context.Context, id ulid.ULID) (memory.Call, bool, error)
	RecentCalls(ctx context.Context, status string, limit int) ([]memory.Call, error)
	CallsAskedSince(ctx context.Context, mode string, since time.Time) (int, error)
	MarkCallAsked(ctx context.Context, id ulid.ULID, where json.RawMessage, at time.Time) error
	StaleCalls(ctx context.Context, before time.Time, limit int) ([]memory.Call, error)
	ExpireCall(ctx context.Context, id ulid.ULID) (bool, error)
	WaitForOwner(ctx context.Context, id ulid.ULID, waiting string, at time.Time) error
	WaitingCall(ctx context.Context, since time.Time) (memory.Call, bool, error)
	AnswerCall(ctx context.Context, c memory.Call, asCase bool) (bool, error)
	GiveCallReason(ctx context.Context, id ulid.ULID, reason string, at time.Time) error
	CallsToLookBack(ctx context.Context, types []string, from, to time.Time, limit int) ([]memory.Call, error)
	SetOutcome(ctx context.Context, id ulid.ULID, outcome, verdict, note, by string, at time.Time) (bool, error)
	IntentionWork(ctx context.Context, intention ulid.ULID) (memory.Work, bool, error)
	Work(ctx context.Context, id ulid.ULID) (memory.Work, bool, error)
	SaveAction(ctx context.Context, a memory.Action) error
	SeenFrom(ctx context.Context, source string) (bool, error)
	LastFrom(ctx context.Context, source string) (string, bool, error)
	SetAction(ctx context.Context, id ulid.ULID, from []string, status, result string, at time.Time) (bool, error)
	Actions(ctx context.Context, statuses []string, limit int) ([]memory.Action, error)
	ActionsSince(ctx context.Context, since time.Time) (int, error)
	SetWorkData(ctx context.Context, id ulid.ULID, kind string, data json.RawMessage) error
	WorksMissingData(ctx context.Context, kinds []string, limit int) ([]memory.Work, error)
	Intention(ctx context.Context, id ulid.ULID) (memory.Intention, bool, error)
	WorksFor(ctx context.Context, person string, limit int) ([]memory.Work, error)
	SaveWorkEmbedding(ctx context.Context, id ulid.ULID, v []float32, model string) error
	SaveProject(ctx context.Context, p memory.Project) error
	Project(ctx context.Context, id ulid.ULID) (memory.Project, bool, error)
	ActiveProjects(ctx context.Context, limit int) ([]memory.Project, error)
	SaveWork(ctx context.Context, w memory.Work) error
	WorksBetween(ctx context.Context, from, to time.Time) ([]memory.Work, error)
	ProjectWorks(ctx context.Context, project ulid.ULID, limit int) ([]memory.Work, error)
	LatestWork(ctx context.Context, kind string) (memory.Work, bool, error)
	CostSince(ctx context.Context, since time.Time, actions []string) (float64, error)
	Conversations(ctx context.Context) ([]memory.Conversation, error)
	LastPercept(ctx context.Context, person, kind string) (time.Time, bool, error)
	// Standing is its account's use of the week; the zero value meters nothing.
	Standing(ctx context.Context) (usage.Standing, error)
	UnaskedFacts(ctx context.Context, person string, since time.Time, limit int) ([]memory.Fact, error)
	MarkFactsAsked(ctx context.Context, ids []ulid.ULID, at time.Time) error
	NextDue(ctx context.Context, now time.Time, promiseLead, askExpires time.Duration, canAsk bool, sumAfter time.Duration) (time.Time, bool, error)
	Topics(ctx context.Context, person string, quietSince time.Time, limit int) ([]memory.Topic, error)
	OldTopics(ctx context.Context, person string, before time.Time, limit int) ([]memory.Topic, error)
	SaveTopicEmbedding(ctx context.Context, id string, v []float32, model string) error
	Topic(ctx context.Context, id string) (memory.Topic, bool, error)
	StartTopic(ctx context.Context, t memory.Topic) error
	SetTopic(ctx context.Context, percept ulid.ULID, topic string, at time.Time) error
	NameTopic(ctx context.Context, id, title string) error
	TopicExchanges(ctx context.Context, topic string, after time.Time, exclude ulid.ULID, limit int) ([]memory.Exchange, error)
	TopicsToSum(ctx context.Context, before time.Time, limit int) ([]memory.Topic, error)
	SumTopic(ctx context.Context, t memory.Topic) error
	QuietTopics(ctx context.Context, before time.Time) error
	PerceptTopic(ctx context.Context, id ulid.ULID) (string, error)
	RecentApprovals(ctx context.Context, limit int) ([]memory.Approval, error)
	Morning(ctx context.Context, day string) (memory.Morning, bool, error)
	SaveMorning(ctx context.Context, mo memory.Morning) error
	MorningSent(ctx context.Context, day string, at time.Time) error
}

// WorkingMemory bounds how much of the current conversation the mind keeps
// in view: at most Turns earlier exchanges, none older than Window.
type WorkingMemory struct {
	Turns  int
	Window time.Duration
}

type Replier interface {
	Reply(ctx context.Context, p perception.Percept, text string) error
}

// Drafter shows a reply while it's written, all of it so far each time; the
// Replier then sends it.
type Drafter interface {
	Draft(ctx context.Context, p perception.Percept, text string) error
}

// Prompt's Ref versions Text and is recorded on every System 2 decision.
type Prompt struct {
	Ref  string
	Text string
}

type Config struct {
	Life character.Character
	// Judgment is seeded for the owner to review; only what they approve is used.
	// JudgmentApproved seeds it approved: a ready character's, which Tinio wrote
	// and stands behind, rather than the owner.
	Judgment         character.Judgment
	JudgmentApproved bool
	Percepts         <-chan perception.Percept
	Store            Store
	Replier          Replier
	Drafter          Drafter // nil: a reply shows only once it's written
	// Making tells the owner's app what's being made for them ("" when it's
	// done); nil: nothing's said.
	Making  func(what string)
	System1 system1.DecisionModel
	System2 system2.Reasoner
	// DaySystem2 thinks for its own day: planning, learning, the weekly review,
	// what it watches. nil: System2. Replies, and what it says to someone,
	// always use System2.
	DaySystem2 system2.Reasoner
	// WorkSystem2 makes works, for itself or someone: an activity's piece, the
	// steps to it, a work's data. nil: System2.
	WorkSystem2 system2.Reasoner
	// LightSystem2 writes replies once its account's week is used (light mode).
	// nil: System2.
	LightSystem2 system2.Reasoner
	Incoming     questions.Set
	FollowUp     questions.Set // zero value: never checked
	// LifeArea zero value: nothing is labelled and every reply searches memory.
	LifeArea     questions.Set
	PersonCircle questions.Set
	// FactRelevance is a one-question template, asked once per fact ({fact});
	// zero value: no recall.
	FactRelevance questions.Set
	// OpenThreads replaces FactRelevance for small talk; zero value: small talk
	// recalls like any other message.
	OpenThreads      questions.Set
	RecallCandidates int // facts considered per reply: same life area first, then newest
	ReplyPrompt      Prompt
	// RichChat: the chat shows Markdown and answers to tap (the web app);
	// false: plain text, like Telegram.
	RichChat bool
	Working  WorkingMemory
	// TopicSort sorts messages into topics, so a reply is written from what
	// it's about; zero value: there are none, and replies have the last few
	// minutes. TopicPrompt sums topics up; zero: they're never summed up.
	TopicSort   questions.Set
	TopicPrompt Prompt
	// Ticks is the clock, about once a minute; nil: the agent never speaks first.
	Ticks <-chan time.Time
	// Zone is for reminders and check-in times; nil: UTC.
	Zone      *time.Location
	CheckIns  CheckIns
	Quiet     QuietHours
	Day       Day
	Morning   Morning
	Abilities *ability.Set
	// SkillLevel zero value: works aren't scored, and skills only move with feedback.
	SkillLevel questions.Set
	// Autonomy is what it may do on its own; the zero value allows everything.
	Autonomy action.Policy
	// Asker nil: requests to the owner wait unsent and expire.
	Asker Asker
	// Filer nil: everything it sends is a message.
	Filer Filer
	// Formats is the files it can send things as; nil: none.
	Formats *render.Set
	// Doers are tools that change things; ActionsPerDay caps how many it
	// proposes a day.
	Doers         []Doer
	ActionsPerDay int
	// Watchers are tools looked at on a schedule; what's new is perceived.
	Watchers []Watcher
	// Skills are know-how its owner gave it (agent_skills).
	Skills []agentskill.Skill
	Calls  Calls
	// Born: they were seeded at an earlier start (Seed), so Run doesn't again.
	Born bool
	// RestAfter, if set, ends Run with a *Resting once they've heard nothing for
	// this long and have nothing in hand or due soon. Zero: they never rest.
	RestAfter time.Duration
	// Kept is what they kept when they last went to rest; zero the first time.
	Kept Kept
	// Embedder nil: it takes the owner's newest decision of the right type instead.
	Embedder embed.Embedder
	Log      *slog.Logger
}

type Mind struct {
	Config
	policyMu sync.RWMutex // guards Autonomy: the owner can change it in a chat while its day reads it
	// sendMu lets one loop at a time send approvals: a chat asks at once, and
	// a tick sends what's unsent, so both could send the same one.
	sendMu sync.Mutex
	// skillMu guards skillVecs, the skills' descriptions embedded once, by
	// whichever loop needs them first.
	skillMu   sync.Mutex
	skillVecs map[string][]float32
	changed   atomic.Bool // they heard something since the last tick that did its work (due)
	reload    atomic.Bool // their character changed: rest at the next tick and wake at once (Reload)
	// held is when messages left unheard while their account rested are to be
	// read (unix nanoseconds; 0: none): set by the talk loop, acted on by the life loop.
	held atomic.Int64

	dataOnce    sync.Once
	dataFormats map[string]render.DataFormat // what each kind of work it makes carries, from its life; read by both loops

	// stepMu is held by the talk loop while it handles a message, and tried by
	// the life loop before it goes to rest; it guards resting and lastHeard.
	stepMu    sync.Mutex
	resting   bool
	lastHeard time.Time

	// Only the life loop touches these.
	lastTick     time.Time
	reviewed     time.Time
	reviewTries  tries                // the weekly review failing in a row
	morningDone  string               // the local date of the last morning it sent, or had none to send
	morningTries tries                // preparing the morning failing in a row
	looked       map[string]time.Time // when each watcher last looked
	unfillable   map[ulid.ULID]bool   // works whose data it failed to write; not tried again until a restart
	dataLeft     bool                 // the last tick filled in an old work's data, so there may be more
	nextDue      time.Time            // what nextWake said after the last full tick; zero: work it out
	acting       sync.Mutex           // held while approved actions run
	sumLater     map[string]time.Time // topics that failed to be summed up, and when to try again
}

func New(c Config) *Mind {
	if c.Zone == nil {
		c.Zone = time.UTC
	}
	k := c.Kept
	return &Mind{Config: c, lastTick: k.LastTick, reviewed: k.Reviewed}
}

// Run runs two loops until ctx is done, the percepts end, or they go to rest
// (a *Resting): one talks, the other lives its day on the clock's ticks, so a
// slow plan, activity or review never keeps a reply waiting.
func (m *Mind) Run(ctx context.Context) error {
	m.loadPermissions(ctx)
	if !m.Born {
		m.Seed(ctx)
	}
	m.labelUnlabelledFacts(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	lived := make(chan struct{})
	var rest *Resting
	go func() {
		defer close(lived)
		defer cancel()
		rest = m.live(ctx)
	}()
	m.talk(ctx)
	cancel()
	<-lived
	if rest != nil {
		return rest
	}
	return nil
}

// Seed copies what they were born with into memory: who their owner is to
// them, their skills, and their owner's judgment for review. Each part adds only what's
// missing, so running it again is safe but costly: dozens of writes. False if
// anything wasn't saved, so it's run again next time.
func (m *Mind) Seed(ctx context.Context) bool {
	known, skilled, judged := m.bornKnowing(ctx), m.bornWith(ctx), m.seedJudgment(ctx)
	return known && skilled && judged
}

func (m *Mind) talk(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-m.Percepts:
			if !ok {
				return
			}
			if !m.hear(ctx, p) {
				return
			}
		}
	}
}

// live returns when ctx is done, or with a *Resting when they go to rest.
func (m *Mind) live(ctx context.Context) *Resting {
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-m.Ticks:
			if m.due(now) {
				m.changed.Store(false) // before it: what's heard meanwhile counts for the next
				func() {
					defer m.survive("tick "+now.Format(time.RFC3339), nil)
					m.tick(ctx, now)
				}()
				m.planNext(ctx, now)
			}
			if r := m.restIfIdle(ctx, now); r != nil {
				return r
			}
		}
	}
}

// survive, deferred, keeps a panic in one step from ending the process, which
// many agents share: the step is logged with its stack and dropped, and err
// (if not nil) says so. recover reaches only the goroutine it runs in, so
// every goroutine the mind starts defers this.
func (m *Mind) survive(step string, err *error) {
	r := recover()
	if r == nil {
		return
	}
	m.Log.Error("a step panicked and was dropped", "step", step, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	if err != nil {
		*err = fmt.Errorf("panicked: %v", r)
	}
}

func (m *Mind) policy() action.Policy {
	m.policyMu.RLock()
	defer m.policyMu.RUnlock()
	return m.Autonomy
}

func (m *Mind) setPolicy(p action.Policy) {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	m.Autonomy = p
}

func (m *Mind) handle(ctx context.Context, p perception.Percept) {
	log := m.Log.With("percept", p.ID.String(), "source", p.Source)
	state := usage.Normal
	if p.Kind == perception.KindMessage || p.Kind == perception.KindHello {
		st, err := m.Store.Standing(ctx)
		if err != nil {
			log.Warn("reading the account's usage failed; replying as usual", "err", err)
		}
		if state = st.State(p.At); state == usage.Resting {
			// Left unsaved, so unheard: given to them again when they wake at the reset.
			m.held.Store(st.ResetsAt(p.At).UnixNano())
			m.changed.Store(true)
			log.Info("resting: the message waits for the reset", "until", st.ResetsAt(p.At))
			return
		}
	}
	isNew, err := m.Store.SavePercept(ctx, p)
	if err != nil {
		log.Error("save percept", "err", err)
		return
	}
	if !isNew {
		log.Debug("already handled, skipping", "external_id", p.ExternalID)
		return
	}
	if p.Kind == perception.KindApproval {
		m.answer(ctx, p)
		return
	}
	if p.Kind == perception.KindCall {
		m.pickAnswered(ctx, p)
		return
	}
	if p.Kind == perception.KindHello {
		m.hello(ctx, p)
		return
	}
	if p.Kind == perception.KindReaction {
		m.reacted(ctx, p)
		return
	}

	// The name comes from memory once known, so a changed account name does
	// not change them.
	person := m.meet(ctx, p)
	if person.Name != "" {
		p.SpeakerName = person.Name
	}
	if m.ownerReplied(ctx, p) { // why they chose differently, just asked
		return
	}

	start := time.Now()
	root := newDecision(p, nil, start)
	chain := []*decision.Decision{root} // root first; a System 2 hand-off is its child
	var (
		reply    string
		said     Said
		attached *render.File
		learn    func() []*decision.Decision // keeps what the reply learned, once it's sent; nil if nothing
		after    []*decision.Decision        // what the reply's answer changed: saved after it
	)
	if p.Text == "" {
		root.Tier, root.SetID, root.Action = decision.TierReflex, nonTextSetID, "reply"
		reply = "I can only read text for now."
	} else {
		recent := m.recall(ctx, p)
		checks := m.checkAhead(ctx, p)
		sorted := m.sortTopic(ctx, p, recent)
		// What it made and the skill that fits don't wait on the reading: they're
		// looked up meanwhile, and joined before the fact check reads the store.
		looked := make(chan related, 1)
		go func() {
			var r related
			defer func() { looked <- r }()
			defer m.survive("looking up works and skills", nil)
			r = m.lookUp(ctx, p, person, recent)
		}()
		var (
			why    string
			labels *lifeLabels
		)
		reply, why, labels = m.decideIncoming(ctx, p, root, recent)
		rel := <-looked
		aheadFacts, aheadThreads := checks()
		about, starts, sort := sorted()
		if sort != nil {
			sort.ParentID = &root.ID
			chain = append(chain, sort)
		}
		about = m.placeTopic(ctx, &p, about, starts, root.Action == "continue", recent)
		if why == "" {
			root.CostUSD += aheadFacts.resp.CostUSD + aheadThreads.resp.CostUSD
		} else {
			var (
				facts          []memory.Fact
				recallDecision *decision.Decision
			)
			if m.OpenThreads.ID != "" && m.readAs(root, "small_talk") {
				root.CostUSD += aheadFacts.resp.CostUSD
				facts, recallDecision = m.recallOpenThreads(ctx, p, root, aheadThreads)
			} else {
				root.CostUSD += aheadThreads.resp.CostUSD
				facts, recallDecision = m.recallFacts(ctx, p, root, labels, aheadFacts)
			}
			if recallDecision != nil {
				chain = append(chain, recallDecision)
			}
			child := newDecision(p, &root.ID, time.Now())
			writer := m.System2
			if state == usage.Light && m.LightSystem2 != nil {
				writer = m.LightSystem2
			}
			reply, said, attached, learn = m.reason(ctx, writer, p, person, root, child, m.talkedOf(ctx, p, recent, about), facts, why, rel, about)
			chain = append(chain, child)
		}
	}

	// The last decision in the chain is the one that acted, so it owns the outcome.
	actor := chain[len(chain)-1]
	actor.Reply = reply
	if attached != nil {
		actor.Reply += attachedLine(attached.Name)
	}
	actor.Outcome = &decision.Outcome{Status: decision.OutcomeOK}
	if err := m.send(ctx, p, reply, attached, said); err != nil {
		log.Warn("reply failed", "err", err)
		actor.Outcome = &decision.Outcome{Status: decision.OutcomeFailed, Detail: err.Error()}
	}
	now := time.Now()
	if learn != nil {
		after = learn()
	}
	actor.Outcome.At = now.UTC()
	actor.Latency = now.Sub(actor.At)
	if actor != root {
		root.Outcome = &decision.Outcome{Status: decision.OutcomeOK, Detail: "handed to System 2", At: now.UTC()}
		root.Latency = now.Sub(start)
	}
	for _, d := range chain {
		if d.Outcome == nil {
			d.Outcome = &decision.Outcome{Status: decision.OutcomeOK, At: now.UTC()}
		}
	}
	chain = append(chain, after...)

	for _, d := range chain {
		if err := m.Store.SaveDecision(ctx, *d); err != nil {
			log.Error("save decision", "err", err)
			return
		}
		log.Info("decided", "decision", d.ID.String(), "tier", d.Tier, "set", d.SetID,
			"action", d.Action, "provider", d.Provider, "outcome", d.Outcome.Status)
	}
}

func newDecision(p perception.Percept, parent *ulid.ULID, at time.Time) *decision.Decision {
	return &decision.Decision{ID: ulid.Make(), ParentID: parent, At: at.UTC(), PerceptID: p.ID, Gate: decision.GateAuto}
}

func (m *Mind) recall(ctx context.Context, p perception.Percept) []memory.Exchange {
	if m.Working.Turns <= 0 || p.Thread == "" {
		return nil
	}
	exchanges, err := m.Store.RecentExchanges(ctx, p.Source, p.Thread,
		p.At.Add(-m.Working.Window), p.ID, m.Working.Turns)
	if err != nil {
		m.Log.Warn("recall failed; deciding without conversation context", "percept", p.ID.String(), "err", err)
		return nil
	}
	return exchanges
}

// decideIncoming fills in d and returns either a reply or, when System 2
// should answer, the reason for handing over. labels is nil if the message
// was not filed by life area.
func (m *Mind) decideIncoming(ctx context.Context, p perception.Percept, d *decision.Decision,
	recent []memory.Exchange) (reply, toSystem2 string, labels *lifeLabels) {
	d.Tier, d.SetID = decision.TierSystem1, m.Incoming.Ref()
	n := m.namesFor(p)

	// The follow-up and life-area checks run alongside the intent read, each
	// with its own state, since extra state upsets the intent read.
	var (
		follow, life       system1.Response
		followErr, lifeErr error
		lifeLabelsFound    lifeLabels
		wg                 sync.WaitGroup
	)
	previous := lastReply(recent)
	checkFollowUp := previous != "" && m.FollowUp.ID != ""
	if checkFollowUp {
		d.SetID += "," + m.FollowUp.Ref()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.survive("follow-up check", &followErr)
			follow, followErr = m.System1.Decide(ctx, system1.Request{
				State:     map[string]any{"message": p.Text, "previous_reply": previous},
				Questions: m.FollowUp.Questions,
			})
		}()
	}
	checkLife := m.LifeArea.ID != ""
	if checkLife {
		d.SetID += "," + m.LifeArea.Ref()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.survive("life-area labelling", &lifeErr)
			lifeLabelsFound, life, lifeErr = m.labelText(ctx, n.message(p.Text), n)
		}()
	}

	// Send only what the questions need: an unrelated timestamp in the state
	// moved intent confidence by up to 0.2 in testing.
	resp, err := m.System1.Decide(ctx, system1.Request{
		State:     map[string]any{"message": p.Text},
		Questions: m.Incoming.Questions,
	})
	wg.Wait()
	if err != nil {
		m.Log.Error("system 1 failed", "percept", p.ID.String(), "err", err)
		d.Action = "error"
		return "Sorry, I couldn't think that through just now.", "", nil
	}
	d.Provider, d.Answers, d.CostUSD = resp.Provider, maps.Clone(resp.Answers), resp.CostUSD

	continues := false
	if checkFollowUp {
		if followErr != nil {
			m.Log.Warn("follow-up check failed; routing on intent alone", "percept", p.ID.String(), "err", followErr)
		} else {
			maps.Copy(d.Answers, follow.Answers)
			d.CostUSD += follow.CostUSD
			continues = isYes(m.FollowUp, follow.Answers, m.FollowUp.Primary)
		}
	}
	if checkLife {
		if lifeErr != nil {
			m.Log.Warn("life-area labelling failed; searching all of memory", "percept", p.ID.String(), "err", lifeErr)
		} else {
			maps.Copy(d.Answers, life.Answers)
			d.CostUSD += life.CostUSD
			labels = &lifeLabelsFound
		}
	}

	r := routeFor(m.Incoming, resp.Answers)
	switch {
	case continues:
		d.Action = "continue"
		return "", n.speaker + " is answering what you just asked", labels
	case r.Kind == routeAct:
		d.Action = r.Choice
		return "", "it needs a reply", labels
	case r.Kind == routeAsk:
		d.Action = string(routeAsk)
		return "", "the first read was only fairly sure", labels
	default:
		d.Action = string(routeEscalate)
		return "", "the first read was unsure", labels
	}
}

func (m *Mind) readAs(d *decision.Decision, choice string) bool {
	a, ok := d.Answers[m.Incoming.Primary]
	return ok && !a.Refused && a.Choice == choice
}

func lastReply(recent []memory.Exchange) string {
	if len(recent) == 0 {
		return ""
	}
	return recent[len(recent)-1].Reply
}

// checked is a fact check asked while the message was read: the candidates it
// was given, and System 1's answers (none if it wasn't asked or failed).
type checked struct {
	candidates []memory.Fact
	resp       system1.Response
}

// checkAhead loads the facts each way of recalling (recallFacts,
// recallOpenThreads) would consider and has System 1 check them, both in one
// call, while the message is read; wait returns what it found. Until an agent knows more facts
// than it considers, the reading doesn't change which, so a reply rarely waits
// on a check after it. The way not taken is paid for, and cheap.
func (m *Mind) checkAhead(ctx context.Context, p perception.Percept) (wait func() (facts, threads checked)) {
	var facts, threads checked
	load := func(subjects []string) []memory.Fact {
		fs, err := m.Store.RecallCandidates(ctx, subjects, nil, m.RecallCandidates)
		if err != nil {
			m.Log.Warn("loading facts to check ahead failed", "percept", p.ID.String(), "err", err)
		}
		return fs
	}
	if m.FactRelevance.ID != "" && m.RecallCandidates > 0 {
		facts.candidates = load(recallSubjects(p))
	}
	if m.OpenThreads.ID != "" && p.Speaker != "" && m.RecallCandidates > 0 {
		since := p.At.Add(-openThreadWindow)
		for _, f := range load([]string{p.Speaker}) {
			if !f.At.Before(since) {
				threads.candidates = append(threads.candidates, f)
			}
		}
	}
	all := slices.Clone(facts.candidates)
	for _, f := range threads.candidates {
		if !slices.ContainsFunc(all, func(x memory.Fact) bool { return x.ID == f.ID }) {
			all = append(all, f)
		}
	}
	if len(all) == 0 {
		return func() (checked, checked) { return facts, threads }
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var err error
		defer m.survive("checking facts ahead", &err)
		n := m.namesFor(p)
		var resp system1.Response
		resp, err = m.System1.Decide(ctx, system1.Request{
			State: map[string]any{"facts": factTexts(all, n), "message": n.message(p.Text)},
			Questions: slices.Concat(factQuestions(m.FactRelevance, facts.candidates, all, "relevant:", n),
				factQuestions(m.OpenThreads, threads.candidates, all, "open:", n)),
		})
		// One call, counted once: with the facts, unless there were none.
		if len(facts.candidates) > 0 {
			facts.resp.CostUSD = resp.CostUSD
		} else {
			threads.resp.CostUSD = resp.CostUSD
		}
		if err != nil {
			m.Log.Warn("checking facts ahead failed; checked after reading", "percept", p.ID.String(), "err", err)
			return
		}
		answers := func(prefix string) map[string]decision.Answer {
			out := map[string]decision.Answer{}
			for name, a := range resp.Answers {
				if id, ok := strings.CutPrefix(name, prefix); ok {
					out[id] = a
				}
			}
			return out
		}
		facts.resp.Provider, facts.resp.Answers = resp.Provider, answers("relevant:")
		threads.resp.Provider, threads.resp.Answers = resp.Provider, answers("open:")
	}()
	return func() (checked, checked) {
		<-done
		return facts, threads
	}
}

func recallSubjects(p perception.Percept) []string {
	if p.Speaker == "" {
		return []string{memory.SubjectSelf}
	}
	return []string{memory.SubjectSelf, p.Speaker}
}

// recallFacts picks the facts that would help reply to p, by how the message
// was filed by life area (labels; nil if it was not), using what ahead already
// checked:
//   - not about the agent's or the speaker's life ("capital of France?"): memory is not searched;
//   - about one of them in general ("Tell me about yourself"): every candidate goes through;
//   - otherwise System 1 checks each candidate, the message's life areas first.
//
// The returned decision is nil when there was nothing to do; then root pays
// for what was checked ahead.
func (m *Mind) recallFacts(ctx context.Context, p perception.Percept, root *decision.Decision,
	labels *lifeLabels, ahead checked) ([]memory.Fact, *decision.Decision) {
	unused := func() ([]memory.Fact, *decision.Decision) {
		root.CostUSD += ahead.resp.CostUSD
		return nil, nil
	}
	if m.FactRelevance.ID == "" || m.RecallCandidates <= 0 {
		return unused()
	}
	if labels != nil && !labels.Personal && !labels.Broad {
		return unused()
	}
	var areas []string
	if labels != nil {
		areas = labels.Areas
	}
	candidates, err := m.Store.RecallCandidates(ctx, recallSubjects(p), areas, m.RecallCandidates)
	if err != nil {
		m.Log.Warn("loading facts failed; replying without them", "percept", p.ID.String(), "err", err)
		return unused()
	}
	if len(candidates) == 0 {
		return unused()
	}

	start := time.Now()
	d := newDecision(p, &root.ID, start)
	if labels != nil && labels.Broad {
		d.Tier, d.SetID, d.Action = decision.TierReflex, "reflex.recall_all@1", "recall_all"
		d.CostUSD = ahead.resp.CostUSD
		slices.Reverse(candidates) // oldest first, the order they were told
		d.Outcome = &decision.Outcome{Status: decision.OutcomeOK,
			Detail: fmt.Sprintf("broad question: passed all %d facts", len(candidates)), At: time.Now().UTC()}
		d.Latency = time.Since(start)
		return candidates, d
	}
	return m.checkFacts(ctx, p, d, m.FactRelevance, "recall", candidates, true, ahead), d
}

// openThreadWindow is how far back "what's going on with you" reaches: a
// friend asks about last week's interview, not about a cold from June.
const openThreadWindow = 30 * 24 * time.Hour

// recallOpenThreads is recall for small talk: what's been going on with the
// speaker lately, as checkAhead found it.
func (m *Mind) recallOpenThreads(ctx context.Context, p perception.Percept, root *decision.Decision, ahead checked) ([]memory.Fact, *decision.Decision) {
	if len(ahead.candidates) == 0 {
		return nil, nil
	}
	d := newDecision(p, &root.ID, time.Now())
	return m.checkFacts(ctx, p, d, m.OpenThreads, "recall_open", ahead.candidates, false, ahead), d
}

// checkFacts returns the candidates System 1 passes, oldest first, asking only
// about those ahead hasn't answered. The step is recorded on d, with what was
// checked ahead.
func (m *Mind) checkFacts(ctx context.Context, p perception.Percept, d *decision.Decision, set questions.Set,
	action string, candidates []memory.Fact, withMessage bool, ahead checked) []memory.Fact {
	start := time.Now()
	d.Tier, d.SetID, d.Action = decision.TierSystem1, set.Ref(), action
	d.Provider, d.CostUSD = ahead.resp.Provider, ahead.resp.CostUSD

	answers := map[string]decision.Answer{}
	var pending []memory.Fact
	for _, f := range candidates {
		if a, ok := ahead.resp.Answers[f.ID.String()]; ok {
			answers[f.ID.String()] = a
		} else {
			pending = append(pending, f)
		}
	}
	if len(pending) > 0 {
		resp, err := m.askFacts(ctx, p, set, pending, withMessage)
		if err != nil {
			m.Log.Warn("fact check failed; replying without facts", "percept", p.ID.String(), "set", set.Ref(), "err", err)
			d.Action = "error"
			d.Outcome = &decision.Outcome{Status: decision.OutcomeFailed, Detail: err.Error(), At: time.Now().UTC()}
			d.Latency = time.Since(start)
			return nil
		}
		d.Provider, d.CostUSD = resp.Provider, d.CostUSD+resp.CostUSD
		maps.Copy(answers, resp.Answers)
	}
	d.Answers = answers

	var kept []memory.Fact
	for _, f := range candidates {
		if a := answers[f.ID.String()]; !a.Refused && a.Probability >= set.Thresholds.Act {
			kept = append(kept, f)
		}
	}
	slices.SortStableFunc(kept, func(a, b memory.Fact) int { return a.At.Compare(b.At) })
	d.Outcome = &decision.Outcome{Status: decision.OutcomeOK,
		Detail: fmt.Sprintf("kept %d of %d facts", len(kept), len(candidates)), At: time.Now().UTC()}
	d.Latency = time.Since(start)
	return kept
}

// askFacts asks set's one question of each fact, in one call.
func (m *Mind) askFacts(ctx context.Context, p perception.Percept, set questions.Set, facts []memory.Fact,
	withMessage bool) (system1.Response, error) {
	n := m.namesFor(p)
	state := map[string]any{"facts": factTexts(facts, n)}
	if withMessage {
		state["message"] = n.message(p.Text)
	}
	return m.System1.Decide(ctx, system1.Request{State: state, Questions: factQuestions(set, facts, facts, "", n)})
}

func factTexts(facts []memory.Fact, n names) []string {
	texts := make([]string, len(facts))
	for i, f := range facts {
		texts[i] = n.fact(f)
	}
	return texts
}

// factQuestions is set's one question of each of facts, named prefix and the
// fact's id, pointing at its place in all, the facts System 1 is given.
func factQuestions(set questions.Set, facts, all []memory.Fact, prefix string, n names) []system1.Question {
	if len(facts) == 0 {
		return nil
	}
	qs := make([]system1.Question, len(facts))
	for i, f := range facts {
		at := slices.IndexFunc(all, func(x memory.Fact) bool { return x.ID == f.ID })
		qs[i] = set.Questions[0]
		qs[i].Name = prefix + f.ID.String()
		qs[i].Instructions = n.fill(strings.ReplaceAll(qs[i].Instructions, "{fact}", fmt.Sprintf("`facts[%d]`", at)))
	}
	return qs
}

// related is what a reply may draw on that doesn't wait on reading the
// message: what it made that the talk is about, and the skill that fits.
type related struct {
	works    string
	workRefs map[string]memory.Work
	skill    string
	cost     float64
}

// lookUp finds both at once: each waits on an embedding.
func (m *Mind) lookUp(ctx context.Context, p perception.Percept, person memory.Person, recent []memory.Exchange) related {
	var (
		r         related
		skillCost float64
		wg        sync.WaitGroup
	)
	about := talkAbout(recent, p.Text)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer m.survive("finding the skill that fits", nil)
		r.skill, skillCost = m.skillNote(ctx, about)
	}()
	if canPromise(p, person) {
		r.works, r.workRefs, r.cost = m.worksNote(ctx, p.Speaker, about)
	}
	wg.Wait()
	r.cost += skillCost
	return r
}

// reason writes the reply. What it learned (facts, reminders, corrections, ...)
// is kept by learn, which the caller runs once the reply is sent, so the reply
// never waits on it; learn returns the decisions it made, and is nil if
// System 2 failed.
func (m *Mind) reason(ctx context.Context, writer system2.Reasoner, p perception.Percept, person memory.Person, parent, d *decision.Decision,
	recent []memory.Exchange, facts []memory.Fact, why string, rel related, about topical) (reply string, said Said, file *render.File, learn func() []*decision.Decision) {
	d.Tier, d.SetID, d.Action = decision.TierSystem2, m.ReplyPrompt.Ref, "reply"
	n := m.namesFor(p)
	note, refs := m.system2Note(parent, why, facts, n)
	promises, promised := m.promisesNote(ctx, p, person, n)

	req := system2.Request{
		Instructions: m.instructions(ctx, person, n),
		History:      turns(recent),
		Note: m.now(p.At) + m.peopleNote(ctx) + m.skillsFor(ctx, person) + "\n\n" + note + m.topicNote(p, about, n) + rel.works +
			rel.skill + promises + m.permissionsNote(p) + nameNote(recent, n.speaker),
		Message:      p.Text,
		CollectFacts: true,
	}
	if m.Drafter != nil {
		var shown time.Time
		req.Draft = func(text string) {
			if time.Since(shown) < draftEvery {
				return // what's left shows with the reply
			}
			shown = time.Now()
			if err := m.Drafter.Draft(ctx, p, text); err != nil {
				m.Log.Debug("draft not shown", "percept", p.ID.String(), "err", err)
			}
		}
	}
	resp, err := writer.Reason(ctx, req)
	spent, looked := rel.cost, false
	for round := 1; err == nil && len(resp.Looks) > 0 && round <= lookRounds && len(m.lookable(person)) > 0; round++ {
		more, cost, read := m.look(ctx, p, person, resp.Looks, round == lookRounds)
		spent += resp.CostUSD + cost
		said.Read = append(said.Read, read...)
		req.Note += more
		looked = true
		resp, err = writer.Reason(ctx, req)
	}
	d.Provider, d.CostUSD = resp.Provider, resp.CostUSD+spent // a failed call is paid for too
	if err == nil && resp.Text == "" {
		err = errors.New("it asked to look again after its last look")
	}
	if err != nil {
		m.Log.Error("system 2 failed", "percept", p.ID.String(), "err", err)
		d.Action = "error"
		return "Sorry, I couldn't think that through just now.", Said{}, nil, nil
	}
	if m.RichChat {
		said.Answers, said.Many = resp.Answers, resp.Many
	} else {
		said = Said{} // the chat shows only words
	}
	m.Log.Debug("system 2 usage", "input_tokens", resp.Usage.InputTokens, "cached_tokens", resp.Usage.CachedTokens,
		"output_tokens", resp.Usage.OutputTokens, "reasoning_tokens", resp.Usage.ReasoningTokens)

	return resp.Text, said, m.sendWith(p, resp.Send, rel.workRefs), func() []*decision.Decision {
		var fs []memory.Fact
		for _, mem := range resp.Remember {
			subject := memory.SubjectSelf
			if mem.About == system2.AboutSpeaker {
				if p.Speaker == "" {
					continue
				}
				subject = p.Speaker
			}
			fs = append(fs, memory.Fact{Subject: subject, Text: mem.Text, Areas: m.filedAreas(mem.Areas)})
		}
		if about.unnamed && resp.Topic != "" {
			if err := m.Store.NameTopic(ctx, about.topic.ID, cutRunes(resp.Topic, 60)); err != nil {
				m.Log.Error("naming a topic failed", "percept", p.ID.String(), "err", err)
			}
		}
		_, cost := m.saveFacts(ctx, p, fs, n)
		d.CostUSD += cost + m.learnRelation(ctx, p, person, resp.WhoTheyAre)
		m.saveReminders(ctx, p, resp.Reminders)
		m.savePromises(ctx, p, person, resp.Promises, rel.workRefs)
		m.movePromises(ctx, p, resp.Moves, promised)
		if p.Speaker != "" && p.Speaker == m.policy().Owner { // only its owner can have it act, for now
			m.propose(ctx, p, resp.Acts, p.At, !looked) // what it read can't make it act unasked
		}
		var more []*decision.Decision
		more = append(more, m.correctFacts(ctx, p, d, refs, resp.Corrections, n)...)
		more = append(more, m.changePermissions(ctx, p, d, resp.Permissions)...)
		more = append(more, m.labelMisread(ctx, p, d, resp.Misread)...)
		more = append(more, m.takeFeedback(ctx, p, d, resp.Feedback)...)
		more = append(more, m.takeLessons(ctx, p, d, resp.Lessons)...)
		more = append(more, m.takeTastes(ctx, p, d, resp.Tastes)...)
		return more
	}
}

// canPromise is false for strangers, whose requests could be anything.
func canPromise(p perception.Percept, person memory.Person) bool {
	return p.Speaker != "" && person.Circle != memory.CircleStranger
}

// promisesNote also returns the open promises it lists, by their ref ("p1").
func (m *Mind) promisesNote(ctx context.Context, p perception.Percept, person memory.Person, n names) (string, map[string]memory.Intention) {
	if !canPromise(p, person) {
		return fmt.Sprintf("\n\nYou don't know %s well yet: don't promise to make them anything; leave promises empty.", n.speaker), nil
	}
	var gate string
	switch m.policy().Check(action.Commit, p.Speaker) {
	case action.Forbid:
		return fmt.Sprintf("\n\nYou can't promise to make %s anything; leave promises empty.", n.speaker), nil
	case action.Ask:
		owner := orDefault(m.person(ctx, m.policy().Owner).Name, "someone")
		gate = fmt.Sprintf("\n\nBefore you promise to make anything for %s, you check with %s. If %s asks and you'd like to, "+
			"don't say yes yet: say you'll check and get back to them, and still put it in promises.", n.speaker, owner, n.speaker)
	}
	open, err := m.Store.OpenIntentions(ctx, p.Speaker, memory.IntentionTask)
	if err != nil || len(open) == 0 {
		return gate, nil
	}
	refs := map[string]memory.Intention{}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nWhat you've promised %s and not sent yet, each with a ref (at most %d at a time). If they add to one, just "+
		"say you'll work it in; don't make it now:", n.speaker, maxOpenPromises)
	for k, i := range open {
		ref := fmt.Sprintf("p%d", k+1)
		refs[ref] = i
		as := "a message"
		if f := i.Args["as"]; f != "" {
			as = "a " + f + " file"
		}
		fmt.Fprintf(&b, "\n- [%s] by %s, as %s: %s", ref, i.DueAt.In(m.Zone).Format("Mon 2 Jan 15:04"), as, i.Text)
	}
	return gate + b.String(), refs
}

// movePromises sets the new time of promises it already made, by their ref
// in the note: "now", or a local time (one already past is now); and what
// one goes as, if that changed and it can send that. The next tick makes and
// sends what is due.
func (m *Mind) movePromises(ctx context.Context, p perception.Percept, moves []system2.Move, promised map[string]memory.Intention) {
	for _, mv := range moves {
		log := m.Log.With("percept", p.ID.String(), "promise", mv.Promise, "by", mv.By)
		i, ok := promised[mv.Promise]
		if !ok {
			log.Warn("asked to move a promise it hasn't made; left alone")
			continue
		}
		due := p.At
		if mv.By != "now" {
			t, err := time.ParseInLocation("2006-01-02T15:04", mv.By, m.Zone)
			if err != nil {
				log.Warn("a promise's new time can't be read; left alone", "err", err)
				continue
			}
			if t.After(p.At) {
				due = t
			}
		}
		if err := m.Store.PostponeIntention(ctx, i.ID, due.UTC()); err != nil {
			log.Error("moving a promise failed", "err", err)
			continue
		}
		log.Info("moved a promise", "what", i.Text, "from", i.DueAt.In(m.Zone).Format(time.RFC3339), "to", due.In(m.Zone).Format(time.RFC3339))
		if mv.As == "" || mv.As == i.Args["as"] {
			continue
		}
		if !m.canSendAs(mv.As) {
			log.Warn("asked to send a promise as a file it can't; it goes as it was", "as", mv.As)
			continue
		}
		if err := m.Store.SetIntentionArg(ctx, i.ID, "as", mv.As); err != nil {
			log.Error("changing what a promise goes as failed", "err", err)
			continue
		}
		log.Info("a promise goes as another file now", "what", i.Text, "as", mv.As)
	}
}

// savePromises drops a promise in the past, with a bad time, past
// maxOpenPromises or to a stranger; an ability it doesn't have is left out.
// One to send a work it already made (by its ref in works) keeps the work's id.
// When the gate says ask, the task waits for the owner's approval.
func (m *Mind) savePromises(ctx context.Context, p perception.Percept, person memory.Person, ps []system2.Promise,
	works map[string]memory.Work) {
	if len(ps) == 0 {
		return
	}
	log := m.Log.With("percept", p.ID.String(), "person", p.Speaker)
	if !canPromise(p, person) {
		log.Warn("dropped promises to a stranger", "promises", len(ps))
		return
	}
	verdict := m.policy().Check(action.Commit, p.Speaker)
	if verdict == action.Forbid {
		log.Warn("dropped promises: not allowed", "promises", len(ps))
		return
	}
	open, err := m.Store.OpenIntentions(ctx, p.Speaker, memory.IntentionTask)
	if err != nil {
		log.Error("loading open promises failed; dropped", "err", err)
		return
	}
	count := len(open)
	for _, pr := range ps {
		due, err := time.ParseInLocation("2006-01-02T15:04", pr.By, m.Zone)
		if err != nil || !due.After(p.At) || count >= maxOpenPromises {
			log.Warn("dropped a promise", "by", pr.By, "what", pr.What, "open", count, "err", err)
			continue
		}
		i := memory.Intention{ID: ulid.Make(), Subject: p.Speaker, Kind: memory.IntentionTask, PerceptID: p.ID, Text: pr.What,
			DueAt: due.UTC(), Status: memory.IntentionPending, CreatedAt: time.Now().UTC()}
		if ab, ok := m.Abilities.Get(pr.Ability); ok && ab.Describe() != "" && m.allowed(ab) {
			i.Ability = ab.Name()
			if pr.Query != "" {
				i.Args = map[string]string{"query": pr.Query}
			}
		}
		setArg := func(k, v string) {
			if i.Args == nil {
				i.Args = map[string]string{}
			}
			i.Args[k] = v
		}
		if m.canSendAs(pr.As) {
			setArg("as", pr.As)
		}
		if w, ok := works[pr.Work]; ok { // sent as it is, not made again
			setArg("work", w.ID.String())
		}
		if verdict == action.Ask {
			i.Status = memory.IntentionAwaiting
		}
		if err := m.Store.SaveIntention(ctx, i); err != nil {
			log.Error("save promise", "err", err)
			continue
		}
		count++
		if m.Making != nil && verdict != action.Ask && p.Speaker == m.policy().Owner && due.Sub(p.At) <= time.Hour {
			// Soon: their owner sees it's in hand from now, not only once it's being made.
			m.Making(fmt.Sprintf("%s, by %s", cutRunes(pr.What, 90), due.Format("15:04")))
		}
		if verdict == action.Ask {
			what := fmt.Sprintf("%s, by %s", pr.What, due.Format("Mon 2 Jan 15:04"))
			if err := m.ask(ctx, memory.Approval{Class: string(action.Commit), Person: p.Speaker, What: what, Ref: i.ID, Topic: p.Topic,
				Why: orDefault(p.SpeakerName, fallbackName) + " asked for it."}); err != nil {
				log.Error("asking the owner failed", "err", err)
			}
			continue
		}
		log.Info("promised", "due", due.Format(time.RFC3339), "what", pr.What, "ability", i.Ability, "as", i.Args["as"])
	}
}

func (m *Mind) saveReminders(ctx context.Context, p perception.Percept, rs []system2.Reminder) {
	for _, r := range rs {
		due, err := time.ParseInLocation("2006-01-02T15:04", r.When, m.Zone)
		if err != nil || !due.After(p.At) || p.Speaker == "" {
			m.Log.Warn("dropped a reminder", "percept", p.ID.String(), "when", r.When, "what", r.What, "err", err)
			continue
		}
		err = m.Store.SaveIntention(ctx, memory.Intention{ID: ulid.Make(), Subject: p.Speaker, Kind: memory.IntentionRemind,
			PerceptID: p.ID, Text: r.What, DueAt: due.UTC(), Status: memory.IntentionPending, CreatedAt: time.Now().UTC()})
		if err != nil {
			m.Log.Error("save reminder", "percept", p.ID.String(), "err", err)
			continue
		}
		m.Log.Info("reminder set", "person", p.Speaker, "due", due.Format(time.RFC3339), "what", r.What)
	}
}

// formatting is how replies are laid out, by what the chat shows.
func (m *Mind) formatting() string {
	if !m.RichChat {
		return "Plain text: no Markdown, because your chat shows it as raw symbols."
	}
	return "Usually plain text, like a message. Only when it helps something be read or used, format it in Markdown: a " +
		"numbered list for steps or options, **bold** for the one thing that matters, a link as [words](url), and code in " +
		"a fenced block with its language (```python). Never headings or tables: a table belongs in a sheet you make."
}

func (m *Mind) answersHelp(n names) string {
	if !m.RichChat {
		return "- Leave `answers` empty and `many` false."
	}
	return fmt.Sprintf("- When you ask %s something with a few likely answers (a choice between options, yes or no, a time or a "+
		"day), put the likeliest 2 to 4 in `answers`, each a few words as %s would say it (\"Simplified\", \"9am\", \"Yes, go ahead\"), "+
		"so they can tap one instead of typing; set `many` if several can be picked. Leave `answers` empty when you ask nothing, or "+
		"ask what only they can say (a name, a description).", n.speaker, n.speaker)
}

func (m *Mind) now(t time.Time) string {
	return fmt.Sprintf("It is now %s (%s time).", t.In(m.Zone).Format("Monday 2 January 2006, 15:04"), m.Zone)
}

func (m *Mind) instructions(ctx context.Context, person memory.Person, n names) string {
	story := "- " + strings.Join(m.Life.Story, "\n- ")
	personality := "- Be yourself."
	if len(m.Life.Personality) > 0 {
		personality = "- " + strings.Join(m.Life.Personality, "\n- ")
	}
	return strings.NewReplacer("{name}", n.agent, "{speaker}", n.speaker, "{owner}", m.ownerName(ctx), "{background}", story,
		"{personality}", personality, "{who_they_are}", m.whoTheyAre(person, n),
		"{makes}", m.makesList(), "{abilities}", m.abilitiesList(), "{files}", m.filesHelp(n), "{actions}", m.actionsFor(person, n), "{looks}", m.looksHelp(person, n), "{intents}", strings.Join(m.intents(), ", "),
		"{correct_self}", m.correctSelf(person), "{permissions_help}", m.permissionsHelp(person, n),
		"{fact_areas}", m.factAreasHelp(), "{formatting}", m.formatting(), "{answers_help}", m.answersHelp(n),
		"{feedback_help}", m.feedbackHelp(ctx, person)).Replace(m.ReplyPrompt.Text)
}

// talkAbout is what a reply to message is about: it and the exchanges just
// before, so "go ahead" carries what it agrees to.
func talkAbout(recent []memory.Exchange, message string) string {
	var parts []string
	for _, e := range recent[max(0, len(recent)-talkTurns):] {
		parts = append(parts, e.Message, e.Reply)
	}
	return strings.TrimSpace(strings.Join(append(parts, message), "\n"))
}

func turns(recent []memory.Exchange) []system2.Turn {
	var ts []system2.Turn
	for _, e := range recent {
		if e.Message != "" { // "" when the agent spoke first
			ts = append(ts, system2.Turn{Role: system2.RolePerson, Text: e.Message})
		}
		if e.Reply != "" {
			ts = append(ts, system2.Turn{Role: system2.RoleAgent, Text: e.Reply})
		}
	}
	return ts
}

func (m *Mind) system2Note(s1 *decision.Decision, why string, facts []memory.Fact, n names) (string, map[string]memory.Fact) {
	refs := map[string]memory.Fact{}
	var b strings.Builder
	fmt.Fprintf(&b, "You are answering %s's latest message because %s.", n.speaker, why)
	if a, ok := s1.Answers[m.Incoming.Primary]; ok && !a.Refused {
		read := fmt.Sprintf("%q", a.Choice)
		if phrase, ok := intentPhrases[a.Choice]; ok {
			read = n.speaker + " " + phrase
		}
		fmt.Fprintf(&b, " A quick first read, without the conversation, took it as %s (%.0f%% sure).", read, a.Confidence*100)
	}
	var mine, theirs []memory.Fact
	for _, f := range facts {
		if f.Subject == memory.SubjectSelf {
			mine = append(mine, f)
		} else {
			theirs = append(theirs, f)
		}
	}
	if len(mine) == 0 && len(theirs) == 0 {
		fmt.Fprintf(&b, "\n\nBeyond your life story, nothing you remember about yourself or %s is relevant here.", n.speaker)
		return b.String(), refs
	}
	b.WriteString("\n\nWhat you remember that may help, with when you learned it (and a ref, for corrections):")
	writeFacts := func(title string, fs []memory.Fact) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:", title)
		for _, f := range fs {
			ref := fmt.Sprintf("f%d", len(refs)+1)
			refs[ref] = f
			fmt.Fprintf(&b, "\n- [%s] %s: %s", ref, f.At.Format("2 Jan 2006"), n.fact(f))
		}
	}
	writeFacts("About yourself", mine)
	writeFacts("About "+n.speaker, theirs)
	if len(theirs) == 0 {
		fmt.Fprintf(&b, "\nNothing you remember about %s is relevant here.", n.speaker)
	}
	return b.String(), refs
}

// intentPhrases puts the intent read in the agent's own terms for System 2:
// the question set speaks of "writer" and "reader", and the agent is the reader.
var intentPhrases = map[string]string{
	"share":                "sharing something about their own life",
	"ask_about_reader":     "asking about you",
	"ask_reader_abilities": "asking what you can do or plan to do",
	"ask_about_writer":     "asking what you know about them",
	"ask_for_help":         "asking for help, advice or information",
	"remind":               "asking to be reminded of something",
	"small_talk":           "making small talk",
}

const namedLately = 4

func nameNote(recent []memory.Exchange, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == fallbackName {
		return ""
	}
	n := 0
	for i := len(recent) - 1; i >= 0 && n < namedLately; i-- {
		if recent[i].Reply == "" {
			continue
		}
		n++
		if strings.Contains(strings.ToLower(recent[i].Reply), strings.ToLower(name)) {
			return fmt.Sprintf("\n\nYou used %s's name in one of your last messages: don't use it in this one.", name)
		}
	}
	return ""
}
