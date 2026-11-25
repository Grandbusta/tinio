package mind

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// Growth: a skill's level comes from evidence, never from what the agent
// claims: System 1 scores each work that shows the skill, and its owner's
// feedback counts double.

const (
	evidenceWindow = 10
	ownerWeight    = 2
	scoreChars     = 4000
)

// bornWith gives it each skill its character starts with that it doesn't
// have yet; after that, memory wins. False if something couldn't be saved.
func (m *Mind) bornWith(ctx context.Context) bool {
	ok := true
	now := time.Now().UTC()
	for _, sk := range m.Life.Skills {
		if err := m.Store.AddSkill(ctx, memory.Skill{Name: sk.Name, Level: float64(sk.Level), ShownBy: sk.ShownBy,
			UpdatedAt: now}); err != nil {
			m.Log.Warn("storing a skill failed", "skill", sk.Name, "err", err)
			ok = false
			continue
		}
		ok = m.startingScore(ctx, sk.Name, float64(sk.Level)) && ok
	}
	return ok
}

// startingScore counts the life story's level as a first score, once, so
// the first real score moves its level instead of replacing it. False if it
// couldn't be saved.
func (m *Mind) startingScore(ctx context.Context, skill string, level float64) bool {
	ev, err := m.Store.RecentEvidence(ctx, skill, 1000)
	if err != nil {
		return false
	}
	at := time.Now().UTC()
	for _, e := range ev {
		if e.Source == memory.EvidenceLife {
			return true
		}
		if e.At.Before(at) {
			at = e.At.Add(-time.Second)
		}
	}
	if err := m.Store.SaveEvidence(ctx, memory.Evidence{ID: ulid.Make(), Skill: skill, Score: level, Source: memory.EvidenceLife,
		Note: "where its life story starts it", At: at}); err != nil {
		m.Log.Warn("storing a starting score failed", "skill", skill, "err", err)
		return false
	}
	if len(ev) > 0 {
		if lvl, err := m.recomputeLevel(ctx, skill, time.Now().UTC()); err == nil {
			m.Log.Info("counted a skill's starting level", "skill", skill, "level", lvl)
		}
	}
	return true
}

func workState(w memory.Work) map[string]string {
	return map[string]string{"title": w.Title, "kind": w.Kind, "text": cutRunes(w.Text, scoreChars)}
}

func (m *Mind) scoreWork(ctx context.Context, w memory.Work, percept ulid.ULID, parent *ulid.ULID) {
	if m.SkillLevel.ID == "" || len(m.SkillLevel.Questions) == 0 {
		return
	}
	skills, err := m.Store.Skills(ctx)
	if err != nil {
		m.Log.Warn("loading skills failed; the work isn't scored", "err", err)
		return
	}
	for _, sk := range skills {
		if !slices.Contains(sk.ShownBy, w.Kind) {
			continue
		}
		start := time.Now()
		d := &decision.Decision{ID: ulid.Make(), ParentID: parent, At: start.UTC(), PerceptID: percept, Tier: decision.TierSystem1,
			SetID: m.SkillLevel.Ref(), Action: "score_skill", Gate: decision.GateAuto}
		resp, err := m.System1.Decide(ctx, system1.Request{
			State:     map[string]any{"skill": sk.Name, "work": workState(w)},
			Questions: m.SkillLevel.Questions,
		})
		if err != nil {
			m.Log.Warn("scoring a work failed", "skill", sk.Name, "err", err)
			continue
		}
		d.Provider, d.Answers, d.CostUSD = resp.Provider, resp.Answers, resp.CostUSD
		a, ok := resp.Answers[m.SkillLevel.Questions[0].Name]
		if !ok || a.Refused {
			continue
		}
		score := math.Round((a.Score+1)*10) / 10 // level index 0-9 → 1-10
		level, err := m.addEvidence(ctx, memory.Evidence{Skill: sk.Name, WorkID: w.ID, Score: score, Source: memory.EvidenceSystem1,
			Note: w.Title})
		if err != nil {
			m.Log.Error("save evidence", "err", err)
			continue
		}
		d.Latency = time.Since(start)
		d.Outcome = &decision.Outcome{Status: decision.OutcomeOK, At: time.Now().UTC(),
			Detail: fmt.Sprintf("%s: %q scored %.1f; level %.1f → %.1f", sk.Name, w.Title, score, sk.Level, level)}
		if err := m.Store.SaveDecision(ctx, *d); err != nil {
			m.Log.Error("save decision", "err", err)
		}
		m.Log.Info("scored a work", "skill", sk.Name, "work", w.Title, "score", score, "level", level)
	}
}

