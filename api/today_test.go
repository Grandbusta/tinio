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

// Your day is each agent's morning once it's sent: the things it chose, the
// draft it made for one, which the owner ticked, and its requests waiting.
// A morning not sent yet shows nothing but when it comes.
func TestYourDayIsEachAgentsMorning(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	rec, out := ta.do(t, "POST", "/v1/agents", map[string]any{"character": "zara", "owner_name": "Ada", "relation": "friend", "timezone": "UTC",
		"morning": map[string]any{"on": true, "at": "06:30", "days": []int{1, 2, 3, 4, 5, 6, 0}}}, ada, site)
	if rec.Code != http.StatusCreated {
		t.Fatalf("raising Zara: %d %v", rec.Code, out)
	}
	zara := data(out)["agent"].(map[string]any)["id"].(string)
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	ta.now = midnight.Add(9 * time.Hour)
	ctx, st := context.Background(), store.New(ta.db).For(zara)
	day := func() map[string]any {
		t.Helper()
		rec, out := ta.do(t, "GET", "/v1/today", nil, ada, "")
		days, _ := data(out)["days"].([]any)
		if rec.Code != http.StatusOK || len(days) != 1 {
			t.Fatalf("your day: %d %v", rec.Code, out)
		}
		return days[0].(map[string]any)
	}

	mo := memory.Morning{Day: midnight.Format(time.DateOnly), PreparedAt: midnight.Add(5*time.Hour + 45*time.Minute),
		Opening: "Morning, Ada.", Items: []memory.MorningItem{
			{ID: "1", What: "Chase the supplier", How: "A short note.", Source: "from the topic Supplier"},
			{ID: "2", What: "Book the tasting", Source: "from your calendar"},
		}}
	if err := st.SaveMorning(ctx, mo); err != nil {
		t.Fatal(err)
	}
	if d := day(); d["sent"] != "" || len(d["items"].([]any)) != 0 || d["next"] != "06:30" {
		t.Errorf("before it's sent: %v; want only when it comes", d)
	}

	w := memory.Work{ID: ulid.Make(), Kind: "writing", Title: "Note to the supplier", Text: "Dear...", CreatedAt: mo.PreparedAt}
	if err := st.SaveWork(ctx, w); err != nil {
		t.Fatal(err)
	}
	mo.Items[0].Work = w.ID.String()
	if err := st.SaveMorning(ctx, mo); err != nil {
		t.Fatal(err)
	}
	if err := st.MorningSent(ctx, mo.Day, midnight.Add(6*time.Hour+30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	rec, out = ta.do(t, "POST", "/v1/agents/"+zara+"/today/items/2", map[string]any{"done": true}, ada, site)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticking it: %d %v", rec.Code, out)
	}
	d := day()
	items := d["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if d["sent"] != "06:30" || first["work"].(map[string]any)["title"] != "Note to the supplier" || first["done"] != false || second["done"] != true {
		t.Errorf("after it's sent and one ticked: %v", d)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/today/items/9", map[string]any{"done": true}, ada, site); rec.Code != http.StatusNotFound {
		t.Errorf("ticking a thing that isn't there: %d", rec.Code)
	}

	rec, out = ta.do(t, "GET", "/v1/agents/"+zara+"/settings", nil, ada, "")
	if m := data(out)["morning"].(map[string]any); rec.Code != http.StatusOK || m["at"] != "06:30" || len(m["days"].([]any)) != 7 {
		t.Errorf("the morning as raised: %d %v", rec.Code, data(out)["morning"])
	}
	rec, out = ta.do(t, "POST", "/v1/agents/"+zara+"/settings", map[string]any{"morning": map[string]any{"on": true, "at": "7:30", "days": []int{1}}}, ada, site)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a time that isn't HH:MM: %d %v", rec.Code, out)
	}
}
