package mind

import (
	"context"
	"errors"
	"fmt"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/ability/journal"
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// at is a time on a day in October 2026, in the agent's zone (lagos, UTC+1).
func at(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, lagos) }

func lifeMind(s2 *fakeSystem2) (*Mind, *fakeStore) {
	m, s := newMind(&fakeReplier{}, &fakeSystem1{}, s2)
	m.Zone = lagos
	m.Life.Routine = []string{"Tobi makes music in the evenings."}
	m.Day = Day{PlanAt: 8 * time.Hour, JournalAt: 22 * time.Hour, MaxActivities: 3, BudgetUSD: 0.05,
		PlanPrompt:     Prompt{Ref: "plan@test", Text: "You are {name}. Routine:\n{routine}\nUp to {max}."},
		ActivityPrompt: Prompt{Ref: "activity@test", Text: "You are {name}, doing something."}}
	m.Abilities = ability.NewSet(journal.Journal{Store: s})
	return m, s
}

// An owner who's written nothing for days slows its day to one thing, then
// stops it; what the agent says first (a check-in) doesn't count. The day is
// back as soon as they write.
func TestAQuietOwnerSlowsItsDay(t *testing.T) {
	const ada = "account:ada"
	plan := `{"calls":[],"activities":[
		{"at":"16:00","what":"Draft the newsletter","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""},
		{"at":"17:00","what":"Read about pricing","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""},
		{"at":"18:30","what":"Outline a talk","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""}]}`
	planned := func(s *fakeStore) int {
		n := 0
		for _, i := range s.intentions {
			if i.Kind == memory.IntentionActivity && i.Text != journalText {
				n++
			}
		}
		return n
	}
	for _, c := range []struct {
		name      string
		lastWrote time.Time // zero: never
		plans     bool
		things    int
	}{
		{"wrote yesterday", at(28, 9, 0), true, 3},
		{"quiet four days", at(25, 9, 0), true, 1},
		{"quiet eight days", at(21, 9, 0), false, 0},
		{"never wrote", time.Time{}, false, 0},
	} {
		s2 := &fakeSystem2{formats: map[string]string{"day_plan": plan}}
		m, s := lifeMind(s2)
		m.Autonomy = action.Policy{Owner: ada, Classes: action.Defaults()}
		s.percepts = []perception.Percept{{Speaker: ada, Kind: perception.KindCheckIn, At: at(28, 13, 0)}}
		if !c.lastWrote.IsZero() {
			s.percepts = append(s.percepts, perception.Percept{Speaker: ada, Kind: perception.KindMessage, At: c.lastWrote})
		}
		m.tick(context.Background(), at(29, 9, 0))
		if got := len(s2.requests) > 0; got != c.plans || planned(s) != c.things {
			t.Errorf("%s: planned %v with %d things; want %v with %d", c.name, got, planned(s), c.plans, c.things)
		}
		if c.things == 1 && !strings.Contains(s2.requests[0].Note, "just one thing") {
			t.Errorf("%s: the plan isn't told why it's one thing: %q", c.name, s2.requests[0].Note)
		}
		if c.plans {
			continue
		}
		s.percepts = append(s.percepts, perception.Percept{Speaker: ada, Kind: perception.KindMessage, At: at(29, 15, 0)})
		m.tick(context.Background(), at(29, 15, 1))
		if len(s2.requests) != 1 || planned(s) != 3 {
			t.Errorf("%s, then they wrote: %d plans with %d things; want its day back", c.name, len(s2.requests), planned(s))
		}
	}
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

// A plan that fails isn't the day lost: it's planned again later, not the
// next minute; and so is a weekly review.
func TestAFailedPlanOrReviewIsTriedAgain(t *testing.T) {
	ctx := context.Background()
	plan := `{"calls":[],"activities":[{"at":"16:00","what":"Draft the newsletter","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""}]}`
	s2 := &fakeSystem2{formats: map[string]string{"day_plan": plan}, err: errors.New("timeout")}
	m, s := lifeMind(s2)
	m.tick(ctx, at(29, 8, 1))
	m.tick(ctx, at(29, 8, 2))
	if len(s2.requests) != 1 {
		t.Fatalf("%d plans asked in two minutes; want the failed one put off", len(s2.requests))
	}
	s2.err = nil
	m.tick(ctx, at(29, 8, 1).Add(retryAfter))
	if len(s2.requests) != 2 || len(s.intentions) == 0 {
		t.Errorf("tried again: %d requests, %d planned; want the day planned", len(s2.requests), len(s.intentions))
	}

	r, rs, r2, _ := reviewMind(`{"title":"A week","text":"x","goals":[],"new_goals":[],"projects":[],"dreams":[]}`)
	r2.err = errors.New("timeout")
	r.tick(ctx, at(25, 20, 0))
	r.tick(ctx, at(25, 20, 1))
	if len(r2.requests) != 1 {
		t.Fatalf("%d reviews asked in two minutes; want the failed one put off", len(r2.requests))
	}
	r2.err = nil
	r.tick(ctx, at(25, 20, 0).Add(retryAfter))
	if _, ok, _ := rs.LatestWork(ctx, "review"); len(r2.requests) != 2 || !ok {
		t.Errorf("tried again: %d requests, reviewed %v; want the week reviewed", len(r2.requests), ok)
	}
}

func TestPlanDay(t *testing.T) {
	ctx := context.Background()
	song := memory.Project{ID: ulid.Make(), Title: "Lagos Rain", Kind: "music", Status: memory.ProjectActive,
		Notes: "Verse 1 done; next: verse 2", CreatedAt: at(27, 10, 0), UpdatedAt: at(28, 18, 0)}
	s2 := &fakeSystem2{formats: map[string]string{"day_plan": `{"activities":[
		{"at":"10:00","what":"Write verse 2 of Lagos Rain","project_id":"` + song.ID.String() + `","new_project_title":"","project_kind":""},
		{"at":"07:00","what":"Already past","project_id":"","new_project_title":"","project_kind":""},
		{"at":"23:00","what":"After the journal","project_id":"","new_project_title":"","project_kind":""},
		{"at":"14:00","what":"Sketch a beat for a new song","project_id":"not-an-id","new_project_title":"Night Drive","project_kind":"music"},
		{"at":"16:30","what":"Write notes on chord progressions","project_id":"","new_project_title":"","project_kind":"","for":"owner"},
		{"at":"18:00","what":"One too many","project_id":"","new_project_title":"","project_kind":""}]}`}}
	m, s := lifeMind(s2)
	m.Autonomy.Owner = owner
	s.percepts = []perception.Percept{{Speaker: owner, Kind: perception.KindMessage, At: at(28, 20, 0)}}
	s.projects[song.ID] = song

	m.tick(ctx, at(29, 7, 59))
	if len(s2.requests) != 0 {
		t.Fatal("planned before plan time")
	}
	m.tick(ctx, at(29, 9, 0))
	if len(s2.requests) != 1 {
		t.Fatalf("system 2 requests = %d, want one plan", len(s2.requests))
	}
	var got []string
	var newProject ulid.ULID
	for _, i := range s.intentions {
		got = append(got, i.DueAt.In(lagos).Format("15:04")+" "+i.Text)
		if strings.HasPrefix(i.Text, "Sketch") {
			newProject = i.ProjectID
		}
		if strings.HasPrefix(i.Text, "Write verse 2") && i.ProjectID != song.ID {
			t.Errorf("verse 2 is not part of Lagos Rain: %+v", i)
		}
		// Work for its owner is made for them, so it never reaches its public page.
		if forOwner := strings.HasPrefix(i.Text, "Write notes"); forOwner != (i.Subject == owner) {
			t.Errorf("%q is for %q", i.Text, i.Subject)
		}
	}
	want := []string{"10:00 Write verse 2 of Lagos Rain", "14:00 Sketch a beat for a new song",
		"16:30 Write notes on chord progressions", "22:00 " + journalText}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("planned:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if p, ok := s.projects[newProject]; !ok || p.Title != "Night Drive" || p.Kind != "music" {
		t.Errorf("new project = %+v", p)
	}

	m.tick(ctx, at(29, 9, 1))
	restarted, _ := lifeMind(s2)
	restarted.Store = s
	restarted.tick(ctx, at(29, 9, 2))
	if len(s2.requests) != 1 {
		t.Errorf("planned the same day again: %d requests", len(s2.requests))
	}
}

const activityJSON = `{"title":"Lagos Rain, verse 2","kind":"lyrics","text":"Rain on the danfo roof...",
	"project_notes":"Verses done; next: the bridge","project_done":false,
	"remember":["Tobi wrote the second verse of Lagos Rain"," "]}`

func activityMind() (*Mind, *fakeStore, *fakeSystem2, memory.Project, memory.Intention) {
	s2 := &fakeSystem2{formats: map[string]string{"activity": activityJSON}}
	m, s := lifeMind(s2)
	m.planned = "2026-10-29"
	song := memory.Project{ID: ulid.Make(), Title: "Lagos Rain", Kind: "music", Status: memory.ProjectActive,
		Notes: "Verse 1 done; next: verse 2", CreatedAt: at(27, 10, 0), UpdatedAt: at(28, 18, 0)}
	s.projects[song.ID] = song
	s.works = []memory.Work{{ID: ulid.Make(), ProjectID: song.ID, Kind: "lyrics", Title: "Lagos Rain, verse 1",
		Text: "Wake up to the horns...", CreatedAt: at(28, 18, 0)}}
	i := memory.Intention{ID: ulid.Make(), Subject: memory.SubjectSelf, Kind: memory.IntentionActivity, ProjectID: song.ID,
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
	if len(s.facts) != 1 || s.facts[0].Subject != memory.SubjectSelf || s.facts[0].Text != "Tobi wrote the second verse of Lagos Rain" {
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

func TestRounds(t *testing.T) {
	ctx := context.Background()
	s2 := &fakeSystem2{formats: map[string]string{"day_plan": `{"activities":[
		{"at":"08:30","what":"Study customer discovery","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""},
		{"at":"12:00","what":"Write outreach email","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""},
		{"at":"16:00","what":"Role-play a sales call","project_id":"","new_project_title":"","project_kind":"","ability":"","query":"","goal_id":"","resource_id":""}],
		"calls":[{"type":"focus","situation":"What to focus on","options":["sales","music"],"pick":0,"why":"","confidence":0.6,"stakes":"low","reversible":true}]}`}}
	m, s := lifeMind(s2)
	m.Calls = Calls{On: true, AsksPerDay: 3}
	m.Day.Pace = 4 // 08:00 to 22:00 in four rounds: 08:00, 11:30, 15:00, 18:30

	due := func(from, to time.Time) []string {
		var out []string
		is, _ := s.IntentionsDue(ctx, from, to)
		for _, i := range is {
			out = append(out, i.Text)
		}
		return out
	}

	m.planDay(ctx, at(29, 8, 1), livelyDay, 0)
	if got := due(at(29, 0, 0), at(29, 23, 59)); len(got) != 4 || got[3] != journalText {
		t.Fatalf("first plan = %q; want rounds 1 to 3 (before 18:30) in one plan, and the journal", got)
	}
	if !strings.Contains(s2.requests[0].Note, "rounds 1 to 3 of 4") {
		t.Errorf("note doesn't say which rounds:\n%s", s2.requests[0].Note)
	}
	if len(s.calls) != 1 {
		t.Errorf("calls = %d", len(s.calls))
	}

	m.planDay(ctx, at(29, 11, 31), livelyDay, 0) // round 2: the first plan has it
	if len(s2.requests) != 1 {
		t.Fatal("planned a round the first plan covered")
	}

	m2, _ := lifeMind(s2)
	m2.Store, m2.Day.Pace = s, 4
	m2.planDay(ctx, at(29, 12, 0), livelyDay, 0)
	m2.planDay(ctx, at(29, 15, 1), livelyDay, 0)
	if len(s2.requests) != 1 {
		t.Fatal("planned again after a restart, with what it planned still to do")
	}

	for _, i := range s.intentions {
		if i.Ability != journal.Name {
			s.FinishIntention(ctx, i.ID, memory.IntentionDone, "done", at(29, 17, 0))
		}
	}
	m.planDay(ctx, at(29, 18, 31), livelyDay, 0)
	if len(s2.requests) != 2 || !strings.Contains(s2.requests[1].Note, "round 4 of 4") {
		t.Errorf("round 4 wasn't planned once the rest was done: %d requests", len(s2.requests))
	}
	if len(s.calls) != 1 {
		t.Error("a later plan made calls")
	}

	// The next day's rounds start again: yesterday's last round doesn't cover today's.
	for _, i := range s.intentions {
		s.FinishIntention(ctx, i.ID, memory.IntentionDone, "done", at(29, 23, 0))
	}
	m.planDay(ctx, at(30, 8, 1), livelyDay, 0)
	for _, i := range s.intentions {
		s.FinishIntention(ctx, i.ID, memory.IntentionDone, "done", at(30, 17, 0))
	}
	m.planDay(ctx, at(30, 18, 31), livelyDay, 0)
	if len(s2.requests) != 4 || !strings.Contains(s2.requests[3].Note, "round 4 of 4") {
		t.Errorf("the next day: %d requests; want rounds 1 to 3, then round 4", len(s2.requests))
	}
}

func TestAStageMovesOnOnlyWhenTheCheckAgrees(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		sure float64
		want int
	}{"the check agrees": {0.9, 1}, "notes about it, not the thing": {0.2, 0}} {
		m, s, s2, _, _ := activityMind()
		s1 := &fakeSystem1{stageMet: map[string]float64{"Outreach notes": c.sure}}
		m.System1 = s1
		m.StageCheck = questions.Set{ID: "stage_check", Version: 1, Primary: "met",
			Questions:  []system1.Question{{Name: "met", Type: decision.QuestionPredicate, Instructions: "?"}},
			Thresholds: questions.Thresholds{Act: 0.7, Ask: 0.5}}
		plan := memory.LearningPlan{ID: ulid.Make(), Skill: "sales and marketing", Status: memory.PlanActive, Stages: []memory.Stage{
			{Learn: "outreach", Practice: "two intro emails", DoneWhen: "two emails with one clear request"}, {Learn: "calls"}}}
		s.plans = []memory.LearningPlan{plan}
		s2.formats["activity"] = `{"title":"Outreach notes","kind":"notes","text":"How to write outreach...","project_notes":"","project_done":false,
			"remember":[],"lessons":[],"ask_for_review":false,"review_question":"","stage_done":"` + plan.ID.String() + `"}`
		m.tick(ctx, at(29, 10, 0))
		if s.plans[0].Stage != c.want {
			t.Errorf("%s: stage = %d, want %d", name, s.plans[0].Stage, c.want)
		}
		if len(s1.stageCalls) != 1 {
			t.Fatalf("%s: checked %d times", name, len(s1.stageCalls))
		}
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

// On a day when it made nothing and talked with no one, the journal isn't
// written: there's nothing to say, so nothing is paid for.
func TestNoJournalOnAQuietDay(t *testing.T) {
	ctx := context.Background()
	s2 := &fakeSystem2{formats: map[string]string{"activity": activityJSON}}
	m, s := lifeMind(s2)
	m.planned = "2026-10-29"
	journalFor := func(day int) memory.Intention {
		i := memory.Intention{ID: ulid.Make(), Subject: memory.SubjectSelf, Kind: memory.IntentionActivity, Ability: journal.Name,
			Text: journalText, DueAt: at(day, 22, 0), Status: memory.IntentionPending, CreatedAt: at(day, 8, 0)}
		s.intentions = append(s.intentions, i)
		return i
	}

	quiet := journalFor(29)
	m.tick(ctx, at(29, 22, 0))
	if len(s2.requests) != 0 || s.intention(quiet.ID).Status != memory.IntentionCancelled {
		t.Fatalf("a quiet day's journal: %d requests, status %s", len(s2.requests), s.intention(quiet.ID).Status)
	}

	m.planned = "2026-10-30"
	s.works = append(s.works, memory.Work{ID: ulid.Make(), Kind: "lyrics", Title: "Lagos Rain, verse 3", CreatedAt: at(30, 15, 0).UTC()})
	journalFor(30)
	m.tick(ctx, at(30, 22, 0))
	if len(s2.requests) != 1 {
		t.Errorf("a day it made something: %d requests, want its journal written", len(s2.requests))
	}
}
