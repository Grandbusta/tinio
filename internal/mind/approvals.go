package mind

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/render"
)

// Approvals: what the autonomy gate makes the agent ask its owner first. An
// approval unanswered is denied when it expires.

type Asker interface {
	// Ask returns where it sent the request, for Settle.
	Ask(ctx context.Context, p perception.Percept, r memory.Request, id string) (json.RawMessage, error)
	Settle(ctx context.Context, where json.RawMessage, text string) error
}

const approvalsPerTick = 10

func (m *Mind) ask(ctx context.Context, a memory.Approval) error {
	a.ID, a.Status, a.AskedAt = ulid.Make(), memory.ApprovalPending, time.Now().UTC()
	if err := m.Store.SaveApproval(ctx, a); err != nil {
		return err
	}
	m.Log.Info("will ask the owner", "approval", a.ID.String(), "class", a.Class, "person", a.Person)
	m.sendApprovals(ctx)
	return nil
}

func (m *Mind) sendApprovals(ctx context.Context) {
	if m.Asker == nil || m.policy().Owner == "" {
		return
	}
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	as, err := m.Store.UnsentApprovals(ctx, approvalsPerTick)
	if err != nil || len(as) == 0 {
		if err != nil {
			m.Log.Warn("loading approvals to send failed", "err", err)
		}
		return
	}
	conv, ok := m.conversationWith(ctx, m.policy().Owner)
	if !ok {
		m.Log.Warn("can't ask the owner yet: they haven't messaged the agent", "owner", m.policy().Owner, "waiting", len(as))
		return
	}
	owner := m.person(ctx, m.policy().Owner)
	p := outgoing(conv, owner, perception.KindApproval, time.Now()) // only says where; not kept
	for _, a := range as {
		p.Topic = a.Topic
		where, err := m.Asker.Ask(ctx, p, m.request(ctx, a), a.ID.String())
		if err != nil {
			m.Log.Warn("asking the owner failed; trying again next tick", "approval", a.ID.String(), "err", err)
			return
		}
		if err := m.Store.MarkApprovalSent(ctx, a.ID, where, time.Now().UTC()); err != nil {
			m.Log.Error("mark approval sent", "err", err)
		}
	}
}

// request is what the owner is asked: in words, and part by part for a card.
func (m *Mind) request(ctx context.Context, a memory.Approval) memory.Request {
	who := orDefault(m.person(ctx, a.Person).Name, fallbackName)
	r := memory.Request{Why: a.Why, When: "As soon as you say yes"}
	var ask string
	switch {
	case a.Kind == memory.KindAction:
		tool, _, _ := strings.Cut(a.What, ": ")
		r.Title, r.Quote, r.Where, r.When = "Use "+tool+"?", a.Text, tool, "Within a minute of your yes"
		ask = fmt.Sprintf("Can I use %s?\n\n%s\n\nWhy: %s", tool, a.Text, orDefault(a.Why, "you asked"))
	case a.Class == string(action.ReachOut):
		r.Title, r.Quote, r.Where = "Message "+who+"?", a.Text, who
		r.Undo, r.Final, r.Editable = "Can't be undone once it's sent", true, true
		ask = fmt.Sprintf("Can I message %s? It'd be this:\n\n%s", who, a.Text)
	case a.Class == string(action.Commit):
		r.Title, r.Quote, r.Where, r.Undo = "Say yes to "+who+"?", a.What, who, "You can tell them later it's off"
		ask = fmt.Sprintf("%s asked me to make something: %s. Can I say yes?", who, a.What)
	case a.Class == string(action.Publish):
		r.Title, r.Quote, r.Where, r.Undo = "Put this on my public page?", a.What, "My public page", "You can take it down"
		ask = "Can I put this on my public page?\n\n" + a.What
	case a.Class == string(action.Dream):
		r.Title = orDefault(map[string]string{"add": "Add a dream?", "reword": "Change a dream?", "drop": "Let go of a dream?"}[a.Kind], "Change my dreams?")
		r.Quote, r.Where, r.Undo, r.Editable = a.Text, "My dreams", "You can change it back", a.Kind != "drop"
		ask = "Can I " + a.What + "?"
	default:
		r.Title = "Can I " + a.What + "?"
		ask = r.Title
	}
	if exp := m.policy().AskExpires; exp > 0 {
		r.Expires = a.AskedAt.Add(exp)
		ask += fmt.Sprintf("\n\n(If I don't hear back by %s, I won't.)", r.Expires.In(m.Zone).Format("Mon 2 Jan 15:04"))
	}
	r.Text = ask
	return r
}

