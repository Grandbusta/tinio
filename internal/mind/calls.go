package mind

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
)

// Calls: learning its owner's judgment on its own decisions. Every answer the
// owner gives becomes one of their decisions, approved, and how often they
// agree, by type, is how it will earn a type of decision.
//
// Every request the owner answers is a call: a yes or no that teaches it how
// they decide. A call asked before (none are made now) waits for the answer;
// unanswered in time, it goes with its pick, and that isn't counted.

type Calls struct {
	On         bool
	AsksPerDay int // after-calls asked on Telegram a day; the rest wait for the console
	// ReplyWindow is how long after a different pick (or "something else")
	// the owner's next message is taken as their reason (or answer).
	ReplyWindow time.Duration
}

type Chooser interface {
	// Choose adds a button for something else, and returns where it was sent.
	Choose(ctx context.Context, p perception.Percept, text, id string, n, pick int) (json.RawMessage, error)
}

const (
	callsPerTick = 10
	maxOptions   = 4
)

func newCall(typ, mode, situation string, options []string, pick int, why string, confidence float64, now time.Time) (memory.Call, bool) {
	var opts []string
	for _, o := range options {
		if o = strings.TrimSpace(o); o != "" && !slices.Contains(opts, o) {
			opts = append(opts, o)
		}
	}
	situation = strings.TrimSpace(situation)
	if situation == "" || len(opts) < 2 || len(opts) > maxOptions || pick < 0 || pick >= len(opts) {
		return memory.Call{}, false
	}
	if math.IsNaN(confidence) {
		confidence = 0
	}
	return memory.Call{ID: ulid.Make(), Type: typ, Mode: mode, Situation: situation, Options: opts, Pick: pick,
		Why: strings.TrimSpace(why), Confidence: math.Max(0, math.Min(1, confidence)), Reversible: true, Status: memory.CallLogged,
		CreatedAt: now.UTC()}, true
}

// sendCalls asks every before-call, and up to AsksPerDay after-calls: most at
// stake and least sure first.
func (m *Mind) sendCalls(ctx context.Context, now time.Time) {
	chooser, ok := m.Asker.(Chooser)
	if !m.Calls.On || !ok || m.policy().Owner == "" {
		return
	}
	logged, err := m.Store.RecentCalls(ctx, memory.CallLogged, 50)
	if err != nil || len(logged) == 0 {
		if err != nil {
			m.Log.Warn("loading calls to ask failed", "err", err)
		}
		return
	}
	midnight := m.startOfDay(now)
	asked, err := m.Store.CallsAskedSince(ctx, memory.CallAfter, midnight)
	if err != nil {
		return
	}
	var ask []memory.Call
	var after []memory.Call
	for _, c := range logged {
		switch {
		case c.Mode == memory.CallBefore:
			ask = append(ask, c)
		case !c.CreatedAt.Before(midnight):
			after = append(after, c)
		}
	}
	worth := func(c memory.Call) float64 { return c.AtStake() * (1 - c.Confidence) }
	slices.SortStableFunc(after, func(a, b memory.Call) int { return cmp.Compare(worth(b), worth(a)) })
	for _, c := range after {
		if asked >= m.Calls.AsksPerDay {
			break
		}
		ask, asked = append(ask, c), asked+1
	}
	if len(ask) == 0 {
		return
	}
	conv, ok := m.conversationWith(ctx, m.policy().Owner)
	if !ok {
		return
	}
	p := outgoing(conv, m.person(ctx, m.policy().Owner), perception.KindCall, now) // only says where; not kept
	for i, c := range ask {
		if i >= callsPerTick {
			break
		}
		where, err := chooser.Choose(ctx, p, m.callText(c, now), c.ID.String(), len(c.Options), c.Pick)
		if err != nil {
			m.Log.Warn("asking the owner's call failed; trying again next tick", "call", c.ID.String(), "err", err)
			return
		}
		if err := m.Store.MarkCallAsked(ctx, c.ID, where, now.UTC()); err != nil {
			m.Log.Error("mark call asked", "err", err)
		}
		m.Log.Info("asked the owner's call", "call", c.ID.String(), "type", c.Type, "confidence", c.Confidence)
	}
}

