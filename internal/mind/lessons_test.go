package mind

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

const learningJSON = `{"title":"Lagos Rain, verse 2","kind":"lyrics","text":"Rain on the danfo roof...",
	"project_notes":"","project_done":false,"remember":[],
	"lessons":["Start the hook on the downbeat"," ","Two","Three"]}`

func learningMind() (*Mind, *fakeStore, *fakeSystem2, *fakeReplier, memory.Intention) {
	m, s, s2, _, i := activityMind()
	r := &fakeReplier{}
	m.Replier = r
	s2.formats["activity"] = learningJSON
	m.Autonomy.Owner = owner
	s.people[owner] = memory.Person{Key: owner, Name: "Busta", Circle: "family"}
	s.convs = []memory.Conversation{{Person: owner, Source: "test", Thread: "chat-owner", LastActivity: at(28, 9, 0)}}
	s.skills = []memory.Skill{{Name: "lyric writing", Level: 4, ShownBy: []string{"lyrics"}}}
	return m, s, s2, r, i
}

func TestItLearnsFromItsOwnWork(t *testing.T) {
	m, s, _, _, i := learningMind()
	m.tick(context.Background(), at(29, 10, 0))

	if len(s.lessons) != 2 || s.lessons[0].Text != "Start the hook on the downbeat" || s.lessons[0].Source != memory.LessonFromSelf ||
		s.lessons[0].Skill != "lyric writing" || s.lessons[0].WorkID.IsZero() {
		t.Fatalf("lessons = %+v; want the first two non-empty ones, its own", s.lessons)
	}
	if got := s.intention(i.ID); got.Status != memory.IntentionDone {
		t.Errorf("intention = %+v", got)
	}
}

func TestTheOwnersReviewBecomesLessonsOnThatWork(t *testing.T) {
	m, s, _ := gatedMind(&fakeReplier{}, &fakeSystem2{
		lessons:  []system2.LessonLearned{{Skill: "Lyric Writing", Text: "Cut the hook to six words"}, {Skill: "juggling", Text: "x"}},
		feedback: []system2.Feedback{{Skill: "lyric writing", Level: 3, About: "the hook"}},
	})
	s.skills = []memory.Skill{{Name: "lyric writing", Level: 4, ShownBy: []string{"lyrics"}}}
	work := memory.Work{ID: ulid.Make(), Kind: "lyrics", Title: "Hook", CreatedAt: now.Add(-time.Hour)} // what they just made, so what feedback is likely about
	s.works = append(s.works, work)

	p := percept("1", "The hook is too long, cut it to six words")
	p.Speaker, p.SpeakerName = owner, "Busta"
	m.handle(context.Background(), p)
	if len(s.lessons) != 1 || s.lessons[0].Source != memory.LessonFromOwner || s.lessons[0].WorkID != work.ID || s.lessons[0].Skill != "lyric writing" {
		t.Errorf("lessons = %+v", s.lessons)
	}
	if len(s.evidence) != 1 || s.evidence[0].WorkID != work.ID {
		t.Errorf("evidence = %+v; want the feedback tied to the work it asked about", s.evidence)
	}

	q := percept("2", "Make it longer")
	m.handle(context.Background(), q)
	if len(s.lessons) != 1 {
		t.Errorf("took a lesson from someone who isn't the owner")
	}
}