// answer counts only the owner's tap, and only the first: an approval is
// decided once.
func (m *Mind) answer(ctx context.Context, p perception.Percept) {
	log := m.Log.With("percept", p.ID.String())
	if p.Speaker != m.policy().Owner {
		log.Warn("an approval answered by someone who isn't the owner; ignored", "speaker", p.Speaker)
		return
	}
	var ans perception.Answer
	if err := json.Unmarshal(p.Payload, &ans); err != nil {
		log.Warn("bad approval answer", "err", err)
		return
	}
	id, err := ulid.Parse(ans.Approval)
	if err != nil {
		log.Warn("bad approval id", "approval", ans.Approval)
		return
	}
	a, ok, err := m.Store.Approval(ctx, id)
	if err != nil || !ok {
		log.Warn("no such approval", "approval", ans.Approval, "err", err)
		return
	}
	status := memory.ApprovalDenied
	if ans.Approve {
		status = memory.ApprovalApproved
	}
	var changed string
	if text := strings.TrimSpace(ans.Text); ans.Approve && text != "" {
		if m.request(ctx, a).Editable {
			changed, a.Text = text, text
		} else {
			log.Warn("a changed version of a request that can't be changed; the agent's stands", "approval", ans.Approval)
		}
	}
	m.decide(ctx, a, status, changed, p.ID)
}

func (m *Mind) expireApprovals(ctx context.Context, now time.Time) {
	if m.policy().AskExpires <= 0 {
		return
	}
	stale, err := m.Store.StaleApprovals(ctx, now.Add(-m.policy().AskExpires), approvalsPerTick)
	if err != nil {
		m.Log.Warn("loading expired approvals failed", "err", err)
		return
	}
	for _, a := range stale {
		m.decide(ctx, a, memory.ApprovalExpired, "", ulid.ULID{})
	}
}

// decide settles a once: changed, if not "", is the owner's version of its
// text, already in a.Text.
func (m *Mind) decide(ctx context.Context, a memory.Approval, status, changed string, percept ulid.ULID) {
	now := time.Now()
	ok, err := m.Store.DecideApproval(ctx, a.ID, status, changed, now.UTC())
	if err != nil || !ok {
		if err != nil {
			m.Log.Error("decide approval", "approval", a.ID.String(), "err", err)
		}
		return // already decided: a second tap, or it expired first
	}
	a.Status = status
	detail := m.carryOut(ctx, a, now)
	m.approvalCall(ctx, a, now)

	if m.Asker != nil && len(a.Message) > 0 {
		ending := map[string]string{memory.ApprovalApproved: "✅ You said yes.", memory.ApprovalDenied: "❌ You said no.",
			memory.ApprovalExpired: "⌛ No answer, so I didn't."}[status]
		if changed != "" {
			ending = "✅ You said yes, with your changes."
		}
		if err := m.Asker.Settle(ctx, a.Message, m.request(ctx, a).Text+"\n\n"+ending); err != nil {
			m.Log.Warn("updating the request failed", "approval", a.ID.String(), "err", err)
		}
	}
	gate := decision.GateAsked
	if status != memory.ApprovalApproved {
		gate = decision.GateDenied
	}
	d := decision.Decision{ID: ulid.Make(), At: now.UTC(), PerceptID: percept, Tier: decision.TierReflex,
		SetID: "gate:" + a.Class, Action: status, Gate: gate, Latency: time.Since(now),
		Outcome: &decision.Outcome{Status: decision.OutcomeOK, Detail: detail, At: time.Now().UTC()}}
	if err := m.Store.SaveDecision(ctx, d); err != nil {
		m.Log.Error("save decision", "err", err)
	}
	m.Log.Info("approval decided", "approval", a.ID.String(), "class", a.Class, "status", status, "detail", detail)
}

