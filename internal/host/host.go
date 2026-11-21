// Package host runs many agents in one process. Each is their own mind (their
// store bound to them, their files, their policy) in its own goroutines; what
// they think with (the models, question sets, prompts and formats) is shared.
//
// An agent runs only while they have something to do: the worker claims them
// in agent_runs once they're due, with a lease it renews while it runs them,
// and lets them go when they rest (mind.Resting), to be claimed again when
// they're next due or their owner writes. What agents hear and say goes
// through the database (store.SaveChat), and what's new is told in the
// process (db.Notify), so nothing waits only in memory.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/connect"
	"github.com/Grandbusta/tinio/internal/folders"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/mind"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/store"
)

// Raised is the channel the API notifies, with the agent's id, when one is raised.
const Raised = "agent_raised"

// Changed is the channel the API notifies, with the agent's id, when their
// owner changes their character: one this worker runs rests and wakes at once
// to start with it (mind.Reload); one resting already starts with it.
const Changed = "agent_changed"

const (
	lease      = time.Minute // how long a claim holds without renewal
	renewEvery = 20 * time.Second
	claimEvery = 15 * time.Second // also how soon a lapsed lease is taken up, after the lease itself
	pokeEvery  = 5 * time.Minute  // what a missed message notification costs at most
)

type Host struct {
	pool   *db.DB
	shared shared
	log    *slog.Logger
	id     string // this worker, in agent_runs
	// Max is the most agents it holds at once; the rest wait for another worker.
	Max int
	// RestAfter is how long after they last heard something an idle agent goes
	// to rest; 0: never.
	RestAfter time.Duration
	// Days: they live their own day, as the app's Today shows.
	Days bool
	// Connections, if set, gives each agent the tools of the accounts their
	// owner connected and gave them; http is how they reach those services.
	Connections *connect.Store
	// Folders, if set, gives each agent the folders on this computer the
	// owner gave them.
	Folders *folders.Store
	http    *http.Client
	every   time.Duration // how often each agent ticks: a minute

	// starting lets so many agents start (load, and seed the first time) at
	// once; a crowd starting together would crowd out everyone running.
	starting chan struct{}
	// claimSoon has Run claim now rather than at the next pass: an agent was
	// raised, written to while no one holds them, or put back in line.
	claimSoon chan struct{}

	mu     sync.Mutex
	agents map[string]*runner // those this worker holds
}

// runner is one agent while this worker holds them. Their channels outlive a
// restart of their mind, so nothing waiting in them is lost.
type runner struct {
	row      agentRow
	percepts chan perception.Percept
	ticks    chan time.Time
	wake     chan struct{}             // their owner wrote: look for what they haven't heard
	fed      chan struct{}             // closed once the feed has handed over what was waiting when they woke
	hurry    chan struct{}             // their owner wrote while they waited to start: they go ahead
	paused   atomic.Bool               // panicked too often: held, but not run until the worker restarts
	mind     atomic.Pointer[mind.Mind] // the one running now; nil while they start
	stop     context.CancelFunc
}

type agentRow struct {
	id, account, ownerName string
	born                   bool   // seeded at an earlier start, as claimed
	ready                  bool   // raised from one of Tinio's ready characters, whose judgment comes approved
	kept                   []byte // mind.Kept, as JSON: what they kept when they last rested
	ownerWaiting           bool   // claimed because their owner wrote: they start ahead of the rest
	pace                   int    // 0: 1
	quietFrom, quietUntil  string // HH:MM; both "": no quiet hours
}

// speaker is who their owner is to their mind: their policy's owner, and
// the person in their character with this account.
func (a agentRow) speaker() string { return "account:" + a.account }

