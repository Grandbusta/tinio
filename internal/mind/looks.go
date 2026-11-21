package mind

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// Looks: in a reply to its owner, it may read with its tools (Wikipedia, its
// connections: the owner's calendar, a file, an issue) before
// it writes, rather than promise to look and answer later. System 2 asks for up to lookAtOnce
// reads, they run at once, each within lookFor, and it's asked again with
// what they returned, at most lookRounds times. What it read is material,
// never instructions: anything it then proposes to change asks the owner.

const (
	lookRounds = 2
	lookAtOnce = 3
	lookFor    = 25 * time.Second
)

// lookable is the reading tools a reply to person may use, only for its
// owner: the web, Wikipedia and its connections, not what carries a project
// on over days (a book).
func (m *Mind) lookable(person memory.Person) map[string]ability.Ability {
	if person.Key == "" || person.Key != m.policy().Owner {
		return nil
	}
	out := map[string]ability.Ability{}
	for _, a := range m.Abilities.Planned() {
		if _, ongoing := a.(ability.Continuer); a.Risk() != ability.Private && !ongoing && m.allowed(a) {
			out[a.Name()] = a
		}
	}
	return out
}

func (m *Mind) looksHelp(person memory.Person, n names) string {
	tools := m.lookable(person)
	if len(tools) == 0 {
		return "- You have no tools to look at before you reply: leave `look` empty."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Tools you can read with before you answer %s, now, in this reply:", n.speaker)
	for _, a := range m.Abilities.Planned() {
		if _, ok := tools[a.Name()]; ok {
			fmt.Fprintf(&b, "\n  - %s: %s", a.Name(), a.Describe())
		}
	}
	fmt.Fprintf(&b, "\n- When answering needs what only one of those can tell you (their calendar, a file, an article) and the note "+
		"doesn't have it yet, put it in `look`: the tool's name and its query, at most %d, and leave `reply` and every other field "+
		"empty. You're then asked again with what they returned, and write your reply from it, as if you'd checked yourself. "+
		"Don't promise to look later what you can look at now.", lookAtOnce)
	return b.String()
}

// look reads with what System 2 asked for, all at once: the note's account of
// it, and what reading cost; last says it can't look again.
func (m *Mind) look(ctx context.Context, p perception.Percept, person memory.Person, looks []system2.Look, last bool) (note string, cost float64) {
	tools := m.lookable(person)
	looks = looks[:min(len(looks), lookAtOnce)]
	found := make([]string, len(looks))
	costs := make([]float64, len(looks))
	var wg sync.WaitGroup
	for i, l := range looks {
		a, ok := tools[l.Tool]
		if !ok {
			found[i] = fmt.Sprintf("\n\nThere's no tool called %s to look with.", l.Tool)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.survive("looking with "+l.Tool, nil)
			ctx, cancel := context.WithTimeout(ctx, lookFor)
			defer cancel()
			mat, err := a.Prepare(ctx, ability.Act{Intention: memory.Intention{Args: map[string]string{"query": l.Query}}, Now: time.Now(), Zone: m.Zone})
			if err != nil {
				m.Log.Warn("a look failed", "percept", p.ID.String(), "tool", l.Tool, "err", err)
				found[i] = fmt.Sprintf("\n\nLooking with %s failed: %s. Say so plainly if it matters.", l.Tool, err)
				return
			}
			found[i], costs[i] = mat.Note, mat.CostUSD
		}()
	}
	wg.Wait()
	then := "\n\nIf you must, you can look once more; otherwise write your reply now."
	if last {
		then = "\n\nYou can't look again: write your reply now from what you have, and leave `look` empty."
	}
	for _, c := range costs {
		cost += c
	}
	return "\n\nWhat you just looked at, to answer this message:" + strings.Join(found, "") + then, cost
}