func (m *Mind) callText(c memory.Call, now time.Time) string {
	var b strings.Builder
	b.WriteString(strings.TrimSuffix(c.Situation, ".") + ".\n")
	for i, o := range c.Options {
		fmt.Fprintf(&b, "\n%d. %s", i+1, o)
		if i == c.Pick {
			b.WriteString(" ★")
		}
	}
	verb := "I went with"
	if c.Mode == memory.CallBefore {
		verb = "I'd go with"
	}
	fmt.Fprintf(&b, "\n\n%s %d", verb, c.Pick+1)
	if c.Why != "" {
		fmt.Fprintf(&b, " because %s", lowerFirst(strings.TrimSuffix(c.Why, ".")))
	}
	if c.Confidence > 0 {
		fmt.Fprintf(&b, " (%d%% sure you'd pick it too)", int(math.Round(c.Confidence*100)))
	}
	b.WriteString(".")
	if c.Stakes != "" {
		at := map[string]string{memory.StakesLow: "Not much rides on it", memory.StakesMedium: "A fair bit rides on it",
			memory.StakesHigh: "A lot rides on it"}[c.Stakes]
		if c.Reversible {
			at += ", and I can undo it."
		} else {
			at += ", and I can't undo it."
		}
		b.WriteString(" " + at)
	}
	b.WriteString(" What would you do?")
	if c.Mode == memory.CallBefore && m.policy().AskExpires > 0 {
		fmt.Fprintf(&b, "\n\n(If I don't hear back by %s, I'll go with mine.)", now.Add(m.policy().AskExpires).In(m.Zone).Format("Mon 2 Jan 15:04"))
	}
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" || strings.HasPrefix(s, "I ") || strings.HasPrefix(s, "I'") {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func (m *Mind) pickAnswered(ctx context.Context, p perception.Percept) {
	log := m.Log.With("percept", p.ID.String())
	if p.Speaker == "" || p.Speaker != m.policy().Owner {
		log.Warn("a call answered by someone who isn't the owner; ignored", "speaker", p.Speaker)
		return
	}
	var pick perception.Pick
	if err := json.Unmarshal(p.Payload, &pick); err != nil {
		log.Warn("bad call answer", "err", err)
		return
	}
	id, err := ulid.Parse(pick.Call)
	if err != nil {
		return
	}
	c, ok, err := m.Store.Call(ctx, id)
	if err != nil || !ok || c.Status != memory.CallAsked || pick.Option >= len(c.Options) {
		log.Info("a call that can't be answered now", "call", pick.Call, "err", err)
		return
	}
	now := time.Now().UTC()
	if pick.Option < 0 { // something else: their next message says what
		if err := m.Store.WaitForOwner(ctx, c.ID, memory.CallWaitingAnswer, now); err != nil {
			m.Log.Error("wait for owner", "err", err)
			return
		}
		m.settleCall(ctx, c, "Something else. Tell me what you'd do, and why.")
		return
	}
	c.OwnerPick, c.AnsweredBy, c.AnsweredAt = pick.Option, memory.AnsweredOnTelegram, now
	ending := fmt.Sprintf("✅ You'd pick %d too.", pick.Option+1)
	if pick.Option != c.Pick {
		c.Waiting = memory.CallWaitingReason
		ending = fmt.Sprintf("You'd pick %d. What tipped it?", pick.Option+1)
	}
	if ok, err := m.Store.AnswerCall(ctx, c, true); err != nil || !ok {
		if err != nil {
			m.Log.Error("answer call", "err", err)
		}
		return
	}
	m.settleCall(ctx, c, ending)
	m.callDecided(ctx, c, p.ID, now)
}

