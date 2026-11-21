package host

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/internal/channel/web"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/store"
)

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TINIO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TINIO_TEST_DATABASE_URL not set")
	}
	t.Setenv("DATABASE_URL", url) // the shared config names it
	pool, err := config.NewDB(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// raise is what the API does: a new account's agent from a ready character,
// their files rendered for the owner, Ada, and the hosts told.
func raise(t *testing.T, pool *pgxpool.Pool, key string) string {
	t.Helper()
	ts, err := character.Templates(tinio.Files, "characters")
	if err != nil {
		t.Fatal(err)
	}
	var tpl character.Template
	for _, x := range ts {
		if x.Key == key {
			tpl = x
		}
	}
	account, id, now := ulid.Make().String(), ulid.Make().String(), time.Now().UTC()
	files, err := tpl.Render(character.Owner{Name: "Ada", Relation: tpl.Relations[0], Account: "account:" + account})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO accounts (id, email, terms_accepted_at, adult_at, created_at) VALUES ($1, $2, $3, $3, $3)`,
			account, strings.ToLower(account)+"@example.com", now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agents (id, account_id, character_key, name, owner_name, relation, pace, created_at)
			VALUES ($1, $2, $3, $4, 'Ada', $5, 1, $6)`, id, account, key, tpl.Name, tpl.Relations[0], now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.agent_id', $1, true)`, id); err != nil {
			return err
		}
		for _, f := range character.Files {
			if _, err := tx.Exec(ctx, `INSERT INTO agent_versions (agent_id, id, section, doc, source, at) VALUES ($1, $2, $3, $4, 'test', $5)`,
				id, ulid.Make().String(), strings.TrimSuffix(f, ".yaml"), string(files[f]), now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_runs (agent_id) VALUES ($1)`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, Raised, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// fakeSystem1 is unsure of everything, so every message goes to System 2.
type fakeSystem1 struct{}

func (fakeSystem1) Decide(_ context.Context, req system1.Request) (system1.Response, error) {
	answers := map[string]decision.Answer{}
	for _, q := range req.Questions {
		answers[q.Name] = decision.Answer{Type: q.Type, Refused: q.Type == decision.QuestionChoice}
	}
	return system1.Response{Provider: "fake1/1", Answers: answers}, nil
}

// fakeSystem2 replies with the name its instructions give it, so a reply
// shows whose mind wrote it.
type fakeSystem2 struct{}

func (fakeSystem2) Reason(_ context.Context, req system2.Request) (system2.Response, error) {
	if req.Format != nil {
		return system2.Response{Provider: "fake2/1", Text: "{}"}, nil
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(req.Instructions, "You are "), ",")
	return system2.Response{Provider: "fake2/1", Text: "I'm " + name + "."}, nil
}

func newHost(t *testing.T, pool *pgxpool.Pool) *Host {
	t.Helper()
	h, err := New(pool, tinio.Files, Models{System1: fakeSystem1{}, System2: fakeSystem2{}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// say is the owner writing to their agent, as the API will.
func say(t *testing.T, pool *pgxpool.Pool, agent, text string) {
	t.Helper()
	m := memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatOwner, Text: text, At: time.Now().UTC()}
	if err := store.New(pool).For(agent).SaveChat(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

// reply waits for what the agent says in their chat.
func reply(t *testing.T, pool *pgxpool.Pool, agent string) string {
	t.Helper()
	var said string
	waitUntil(t, "no reply in "+agent+"'s chat", func() bool {
		chat, err := store.New(pool).For(agent).Chat(context.Background(), "", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range chat {
			if m.Author == memory.ChatAgent {
				said = m.Text
			}
		}
		return said != ""
	})
	return said
}

func (h *Host) holds(agent string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.agents[agent]
	return ok
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
	}
}

func TestEachAgentAnswersAsThemselves(t *testing.T) {
	pool := openPool(t)
	h := newHost(t, pool)
	// Zara is one of Tinio's ready characters, so her judgment comes approved.
	if _, err := pool.Exec(context.Background(), `INSERT INTO characters (key, version, visibility, template, character, judgment, created_at)
		VALUES ('zara', 1, 'ready', '', '', '', now()) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	// Written while no one runs her: she hears it when a host starts.
	zara := raise(t, pool, "zara")
	say(t, pool, zara, "who are you?")

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error)
	go func() { stopped <- h.Run(ctx) }()
	if got := reply(t, pool, zara); got != "I'm Zara." {
		t.Errorf("Zara said %q", got)
	}
	// Raised and written to while the host runs: the notifications start him
	// and wake him, long before a sweep would.
	kenji := raise(t, pool, "kenji")
	waitUntil(t, "Kenji never joined", func() bool { return h.holds(kenji) })
	say(t, pool, kenji, "who are you?")
	if got := reply(t, pool, kenji); got != "I'm Kenji." {
		t.Errorf("Kenji said %q", got)
	}
	if unheard, err := store.New(pool).For(zara).Unheard(context.Background(), 10); err != nil || len(unheard) != 0 {
		t.Errorf("still unheard: %v %v", unheard, err)
	}
	var born bool
	if err := pool.QueryRow(context.Background(), `SELECT born_at IS NOT NULL FROM agent_runs WHERE agent_id = $1`, zara).Scan(&born); err != nil || !born {
		t.Errorf("Zara's seeding wasn't recorded: %v %v", born, err)
	}
	if ps, err := store.New(pool).For(zara).Principles(context.Background(), memory.JudgmentApproved); err != nil || len(ps) == 0 {
		t.Errorf("Zara's ready judgment isn't approved: %d, %v", len(ps), err)
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

func TestAnAgentRunsInOnePlace(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	first, second := newHost(t, pool), newHost(t, pool)
	zara := raise(t, pool, "zara")
	firstCtx, stopFirst := context.WithCancel(ctx)
	stopped := make(chan error)
	go func() { stopped <- first.Run(firstCtx) }()
	waitUntil(t, "Zara never started", func() bool { return first.holds(zara) })

	secondCtx, stopSecond := context.WithCancel(ctx)
	defer stopSecond()
	var running sync.WaitGroup
	if err := second.claim(secondCtx, &running); err != nil {
		t.Fatal(err)
	}
	if second.holds(zara) {
		t.Fatal("a second worker took Zara while the first held her")
	}

	// The first can't reach the database and her lease lapses: the second
	// takes her, and the first stops her when it next renews.
	if _, err := pool.Exec(ctx, `UPDATE agent_runs SET lease_until = now() - interval '1 second' WHERE agent_id = $1`, zara); err != nil {
		t.Fatal(err)
	}
	if err := second.claim(secondCtx, &running); err != nil || !second.holds(zara) {
		t.Fatalf("Zara wasn't taken up after her lease lapsed: %v", err)
	}
	if err := first.renew(firstCtx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the first still runs Zara", func() bool { return !first.holds(zara) })
	stopFirst()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}

	// A worker that stops lets its agents go at once.
	stopSecond()
	running.Wait()
	second.letGo()
	var free bool
	if err := pool.QueryRow(ctx, `SELECT worker IS NULL FROM agent_runs WHERE agent_id = $1`, zara).Scan(&free); err != nil || !free {
		t.Errorf("Zara wasn't let go: %v %v", free, err)
	}
}

func TestAnAgentThatKeepsPanickingIsPaused(t *testing.T) {
	h := &Host{log: slog.New(slog.NewTextHandler(io.Discard, nil)), agents: map[string]*runner{}}
	r := &runner{row: agentRow{id: "a"}, percepts: make(chan perception.Percept, 1)}
	h.agents["a"] = r
	runs := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.keep(context.Background(), r, func(context.Context) error {
			runs++
			panic("a bug in starting them")
		})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("still restarting a mind that always panics")
	}
	if runs != panicsMax || !r.paused.Load() {
		t.Errorf("ran %d times, paused %v", runs, r.paused.Load())
	}
}

func said(t *testing.T, pool *pgxpool.Pool, agent string) int {
	t.Helper()
	chat, err := store.New(pool).For(agent).Chat(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range chat {
		if m.Author == memory.ChatAgent {
			n++
		}
	}
	return n
}

// An idle agent rests, freeing their worker, and their owner writing wakes
// them to answer.
func TestAnAgentRestsAndWakesForTheirOwner(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	h := newHost(t, pool)
	h.every, h.RestAfter = 100*time.Millisecond, 300*time.Millisecond
	zara := raise(t, pool, "zara")
	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan error)
	go func() { stopped <- h.Run(runCtx) }()

	say(t, pool, zara, "who are you?")
	waitUntil(t, "no first reply", func() bool { return said(t, pool, zara) == 1 })
	waitUntil(t, "Zara never rested", func() bool {
		var resting bool
		if err := pool.QueryRow(ctx, `SELECT worker IS NULL AND wake_at > now() AND kept ? 'last_tick' FROM agent_runs WHERE agent_id = $1`,
			zara).Scan(&resting); err != nil {
			t.Fatal(err)
		}
		return resting && !h.holds(zara)
	})
	say(t, pool, zara, "are you there?")
	waitUntil(t, "resting, she never answered", func() bool { return said(t, pool, zara) == 2 })
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

// A message is never lost to going to rest: one saved just before wakes them
// at once, and so does one saved just after.
func TestAMessageAsTheyRestStillWakesThem(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	later := time.Now().Add(3 * time.Hour)
	hold := func(agent string) *store.Store {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE agent_runs SET worker = 'w1', lease_until = now() + interval '1 minute' WHERE agent_id = $1`, agent); err != nil {
			t.Fatal(err)
		}
		return store.New(pool).For(agent)
	}
	wakesAt := func(agent string) time.Time {
		t.Helper()
		var at time.Time
		if err := pool.QueryRow(ctx, `SELECT wake_at FROM agent_runs WHERE agent_id = $1`, agent).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	zara := raise(t, pool, "zara")
	st := hold(zara)
	say(t, pool, zara, "are you there?")
	if ok, err := st.Rest(ctx, "w1", later, []byte(`{}`)); err != nil || !ok {
		t.Fatalf("rest: %v %v", ok, err)
	}
	if wakesAt(zara).After(time.Now()) {
		t.Error("written to just before resting, Zara rests on")
	}

	kenji := raise(t, pool, "kenji")
	st = hold(kenji)
	if ok, err := st.Rest(ctx, "someone else", later, []byte(`{}`)); err != nil || ok {
		t.Fatalf("a worker that doesn't hold him let him rest: %v %v", ok, err)
	}
	if ok, err := st.Rest(ctx, "w1", later, []byte(`{}`)); err != nil || !ok || wakesAt(kenji).Before(later.Add(-time.Second)) {
		t.Fatalf("rest: %v %v, until %s", ok, err, wakesAt(kenji))
	}
	say(t, pool, kenji, "are you there?")
	if wakesAt(kenji).After(time.Now()) {
		t.Error("written to just after resting, Kenji rests on")
	}

	// Their owner waiting puts them ahead of anyone merely due, however long
	// that one has waited.
	sofia := raise(t, pool, "sofia")
	if _, err := pool.Exec(ctx, `UPDATE agent_runs SET wake_at = now() - interval '1 hour' WHERE agent_id = $1`, sofia); err != nil {
		t.Fatal(err)
	}
	if !wakesAt(kenji).Before(wakesAt(sofia)) {
		t.Errorf("Kenji, written to, wakes at %s, after Sofía, only due, at %s", wakesAt(kenji), wakesAt(sofia))
	}
}

// A worker that's full still takes an agent whose owner is waiting, but not
// one who is only due.
func TestAnOwnerWaitingIsClaimedPastTheCap(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	// Only this test's agents are due or waiting: the tests share one database,
	// and other packages' tests write to it meanwhile.
	holdBack := func(ours ...string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE agent_runs SET wake_at = now() + interval '1 day' WHERE worker IS NULL AND NOT agent_id = ANY($1)`,
			ours); err != nil {
			t.Fatal(err)
		}
	}
	h := newHost(t, pool)
	h.Max, h.RestAfter = 8, 0 // full once it holds eight (and two more for owners), and none of them rests
	var full []string
	for range 8 {
		full = append(full, raise(t, pool, "zara"))
	}
	holdBack(full...)
	runCtx, stop := context.WithCancel(ctx)
	var running sync.WaitGroup
	defer func() { stop(); running.Wait(); h.letGo() }()
	if err := h.claim(runCtx, &running); err != nil {
		t.Fatal(err)
	}
	waiting, due := raise(t, pool, "kenji"), raise(t, pool, "henrik")
	say(t, pool, waiting, "are you there?")
	holdBack(waiting, due)
	if err := h.claim(runCtx, &running); err != nil {
		t.Fatal(err)
	}
	if !h.holds(waiting) || h.holds(due) {
		t.Errorf("full, the worker holds Kenji (whose owner waits) %v, Henrik (only due) %v", h.holds(waiting), h.holds(due))
	}
}

// An agent is woken because something is due, so their first tick comes at
// once, not at the next minute.
func TestAWokenAgentTicksAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	go clock(ctx, ticks, time.Hour, 0)
	select {
	case <-ticks:
	case <-time.After(time.Second):
		t.Error("no tick on waking")
	}
}

// Every start slot taken, as by a crowd being born: an agent whose owner is
// waiting starts anyway.
func TestAnOwnerWaitingDoesntQueueToStart(t *testing.T) {
	pool := openPool(t)
	h := newHost(t, pool)
	for range cap(h.starting) {
		h.starting <- struct{}{}
	}
	zara := raise(t, pool, "zara")
	say(t, pool, zara, "who are you?")
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error)
	go func() { stopped <- h.Run(ctx) }()
	if got := reply(t, pool, zara); got != "I'm Zara." {
		t.Errorf("Zara said %q", got)
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

// Already claimed and waiting to start behind a crowd, an agent whose owner
// then writes goes ahead.
func TestAnAgentWrittenToWhileQueuedStartsAtOnce(t *testing.T) {
	pool := openPool(t)
	h := newHost(t, pool)
	for range cap(h.starting) {
		h.starting <- struct{}{}
	}
	zara := raise(t, pool, "zara")
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error)
	go func() { stopped <- h.Run(ctx) }()
	waitUntil(t, "Zara was never claimed", func() bool { return h.holds(zara) })
	say(t, pool, zara, "who are you?")
	if got := reply(t, pool, zara); got != "I'm Zara." {
		t.Errorf("Zara said %q", got)
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

// The owner's tap on their agent's request, in the chat, reaches the mind as a
// Telegram button's would: the request is decided, once, and settled in the chat.
func TestATapOnARequestIsDecidedAndSettled(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	h := newHost(t, pool)
	zara := raise(t, pool, "zara")
	st := store.New(pool).For(zara)
	a := memory.Approval{ID: ulid.Make(), Class: "dream", What: "add a dream", Text: "Zara wants to learn to swim.",
		Status: memory.ApprovalPending, AskedAt: time.Now().UTC()}
	if err := st.SaveApproval(ctx, a); err != nil {
		t.Fatal(err)
	}
	where, err := web.Channel{Store: st}.Ask(ctx, perception.Percept{}, memory.Request{Text: "Can I add a dream?"}, a.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkApprovalSent(ctx, a.ID, where, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var asked struct{ Message string }
	json.Unmarshal(where, &asked)
	tap, _ := json.Marshal(perception.Answer{Approval: a.ID.String(), Approve: false})
	if err := st.SaveChat(ctx, memory.ChatMessage{ID: ulid.Make().String(), Author: memory.ChatOwner, Kind: memory.ChatApprove,
		Payload: tap, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan error)
	go func() { stopped <- h.Run(runCtx) }()
	waitUntil(t, "the request was never settled", func() bool {
		m, ok, err := st.ChatMessage(ctx, asked.Message)
		return err == nil && ok && !m.SettledAt.IsZero()
	})
	if got, _, _ := st.Approval(ctx, a.ID); got.Status != memory.ApprovalDenied {
		t.Errorf("the request is %s; want denied", got.Status)
	}
	if m, _, _ := st.ChatMessage(ctx, asked.Message); !strings.Contains(m.Text, "You said no") {
		t.Errorf("settled as %q", m.Text)
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

// The worker listens again after losing its connection, and a message written
// while it wasn't listening is answered at once, not at the next poke.
func TestTheWorkerListensAgainAfterLosingItsConnection(t *testing.T) {
	pool := openPool(t)
	h := newHost(t, pool)
	zara := raise(t, pool, "zara")
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error)
	go func() { stopped <- h.Run(ctx) }()
	say(t, pool, zara, "hi")
	waitUntil(t, "no first reply", func() bool { return said(t, pool, zara) == 1 })

	var killed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query = $1) k`,
		listens).Scan(&killed); err != nil || killed == 0 {
		t.Fatalf("killed %d listening connections: %v", killed, err)
	}
	say(t, pool, zara, "still there?")
	waitUntil(t, "no reply after the worker lost its connection", func() bool { return said(t, pool, zara) == 2 })
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}

// A paused agent isn't claimed, even when their owner writes; their message
// waits, unheard, for when they're resumed.
func TestAPausedAgentIsntClaimed(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	h := newHost(t, pool)
	h.RestAfter = 0
	paused, other := raise(t, pool, "zara"), raise(t, pool, "kenji")
	if _, err := pool.Exec(ctx, `UPDATE agents SET paused_at = now() WHERE id = $1`, paused); err != nil {
		t.Fatal(err)
	}
	say(t, pool, paused, "are you there?")
	if _, err := pool.Exec(ctx, `UPDATE agent_runs SET wake_at = now() + interval '1 day' WHERE worker IS NULL AND NOT agent_id = ANY($1)`,
		[]string{paused, other}); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	var running sync.WaitGroup
	defer func() { stop(); running.Wait(); h.letGo() }()
	if err := h.claim(runCtx, &running); err != nil {
		t.Fatal(err)
	}
	if h.holds(paused) || !h.holds(other) {
		t.Errorf("claimed the paused agent %v, the other %v", h.holds(paused), h.holds(other))
	}
}
