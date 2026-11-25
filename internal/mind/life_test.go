package mind

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// at is a time on a day in October 2026, in the agent's zone (lagos, UTC+1).
func at(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, lagos) }

func lifeMind(s2 *fakeSystem2) (*Mind, *fakeStore) {
	m, s := newMind(&fakeReplier{}, &fakeSystem1{}, s2)
	m.Zone = lagos
	m.Day = Day{BudgetUSD: 0.05, ActivityPrompt: Prompt{Ref: "activity@test", Text: "You are {name}, doing something."}}
	m.Abilities = ability.NewSet()
	m.Autonomy = action.Policy{Owner: owner, Classes: action.Defaults()}
	return m, s
}

// Work that fails is put off and tried again a few times, further apart each
// time, then given up, its cost counted every time; an answer cut off for
// length is given up at once.
func TestFailedWorkIsTriedAFewTimesThenGivenUp(t *testing.T) {
	ctx := context.Background()
	m, s, s2, _, i := activityMind()
	s2.err = errors.New("timeout")
	when := at(29, 10, 0)
	m.tick(ctx, when)
	m.tick(ctx, when.Add(time.Minute))
	if len(s2.requests) != 1 {
		t.Fatalf("%d requests in two minutes; want the failed one put off", len(s2.requests))
	}
	for range retries {
		m.tick(ctx, s.intention(i.ID).DueAt)
	}
	if got := s.intention(i.ID); got.Status != memory.IntentionMissed || len(s2.requests) != retries+1 {
		t.Errorf("after %d tries: %s; want it given up", len(s2.requests), got.Status)
	}
	var spent float64
	for _, d := range s.decisions {
		spent += d.CostUSD
	}
	if spent < 0.002*float64(retries+1)-1e-9 {
		t.Errorf("recorded $%.4f for %d failed calls; want each counted", spent, retries+1)
	}

	m, s, s2, _, i = activityMind()
	s2.err = fmt.Errorf("openai responses: %w (max_output_tokens)", system2.ErrCutOff)
	m.tick(ctx, when)
	if got := s.intention(i.ID); got.Status != memory.IntentionMissed || len(s2.requests) != 1 {
		t.Errorf("cut off: %s after %d requests; want it given up at once", got.Status, len(s2.requests))
	}
}

// A weekly look back that fails is tried again later, not the next minute.
func TestAFailedReviewIsTriedAgain(t *testing.T) {
	ctx := context.Background()
	r, _, r2, _ := reviewMind(`{"retire_lessons":[],"new_lessons":[],"principles":[],"outcomes":[]}`)
	r2.err = errors.New("timeout")
	r.tick(ctx, at(25, 20, 0))
	r.tick(ctx, at(25, 20, 1))
	if len(r2.requests) != 1 {
		t.Fatalf("%d looks back in two minutes; want the failed one put off", len(r2.requests))
	}
	r2.err = nil
	r.tick(ctx, at(25, 20, 0).Add(retryAfter))
	r.tick(ctx, at(25, 20, 0).Add(retryAfter+time.Minute))
	if len(r2.requests) != 2 {
		t.Errorf("tried again: %d requests; want the week looked back on once more, then done", len(r2.requests))
	}
}

const activityJSON = `{"title":"Lagos Rain, verse 2","kind":"lyrics","text":"Rain on the danfo roof...",
	"project_notes":"Verses done; next: the bridge","project_done":false,
	"remember":["Tobi wrote the second verse of Lagos Rain"," "]}`

func activityMind() (*Mind, *fakeStore, *fakeSystem2, memory.Project, memory.Intention) {
	s2 := &fakeSystem2{formats: map[string]string{"activity": activityJSON}}
	m, s := lifeMind(s2)
	song := memory.Project{ID: ulid.Make(), Title: "Lagos Rain", Kind: "music", Status: memory.ProjectActive,
		Notes: "Verse 1 done; next: verse 2", CreatedAt: at(27, 10, 0), UpdatedAt: at(28, 18, 0)}
	s.projects[song.ID] = song
	s.works = []memory.Work{{ID: ulid.Make(), ProjectID: song.ID, Kind: "lyrics", Title: "Lagos Rain, verse 1",
		Text: "Wake up to the horns...", CreatedAt: at(28, 18, 0)}}
	i := memory.Intention{ID: ulid.Make(), Subject: owner, Kind: memory.IntentionActivity, ProjectID: song.ID,
		Text: "Write verse 2 of Lagos Rain", DueAt: at(29, 10, 0), Status: memory.IntentionPending, CreatedAt: at(29, 8, 0)}
	s.intentions = []memory.Intention{i}
	return m, s, s2, song, i
}

