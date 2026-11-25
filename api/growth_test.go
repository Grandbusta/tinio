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

// Helping you is what they've learned about how their owner likes things,
// newest first (a retired lesson left out), their skills, and the week
// counted: works, requests decided, and the things in their mornings ticked
// done.
func TestHelpingYouIsWhatTheyLearnedAndTheWeek(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	ctx, st := context.Background(), store.New(ta.db).For(zara)
	now := time.Now().UTC()
	ta.now = now
	day := func(d int) time.Time { return now.Add(time.Duration(d) * 24 * time.Hour) }

	old := memory.Lesson{ID: ulid.Make(), Skill: "copywriting", Text: "Long hooks", Source: memory.LessonFromSelf, At: day(-9)}
	for _, err := range []error{
		st.AddSkill(ctx, memory.Skill{Name: "copywriting", Level: 6.5, UpdatedAt: day(-1)}),
		st.SaveLesson(ctx, memory.Lesson{ID: ulid.Make(), Skill: "copywriting", Text: "Posts under 50 words", Source: memory.LessonFromOwner, At: day(-1)}),
		st.SaveLesson(ctx, old), st.RetireLesson(ctx, old.ID, day(-2)),
		st.SaveTaste(ctx, memory.Taste{ID: ulid.Make(), Text: "Nothing goes out on a Sunday", Likes: false, At: day(-3)}),
		st.SaveWork(ctx, memory.Work{ID: ulid.Make(), Kind: "post", Title: "Hooks", CreatedAt: day(-2)}),
		st.SaveWork(ctx, memory.Work{ID: ulid.Make(), Kind: "post", Title: "Last month's", CreatedAt: day(-20)}),
		st.SaveMorning(ctx, memory.Morning{Day: day(-1).Format(time.DateOnly), PreparedAt: day(-1), Items: []memory.MorningItem{
			{ID: "1", What: "Chase the supplier", Source: "a topic", Done: true}, {ID: "2", What: "Book the tasting", Source: "a topic"}}}),
		st.MorningSent(ctx, day(-1).Format(time.DateOnly), day(-1)),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, status := range []string{memory.ApprovalApproved, memory.ApprovalDenied} {
		a := memory.Approval{ID: ulid.Make(), Class: "reach_out", What: "message Sam", Text: "x", Status: memory.ApprovalPending, AskedAt: day(-3)}
		if err := st.SaveApproval(ctx, a); err != nil {
			t.Fatal(err)
		}
		if _, err := st.DecideApproval(ctx, a.ID, status, "", day(-2).Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/growth", nil, ada, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("helping you: %d %v", rec.Code, out)
	}
	d := data(out)
	var learned []string
	for _, x := range d["learned"].([]any) {
		learned = append(learned, x.(map[string]any)["text"].(string)+" ("+x.(map[string]any)["from"].(string)+")")
	}
	if len(learned) != 2 || learned[0] != "Posts under 50 words (owner)" || learned[1] != "Nothing goes out on a Sunday (taste)" {
		t.Errorf("learned = %q; want the lesson then the taste, the retired one left out", learned)
	}
	week := d["week"].(map[string]any)
	if week["made"] != float64(1) || week["decided"] != float64(2) || week["approved"] != float64(1) ||
		week["things"] != float64(2) || week["done"] != float64(1) {
		t.Errorf("week = %v", week)
	}
}
