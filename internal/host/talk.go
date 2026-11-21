package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/channel/web"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// Talk is an agent's mind run on a clock the caller keeps, ticking only when
// told: for testing replies and days (`api eval`), never for running agents.
type Talk struct {
	r    *runner
	st   *store.Store
	said *sayings
	done chan error
}

// sayings is the chat, noting what the mind says to each message as it goes in:
// the chat's order can't tell, since a message is saved before the one before
// it is answered.
type sayings struct {
	web.Channel
	mu    sync.Mutex
	to    map[ulid.ULID][]string  // by percept
	first map[ulid.ULID]time.Time // by percept: when the reply's first draft showed
}

func (s *sayings) note(p perception.Percept, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.to[p.ID] = append(s.to[p.ID], text)
}

func (s *sayings) Reply(ctx context.Context, p perception.Percept, text string) error {
	s.note(p, text)
	return s.Channel.Reply(ctx, p, text)
}

func (s *sayings) Draft(ctx context.Context, p perception.Percept, text string) error {
	s.mu.Lock()
	if _, ok := s.first[p.ID]; !ok {
		s.first[p.ID] = time.Now()
	}
	s.mu.Unlock()
	return s.Channel.Draft(ctx, p, text)
}

func (s *sayings) Ask(ctx context.Context, p perception.Percept, r memory.Request, id string) (json.RawMessage, error) {
	s.note(p, "[asks to go ahead] "+r.Text)
	return s.Channel.Ask(ctx, p, r, id)
}

func (s *sayings) SendFile(ctx context.Context, p perception.Percept, name string, data []byte, caption string) error {
	s.note(p, "[sends "+name+"] "+caption)
	return s.Channel.SendFile(ctx, p, name, data, caption)
}

// Talk starts agent's mind, seeded, for their owner (account, called owner),
// at pace, until ctx is done or End.
func (h *Host) Talk(ctx context.Context, agent, account, owner string, pace int) (*Talk, error) {
	// Unbuffered: a message is taken once the last is answered, a tick once the last is done.
	r := &runner{row: agentRow{id: agent, account: account, ownerName: owner, ready: true, pace: pace},
		percepts: make(chan perception.Percept), ticks: make(chan time.Time)}
	m, done, err := h.mind(ctx, r)
	if err != nil {
		return nil, err
	}
	st := store.New(h.pool).For(agent)
	t := &Talk{r: r, st: st, said: &sayings{Channel: web.Channel{Store: st}, to: map[ulid.ULID][]string{}, first: map[ulid.ULID]time.Time{}}, done: make(chan error, 1)}
	m.Replier, m.Drafter, m.Asker, m.Filer = t.said, t.said, t.said, t.said
	go func() {
		m.Seed(ctx)
		m.Born = true
		defer done()
		t.done <- m.Run(ctx)
	}()
	return t, nil
}

// Say is their owner writing text at at, as the feed hands it on. It returns
// once the mind takes it, when everything said before is answered, with what
// identifies it to Said.
func (t *Talk) Say(ctx context.Context, text string, at time.Time) (ulid.ULID, error) {
	row := memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatOwner, Kind: memory.ChatMessageKind, Text: text, At: at}
	if err := t.st.SaveChat(ctx, row); err != nil {
		return ulid.ULID{}, err
	}
	p := perception.Percept{ID: ulid.Make(), Source: memory.ChatSource, ExternalID: row.ID, Thread: memory.ChatSource,
		Speaker: t.r.row.speaker(), SpeakerName: t.r.row.ownerName, Kind: perceived[row.Kind], Text: text, At: at}
	select {
	case t.r.percepts <- p:
		return p.ID, nil
	case <-ctx.Done():
		return ulid.ULID{}, ctx.Err()
	}
}

// Tick is the clock reaching at. It returns once the mind takes it: when the
// tick before is done.
func (t *Talk) Tick(ctx context.Context, at time.Time) error {
	select {
	case t.r.ticks <- at:
		return nil
	case err := <-t.done:
		t.done <- err // for End
		return fmt.Errorf("the mind stopped: %v", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Said is what they said to a message, in order; complete once it's answered.
func (t *Talk) Said(message ulid.ULID) []string {
	t.said.mu.Lock()
	defer t.said.mu.Unlock()
	return t.said.to[message]
}

// FirstWords is when the reply to a message first showed, as a draft; zero if
// it never did.
func (t *Talk) FirstWords(message ulid.ULID) time.Time {
	t.said.mu.Lock()
	defer t.said.mu.Unlock()
	return t.said.first[message]
}

// End returns once everything said is answered and the mind has stopped.
func (t *Talk) End() error {
	close(t.r.percepts)
	return <-t.done
}
