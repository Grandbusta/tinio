package mind

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/perception"
)

// Their owner opening the chat for the first time is met with a hello that
// says they're an AI, without the OK messaging someone first needs: the owner
// asked for it. Once, however often it comes.
func TestTheFirstHello(t *testing.T) {
	s2 := &fakeSystem2{}
	r := &fakeReplier{}
	m, s := newMind(r, &fakeSystem1{}, s2)
	m.Autonomy = action.Policy{Owner: sam, Classes: action.Defaults(), AskExpires: 24 * time.Hour} // reaching out asks first
	hello := perception.Percept{ID: ulid.Make(), Source: "web", ExternalID: "hello", Thread: "web", Speaker: sam, SpeakerName: "Sam",
		Kind: perception.KindHello, At: now}
	m.handle(context.Background(), hello)
	again := hello
	again.ID = ulid.Make()
	m.handle(context.Background(), again)

	if len(r.sent) != 1 || len(s.approvals) != 0 {
		t.Fatalf("sent %q, asked the owner %d times; want one hello, sent straight", r.sent, len(s.approvals))
	}
	if req := s2.requests[0]; !strings.Contains(req.Note, "first time") || !strings.Contains(req.Note, "an AI") || req.Message != "" {
		t.Errorf("asked System 2 for: note %q, message %q", req.Note, req.Message)
	}
}

// A hello System 2 can't write is still said, plainly: the owner is waiting.
func TestAHelloIsSaidEvenIfItCantBeWritten(t *testing.T) {
	r := &fakeReplier{}
	m, _ := newMind(r, &fakeSystem1{}, &fakeSystem2{err: errors.New("401")})
	m.handle(context.Background(), perception.Percept{ID: ulid.Make(), Source: "web", ExternalID: "hello", Thread: "web",
		Speaker: sam, SpeakerName: "Sam", Kind: perception.KindHello, At: now})
	if len(r.sent) != 1 || !strings.Contains(r.sent[0], "I'm an AI") {
		t.Errorf("sent %q", r.sent)
	}
}

// A character that says how they introduce themselves does that instead of
// saying they're an AI.
func TestAHelloTheCharacterShapes(t *testing.T) {
	s2 := &fakeSystem2{}
	m, _ := newMind(&fakeReplier{}, &fakeSystem1{}, s2)
	m.Life.Hello = "Introduce yourself as a software engineer."
	m.handle(context.Background(), perception.Percept{ID: ulid.Make(), Source: "web", ExternalID: "hello", Thread: "web",
		Speaker: sam, SpeakerName: "Sam", Kind: perception.KindHello, At: now})
	if len(s2.requests) != 1 {
		t.Fatalf("asked System 2 %d times", len(s2.requests))
	}
	if note := s2.requests[0].Note; !strings.Contains(note, "as a software engineer") || strings.Contains(note, "an AI") {
		t.Errorf("note %q", note)
	}
}
