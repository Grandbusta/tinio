package mind

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/usage"
)

const morningJSON = `{"opening":"Morning, Ada. The supplier still hadn't replied last night.","question":"Is the gift card still in the guide?",
	"items":[
		{"what":"Chase Yunnan Tea about the late order","how":"A short note with a date.","source":"from the topic Supplier","draft":true,"brief":"A short, friendly note to Yunnan Tea asking for the order by Friday."},
		{"what":"Plan a launch party","how":"","source":"","draft":false,"brief":""},
		{"what":"Book Saturday's tasting","how":"10 to 12 at the shop.","source":"from the topic Winter blend","draft":false,"brief":""}]}`

func morningMind() (*Mind, *fakeStore, *fakeSystem2, *fakeReplier) {
	const ada = "account:ada"
	s2 := &fakeSystem2{formats: map[string]string{"morning": morningJSON, "activity": activityJSON}}
	m, s := lifeMind(s2)
	r := &fakeReplier{}
	m.Replier = r
	m.Morning = Morning{Prompt: Prompt{Ref: "morning@test", Text: "You are {name}, for {owner}."}, At: 7*time.Hour + 30*time.Minute, Drafts: 2}
	for d := range m.Morning.Days {
		m.Morning.Days[d] = true
	}
	m.Autonomy = action.Policy{Owner: ada, Classes: action.Defaults()}
	s.convs = []memory.Conversation{{Person: ada, Source: "test", Thread: "chat-owner", LastActivity: at(28, 18, 0)}}
	s.percepts = []perception.Percept{{Speaker: ada, Kind: perception.KindMessage, At: at(28, 18, 0)}}
	return m, s, s2, r
}

// Its owner's morning is worked out ahead, with a draft made for what it
// chose to draft, and sent once at their time with the draft after it; a thing
// with nowhere it came from is never sent.
func TestTheMorningIsPreparedAheadAndSentOnTime(t *testing.T) {
	ctx := context.Background()
	m, s, s2, r := morningMind()

	m.tick(ctx, at(29, 6, 44))
	if len(s2.requests) != 0 {
		t.Fatalf("%d requests before it's time to start", len(s2.requests))
	}
	m.tick(ctx, at(29, 6, 45))
	mo, ok := s.mornings["2026-10-29"]
	if !ok || len(mo.Items) != 2 || mo.Items[0].Intention == "" || mo.Items[1].Intention != "" {
		t.Fatalf("prepared %+v; want two things, the first drafted", mo)
	}
	if len(s.works) != 1 || s.works[0].IntentionID.String() != mo.Items[0].Intention {
		t.Fatalf("works %+v; want the draft made ahead, in the same tick", s.works)
	}
	if len(r.sent) != 0 {
		t.Fatalf("sent %q before its time", r.sent)
	}

	m.tick(ctx, at(29, 7, 30))
	if len(r.sent) != 2 {
		t.Fatalf("sent %d messages at its time; want the note and its draft: %q", len(r.sent), r.sent)
	}
	note := r.sent[0]
	for _, want := range []string{"Morning, Ada", "1. Chase Yunnan Tea", "2. Book Saturday's tasting", "gift card"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note %q doesn't have %q", note, want)
		}
	}
	if strings.Contains(note, "launch party") {
		t.Errorf("the note has a thing from nowhere: %q", note)
	}
	if !strings.HasPrefix(r.sent[1], "For 1: Lagos Rain, verse 2") {
		t.Errorf("the draft went as %q", r.sent[1])
	}
	if s.mornings["2026-10-29"].SentAt.IsZero() {
		t.Error("the morning isn't marked sent")
	}

	m.tick(ctx, at(29, 7, 45))
	if len(r.sent) != 2 || len(s2.requests) != 2 {
		t.Errorf("after it was sent: %d messages, %d requests; want nothing more", len(r.sent), len(s2.requests))
	}
}

// An owner who hasn't written for a week gets no morning, and nothing is spent
// on one; in light mode it comes without drafts.
func TestNoMorningForAQuietOwnerAndNoDraftsInLightMode(t *testing.T) {
	ctx := context.Background()
	m, s, s2, r := morningMind()
	s.percepts = []perception.Percept{{Speaker: "account:ada", Kind: perception.KindMessage, At: at(20, 18, 0)}}
	m.tick(ctx, at(29, 6, 45))
	m.tick(ctx, at(29, 7, 30))
	if len(s2.requests) != 0 || len(r.sent) != 0 {
		t.Errorf("quiet a week: %d requests, sent %q; want no morning", len(s2.requests), r.sent)
	}

	m, s, s2, r = morningMind()
	week := at(29, 6, 45).Add(2*time.Hour - usage.Week) // past its week, not resting
	s.standing = usage.Standing{CapUSD: 1.25, Started: week, WeekStart: week, WeekUSD: 1.3}
	m.tick(ctx, at(29, 6, 45))
	m.tick(ctx, at(29, 7, 30))
	if len(s2.requests) != 1 || !strings.Contains(s2.requests[0].Note, "can't draft") || len(r.sent) != 1 {
		t.Errorf("light mode: %d requests, sent %q; want the note alone, nothing drafted", len(s2.requests), r.sent)
	}
}
