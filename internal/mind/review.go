package mind

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// The weekly look back: what it learned this week about how its owner likes
// their work done and how they decide, from what it made for them and what
// they said and chose. It keeps or retires lessons, and draws rules from their
// calls for them to approve. Nothing is written for itself.

const reviewWindow = 7 * 24 * time.Hour

var reviewFormat = &system2.Format{Name: "weekly_review", Schema: map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"retire_lessons", "new_lessons", "principles"},
	"properties": map[string]any{
		"retire_lessons": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
			"description": "ids of lessons you've absorbed, or that a newer lesson replaces"},
		"new_lessons": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"skill", "lesson"},
			"properties": map[string]any{
				"skill":  map[string]any{"type": "string"},
				"lesson": map[string]any{"type": "string"},
			},
		}},
		"principles": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"text", "types", "calls"},
			"properties": map[string]any{
				"text":  map[string]any{"type": "string", "description": "a rule of your owner's, in a sentence"},
				"types": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": character.DecisionTypes}},
				"calls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of the calls it comes from"},
			},
		}},
	},
}}

// weeklyReview skips a week already looked back on (e.g. before a restart).
func (m *Mind) weeklyReview(ctx context.Context, now time.Time) {
	if m.Day.ReviewPrompt.Text == "" {
		return
	}
	at := m.reviewTime(now)
	if now.Before(at) || m.reviewed.Equal(at) || m.reviewTries.waiting(now) {
		return
	}
	was := m.reviewed
	m.reviewed = at
	if err := m.Review(ctx, now); err != nil {
		m.Log.Error("weekly look back failed", "err", err)
		if m.reviewTries.fail(now) {
			m.reviewed = was // tried again later
		}
		return
	}
	m.reviewTries.succeeded()
}

func (m *Mind) reviewTime(now time.Time) time.Time {
	midnight := m.startOfDay(now)
	back := (int(now.In(m.Zone).Weekday()) - int(m.Day.ReviewDay) + 7) % 7
	return midnight.AddDate(0, 0, -back).Add(m.Day.ReviewAt)
}

// Review looks back at the week, now: it is what the weekly look back does,
// and `tinio review` runs it on demand.
func (m *Mind) Review(ctx context.Context, now time.Time) error {
	m.bornWith(ctx) // so a look back run on demand has its skills even before the agent first started
	start := time.Now()
	d := &decision.Decision{ID: ulid.Make(), At: start.UTC(), Tier: decision.TierSystem2, SetID: m.Day.ReviewPrompt.Ref,
		Action: "weekly_review", Gate: decision.GateAuto}

	var note strings.Builder
	note.WriteString(m.now(now) + " It's time to look back at your week of work for " + m.ownerName(ctx) + ".")
	note.WriteString(m.ownerNote(ctx))
	note.WriteString(m.skillsNote(ctx))
	var made []string
	if works, err := m.Store.WorksBetween(ctx, now.Add(-reviewWindow), now); err == nil && len(works) > 0 {
		note.WriteString("\n\nWhat you made this week:")
		for _, w := range works {
			fmt.Fprintf(&note, "\n- %s, %s: %s", w.CreatedAt.In(m.Zone).Format("Mon 2 Jan"), strings.ReplaceAll(w.Kind, "_", " "), w.Title)
			made = append(made, w.Title)
		}
	} else {
		note.WriteString("\n\nYou made nothing this week.")
	}
	if ls, err := m.Store.Lessons(ctx, "", 100); err == nil && len(ls) > 0 {
		note.WriteString("\n\nWhat you've learned, by skill (your active lessons):")
		for _, l := range ls {
			fmt.Fprintf(&note, "\n- [lesson %s] %s, from %s: %s", l.ID, l.Skill, lessonSource(l.Source), l.Text)
		}
	}
	about := "Looking back at a week of work for your owner."
	if len(made) > 0 {
		about += " What was made: " + strings.Join(made, "; ") + "."
	}
	note.WriteString(m.judgmentNote(ctx, reviewJudgment, about))
	disagreements, callCases := m.disagreementsNote(ctx, now)
	note.WriteString(disagreements)

	resp, err := m.ownDay().Reason(ctx, system2.Request{
		Instructions: m.lifeInstructions(m.Day.ReviewPrompt),
		Note:         note.String(),
		Format:       reviewFormat,
	})
	d.Provider, d.CostUSD = resp.Provider, resp.CostUSD
	if err != nil {
		m.finishLife(ctx, d, start, decision.OutcomeFailed, err.Error())
		return err
	}
	var out struct {
		Principles []struct {
			Text  string   `json:"text"`
			Types []string `json:"types"`
			Calls []string `json:"calls"`
		} `json:"principles"`
		RetireLessons []string `json:"retire_lessons"`
		NewLessons    []struct {
			Skill  string `json:"skill"`
			Lesson string `json:"lesson"`
		} `json:"new_lessons"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &out); err != nil {
		m.finishLife(ctx, d, start, decision.OutcomeFailed, "the look back is not the requested JSON: "+err.Error())
		return err
	}

	var changes []string
	if ls, err := m.Store.Lessons(ctx, "", 100); err == nil {
		active := map[string]bool{}
		for _, l := range ls {
			active[l.ID.String()] = true
		}
		for _, id := range out.RetireLessons {
			if active[strings.TrimSpace(id)] {
				if err := m.Store.RetireLesson(ctx, ulid.MustParse(strings.TrimSpace(id)), now.UTC()); err == nil {
					changes = append(changes, "retired a lesson")
				}
			}
		}
	}
	if skills, err := m.Store.Skills(ctx); err == nil {
		for _, nl := range out.NewLessons {
			for _, sk := range skills {
				if strings.EqualFold(sk.Name, strings.TrimSpace(nl.Skill)) && strings.TrimSpace(nl.Lesson) != "" &&
					m.saveLesson(ctx, memory.Lesson{Skill: sk.Name, Text: strings.TrimSpace(nl.Lesson), Source: memory.LessonFromSelf}) {
					changes = append(changes, "new lesson: "+strings.TrimSpace(nl.Lesson))
				}
			}
		}
	}
	for _, pr := range out.Principles {
		text := strings.TrimSpace(pr.Text)
		var types, keys []string
		for _, t := range pr.Types {
			if slices.Contains(character.DecisionTypes, t) && !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
		for _, id := range pr.Calls {
			if k, ok := callCases[strings.TrimSpace(id)]; ok {
				keys = append(keys, k)
			}
		}
		if text == "" || len(keys) == 0 { // a principle comes from its owner's own choices
			continue
		}
		if err := m.Store.SavePrinciple(ctx, memory.Principle{ID: ulid.Make(), Text: text, Types: types, Cases: keys,
			Status: memory.JudgmentProposed, Source: memory.JudgmentFromReview, CreatedAt: now.UTC()}); err != nil {
			m.Log.Error("save principle", "err", err)
			continue
		}
		changes = append(changes, "proposed a principle: "+text)
	}
	d.Reply = strings.Join(changes, "\n")
	m.finishLife(ctx, d, start, decision.OutcomeOK, fmt.Sprintf("looked back at the week; %d changes", len(changes)))
	m.Log.Info("weekly look back", "changes", len(changes), "cost_usd", d.CostUSD)
	return nil
}
