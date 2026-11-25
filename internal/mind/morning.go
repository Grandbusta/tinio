package mind

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/render"
)

// The owner's morning: before their day starts, the agent reads their
// yesterday for its job and chooses up to three things for their today, each
// from something it knows (a topic, a promise, their calendar), drafts what
// it can for them, and sends one note in the chat at their time.

// Morning is when and how. Prompt zero: no mornings.
type Morning struct {
	Prompt Prompt
	At     time.Duration // from midnight, in its (its owner's) time zone
	Days   [7]bool       // by time.Weekday
	Drafts int           // things it may draft ahead, at most
	// Email sends the owner a copy of the note; nil: none.
	Email func(ctx context.Context, subject, text string) error
}

const (
	// morningLead: it starts this long before the note is due, so the drafts
	// are made by then.
	morningLead = 45 * time.Minute
	// morningWait: a draft still being made holds the note back this long at
	// most; then it goes without it.
	morningWait = 30 * time.Minute
	// morningLate: a morning not sent by this long after its time (after
	// downtime, say) is skipped: it's no longer a morning.
	morningLate   = 4 * time.Hour
	morningItems  = 3
	morningTopics = 10
	morningTalk   = 16 // yesterday's exchanges, at most
	calendarName  = "google.calendar"
	// morningDraft marks a draft for the morning (an intention's Args["for"]).
	morningDraft = "morning"
)

func (m *Mind) morningOn() bool { return m.Morning.Prompt.Text != "" && m.policy().Owner != "" }

// morningTimes is today's morning (its date), when it starts and when it's
// sent: at its time, or when the owner's quiet hours end. false if there's
// none today.
func (m *Mind) morningTimes(now time.Time) (string, time.Time, time.Time, bool) {
	midnight := m.startOfDay(now)
	if !m.Morning.Days[midnight.Weekday()] {
		return "", time.Time{}, time.Time{}, false
	}
	send := midnight.Add(m.Morning.At)
	if m.Quiet.at(send) {
		send = m.Quiet.end(send)
	}
	return midnight.Format(time.DateOnly), send.Add(-morningLead), send, true
}

// morning prepares today's morning once it's time, and sends it at its time.
// drafts is whether it may make anything ahead (its account's week allows it,
// and its owner isn't quiet); day paused: none at all.
func (m *Mind) morning(ctx context.Context, now time.Time, day liveliness, drafts bool) {
	if !m.morningOn() {
		return
	}
	today, start, send, ok := m.morningTimes(now)
	if !ok || now.Before(start) || m.morningDone == today {
		return
	}
	if now.After(send.Add(morningLate)) {
		m.morningDone = today
		return
	}
	mo, had, err := m.Store.Morning(ctx, today)
	if err != nil {
		m.Log.Warn("loading this morning failed", "err", err)
		return
	}
	if !had {
		if day == pausedDay {
			m.morningDone = today // not today: their owner hasn't written for a week
			return
		}
		if m.morningTries.waiting(now) {
			return
		}
		if mo, ok = m.prepareMorning(ctx, now, today, drafts && day == livelyDay); !ok {
			m.morningTries.fail(now)
			return
		}
		m.morningTries.succeeded()
	}
	if !mo.SentAt.IsZero() {
		m.morningDone = today
		return
	}
	if now.Before(send) {
		return
	}
	if m.draftsPending(ctx, &mo) && now.Before(send.Add(morningWait)) {
		return
	}
	if m.sendMorning(ctx, now, mo) {
		m.morningDone = today
	}
}

var morningFormat = &system2.Format{Name: "morning", Schema: map[string]any{
	"type": "object", "additionalProperties": false, "required": []string{"opening", "items", "question"},
	"properties": map[string]any{
		"opening":  map[string]any{"type": "string"},
		"question": map[string]any{"type": "string", "description": "empty if nothing is missing"},
		"items": map[string]any{"type": "array", "maxItems": morningItems, "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"what", "how", "source", "draft", "brief"},
			"properties": map[string]any{
				"what":   map[string]any{"type": "string"},
				"how":    map[string]any{"type": "string"},
				"source": map[string]any{"type": "string", "description": "where it came from, as the owner would say it"},
				"draft":  map[string]any{"type": "boolean"},
				"brief":  map[string]any{"type": "string", "description": "what to make for it; empty if no draft"},
			},
		}},
	},
}}