func (m *Mind) addEvidence(ctx context.Context, e memory.Evidence) (float64, error) {
	e.ID, e.At = ulid.Make(), time.Now().UTC()
	if err := m.Store.SaveEvidence(ctx, e); err != nil {
		return 0, err
	}
	return m.recomputeLevel(ctx, e.Skill, e.At)
}

func (m *Mind) recomputeLevel(ctx context.Context, skill string, at time.Time) (float64, error) {
	recent, err := m.Store.RecentEvidence(ctx, skill, evidenceWindow)
	if err != nil {
		return 0, err
	}
	sum, weight := 0.0, 0.0
	for _, r := range recent {
		w := 1.0
		if r.Source == memory.EvidenceOwner {
			w = ownerWeight
		}
		sum, weight = sum+r.Score*w, weight+w
	}
	if weight == 0 {
		return 0, nil
	}
	level := math.Round(sum/weight*10) / 10
	return level, m.Store.SetSkillLevel(ctx, skill, level, at)
}

// A thumb on a work counts as the owner's feedback at these levels (1-10).
const (
	thumbUp   = 7
	thumbDown = 4
)

// reacted takes the owner's thumb on a work as their feedback on each skill
// that kind of work shows, with what was off, if they said. Only the owner's
// count. Nothing is said back.
func (m *Mind) reacted(ctx context.Context, p perception.Percept) {
	log := m.Log.With("percept", p.ID.String())
	if p.Speaker == "" || p.Speaker != m.policy().Owner {
		log.Warn("a thumb from someone who isn't the owner; ignored", "speaker", p.Speaker)
		return
	}
	var r perception.Reaction
	if err := json.Unmarshal(p.Payload, &r); err != nil {
		log.Warn("a thumb that can't be read; ignored", "err", err)
		return
	}
	id, err := ulid.Parse(r.Work)
	if err != nil {
		log.Warn("a thumb on no work; ignored", "work", r.Work)
		return
	}
	w, ok, err := m.Store.Work(ctx, id)
	if err != nil || !ok {
		log.Warn("a thumb on a work that isn't there; ignored", "work", r.Work, "err", err)
		return
	}
	skills, err := m.Store.Skills(ctx)
	if err != nil {
		log.Warn("loading skills failed; the thumb isn't counted", "err", err)
		return
	}
	score, said := float64(thumbUp), "liked it"
	if !r.Good {
		score, said = thumbDown, "said it wasn't quite right"
	}
	note := w.Title
	if r.Note != "" {
		note += ": " + cutRunes(r.Note, 500)
	}
	d := newDecision(p, nil, time.Now())
	d.Tier, d.SetID, d.Action = decision.TierReflex, "reflex.reaction@1", "skill_feedback"
	var moved []string
	for _, sk := range skills {
		if !slices.Contains(sk.ShownBy, w.Kind) {
			continue
		}
		level, err := m.addEvidence(ctx, memory.Evidence{Skill: sk.Name, WorkID: w.ID, Score: score, Source: memory.EvidenceOwner, Note: note})
		if err != nil {
			log.Error("save feedback", "err", err)
			continue
		}
		moved = append(moved, fmt.Sprintf("%s now %.1f", sk.Name, level))
	}
	d.Outcome = &decision.Outcome{Status: decision.OutcomeOK, At: time.Now().UTC(),
		Detail: fmt.Sprintf("the owner %s (%q); %s", said, w.Title, orDefault(strings.Join(moved, ", "), "no skill shows it"))}
	d.Latency = time.Since(d.At)
	if err := m.Store.SaveDecision(ctx, *d); err != nil {
		log.Error("save decision", "err", err)
	}
}

