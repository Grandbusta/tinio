package mind

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
)

// noArea marks a fact that was labelled but fits no life area, so it is not
// labelled again on every start.
const noArea = "other"

// lifeLabels: Personal is whether a message concerns the agent's or the
// speaker's own life; Broad, one of them in general ("Tell me about yourself").
type lifeLabels struct {
	Areas    []string // most likely first; never contains noArea
	Personal bool
	Broad    bool
}

// labelText needs text that already names people (see names) and a
// configured life_area set; check m.LifeArea.ID first.
func (m *Mind) labelText(ctx context.Context, text string, n names) (lifeLabels, system1.Response, error) {
	qs := slices.Clone(m.LifeArea.Questions)
	for i := range qs {
		qs[i].Instructions = n.fill(qs[i].Instructions)
	}
	resp, err := m.System1.Decide(ctx, system1.Request{State: map[string]any{"text": text}, Questions: qs})
	if err != nil {
		return lifeLabels{}, resp, err
	}
	l := lifeLabels{
		Personal: isYes(m.LifeArea, resp.Answers, "personal"),
		Broad:    isYes(m.LifeArea, resp.Answers, "broad"),
	}
	for _, a := range m.LifeArea.Labels(resp.Answers, "area") {
		if a != noArea {
			l.Areas = append(l.Areas, a)
		}
	}
	return l, resp, nil
}

// choices is the choices of set's question q; nil if it has none.
func choices(set questions.Set, q string) []system1.Choice {
	for _, x := range set.Questions {
		if x.Name == q {
			return x.Choices
		}
	}
	return nil
}

// factAreasHelp tells System 2 to file each fact it keeps by the parts of life
// life_area reads messages by, in its words, so recall finds them; "" with no
// life_area.
func (m *Mind) factAreasHelp() string {
	cs := choices(m.LifeArea, "area")
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Give each fact its `areas`: every part of life it's about, from these (%q only if none fits):", noArea)
	for _, c := range cs {
		if c.Value != noArea {
			fmt.Fprintf(&b, "\n  - %s: %s", c.Value, c.Description)
		}
	}
	return b.String() + "\n"
}

// filedAreas is what System 2 filed a fact under, if they're life_area's; nil
// if none is, so System 1 files it instead.
func (m *Mind) filedAreas(given []string) []string {
	cs := choices(m.LifeArea, "area")
	var out []string
	for _, a := range given {
		if a != noArea && !slices.Contains(out, a) && slices.ContainsFunc(cs, func(c system1.Choice) bool { return c.Value == a }) {
			out = append(out, a)
		}
	}
	if len(out) == 0 && len(cs) > 0 && slices.Contains(given, noArea) {
		return []string{noArea}
	}
	return out
}

func factAreas(l lifeLabels) []string {
	if len(l.Areas) == 0 {
		return []string{noArea}
	}
	return slices.Clone(l.Areas)
}

// saveFacts keeps facts, each with its Subject and Text, as learned from p:
// those without Areas labelled all at once, then all saved in order. A fact whose labelling fails is
// still saved, and labelled on the next start. It returns how many were saved
// and what labelling cost.
func (m *Mind) saveFacts(ctx context.Context, p perception.Percept, fs []memory.Fact, n names) (int, float64) {
	costs := make([]float64, len(fs))
	var wg sync.WaitGroup
	for i := range fs {
		fs[i].ID, fs[i].PerceptID, fs[i].At = ulid.Make(), p.ID, p.At
		if m.LifeArea.ID == "" || len(fs[i].Areas) > 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			defer m.survive("labelling a fact", &err)
			var l lifeLabels
			var resp system1.Response
			if l, resp, err = m.labelText(ctx, n.fact(fs[i]), n); err != nil {
				m.Log.Warn("labelling fact failed; saving it unlabelled", "percept", p.ID.String(), "err", err)
				return
			}
			fs[i].Areas, costs[i] = factAreas(l), resp.CostUSD
		}()
	}
	wg.Wait()
	saved, cost := 0, 0.0
	for i, f := range fs {
		cost += costs[i]
		if err := m.Store.SaveFact(ctx, f); err != nil {
			m.Log.Error("save fact", "percept", p.ID.String(), "err", err)
			continue
		}
		saved++
		m.Log.Info("remembered", "percept", p.ID.String(), "fact", f.ID.String(), "subject", f.Subject, "areas", f.Areas)
	}
	return saved, cost
}

// labelUnlabelledFacts labels a person's facts with fallbackName, since
// speakers' names are not known at start.
func (m *Mind) labelUnlabelledFacts(ctx context.Context) {
	if m.LifeArea.ID == "" {
		return
	}
	facts, err := m.Store.UnlabelledFacts(ctx, 100)
	if err != nil {
		m.Log.Warn("loading unlabelled facts failed", "err", err)
		return
	}
	n := names{agent: m.Life.Name, speaker: fallbackName}
	for _, f := range facts {
		l, _, err := m.labelText(ctx, n.fact(f), n)
		if err != nil {
			m.Log.Warn("labelling fact failed", "fact", f.ID.String(), "err", err)
			continue
		}
		if err := m.Store.SetFactAreas(ctx, f.ID, factAreas(l)); err != nil {
			m.Log.Warn("saving fact areas failed", "fact", f.ID.String(), "err", err)
			continue
		}
		m.Log.Info("labelled fact", "fact", f.ID.String(), "areas", factAreas(l))
	}
}
