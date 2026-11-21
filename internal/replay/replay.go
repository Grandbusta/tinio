// Package replay re-asks System 1 about cases whose right answers are known,
// with a question set as it is now, so a change (to the set, its thresholds or
// the model) can be checked before it is trusted. The cases are written for
// it (evals/, Load) or are messages owners labelled (FromStore). It never
// changes anything.
package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/store"
)

// Case is what System 1 is given, as the mind gives it, and the right answer.
type Case struct {
	State map[string]any `json:"state"`
	// Fill is the names put in the questions' {agent} and {speaker}, as the
	// mind does.
	Fill  map[string]string `json:"fill,omitempty"`
	Label string            `json:"label"` // a choice, or "true" / "false"
	Note  string            `json:"note,omitempty"`
	// Then is what was answered when it was first asked: a labelled message's.
	Then *decision.Answer `json:"-"`
}

// Load reads cases, one JSON object a line; blank lines and lines starting
// with # are skipped.
func Load(r io.Reader) ([]Case, error) {
	var cases []Case
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if c.Label == "" || len(c.State) == 0 {
			return nil, fmt.Errorf("line %d: a case needs a state and a label", n)
		}
		cases = append(cases, c)
	}
	return cases, sc.Err()
}

type Store interface {
	LabelledExamples(ctx context.Context, setID, question string, limit int) ([]store.Example, error)
}

// FromStore is the newest messages an owner labelled for set's primary
// question, as cases: the message is the whole state, as for incoming_message.
func FromStore(ctx context.Context, st Store, set questions.Set, limit int) ([]Case, error) {
	examples, err := st.LabelledExamples(ctx, set.ID, set.Primary, limit)
	if err != nil {
		return nil, err
	}
	cases := make([]Case, len(examples))
	for i, e := range examples {
		cases[i] = Case{State: map[string]any{"message": e.Text}, Label: e.Label, Then: &e.Answer}
	}
	return cases, nil
}

type Row struct {
	Case
	Now     string // the answer now: a choice, "true" / "false", or a level (1 to 10, to one place)
	NowConf float64
	Right   bool
	Latency time.Duration
	Failed  error
}

// scoreSlack is how far a level may be from the right one and count as right:
// demanding teachers differ by one.
const scoreSlack = 1.0

type Report struct {
	Set      string // and version
	Question string
	Provider string // the model that answered, as its provider names it
	Act      float64
	Rows     []Row
	Suggest  float64 // 0 if there's too little to go on
	CostUSD  float64
	Skipped  int // not asked: the cost cap was reached
}

const MinExamples = 10

// Target is the share of answers at or above the act threshold that should be right.
const Target = 0.95

// Options bound a run: Parallel asks at a time (1 if 0), and past MaxCostUSD
// (0: no cap) no more are asked; what's being asked then (Parallel-1 at most)
// still finishes.
type Options struct {
	Parallel   int
	MaxCostUSD float64
}