// New reads the question sets and prompts in files once, for every agent.
func New(pool *db.DB, files fs.FS, models Models, log *slog.Logger) (*Host, error) {
	sh, err := loadShared(files, models)
	if err != nil {
		return nil, err
	}
	return &Host{pool: pool, shared: sh, log: log, id: ulid.Make().String(), Max: 20000, RestAfter: 5 * time.Minute,
		http:  &http.Client{Timeout: time.Minute},
		every: time.Minute, starting: make(chan struct{}, 32), claimSoon: make(chan struct{}, 1), agents: map[string]*runner{}}, nil
}

// Run runs agents no other worker holds, up to Max, until ctx is done, then
// lets them go for other workers at once. It stops them all and returns an
// error if it can't renew their leases before they lapse, since another worker
// may then claim them.
func (h *Host) Run(ctx context.Context) error {
	// Deferred in this order: the agents are told to stop, waited for, and only
	// then let go, so no one else runs them while they're still running here.
	defer h.letGo()
	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.listen(ctx)
	}()
	claim, renew, poke := time.NewTicker(claimEvery), time.NewTicker(renewEvery), time.NewTicker(pokeEvery)
	defer claim.Stop()
	defer renew.Stop()
	defer poke.Stop()
	claimNow := func() {
		if err := h.claim(ctx, &wg); err != nil && ctx.Err() == nil {
			h.log.Warn("host: can't claim agents", "err", err)
		}
	}
	claimNow()
	renewed := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-h.claimSoon:
			claimNow()
		case <-claim.C:
			claimNow()
		case <-renew.C:
			if err := h.renew(ctx); err == nil {
				renewed = time.Now()
			} else if ctx.Err() != nil {
				return nil
			} else if time.Since(renewed) > lease-renewEvery {
				return fmt.Errorf("host: couldn't renew leases before they lapse: %w", err)
			} else {
				h.log.Warn("host: can't renew leases", "err", err)
			}
		case <-poke.C:
			h.mu.Lock()
			for _, r := range h.agents {
				r.poke()
			}
			h.mu.Unlock()
		}
	}
}

// ownersPerClaim is the most agents whose owner is waiting it claims at once,
// past Max; it may hold a quarter more than Max for them.
const ownersPerClaim = 1000

// claim takes free agents: first those whose owner is waiting on them, even
// past Max, then those who are due (or whose worker's lease lapsed), soonest
// first, as many as there's room for. Never one another worker holds, and
// never one another worker is claiming this moment.
func (h *Host) claim(ctx context.Context, wg *sync.WaitGroup) error {
	h.mu.Lock()
	held := len(h.agents)
	h.mu.Unlock()
	owners, err := h.claimWhere(ctx, wg, `worker IS NULL AND wake_at = `+store.WaitingOwner, min(h.Max+h.Max/4-held, ownersPerClaim))
	if err != nil {
		return err
	}
	_, err = h.claimWhere(ctx, wg, `(worker IS NULL AND wake_at <= now()) OR lease_until < now()`, h.Max-held-owners)
	return err
}

