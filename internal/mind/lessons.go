package mind

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// Lessons: specific advice about doing a skill well ("check the error after
// every file read"). Active ones go into every practice note until its weekly
// review retires them.

const (
	lessonsPerSkill = 3
	lessonsPerWork  = 2
	askReviewEvery  = 24 * time.Hour
	// judgedWithin: an owner's feedback is tied to the newest work made this
	// recently, what they're likely judging.
	judgedWithin = 48 * time.Hour
)

func (m *Mind) lessonsNote(ctx context.Context) string {
	skills, err := m.Store.Skills(ctx)
	if err != nil || len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	for _, sk := range skills {
		ls, err := m.Store.Lessons(ctx, sk.Name, lessonsPerSkill)
		if err != nil || len(ls) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s:", sk.Name)
		for _, l := range ls {
			fmt.Fprintf(&b, "\n- %s", l.Text)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\nThings you've learned the hard way (use them, when they apply):" + b.String()
}

// learnFromWork files lessons under skills; with none, under each skill the
// work's kind shows.
func (m *Mind) learnFromWork(ctx context.Context, w memory.Work, lessons []string, source string, skills []string) {
	if len(skills) == 0 {
		all, err := m.Store.Skills(ctx)
		if err != nil {
			return
		}
		for _, sk := range all {
			if slices.Contains(sk.ShownBy, w.Kind) {
				skills = append(skills, sk.Name)
			}
		}
	}
	n := 0
	for _, text := range lessons {
		if text = strings.TrimSpace(text); text == "" || n >= lessonsPerWork {
			continue
		}
		n++
		for _, sk := range skills {
			m.saveLesson(ctx, memory.Lesson{Skill: sk, Text: text, Source: source, WorkID: w.ID})
		}
	}
}

// judged is the newest work it made within judgedWithin of at: what its
// owner's feedback is likely about. Zero if none.
func (m *Mind) judged(ctx context.Context, at time.Time) memory.Work {
	ws, err := m.Store.WorksBetween(ctx, at.Add(-judgedWithin), at.Add(time.Second))
	if err != nil || len(ws) == 0 {
		return memory.Work{}
	}
	return ws[len(ws)-1]
}

func (m *Mind) saveLesson(ctx context.Context, l memory.Lesson) bool {
	l.ID, l.At = ulid.Make(), time.Now().UTC()
	if err := m.Store.SaveLesson(ctx, l); err != nil {
		m.Log.Error("save lesson", "err", err)
		return false
	}
	m.Log.Info("learned a lesson", "skill", l.Skill, "source", l.Source, "lesson", l.Text)
	return true
}

// takeLessons ties advice to the work it last asked the owner to review, if
// any.
func (m *Mind) takeLessons(ctx context.Context, p perception.Percept, reply *decision.Decision, ls []system2.LessonLearned) []*decision.Decision {
	if len(ls) == 0 || p.Speaker == "" || p.Speaker != m.policy().Owner {
		return nil
	}
	skills, err := m.Store.Skills(ctx)
	if err != nil {
		return nil
	}
	asked := m.judged(ctx, p.At)
	var out []*decision.Decision
	for _, l := range ls {
		name := ""
		for _, sk := range skills {
			if strings.EqualFold(sk.Name, l.Skill) {
				name = sk.Name
			}
		}
		if name == "" || !m.saveLesson(ctx, memory.Lesson{Skill: name, Text: l.Text, Source: memory.LessonFromOwner, WorkID: asked.ID}) {
			continue
		}
		d := m.child(p, reply, "lesson")
		d.Outcome.Detail = fmt.Sprintf("%s: %s", name, l.Text)
		out = append(out, d)
	}
	return out
}

func lessonSource(s string) string {
	switch s {
	case memory.LessonFromOwner:
		return "your owner's correction"
	case memory.LessonFromOthers:
		return "studying others"
	}
	return "your own mistakes"
}

const tastesInNote = 12

// takeTastes: only the owner's taste counts; a kind it doesn't make counts
// for any.
func (m *Mind) takeTastes(ctx context.Context, p perception.Percept, reply *decision.Decision, ts []system2.Taste) []*decision.Decision {
	if len(ts) == 0 || p.Speaker == "" || p.Speaker != m.policy().Owner {
		return nil
	}
	asked := m.judged(ctx, p.At)
	var out []*decision.Decision
	for _, t := range ts {
		kind := t.Kind
		if !slices.Contains(m.craftKinds(), kind) {
			kind = ""
		}
		taste := memory.Taste{ID: ulid.Make(), Kind: kind, Likes: t.Likes, Text: t.Text, WorkID: asked.ID, Source: memory.TasteFromTelegram,
			At: time.Now().UTC()}
		if err := m.Store.SaveTaste(ctx, taste); err != nil {
			m.Log.Error("save taste", "err", err)
			continue
		}
		d := m.child(p, reply, "taste")
		d.Outcome.Detail = tasteLine(taste)
		out = append(out, d)
		m.Log.Info("learned its owner's taste", "kind", kind, "likes", t.Likes, "taste", t.Text)
	}
	return out
}

func (m *Mind) tasteNote(ctx context.Context) string {
	ts, err := m.Store.Tastes(ctx, tastesInNote)
	if err != nil || len(ts) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nWhat %s likes and doesn't in your work (their taste: weigh it when it applies, beside what makes it good):", m.ownerName(ctx))
	for _, t := range ts {
		fmt.Fprintf(&b, "\n- %s", tasteLine(t))
	}
	return b.String()
}

func tasteLine(t memory.Taste) string {
	line := "likes"
	if !t.Likes {
		line = "doesn't like"
	}
	if t.Kind != "" {
		line = strings.ReplaceAll(t.Kind, "_", " ") + ", " + line
	}
	return line + ": " + t.Text
}