// ownerReplied reports whether p was what a call was waiting for; if so it
// has already thanked them.
func (m *Mind) ownerReplied(ctx context.Context, p perception.Percept) bool {
	if !m.Calls.On || p.Speaker == "" || p.Speaker != m.policy().Owner || strings.TrimSpace(p.Text) == "" || p.Kind != perception.KindMessage {
		return false
	}
	c, ok, err := m.Store.WaitingCall(ctx, time.Now().Add(-m.Calls.ReplyWindow).UTC())
	if err != nil || !ok {
		return false
	}
	now, text := time.Now().UTC(), strings.TrimSpace(p.Text)
	reply := "Thanks, that helps. I'll remember it."
	switch c.Waiting {
	case memory.CallWaitingReason:
		if err := m.Store.GiveCallReason(ctx, c.ID, text, now); err != nil {
			m.Log.Error("call reason", "err", err)
			return false
		}
	case memory.CallWaitingAnswer:
		c.OwnerPick, c.OwnerChoice, c.OwnerReason, c.AnsweredBy, c.AnsweredAt, c.Waiting = -1, text, text, memory.AnsweredOnTelegram, now, ""
		if ok, err := m.Store.AnswerCall(ctx, c, true); err != nil || !ok {
			return false
		}
		reply = "Got it, thanks. I'll keep that in mind."
		m.callDecided(ctx, c, p.ID, now)
	default:
		return false
	}
	d := decision.Decision{ID: ulid.Make(), At: now, PerceptID: p.ID, Tier: decision.TierReflex, SetID: "judgment:" + c.Type,
		Action: "take_" + c.Waiting, Reply: reply, Gate: decision.GateAuto}
	err = m.Replier.Reply(ctx, p, reply)
	d.Outcome = &decision.Outcome{Status: decision.OutcomeOK, Detail: "the owner's " + c.Waiting + " for a call", At: time.Now().UTC()}
	if err != nil {
		d.Outcome.Status, d.Outcome.Detail = decision.OutcomeFailed, err.Error()
	}
	d.Latency = time.Since(now)
	if err := m.Store.SaveDecision(ctx, d); err != nil {
		m.Log.Error("save decision", "err", err)
	}
	m.Log.Info("took the owner's "+c.Waiting+" for a call", "call", c.ID.String(), "type", c.Type)
	return true
}

// expireCalls closes calls the owner didn't answer in time: a before-call
// goes with its pick (not counted), an after-call waits for the console.
func (m *Mind) expireCalls(ctx context.Context, now time.Time) {
	if !m.Calls.On || m.policy().AskExpires <= 0 {
		return
	}
	stale, err := m.Store.StaleCalls(ctx, now.Add(-m.policy().AskExpires).UTC(), callsPerTick)
	if err != nil {
		m.Log.Warn("loading expired calls failed", "err", err)
		return
	}
	for _, c := range stale {
		if ok, err := m.Store.ExpireCall(ctx, c.ID); err != nil || !ok {
			continue
		}
		ending := "⌛ No answer."
		if c.Mode == memory.CallBefore {
			ending = "⌛ No answer, so I went with mine."
			m.Log.Info("a call went unanswered; went with its pick", "call", c.ID.String(), "type", c.Type)
		}
		m.settleCall(ctx, c, ending)
	}
}

func (m *Mind) callDecided(ctx context.Context, c memory.Call, percept ulid.ULID, now time.Time) {
	detail := "graded"
	action := "disagreed"
	if c.OwnerPick == c.Pick {
		action = "agreed"
	}
	d := decision.Decision{ID: ulid.Make(), At: now.UTC(), PerceptID: percept, Tier: decision.TierReflex, SetID: "judgment:" + c.Type,
		Action: action, Gate: decision.GateAuto, Outcome: &decision.Outcome{Status: decision.OutcomeOK, Detail: detail, At: time.Now().UTC()}}
	if err := m.Store.SaveDecision(ctx, d); err != nil {
		m.Log.Error("save decision", "err", err)
	}
	m.Log.Info("the owner answered a call", "call", c.ID.String(), "type", c.Type, "agreed", c.OwnerPick == c.Pick, "detail", detail)
}