// Run asks s1 about every case, with set's questions, and scores the answers
// to question (set's primary if "", else its first question).
func Run(ctx context.Context, s1 system1.DecisionModel, set questions.Set, question string, cases []Case, o Options) (Report, error) {
	if question == "" {
		question = set.Primary
	}
	if question == "" && len(set.Questions) > 0 {
		question = set.Questions[0].Name
	}
	i := slices.IndexFunc(set.Questions, func(q system1.Question) bool { return q.Name == question })
	if i < 0 {
		return Report{}, fmt.Errorf("replay: %s has no question %q", set.Ref(), question)
	}
	scored := set.Questions[i].Type == decision.QuestionScore
	r := Report{Set: set.Ref(), Question: question, Act: set.Thresholds.Act, Rows: make([]Row, len(cases))}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		next = make(chan int)
		// free has a token for each worker free, its last case's cost counted, so
		// the cap is checked against everything finished.
		free = make(chan struct{}, max(o.Parallel, 1))
	)
	for range max(o.Parallel, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				row := Row{Case: cases[i]}
				start := time.Now()
				resp, err := s1.Decide(ctx, system1.Request{State: cases[i].State, Questions: filled(set.Questions, cases[i].Fill)})
				row.Latency = time.Since(start)
				if err != nil {
					row.Failed = err
				} else {
					row.Now, row.NowConf = value(resp.Answers[question])
					row.Right = right(row.Now, row.Label, scored)
				}
				mu.Lock()
				r.Rows[i] = row
				r.CostUSD += resp.CostUSD
				if r.Provider == "" {
					r.Provider = resp.Provider
				}
				mu.Unlock()
				<-free
			}
		}()
	}
	asked := 0
	for i := range cases {
		free <- struct{}{}
		mu.Lock()
		over := o.MaxCostUSD > 0 && r.CostUSD >= o.MaxCostUSD
		mu.Unlock()
		if over || ctx.Err() != nil {
			break
		}
		next <- i
		asked++
	}
	close(next)
	wg.Wait()
	r.Rows, r.Skipped = r.Rows[:asked], len(cases)-asked
	r.Suggest = suggest(r.Rows)
	return r, nil
}

// filled is qs with fill's names in place of {agent} and the like.
func filled(qs []system1.Question, fill map[string]string) []system1.Question {
	if len(fill) == 0 {
		return qs
	}
	var pairs []string
	for k, v := range fill {
		pairs = append(pairs, "{"+k+"}", v)
	}
	put := strings.NewReplacer(pairs...).Replace
	out := slices.Clone(qs)
	for i := range out {
		out[i].Instructions = put(out[i].Instructions)
		out[i].Choices = slices.Clone(out[i].Choices)
		for j := range out[i].Choices {
			out[i].Choices[j].Description = put(out[i].Choices[j].Description)
		}
	}
	return out
}

// right is whether ans is label: for a score, a level within scoreSlack of it.
func right(ans, label string, scored bool) bool {
	if !scored {
		return ans == label
	}
	a, err1 := strconv.ParseFloat(ans, 64)
	l, err2 := strconv.ParseFloat(label, 64)
	return err1 == nil && err2 == nil && math.Abs(a-l) <= scoreSlack
}

func value(a decision.Answer) (string, float64) {
	switch {
	case a.Refused:
		return "refused", 0
	case a.Type == decision.QuestionScore:
		return strconv.FormatFloat(math.Round((a.Score+1)*10)/10, 'f', -1, 64), a.Confidence // level index 0-9 → 1-10, as the mind does
	case a.Type == decision.QuestionPredicate:
		if a.Probability >= 0.5 {
			return "true", a.Probability
		}
		return "false", 1 - a.Probability
	}
	return a.Choice, a.Confidence
}

// suggest is the lowest act threshold (in steps of 0.05) at which at least
// Target of the answers at or above it are right. 0 with fewer than
// MinExamples answers.
func suggest(rows []Row) float64 {
	var ok []Row
	for _, r := range rows {
		if r.Failed == nil {
			ok = append(ok, r)
		}
	}
	if len(ok) < MinExamples {
		return 0
	}
	for t := 0.50; t <= 0.99; t += 0.05 {
		acted, right := at(ok, t)
		if acted > 0 && float64(right)/float64(acted) >= Target {
			return float64(int(t*100+0.5)) / 100
		}
	}
	return 0.99
}

// at is how many answers now are at or above t, and how many of those are right.
func at(rows []Row, t float64) (acted, right int) {
	for _, r := range rows {
		if r.Failed == nil && r.NowConf >= t {
			acted++
			if r.Right {
				right++
			}
		}
	}
	return acted, right
}

// Summary is the run in numbers.
type Summary struct {
	Asked, Failed, Right int
	Acted, ActedRight    int // at or above the act threshold
	P50, P95             time.Duration
	// Scored answers were levels, Off away from the right ones in all.
	Scored int
	Off    float64
}