func (m *Mind) carryOut(ctx context.Context, a memory.Approval, now time.Time) string {
	if a.Kind == memory.KindAction {
		return m.approveAction(ctx, a, now)
	}
	yes := a.Status == memory.ApprovalApproved
	why := map[string]string{memory.ApprovalDenied: "the owner said no", memory.ApprovalExpired: "no answer from the owner"}[a.Status]
	switch action.Class(a.Class) {
	case action.Dream:
		if !yes {
			return "kept its dreams as they are"
		}
		return m.changeDream(ctx, a, now)

	case action.Publish:
		public, by := yes, memory.PublicByOwner
		if a.Status == memory.ApprovalExpired {
			by = memory.PublicByGate
		}
		if _, err := m.Store.SetWorkPublic(ctx, a.Ref, public, by); err != nil {
			return "publishing failed: " + err.Error()
		}
		return map[bool]string{true: "published the work", false: "kept the work off the page"}[yes]

	case action.ReachOut:
		if !yes {
			m.finishRef(ctx, a.Ref, memory.IntentionMissed, why, now)
			return "didn't send the message"
		}
		if err := m.sendApproved(ctx, a, now); err != nil {
			m.finishRef(ctx, a.Ref, memory.IntentionMissed, "sending failed", now)
			return "sending failed: " + err.Error()
		}
		m.finishRef(ctx, a.Ref, memory.IntentionDone, "", now)
		return "sent the message"

	case action.Commit:
		person := m.person(ctx, a.Person)
		conv, ok := m.conversationWith(ctx, a.Person)
		if !yes {
			m.finishRef(ctx, a.Ref, memory.IntentionCancelled, why, now)
			if ok {
				p := outgoing(conv, person, perception.KindReminder, now)
				m.speakFirst(ctx, p, person, nil, speech{action: "follow_up", approved: true, note: fmt.Sprintf(
					"You told %s you'd check whether you could make this for them: %s. You can't this time. Tell them, "+
						"kindly and briefly, like a friend would; don't make excuses or blame anyone.", m.namesFor(p).speaker, a.What)})
			}
			return "dropped the promise"
		}
		if err := m.Store.ResumeIntention(ctx, a.Ref, now.Add(time.Hour).UTC()); err != nil {
			return "keeping the promise failed: " + err.Error()
		}
		if ok {
			due := a.What
			if is, err := m.Store.OpenIntentions(ctx, a.Person, memory.IntentionTask); err == nil {
				for _, i := range is {
					if i.ID == a.Ref {
						due = fmt.Sprintf("%s, by %s", i.Text, i.DueAt.In(m.Zone).Format("Monday 15:04"))
					}
				}
			}
			p := outgoing(conv, person, perception.KindReminder, now)
			m.speakFirst(ctx, p, person, nil, speech{action: "follow_up", approved: true, note: fmt.Sprintf(
				"You told %s you'd check whether you could make this for them. You can: %s. Tell them it's on and when "+
					"you'll send it, briefly, like a friend would.", m.namesFor(p).speaker, due)})
		}
		return "kept the promise"
	}
	return "nothing to do"
}

func (m *Mind) sendApproved(ctx context.Context, a memory.Approval, now time.Time) error {
	conv, ok := m.conversationWith(ctx, a.Person)
	if !ok {
		return fmt.Errorf("no conversation with %s", a.Person)
	}
	p := outgoing(conv, m.person(ctx, a.Person), orDefault(a.Kind, perception.KindReminder), now)
	if _, err := m.Store.SavePercept(ctx, p); err != nil {
		return err
	}
	d := decision.Decision{ID: ulid.Make(), At: now.UTC(), PerceptID: p.ID, Tier: decision.TierReflex,
		SetID: "gate:" + a.Class, Action: "send", Reply: a.Text, Gate: decision.GateAsked}
	f, err := m.approvedFile(ctx, a)
	if err != nil {
		return err
	}
	text := a.Text
	if f != nil {
		text = strings.TrimSuffix(text, attachedLine(f.Name))
	}
	err = m.send(ctx, p, text, f)
	d.Outcome = &decision.Outcome{Status: decision.OutcomeOK, Detail: "approved by the owner", At: time.Now().UTC()}
	if err != nil {
		d.Outcome = &decision.Outcome{Status: decision.OutcomeFailed, Detail: err.Error(), At: time.Now().UTC()}
	}
	d.Latency = time.Since(now)
	if serr := m.Store.SaveDecision(ctx, d); serr != nil {
		m.Log.Error("save decision", "err", serr)
	}
	return err
}

func (m *Mind) approvedFile(ctx context.Context, a memory.Approval) (*render.File, error) {
	if a.Kind != perception.KindDelivery || a.Ref.IsZero() {
		return nil, nil
	}
	i, ok, err := m.Store.Intention(ctx, a.Ref)
	if err != nil || !ok || !m.canSendAs(i.Args["as"]) {
		return nil, err
	}
	w, ok, err := m.Store.IntentionWork(ctx, i.ID)
	if err != nil || !ok {
		return nil, err
	}
	return m.makeFile(i.Args["as"], w)
}

func (m *Mind) finishRef(ctx context.Context, id ulid.ULID, status, result string, now time.Time) {
	if !id.IsZero() {
		m.finish(ctx, memory.Intention{ID: id}, status, result, now)
	}
}