func (m *Mind) settleCall(ctx context.Context, c memory.Call, ending string) {
	if m.Asker == nil || len(c.Message) == 0 {
		return
	}
	if err := m.Asker.Settle(ctx, c.Message, m.callText(c, c.AskedAt)+"\n\n"+ending); err != nil {
		m.Log.Warn("updating the call failed", "call", c.ID.String(), "err", err)
	}
}

func (m *Mind) approvalCall(ctx context.Context, a memory.Approval, now time.Time) {
	if !m.Calls.On || (a.Status != memory.ApprovalApproved && a.Status != memory.ApprovalDenied) {
		return
	}
	situation, _, _ := strings.Cut(m.request(ctx, a).Text, "\n\n(If I don't")
	c := memory.Call{ID: ulid.Make(), Type: a.Class, Mode: memory.CallBefore, Situation: situation, Options: []string{"yes", "no"},
		Ref: a.ID, Status: memory.CallLogged, CreatedAt: a.AskedAt}
	c.Stakes, c.Reversible = approvalStakes(action.Class(a.Class))
	if err := m.Store.SaveCall(ctx, c); err != nil {
		m.Log.Error("save call", "err", err)
		return
	}
	m.changed.Store(true)
	c.OwnerPick, c.AnsweredBy, c.AnsweredAt = 1, memory.AnsweredByApproval, now.UTC()
	if a.Status == memory.ApprovalApproved {
		c.OwnerPick = 0
	}
	if _, err := m.Store.AnswerCall(ctx, c, false); err != nil {
		m.Log.Error("answer call", "err", err)
	}
}

func (m *Mind) disagreementsNote(ctx context.Context, now time.Time) (string, map[string]string) {
	calls, err := m.Store.RecentCalls(ctx, memory.CallAnswered, 100)
	if err != nil {
		return "", nil
	}
	owner := m.ownerName(ctx)
	cases := map[string]string{} // call id: its case's key
	var b strings.Builder
	for _, c := range calls {
		if c.Agreed == nil || *c.Agreed || c.AnsweredAt.Before(now.Add(-reviewWindow)) || c.AnsweredBy == memory.AnsweredByApproval {
			continue
		}
		theirs := c.OwnerChoice
		if c.OwnerPick >= 0 && c.OwnerPick < len(c.Options) {
			theirs = c.Options[c.OwnerPick]
		}
		fmt.Fprintf(&b, "\n- [call %s] %s You picked %q; %s picked %q.", c.ID, strings.TrimSuffix(c.Situation, ".")+".", c.Options[c.Pick], owner, theirs)
		if c.OwnerReason != "" && c.OwnerReason != theirs {
			fmt.Fprintf(&b, " Why: %s", c.OwnerReason)
		}
		if !c.CaseID.IsZero() {
			cases[c.ID.String()] = c.CaseID.String()
		}
	}
	if b.Len() == 0 {
		return "", nil
	}
	return fmt.Sprintf("\n\nWhere %s chose differently from you this week (by call id):%s", owner, b.String()), cases
}

func (m *Mind) ownerName(ctx context.Context) string {
	if m.policy().Owner != "" {
		if n := m.person(ctx, m.policy().Owner).Name; n != "" {
			return n
		}
	}
	return "your owner"
}

func approvalStakes(class action.Class) (string, bool) {
	switch class {
	case action.ReachOut:
		return memory.StakesMedium, false
	case action.Commit:
		return memory.StakesMedium, false
	case action.Publish:
		return memory.StakesMedium, true
	case action.Spend, action.ExternalMessage:
		return memory.StakesHigh, false
	}
	return memory.StakesLow, true
}
