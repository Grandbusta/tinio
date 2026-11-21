package store

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
)

// What an agent rests until: a missed time here is a reminder sent hours late.
func TestNextDue(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	lead, expires := 30*time.Minute, 24*time.Hour
	next := func(canAsk bool) time.Time {
		t.Helper()
		due, ok, err := s.NextDue(ctx, now, lead, expires, canAsk, 0)
		if err != nil || !ok {
			t.Fatalf("next due: %v %v", ok, err)
		}
		return due.UTC()
	}
	if _, ok, err := s.NextDue(ctx, now, lead, expires, true, 0); err != nil || ok {
		t.Fatalf("nothing due yet: %v %v", ok, err)
	}

	remind := memory.Intention{ID: ulid.Make(), Subject: "test:sam", Kind: memory.IntentionRemind, Text: "call mum",
		DueAt: now.Add(3 * time.Hour), Status: memory.IntentionPending, CreatedAt: now}
	promise := memory.Intention{ID: ulid.Make(), Subject: "test:sam", Kind: memory.IntentionTask, Text: "a poem",
		DueAt: now.Add(2 * time.Hour), Status: memory.IntentionPending, CreatedAt: now}
	for _, i := range []memory.Intention{remind, promise} {
		if err := s.SaveIntention(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	if got := next(true); !got.Equal(promise.DueAt.Add(-lead)) {
		t.Errorf("with a promise due: %s; want its lead, %s", got, promise.DueAt.Add(-lead))
	}

	ask := memory.Approval{ID: ulid.Make(), Class: "reach_out", Person: "test:sam", What: "check in", Text: "hi",
		Kind: "check_in", Status: memory.ApprovalPending, AskedAt: now.Add(-23 * time.Hour)}
	if err := s.SaveApproval(ctx, ask); err != nil {
		t.Fatal(err)
	}
	if got := next(false); !got.Equal(ask.AskedAt.Add(expires)) {
		t.Errorf("an unsent request, no way to ask: %s; want when it expires, %s", got, ask.AskedAt.Add(expires))
	}
	if got := next(true); !got.Equal(now) {
		t.Errorf("an unsent request they can send: %s; want now", got)
	}
}
