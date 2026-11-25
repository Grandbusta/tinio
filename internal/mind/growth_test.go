package mind

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

var testSkillLevel = questions.Set{ID: "skill_level", Version: 1,
	Questions: []system1.Question{{Name: "level", Type: decision.QuestionScore, Instructions: "How good is `work` as `skill`?"}}}

func TestWorksAreScoredIntoSkills(t *testing.T) {
	s1 := &fakeSystem1{scores: map[string]float64{"Lagos Rain, verse 2": 4.9}} // index 4.9 → 5.9
	m, s, _, _, _ := activityMind()
	m.System1, m.SkillLevel = s1, testSkillLevel
	s.skills = []memory.Skill{{Name: "lyric writing", Level: 4, ShownBy: []string{"lyrics"}},
		{Name: "music production", Level: 3, ShownBy: []string{"beat_idea"}}}
	s.evidence = []memory.Evidence{{Skill: "lyric writing", Score: 4, Source: memory.EvidenceOwner}} // counts double
	m.tick(context.Background(), at(29, 10, 0))

	if len(s1.scoreCalls) != 1 || s1.scoreCalls[0].State.(map[string]any)["skill"] != "lyric writing" {
		t.Fatalf("scored %d times; want once, for the skill lyrics show", len(s1.scoreCalls))
	}
	if e := s.evidence[1]; e.Score != 5.9 || e.Source != memory.EvidenceSystem1 || e.WorkID.IsZero() {
		t.Errorf("evidence = %+v", e)
	}
	if lvl := s.skills[0].Level; lvl != 4.6 { // (4×2 + 5.9) / 3
		t.Errorf("level = %.2f, want 4.6", lvl)
	}
}

func TestTheOwnersFeedbackCounts(t *testing.T) {
	m, s, _ := gatedMind(&fakeReplier{}, &fakeSystem2{})
	s.skills = []memory.Skill{{Name: "lyric writing", Level: 4}}
	reply := &decision.Decision{ID: ulid.Make()}
	fb := []system2.Feedback{{Skill: "Lyric Writing", Level: 2, About: "the verse"}, {Skill: "dancing", Level: 9}}
	if ds := m.takeFeedback(context.Background(), from(sam), reply, fb); len(ds) != 0 {
		t.Error("someone who isn't the owner moved a skill")
	}
	ds := m.takeFeedback(context.Background(), from(owner), reply, fb)
	if len(ds) != 1 || s.skills[0].Level != 2 || s.evidence[0].Source != memory.EvidenceOwner || s.evidence[0].Note != "the verse" {
		t.Errorf("decisions %d, skills %+v, evidence %+v", len(ds), s.skills, s.evidence)
	}
}

func reviewMind(review string) (*Mind, *fakeStore, *fakeSystem2, *fakeAsker) {
	s2 := &fakeSystem2{formats: map[string]string{"weekly_review": review}}
	m, s, asker := gatedMind(&fakeReplier{}, s2)
	m.Zone = lagos
	s.percepts = append(s.percepts, perception.Percept{Speaker: owner, Kind: perception.KindMessage, At: at(25, 10, 0)}) // they've been writing
	m.Day = Day{ReviewDay: time.Sunday, ReviewAt: 20 * time.Hour, ReviewPrompt: Prompt{Ref: "review@test", Text: "You are {name}, reviewing."}}
	return m, s, s2, asker
}

// The weekly look back keeps what it learned about how its owner likes things:
// a new lesson for a skill it has, none for one it hasn't, and a retired one
// gone; once a week, not before its time.
func TestWeeklyReview(t *testing.T) {
	old := memory.Lesson{ID: ulid.Make(), Skill: "Music Production", Text: "Keep hooks long", Source: memory.LessonFromSelf}
	review := `{"retire_lessons":["` + old.ID.String() + `","nope"],"principles":[],"outcomes":[],
		"new_lessons":[{"skill":"music production","lesson":"Keep hooks to six words"},{"skill":"Knitting","lesson":"x"}]}`
	m, s, s2, _ := reviewMind(review)
	s.skills = []memory.Skill{{Name: "Music Production", Level: 4}}
	s.lessons = []memory.Lesson{old}

	m.tick(context.Background(), at(25, 19, 59)) // Sunday 25 October, before its time
	if len(s2.requests) != 0 {
		t.Fatal("looked back early")
	}
	m.tick(context.Background(), at(25, 20, 0))
	if len(s2.requests) != 1 {
		t.Fatalf("requests = %d", len(s2.requests))
	}
	var active []string
	for _, l := range s.lessons {
		if l.RetiredAt.IsZero() {
			active = append(active, l.Skill+": "+l.Text)
		}
	}
	if len(active) != 1 || active[0] != "Music Production: Keep hooks to six words" {
		t.Errorf("active lessons %q; want only the new one, for the skill it has", active)
	}
	m.tick(context.Background(), at(25, 21, 0))
	if len(s2.requests) != 1 {
		t.Errorf("looked back twice: %d", len(s2.requests))
	}
}