func TestActivityMakesAWorkAndRemembersIt(t *testing.T) {
	m, s, s2, song, i := activityMind()
	m.tick(context.Background(), at(29, 10, 0))

	if len(s2.requests) != 1 {
		t.Fatalf("system 2 requests = %d", len(s2.requests))
	}
	w := s.works[len(s.works)-1]
	if w.Title != "Lagos Rain, verse 2" || w.Kind != "lyrics" || w.ProjectID != song.ID || w.IntentionID != i.ID {
		t.Errorf("work = %+v", w)
	}
	if p := s.projects[song.ID]; p.Notes != "Verses done; next: the bridge" || p.Status != memory.ProjectActive {
		t.Errorf("project = %+v", p)
	}
	if len(s.facts) != 1 || s.facts[0].Subject != owner || s.facts[0].Text != "Tobi wrote the second verse of Lagos Rain" {
		t.Errorf("facts = %+v", s.facts)
	}
	if got := s.intention(i.ID); got.Status != memory.IntentionDone || got.Result != "Lagos Rain, verse 2" {
		t.Errorf("intention = %+v", got)
	}
}

func TestBusyActivityWaits(t *testing.T) {
	m, s, s2, _, i := activityMind()
	s.convs = []memory.Conversation{{Person: sam, Source: "test", Thread: "chat-1", LastActivity: at(29, 9, 58)}}
	m.tick(context.Background(), at(29, 10, 0))
	if len(s2.requests) != 0 || !s.intention(i.ID).DueAt.Equal(at(29, 10, 30)) {
		t.Errorf("requests %d, due %v; want postponed to 10:30", len(s2.requests), s.intention(i.ID).DueAt.In(lagos))
	}
}

func TestOverBudgetOrLateActivityIsMissed(t *testing.T) {
	m, s, s2, _, i := activityMind()
	s.spent = 0.05
	m.tick(context.Background(), at(29, 10, 0))
	if got := s.intention(i.ID); got.Status != memory.IntentionMissed || len(s2.requests) != 0 {
		t.Errorf("over budget: %+v, %d requests", got, len(s2.requests))
	}

	m, s, s2, _, i = activityMind()
	m.tick(context.Background(), at(29, 16, 1))
	if got := s.intention(i.ID); got.Status != memory.IntentionMissed || len(s2.requests) != 0 {
		t.Errorf("6h late: %+v, %d requests", got, len(s2.requests))
	}
}

// What it makes, for itself or for someone, is made on the work model, never
// the day's; without a work model, on the reply's.
func TestWorksAreMadeOnTheWorkModel(t *testing.T) {
	m, _, work, _, _ := activityMind()
	reply, day := &fakeSystem2{}, &fakeSystem2{}
	m.System2, m.DaySystem2, m.WorkSystem2 = reply, day, work
	m.tick(context.Background(), at(29, 10, 0))
	if len(work.requests) != 1 || len(day.requests)+len(reply.requests) != 0 {
		t.Errorf("its own activity: work model %d requests, day's %d, reply's %d; want it on the work model's",
			len(work.requests), len(day.requests), len(reply.requests))
	}

	m, _, reply, _, _ = activityMind()
	m.DaySystem2 = &fakeSystem2{}
	m.tick(context.Background(), at(29, 10, 0))
	if len(reply.requests) != 1 {
		t.Errorf("no work model: reply's model %d requests; want the activity on it", len(reply.requests))
	}

	work = &fakeSystem2{formats: map[string]string{"activity": verseJSON}}
	pm, s := promiseMind(&fakeReplier{}, work)
	pm.System2, pm.DaySystem2, pm.WorkSystem2 = &fakeSystem2{}, &fakeSystem2{}, work
	s.intentions = []memory.Intention{promised("Tobi writes Sam a verse about Sunday mornings", "", "", now.Add(-time.Minute))}
	pm.tick(context.Background(), now)
	if sent := pm.System2.(*fakeSystem2); len(work.requests) != 1 || len(sent.requests) != 1 || len(pm.DaySystem2.(*fakeSystem2).requests) != 0 {
		t.Errorf("a promise to Sam: work model %d requests, reply's %d, day's %d; want it made on the work model's and sent on the reply's",
			len(work.requests), len(sent.requests), len(pm.DaySystem2.(*fakeSystem2).requests))
	}
}