// takeFeedback counts only the owner's feedback, and only for a skill it has.
func (m *Mind) takeFeedback(ctx context.Context, p perception.Percept, reply *decision.Decision, fs []system2.Feedback) []*decision.Decision {
	if len(fs) == 0 || p.Speaker == "" || p.Speaker != m.policy().Owner {
		return nil
	}
	skills, err := m.Store.Skills(ctx)
	if err != nil {
		return nil
	}
	asked := m.judged(ctx, p.At)
	var out []*decision.Decision
	for _, f := range fs {
		name := ""
		for _, sk := range skills {
			if strings.EqualFold(sk.Name, f.Skill) {
				name = sk.Name
			}
		}
		if name == "" {
			m.Log.Warn("feedback on a skill it doesn't have", "skill", f.Skill)
			continue
		}
		level, err := m.addEvidence(ctx, memory.Evidence{Skill: name, WorkID: asked.ID, Score: float64(f.Level), Source: memory.EvidenceOwner,
			Note: f.About})
		if err != nil {
			m.Log.Error("save feedback", "err", err)
			continue
		}
		d := m.child(p, reply, "skill_feedback")
		d.Outcome.Detail = fmt.Sprintf("%s: the owner said %d (%s); level now %.1f", name, f.Level, f.About, level)
		out = append(out, d)
	}
	return out
}

func (m *Mind) feedbackHelp(ctx context.Context, person memory.Person) string {
	if person.Key == "" || person.Key != m.policy().Owner {
		return ""
	}
	skills, err := m.Store.Skills(ctx)
	if err != nil || len(skills) == 0 {
		return ""
	}
	names := make([]string, len(skills))
	for i, sk := range skills {
		names[i] = sk.Name
	}
	return "\nWhat they think of your work:\n- If they judge something you made (\"that verse was weak\", \"this beat idea is fire\"), put it in `feedback`: " +
		"the skill it shows (one of: " + strings.Join(names, ", ") + "), the level 1-10 their words mean (weak 2-3, okay 4-5, good 6, " +
		"great 7-8, amazing 9), and what they judged. Any comment on your work counts, even one that's only advice: \"too wordy, cut it\" " +
		"says it was about a 3. Only when they're judging your work. Take it like a person would.\n" +
		"- If they give you specific advice about doing it better (\"cut the hook to six words\", \"handle the error\"), also put each piece in " +
		"`lessons`: the skill, and the lesson as a short rule for next time that will still help on your next piece (\"keep hooks to about " +
		"six punchy words\", not \"cut this one\"), in your words. Thank them like a learner would.\n" +
		"- If they say what they like or don't in work like yours, beyond how good it is (\"I love when a hook repeats one word\", " +
		"\"I'm not into long intros\"), put each in `tastes`: the kind of work it's about (one of: " + strings.Join(m.craftKinds(), ", ") +
		"; empty for any), likes true or false, and the taste in general terms, so it still applies to your next piece (\"likes hooks " +
		"built on one repeated word\"). Their taste is theirs: keep it apart from how good the work was.\n"
}

func (m *Mind) craftKinds() []string {
	var kinds []string
	for _, c := range m.Life.Crafts() {
		kinds = append(kinds, c.Kind)
	}
	return kinds
}

// skillsNote is its skills and their latest scores, for its weekly look back.
func (m *Mind) skillsNote(ctx context.Context) string {
	var b strings.Builder
	if skills, err := m.Store.Skills(ctx); err == nil && len(skills) > 0 {
		b.WriteString("\n\nYour skills (1-10, from what you've made and feedback):")
		for _, sk := range skills {
			fmt.Fprintf(&b, "\n- %s: %.1f", sk.Name, sk.Level)
			if ev, err := m.Store.RecentEvidence(ctx, sk.Name, 3); err == nil && len(ev) > 0 {
				var last []string
				for _, e := range ev {
					last = append(last, fmt.Sprintf("%.1f", e.Score))
				}
				fmt.Fprintf(&b, " (latest scores, newest first: %s)", strings.Join(last, ", "))
			}
		}
	}
	return b.String()
}

// skillsFor is its skills, for a reply to its owner. In the note, not the
// instructions: skill levels move with every piece of feedback, and the
// instructions stay the same so the prompt cache holds.
func (m *Mind) skillsFor(ctx context.Context, person memory.Person) string {
	if person.Key == "" || person.Key != m.policy().Owner {
		return ""
	}
	skills, err := m.Store.Skills(ctx)
	if err != nil || len(skills) == 0 {
		return ""
	}
	names := make([]string, len(skills))
	for i, sk := range skills {
		names[i] = fmt.Sprintf("%s (%.1f)", sk.Name, sk.Level)
	}
	return fmt.Sprintf("\n\nYour skills: %s.", strings.Join(names, ", "))
}
