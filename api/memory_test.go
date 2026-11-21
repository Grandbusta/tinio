package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// Memory is what they remember about their owner and about themselves (no
// one else); their owner corrects a fact, which keeps its filing, or has it
// forgotten for good, and only one of their own agent's.
func TestTheOwnerCorrectsAndForgetsWhatTheyRemember(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara, other := ta.raiseZara(t, ada), ta.raiseZara(t, ada)
	ctx, st := context.Background(), store.New(ta.db).For(zara)
	var account string
	if err := ta.db.QueryRow(ctx, `SELECT account_id FROM agents WHERE id = $1`, zara).Scan(&account); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ta.now = now
	p := perception.Percept{ID: ulid.Make(), Source: memory.ChatSource, ExternalID: ulid.Make().String(), Thread: memory.ChatSource,
		Speaker: "account:" + account, Kind: perception.KindMessage, Text: "I live in Yaba", At: now.Add(-time.Hour)}
	if _, err := st.SavePercept(ctx, p); err != nil {
		t.Fatal(err)
	}
	mine := memory.Fact{ID: ulid.Make(), PerceptID: p.ID, Subject: "account:" + account, Text: "Ada lives in Yaba.", At: p.At, Areas: []string{"home"}}
	hers := memory.Fact{ID: ulid.Make(), PerceptID: p.ID, Subject: memory.SubjectSelf, Text: "Zara likes short hooks.", At: p.At}
	theirs := memory.Fact{ID: ulid.Make(), PerceptID: p.ID, Subject: "account:someone", Text: "Someone else's.", At: p.At}
	for _, f := range []memory.Fact{mine, hers, theirs} {
		if err := st.SaveFact(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	rec, out := ta.do(t, "GET", "/v1/agents/"+zara+"/memory", nil, ada, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("memory: %d %v", rec.Code, out)
	}
	about := map[string]string{}
	for _, f := range data(out)["facts"].([]any) {
		f := f.(map[string]any)
		about[f["text"].(string)] = f["about"].(string) + "/" + f["from"].(string)
	}
	if about[mine.Text] != "you/message" || about[hers.Text] != "self/message" || about[theirs.Text] != "" {
		t.Errorf("facts = %v", about)
	}

	if rec, out := ta.do(t, "POST", "/v1/agents/"+zara+"/facts/"+mine.ID.String(), map[string]string{"text": " Ada lives in  Surulere. "}, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("correcting: %d %v", rec.Code, out)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/facts/"+mine.ID.String(), map[string]string{"text": ""}, ada, site); rec.Code != http.StatusBadRequest {
		t.Errorf("an empty correction: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+zara+"/facts/"+hers.ID.String()+"/forget", nil, ada, site); rec.Code != http.StatusOK {
		t.Fatalf("forgetting: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", "/v1/agents/"+other+"/facts/"+theirs.ID.String()+"/forget", nil, ada, site); rec.Code != http.StatusNotFound {
		t.Errorf("forgetting another agent's fact: %d", rec.Code)
	}
	fs, err := st.RecallCandidates(ctx, []string{"account:" + account, memory.SubjectSelf}, []string{"home"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Text != "Ada lives in Surulere." || len(fs[0].Areas) != 1 || fs[0].Areas[0] != "home" {
		t.Errorf("what recall finds now = %+v", fs)
	}
}
