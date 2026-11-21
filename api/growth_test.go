package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Growth is their active dreams with the goals that serve them (a dropped one
// left out), each goal's learning plan and skill level, their newest review,
// and the week counted: works but not the journal, requests decided, calls
// guessed.
func TestGrowthIsTheirDreamsGoalsAndWeek(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	ctx, st := context.Background(), store.New(ta.db).For(zara)
	now := time.Now().UTC()
	ta.now = now
	day := func(d int) time.Time { return now.Add(time.Duration(d) * 24 * time.Hour) }

	// Zara was raised with dreams of her own; this one is added, and one dropped.
	dream := memory.Dream{ID: ulid.Make(), Text: "Zara runs a studio of her own", Status: memory.DreamActive, Source: memory.DreamFromSelf, Since: day(-30)}
	gone := memory.Dream{ID: ulid.Make(), Text: "Zara moves to Lisbon", Status: memory.DreamDropped, Source: memory.DreamFromSelf, Since: day(-40)}
	learn := memory.Goal{ID: ulid.Make(), DreamID: dream.ID, Text: "Write hooks that stop the scroll", Status: memory.GoalActive,
		Skill: "copywriting", Due: day(30).Truncate(24 * time.Hour), CreatedAt: day(-20), UpdatedAt: day(-1)}
	for _, err := range []error{
		st.SaveDream(ctx, dream), st.SaveDream(ctx, gone), st.SaveGoal(ctx, learn),
		st.SaveGoal(ctx, memory.Goal{ID: ulid.Make(), DreamID: dream.ID, Text: "Dropped", Status: memory.GoalDropped, CreatedAt: day(-19), UpdatedAt: day(-1)}),
		st.SaveGoal(ctx, memory.Goal{ID: ulid.Make(), DreamID: dream.ID, Text: "Ten clients", Status: memory.GoalProposed, Progress: "Two so far", CreatedAt: day(-1), UpdatedAt: day(-1)}),
		st.AddSkill(ctx, memory.Skill{Name: "copywriting", Level: 6.5, UpdatedAt: day(-1)}),
		st.SavePlan(ctx, memory.LearningPlan{ID: ulid.Make(), GoalID: learn.ID, Skill: "copywriting", Stage: 1, Status: memory.PlanActive,
			Stages: []memory.Stage{{N: 0, Learn: "What a hook is", DoneAt: day(-5)}, {N: 1, Learn: "Hooks for a week"}}, CreatedAt: day(-20), UpdatedAt: day(-5)}),
		st.SaveWork(ctx, memory.Work{ID: ulid.Make(), Kind: "post", Title: "Hooks", CreatedAt: day(-2)}),
		st.SaveWork(ctx, memory.Work{ID: ulid.Make(), Kind: "journal", Title: "Journal", CreatedAt: day(-2)}),
		st.SaveWork(ctx, memory.Work{ID: ulid.Make(), Kind: "post", Title: "Last month's", CreatedAt: day(-20)}),
		st.SaveWork(ctx, memory.Work{ID: ulid.Make(), Kind: "review", Title: "A busy week", Text: "I wrote a lot.", CreatedAt: day(-1)}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, status := range []string{memory.ApprovalApproved, memory.ApprovalDenied, memory.ApprovalExpired} {
		a := memory.Approval{ID: ulid.Make(), Class: "dream", What: "add a dream", Text: "x", Status: memory.ApprovalPending, AskedAt: day(-3)}
		if err := st.SaveApproval(ctx, a); err != nil {
			t.Fatal(err)
		}
		if _, err := st.DecideApproval(ctx, a.ID, status, "", day(-2).Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []int{0, 1} {
		c := memory.Call{ID: ulid.Make(), Type: memory.CallFocus, Mode: memory.CallAfter, Situation: "What first?", Options: []string{"a", "b"},
			Status: memory.CallAsked, CreatedAt: day(-3)}
		if err := st.SaveCall(ctx, c); err != nil {
			t.Fatal(err)
		}
		c.OwnerPick, c.AnsweredBy, c.AnsweredAt = owner, memory.AnsweredByApproval, day(-2)
		if _, err := st.AnswerCall(ctx, c, false); err != nil {
			t.Fatal(err)
		}
	}

	rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/growth", nil, ada, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("growth: %d %v", rec.Code, out)
	}
	d := data(out)
	var mine map[string]any
	for _, x := range d["dreams"].([]any) {
		if x.(map[string]any)["text"] == "Zara moves to Lisbon" {
			t.Error("a dropped dream is shown")
		}
		if x.(map[string]any)["id"] == dream.ID.String() {
			mine = x.(map[string]any)
		}
	}
	if mine == nil {
		t.Fatalf("dreams = %v", d["dreams"])
	}
	goals := mine["goals"].([]any)
	if len(goals) != 2 {
		t.Fatalf("goals = %v", goals)
	}
	g := goals[0].(map[string]any)
	plan, _ := g["plan"].(map[string]any)
	stages, _ := plan["stages"].([]any)
	if g["text"] != learn.Text || g["level"] != 6.5 || g["due"] != learn.Due.Format(time.DateOnly) || plan["stage"] != 1.0 ||
		len(stages) != 2 || stages[0].(map[string]any)["done"] != true || stages[1].(map[string]any)["done"] != false {
		t.Errorf("the learning goal = %v", g)
	}
	if p := goals[1].(map[string]any); p["status"] != memory.GoalProposed || p["progress"] != "Two so far" {
		t.Errorf("the proposed goal = %v", p)
	}
	if r, _ := d["review"].(map[string]any); r["title"] != "A busy week" || r["text"] != "I wrote a lot." {
		t.Errorf("review = %v", d["review"])
	}
	week := d["week"].(map[string]any)
	if week["made"] != 1.0 || week["decided"] != 2.0 || week["approved"] != 1.0 || week["answered"] != 2.0 || week["guessed"] != 1.0 {
		t.Errorf("week = %v", week)
	}
}