// prepareMorning works out today's morning and sets out to draft what it
// chose to, at most drafts of Morning.Drafts; false if it failed.
func (m *Mind) prepareMorning(ctx context.Context, now time.Time, today string, drafts bool) (memory.Morning, bool) {
	start := time.Now()
	d := &decision.Decision{ID: ulid.Make(), At: start.UTC(), Tier: decision.TierSystem2, SetID: m.Morning.Prompt.Ref,
		Action: "plan_morning", Gate: decision.GateAuto}
	most := 0
	if drafts {
		most = m.Morning.Drafts
	}
	note, readCost := m.morningNote(ctx, now)
	if most > 0 {
		fmt.Fprintf(note, "\n\nYou may draft up to %d of today's things ahead.", most)
	} else {
		note.WriteString("\n\nYou can't draft anything ahead today: give draft false for every item.")
	}
	resp, err := m.ownDay().Reason(ctx, system2.Request{
		Instructions: m.lifeInstructions(m.Morning.Prompt),
		Note:         note.String(),
		Format:       morningFormat,
	})
	d.Provider, d.CostUSD = resp.Provider, resp.CostUSD+readCost
	if err != nil {
		m.Log.Error("preparing the morning failed", "err", err)
		m.finishLife(ctx, d, start, decision.OutcomeFailed, err.Error())
		return memory.Morning{}, false
	}
	var out struct {
		Opening  string `json:"opening"`
		Question string `json:"question"`
		Items    []struct {
			What   string `json:"what"`
			How    string `json:"how"`
			Source string `json:"source"`
			Draft  bool   `json:"draft"`
			Brief  string `json:"brief"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &out); err != nil {
		m.finishLife(ctx, d, start, decision.OutcomeFailed, "the morning is not the requested JSON: "+err.Error())
		return memory.Morning{}, false
	}
	mo := memory.Morning{Day: today, PreparedAt: now.UTC(), Opening: strings.TrimSpace(out.Opening), Question: strings.TrimSpace(out.Question)}
	owner := m.policy().Owner
	var lines []string
	for _, it := range out.Items {
		what, source := strings.TrimSpace(it.What), strings.TrimSpace(it.Source)
		if what == "" || source == "" || len(mo.Items) == morningItems {
			continue // nothing to do, or nowhere it came from: not sent
		}
		item := memory.MorningItem{ID: fmt.Sprint(len(mo.Items) + 1), What: what, How: strings.TrimSpace(it.How), Source: source}
		if brief := strings.TrimSpace(it.Brief); it.Draft && brief != "" && most > 0 {
			i := memory.Intention{ID: ulid.Make(), Text: brief, Subject: owner, Kind: memory.IntentionActivity, Args: map[string]string{"for": morningDraft},
				Status: memory.IntentionPending, DueAt: now.UTC(), CreatedAt: now.UTC()}
			if err := m.Store.SaveIntention(ctx, i); err != nil {
				m.Log.Error("save intention", "err", err)
			} else {
				item.Intention, most = i.ID.String(), most-1
			}
		}
		mo.Items = append(mo.Items, item)
		lines = append(lines, item.What+" ("+item.Source+")")
	}
	if err := m.Store.SaveMorning(ctx, mo); err != nil {
		m.Log.Error("save morning", "err", err)
		m.finishLife(ctx, d, start, decision.OutcomeFailed, err.Error())
		return memory.Morning{}, false
	}
	d.Reply = strings.Join(append([]string{mo.Opening}, append(lines, mo.Question)...), "\n")
	m.finishLife(ctx, d, start, decision.OutcomeOK, fmt.Sprintf("chose %d things", len(mo.Items)))
	m.Log.Info("prepared the morning", "things", len(mo.Items), "cost_usd", d.CostUSD)
	return mo, true
}

// morningNote is what it knows of its owner's yesterday and today, and what
// reading their calendar cost.
func (m *Mind) morningNote(ctx context.Context, now time.Time) (*strings.Builder, float64) {
	var b strings.Builder
	owner, who := m.policy().Owner, m.ownerName(ctx)
	midnight := m.startOfDay(now)
	yesterday := midnight.AddDate(0, 0, -1)
	b.WriteString(m.now(now))
	b.WriteString(m.ownerNote(ctx))

	if ts, err := m.Store.Topics(ctx, owner, now.AddDate(0, 0, -7), morningTopics); err == nil && len(ts) > 0 {
		fmt.Fprintf(&b, "\n\nWhat you and %s have going, most recent first:", who)
		for _, t := range ts {
			fmt.Fprintf(&b, "\n- %s (last talked %s", orDefault(t.Title, "untitled"), t.LastAt.In(m.Zone).Format("Mon 2 Jan"))
			if t.Quiet {
				b.WriteString(", quiet")
			}
			b.WriteString("): " + orDefault(t.Status, orDefault(t.Summary, "just started")))
			if len(t.Settled) > 0 {
				b.WriteString(" Settled: " + strings.Join(t.Settled, "; ") + ".")
			}
		}
	}
	if conv, ok := m.conversationWith(ctx, owner); ok {
		if xs, err := m.Store.RecentExchanges(ctx, conv.Source, conv.Thread, yesterday, ulid.ULID{}, morningTalk); err == nil && len(xs) > 0 {
			fmt.Fprintf(&b, "\n\nWhat you and %s said since yesterday morning:", who)
			for _, x := range xs {
				fmt.Fprintf(&b, "\n- %s %s: %q", x.At.In(m.Zone).Format("Mon 15:04"), who, cutRunes(x.Message, 300))
				if x.Reply != "" {
					fmt.Fprintf(&b, " You: %q", cutRunes(x.Reply, 300))
				}
			}
		}
	}
	if is, err := m.Store.IntentionsDue(ctx, yesterday, midnight.AddDate(0, 0, 1)); err == nil {
		var due []string
		for _, i := range is {
			if i.Subject == owner && i.Status == memory.IntentionPending && (i.Kind == memory.IntentionRemind || i.Kind == memory.IntentionTask) {
				kind := "a reminder"
				if i.Kind == memory.IntentionTask {
					kind = "something you promised to make"
				}
				due = append(due, fmt.Sprintf("%s, %s: %s", i.DueAt.In(m.Zone).Format("Mon 15:04"), kind, i.Text))
			}
		}
		if len(due) > 0 {
			b.WriteString("\n\nDue today or late:\n- " + strings.Join(due, "\n- "))
		}
	}
	if as, err := m.Store.RecentApprovals(ctx, 20); err == nil {
		var waiting []string
		for _, a := range as {
			if a.Status == memory.ApprovalPending {
				waiting = append(waiting, a.What)
			}
		}
		if len(waiting) > 0 {
			fmt.Fprintf(&b, "\n\nYour requests still waiting for %s's answer:\n- %s", who, strings.Join(waiting, "\n- "))
		}
	}
	if ws, err := m.Store.WorksBetween(ctx, yesterday.AddDate(0, 0, -1), now); err == nil && len(ws) > 0 {
		b.WriteString("\n\nWhat you made lately:")
		for _, w := range ws {
			fmt.Fprintf(&b, "\n- %s, %s: %s", w.CreatedAt.In(m.Zone).Format("Mon 15:04"), strings.ReplaceAll(w.Kind, "_", " "), w.Title)
		}
	}
	if mo, ok, err := m.Store.Morning(ctx, yesterday.Format(time.DateOnly)); err == nil && ok && len(mo.Items) > 0 {
		b.WriteString("\n\nYesterday's morning, and what's left of it:")
		for _, it := range mo.Items {
			state := "not done"
			if it.Done {
				state = "done"
			}
			fmt.Fprintf(&b, "\n- %s (%s)", it.What, state)
		}
	}
	var cost float64
	if cal, ok := m.Abilities.Get(calendarName); ok && m.allowed(cal) {
		day := midnight.Format(time.DateOnly)
		i := memory.Intention{Args: map[string]string{"query": fmt.Sprintf(`{"from": %q, "to": %q}`, day, day)}}
		if mat, err := cal.Prepare(ctx, ability.Act{Intention: i, Now: now, Zone: m.Zone}); err != nil {
			m.Log.Warn("reading their calendar for the morning failed; going on without it", "err", err)
		} else {
			b.WriteString("\n\nToday on " + who + "'s calendar:" + mat.Note)
			cost = mat.CostUSD
		}
	}
	return &b, cost
}

// draftsPending is whether a draft it set out to make is still being made;
// those made are filled in on mo.
func (m *Mind) draftsPending(ctx context.Context, mo *memory.Morning) bool {
	pending := false
	for k, it := range mo.Items {
		if it.Intention == "" || it.Work != "" {
			continue
		}
		id, err := ulid.Parse(it.Intention)
		if err != nil {
			continue
		}
		if w, ok, err := m.Store.IntentionWork(ctx, id); err == nil && ok {
			mo.Items[k].Work = w.ID.String()
			continue
		}
		if i, ok, err := m.Store.Intention(ctx, id); err == nil && ok && i.Status == memory.IntentionPending {
			pending = true
		}
	}
	return pending
}

// sendMorning sends the note, then each draft as a file; false if the note
// couldn't be sent (it's tried again next tick).
func (m *Mind) sendMorning(ctx context.Context, now time.Time, mo memory.Morning) bool {
	owner := m.policy().Owner
	conv, ok := m.conversationWith(ctx, owner)
	if !ok {
		return true // they've never opened the chat: nowhere to send it
	}
	p := outgoing(conv, m.person(ctx, owner), perception.KindMorning, now)
	if _, err := m.Store.SavePercept(ctx, p); err != nil {
		m.Log.Error("save percept", "err", err)
		return false
	}
	text := m.morningText(mo)
	if err := m.send(ctx, p, text, nil, Said{}); err != nil {
		m.Log.Warn("sending the morning failed", "err", err)
		return false
	}
	if err := m.Store.MorningSent(ctx, mo.Day, now.UTC()); err != nil {
		m.Log.Error("morning sent", "err", err)
	}
	if err := m.Store.SaveMorning(ctx, mo); err != nil { // the drafts it made
		m.Log.Error("save morning", "err", err)
	}
	for n, it := range mo.Items {
		id, err := ulid.Parse(it.Work)
		if err != nil {
			continue
		}
		w, ok, err := m.Store.Work(ctx, id)
		if err != nil || !ok {
			continue
		}
		var f *render.File
		if m.Formats != nil {
			if fs := m.Formats.For(w); len(fs) > 0 { // its first, most likely format: a document for words
				f, err = m.makeFile(fs[0].Name(), w)
			}
		}
		if err != nil {
			m.Log.Warn("making the morning's draft a file failed; it goes as a message", "err", err)
		}
		caption := fmt.Sprintf("For %d: %s", n+1, w.Title)
		if f == nil {
			caption += "\n\n" + w.Text
		}
		if err := m.send(ctx, p, caption, f, Said{}); err != nil {
			m.Log.Warn("sending a morning draft failed", "work", it.Work, "err", err)
		}
	}
	if m.Morning.Email != nil {
		subject := fmt.Sprintf("Your %s from %s", now.In(m.Zone).Format("Monday"), m.Life.Name)
		if len(mo.Items) > 0 {
			subject = fmt.Sprintf("Your %s from %s: %s", now.In(m.Zone).Format("Monday"), m.Life.Name, countThings(len(mo.Items)))
		}
		if err := m.Morning.Email(ctx, subject, plainMorning(text)); err != nil {
			m.Log.Warn("emailing the morning failed", "err", err)
		}
	}
	m.Log.Info("sent the morning", "things", len(mo.Items))
	return true
}

// morningText is the note: the opening, the things for today, numbered with
// how, and the question.
func (m *Mind) morningText(mo memory.Morning) string {
	var b strings.Builder
	b.WriteString(mo.Opening)
	if len(mo.Items) > 0 {
		head := "Your " + countWord(len(mo.Items)) + " for today:"
		if len(mo.Items) == 1 {
			head = "One thing for today:"
		}
		if m.RichChat {
			head = "**" + head + "**"
		}
		b.WriteString("\n\n" + head)
		for n, it := range mo.Items {
			what := it.What
			if m.RichChat {
				what = "**" + what + "**"
			}
			fmt.Fprintf(&b, "\n%d. %s", n+1, what)
			if it.How != "" {
				b.WriteString(" " + it.How)
			}
			if it.Work != "" {
				b.WriteString(" (draft below)")
			}
		}
	}
	if mo.Question != "" {
		b.WriteString("\n\n" + mo.Question)
	}
	return strings.TrimSpace(b.String())
}

func countWord(n int) string {
	return [...]string{"none", "one", "two", "three"}[min(n, 3)]
}

func countThings(n int) string {
	if n == 1 {
		return "one thing"
	}
	return countWord(n) + " things"
}

// plainMorning is the note without its Markdown, for email.
func plainMorning(s string) string { return strings.ReplaceAll(s, "**", "") }