func (r Report) Summary() Summary {
	var s Summary
	var lat []time.Duration
	for _, row := range r.Rows {
		s.Asked++
		if row.Failed != nil {
			s.Failed++
			continue
		}
		lat = append(lat, row.Latency)
		if row.Right {
			s.Right++
		}
		if a, err := strconv.ParseFloat(row.Now, 64); err == nil {
			if l, err := strconv.ParseFloat(row.Label, 64); err == nil {
				s.Off += math.Abs(a - l)
				s.Scored++
			}
		}
	}
	s.Acted, s.ActedRight = at(r.Rows, r.Act)
	if len(lat) > 0 {
		slices.Sort(lat)
		s.P50, s.P95 = lat[len(lat)/2], lat[min(len(lat)-1, len(lat)*95/100)]
	}
	return s
}

// Write reports the run: every case if cases, then what it adds up to.
func (r Report) Write(w io.Writer, cases bool) {
	if len(r.Rows) == 0 {
		fmt.Fprintf(w, "No cases for %s.\n", r.Set)
		return
	}
	if cases {
		r.writeCases(w)
	}
	r.writeSummary(w)
}

func (r Report) writeCases(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CASE\tRIGHT\tTHEN\tNOW\t")
	mark := func(right bool, ans string, conf float64) string {
		if right {
			return fmt.Sprintf("✓ %s %.2f", ans, conf)
		}
		return fmt.Sprintf("✗ %s %.2f", ans, conf)
	}
	for _, row := range r.Rows {
		then := ""
		if row.Then != nil {
			a, c := value(*row.Then)
			then = mark(a == row.Label, a, c)
		}
		now := mark(row.Right, row.Now, row.NowConf)
		if row.Failed != nil {
			now = "failed: " + short(row.Failed.Error())
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t\n", short(caseText(row.Case)), row.Label, then, now)
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func (r Report) writeSummary(w io.Writer) {
	s := r.Summary()
	ok := s.Asked - s.Failed
	fmt.Fprintf(w, "%s, %s, by %s: %d of %d right", r.Set, r.Question, r.Provider, s.Right, ok)
	if s.Failed > 0 {
		fmt.Fprintf(w, " (%d failed to ask)", s.Failed)
	}
	if r.Skipped > 0 {
		fmt.Fprintf(w, " (%d not asked: the cost cap)", r.Skipped)
	}
	if s.Scored > 0 {
		fmt.Fprintf(w, " (within %.0f level; %.1f off on average)", scoreSlack, s.Off/float64(s.Scored))
	}
	fmt.Fprintf(w, ". $%.4f; %s typical, %s at the 95th percentile.\n", r.CostUSD, s.P50.Round(time.Millisecond), s.P95.Round(time.Millisecond))
	if ok > 0 {
		fmt.Fprintf(w, "At act %.2f it decides %d of %d alone (%.0f%%), %d of them right (%s).\n", r.Act, s.Acted, ok,
			100*float64(s.Acted)/float64(ok), s.ActedRight, percent(s.ActedRight, s.Acted))
	}
	switch {
	case r.Suggest == 0:
		fmt.Fprintf(w, "Too few cases to suggest a threshold (%d; need %d).\n", ok, MinExamples)
	case r.Suggest == r.Act:
		fmt.Fprintln(w, "The act threshold looks right.")
	case r.Suggest >= 0.99:
		fmt.Fprintf(w, "No threshold makes %.0f%% of its confident answers right: this model isn't good enough at it.\n", 100*Target)
	default:
		a, rt := at(r.Rows, r.Suggest)
		fmt.Fprintf(w, "Suggested act threshold: %.2f (it would decide %d alone, %d right).\n", r.Suggest, a, rt)
	}
}

func percent(n, of int) string {
	if of == 0 {
		return "none"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(of))
}

// caseText is the case's state as one line: its text, or its fields.
func caseText(c Case) string {
	if len(c.State) == 1 {
		for _, v := range c.State {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	b, _ := json.Marshal(c.State)
	return string(b)
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 56 {
		return string(r[:55]) + "…"
	}
	return s
}