// claimWhere claims up to limit agents matching which, soonest first, and starts
// them; how many it claimed.
func (h *Host) claimWhere(ctx context.Context, wg *sync.WaitGroup, which string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT r.agent_id, a.account_id, a.owner_name, a.pace, a.quiet_from, a.quiet_until, r.born_at IS NOT NULL, r.kept,
			r.wake_at = `+store.WaitingOwner+`, EXISTS (SELECT 1 FROM characters c WHERE c.key = a.character_key AND c.account_id IS NULL)
		FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE (`+which+`) AND a.paused_at IS NULL -- not paused by their owner
		ORDER BY r.wake_at LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	claimed, err := db.CollectRows(rows, func(r db.Scanner) (agentRow, error) {
		var a agentRow
		return a, r.Scan(&a.id, &a.account, &a.ownerName, &a.pace, &a.quietFrom, &a.quietUntil, &a.born, &a.kept, &a.ownerWaiting, &a.ready)
	})
	if err != nil {
		return 0, err
	}
	for _, a := range claimed {
		if _, err := tx.Exec(ctx, `UPDATE agent_runs SET worker = $1, lease_until = at_plus(now(), $2) WHERE agent_id = $3`,
			h.id, lease.Seconds(), a.id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	for _, a := range claimed {
		h.start(ctx, wg, a)
	}
	return len(claimed), nil
}

func (h *Host) start(ctx context.Context, wg *sync.WaitGroup, a agentRow) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.agents[a.id]; ok {
		return // still ours, from a lease that lapsed and was claimed again
	}
	ctx, stop := context.WithCancel(ctx)
	r := &runner{row: a, percepts: make(chan perception.Percept, 64), ticks: make(chan time.Time), wake: make(chan struct{}, 1),
		fed: make(chan struct{}), hurry: make(chan struct{}, 1), stop: stop}
	r.poke() // anything they were sent while no one ran them
	h.agents[a.id] = r
	var running sync.WaitGroup
	running.Add(3)
	wg.Add(1)
	go func() {
		defer running.Done()
		select { // so their first tick never finds them idle with a message on its way
		case <-r.fed:
		case <-ctx.Done():
			return
		}
		clock(ctx, r.ticks, h.every, offset(a.id, h.every))
	}()
	go func() {
		defer running.Done()
		h.feed(ctx, r)
	}()
	born := a.born // only this goroutine reads or writes it
	go func() {
		defer running.Done()
		h.keep(ctx, r, func(ctx context.Context) error {
			var done func()
			m, err := func() (*mind.Mind, error) {
				// One whose owner is waiting doesn't queue behind a crowd that's only
				// due, whether they were claimed for it or their owner wrote while
				// they queued; claims already bound how many of them there are.
				if !a.ownerWaiting {
					select {
					case h.starting <- struct{}{}:
						defer func() { <-h.starting }()
					case <-r.hurry:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				m, end, err := h.mind(ctx, r)
				if err != nil {
					return nil, err
				}
				done = end
				if !born && m.Seed(ctx) {
					if _, err := h.pool.Exec(ctx, `UPDATE agent_runs SET born_at = now() WHERE agent_id = $1`, a.id); err == nil {
						born = true
					}
				}
				m.Born = true // seeded now, or before; one that failed is tried again at their next start
				return m, nil
			}()
			if err != nil {
				return err
			}
			defer done()
			r.mind.Store(m)
			defer r.mind.Store(nil)
			err = m.Run(ctx)
			if rest := (*mind.Resting)(nil); errors.As(err, &rest) {
				h.rest(ctx, r, rest)
			}
			return err
		})
	}()
	go func() {
		defer wg.Done()
		running.Wait()
		h.mu.Lock()
		if h.agents[a.id] == r {
			delete(h.agents, a.id)
		}
		h.mu.Unlock()
	}()
}

// renew extends the lease of every agent this worker holds, and stops any whose
// lease another worker has taken (one that lapsed while this one couldn't
// reach the database). A row still marked as this worker's but no longer
// running here (one it couldn't let rest) lapses, for anyone to claim.
func (h *Host) renew(ctx context.Context) error {
	h.mu.Lock()
	held := slices.Collect(maps.Keys(h.agents))
	h.mu.Unlock()
	rows, err := h.pool.Query(ctx, `UPDATE agent_runs SET lease_until = at_plus(now(), $2)
		WHERE worker = $1 AND agent_id IN (SELECT value FROM json_each($3)) RETURNING agent_id`, h.id, lease.Seconds(), held)
	if err != nil {
		return err
	}
	ids, err := db.CollectRows(rows, func(r db.Scanner) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
	if err != nil {
		return err
	}
	mine := map[string]bool{}
	for _, id := range ids {
		mine[id] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, r := range h.agents {
		if !mine[id] {
			h.log.Warn("host: another worker holds them now; stopping them here", "agent", id)
			r.stop()
		}
	}
	return nil
}

// rest lets r go until they're next due, and stops them here. It claims again
// at once, in case resting put them straight back in line.
func (h *Host) rest(ctx context.Context, r *runner, rest *mind.Resting) {
	defer h.claimNow()
	defer r.stop()
	kept, err := json.Marshal(rest.Kept)
	if err == nil {
		_, err = store.New(h.pool).For(r.row.id).Rest(ctx, h.id, rest.Until, kept)
	}
	if err != nil {
		h.log.Warn("host: couldn't let them rest; they're free once their lease lapses", "agent", r.row.id, "err", err)
	}
}

// letGo frees every agent this worker holds, for other workers to claim at
// once rather than when their leases lapse.
func (h *Host) letGo() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := h.pool.Exec(ctx, `UPDATE agent_runs SET worker = NULL, lease_until = NULL WHERE worker = $1`, h.id); err != nil {
		h.log.Warn("host: couldn't let agents go; they're free once their leases lapse", "err", err)
	}
}

// listen claims at once when an agent is raised, and wakes an agent when their
// owner writes. If it falls behind, it wakes everyone it holds and claims, for
// what it missed, and listens again; meanwhile the claims and pokes on the
// clock still do both.
func (h *Host) listen(ctx context.Context) {
	for again := false; ctx.Err() == nil; again = true {
		h.listenOnce(ctx, again)
	}
}

// listenOnce listens until it falls behind or ctx is done. again: this isn't
// the first time.
func (h *Host) listenOnce(ctx context.Context, again bool) {
	raised, stopRaised := h.pool.Listen(Raised, 64)
	defer stopRaised()
	changed, stopChanged := h.pool.Listen(Changed, 64)
	defer stopChanged()
	written, stopWritten := h.pool.Listen(store.ToAgent, 1024)
	defer stopWritten()
	if again {
		h.mu.Lock()
		for _, r := range h.agents {
			r.poke()
		}
		h.mu.Unlock()
		h.claimNow()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-raised:
			if !ok {
				return
			}
			h.claimNow()
		case agent, ok := <-changed:
			if !ok {
				return
			}
			h.mu.Lock()
			r, held := h.agents[agent]
			h.mu.Unlock()
			if held {
				if m := r.mind.Load(); m != nil { // nil: starting, so with the new character anyway
					m.Reload()
					r.tickNow() // now, not at the next minute: a folder just given is asked about at once
				}
			}
		case payload, ok := <-written:
			if !ok {
				return
			}
			agent, _, _ := strings.Cut(payload, ":")
			h.mu.Lock()
			r, held := h.agents[agent]
			h.mu.Unlock()
			if held {
				r.poke()
				select {
				case r.hurry <- struct{}{}:
				default:
				}
				continue
			}
			h.claimNow() // resting: claiming wakes them
		}
	}
}

func (h *Host) claimNow() {
	select {
	case h.claimSoon <- struct{}{}:
	default:
	}
}

// tickNow ticks r's mind at once, if it isn't busy with one already.
func (r *runner) tickNow() {
	select {
	case r.ticks <- time.Now():
	default:
	}
}

func (r *runner) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// perceived is the percept kind each kind of owner row is: a tap carries the
// payload Telegram's buttons do (perception.Answer, perception.Pick).
var perceived = map[string]string{
	memory.ChatMessageKind: perception.KindMessage,
	memory.ChatApprove:     perception.KindApproval,
	memory.ChatPick:        perception.KindCall,
	memory.ChatHello:       perception.KindHello,
}

// feed hands r's mind what their owner wrote that they haven't heard, each
// time they're woken. A message is unheard until the mind saves it as a
// percept, so one waiting when a host stopped is heard on the next; handed
// keeps one from being handed twice meanwhile (and the mind skips any that is).
func (h *Host) feed(ctx context.Context, r *runner) {
	st := store.New(h.pool).For(r.row.id)
	handed := map[string]bool{}
	fed := sync.OnceFunc(func() { close(r.fed) }) // after the first pass, whatever came of it
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		}
		unheard, err := st.Unheard(ctx, cap(r.percepts))
		if err != nil {
			h.log.Warn("host: can't load what their owner wrote", "agent", r.row.id, "err", err)
			fed()
			continue
		}
		waiting := map[string]bool{}
		for _, m := range unheard {
			waiting[m.ID] = true
			if handed[m.ID] {
				continue
			}
			p := perception.Percept{ID: ulid.Make(), Source: memory.ChatSource, ExternalID: m.ID, Thread: memory.ChatSource,
				Speaker: r.row.speaker(), SpeakerName: r.row.ownerName, Kind: perceived[m.Kind], Text: m.Text, Payload: m.Payload, At: m.At,
				Topic: m.Topic}
			if m.ReplyTo != "" {
				if q, ok, err := st.ChatMessage(ctx, m.ReplyTo); err != nil {
					h.log.Warn("host: can't load the message replied to; it's heard without it", "agent", r.row.id, "err", err)
				} else if ok {
					p.Quote = &perception.Quote{ID: q.ID, Agent: q.Author == memory.ChatAgent, Text: q.Text}
				}
			}
			select {
			case r.percepts <- p:
				handed[m.ID] = true
			case <-ctx.Done():
				return
			}
		}
		maps.DeleteFunc(handed, func(id string, _ bool) bool { return !waiting[id] }) // heard now
		fed()
	}
}

const (
	firstWait   = time.Second
	longestWait = 5 * time.Minute
	panicsMax   = 3 // within panicSpan, and the agent is paused
	panicSpan   = 10 * time.Minute
)

// keep runs an agent until ctx is done, starting them again with growing
// waits if they stop or fail. A panic the mind didn't catch would end the
// process for every agent in it, so it's caught here, and an agent that keeps
// panicking is paused for someone to look at.
func (h *Host) keep(ctx context.Context, r *runner, run func(context.Context) error) {
	log := h.log.With("agent", r.row.id)
	var panics []time.Time
	wait := firstWait
	for {
		started := time.Now()
		panicked, err := runSafely(ctx, run)
		if ctx.Err() != nil {
			return
		}
		if panicked {
			panics = append(panics, time.Now())
			for len(panics) > 0 && time.Since(panics[0]) > panicSpan {
				panics = panics[1:]
			}
			if len(panics) >= panicsMax {
				r.paused.Store(true)
				log.Error("host: agent paused after panicking again and again", "panics", len(panics), "err", err)
				return
			}
		}
		if time.Since(started) > panicSpan {
			wait = firstWait // they ran a good while: this is a new failure, not the same one
		}
		log.Error("host: agent stopped; starting them again", "err", err, "panicked", panicked, "in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, longestWait)
	}
}

func runSafely(ctx context.Context, run func(context.Context) error) (panicked bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			panicked, err = true, fmt.Errorf("panicked: %v\n%s", r, debug.Stack())
		}
	}()
	if err := run(ctx); err != nil {
		return false, err
	}
	return false, errors.New("stopped by itself")
}

// offset is how far into each tick's period an agent's tick comes, the same
// every time, so a worker's agents don't all think at once.
func offset(agent string, every time.Duration) time.Duration {
	h := fnv.New32a()
	h.Write([]byte(agent))
	return time.Duration(h.Sum32()) * time.Millisecond % every
}

// clock ticks at once, since an agent is woken because something is due, then
// every period (a minute), offset into it. Each later tick is the minute it was
// due, not when it arrived: something due at 17:50 counts as due at the 17:50
// tick, however late into the minute that comes.
func clock(ctx context.Context, ticks chan<- time.Time, every, offset time.Duration) {
	select {
	case ticks <- time.Now():
	case <-ctx.Done():
		return
	}
	for {
		now := time.Now()
		next := now.Add(-offset).Truncate(every).Add(every)
		select {
		case <-ctx.Done():
			return
		case <-time.After(next.Add(offset).Sub(now)):
			select {
			case ticks <- next:
			case <-ctx.Done():
				return
			}
		}
	}
}
