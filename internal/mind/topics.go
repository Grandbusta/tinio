package mind

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// A reply is written from its topic's newest exchanges, word for word, and
// the chat's newest whatever they're about; older ones are in the topic's
// summary, which the life loop writes once the talk pauses.
const (
	topicTail    = 6
	overallTail  = 2
	topicsToSort = 10                  // open or lately quiet topics a message is sorted among
	quietReach   = 30 * 24 * time.Hour // a quiet topic this recent is always among them
	// Older ones are among them only if they're about what's said, however
	// old: the closest few of the newest oldScanned, by embedding. Only
	// offered: System 1 still decides. With text-embedding-3-small (21 Nov),
	// "can we do the gift guide again?" was 0.44 and 0.54 alike to its
	// topic, and 0.23 to 0.33 to others.
	oldScanned       = 100
	oldOffered       = 3
	minOldSimilarity = 0.3
	quietAfter       = 7 * 24 * time.Hour // an open topic not talked in for this long goes quiet
	sumAfter         = 3 * time.Minute    // a pause this long and a topic is summed up
	sumAtMost        = 40                 // exchanges read to sum one up
	summaryAtMost    = 1500               // characters of a summary kept
	settledAtMost    = 10                 // lines of what's settled kept: the prompt folds older ones into the summary
	othersNamed      = 8                  // other open topics a reply is told the titles of
	sumAgainAfter    = 30 * time.Minute   // a topic that failed to be summed up waits this long
)

// topical is what a message is about: its topic (zero ID: none), and whether
// it still needs a name, which the reply gives it. open is the speaker's open
// topics, which the reply is told only the names of.
type topical struct {
	topic   memory.Topic
	unnamed bool
	open    []memory.Topic
}

// sortTopic finds p's topic while the message is read: the one the speaker
// chose, or System 1's pick among theirs; wait returns it, with the decision
// that sorted it (nil if none did). Off without a TopicSort set.
func (m *Mind) sortTopic(ctx context.Context, p perception.Percept, recent []memory.Exchange) (wait func() (topical, bool, *decision.Decision)) {
	none := func() (topical, bool, *decision.Decision) { return topical{}, false, nil }
	if m.TopicSort.ID == "" || p.Speaker == "" || p.Text == "" {
		return none
	}
	topics, err := m.Store.Topics(ctx, p.Speaker, p.At.Add(-quietReach), topicsToSort)
	if err != nil {
		m.Log.Warn("loading topics failed; the message is in none", "percept", p.ID.String(), "err", err)
		return none
	}
	var open []memory.Topic
	for _, t := range topics {
		if !t.Quiet {
			open = append(open, t)
		}
	}
	if p.Topic != "" {
		t, ok, err := m.Store.Topic(ctx, p.Topic)
		if err != nil {
			m.Log.Warn("loading the topic chosen failed; sorting the message", "percept", p.ID.String(), "err", err)
		}
		if ok && t.Person == p.Speaker {
			return func() (topical, bool, *decision.Decision) {
				return topical{topic: t, unnamed: t.Title == "", open: open}, false, nil
			}
		}
	}
	var old []memory.Topic
	if m.Embedder != nil {
		if old, err = m.Store.OldTopics(ctx, p.Speaker, p.At.Add(-quietReach), oldScanned); err != nil {
			m.Log.Warn("loading old topics failed; sorting among recent ones", "percept", p.ID.String(), "err", err)
		}
	}
	type sorted struct {
		about  topical
		starts bool
		d      *decision.Decision
	}
	done := make(chan sorted, 1)
	go func() {
		var s sorted
		defer func() { done <- s }()
		var err error
		defer m.survive("sorting a message into a topic", &err)
		s.about, s.starts, s.d = m.pickTopic(ctx, p, recent, topics, old)
	}()
	return func() (topical, bool, *decision.Decision) {
		s := <-done
		s.about.open = open
		return s.about, s.starts, s.d
	}
}

func (m *Mind) pickTopic(ctx context.Context, p perception.Percept, recent []memory.Exchange, topics, old []memory.Topic) (topical, bool, *decision.Decision) {
	start := time.Now()
	n := m.namesFor(p)
	d := newDecision(p, nil, start)
	d.Tier, d.SetID, d.Action = decision.TierSystem1, m.TopicSort.Ref(), "no_topic"
	if len(old) > 0 {
		ranked, out, err := rankAlike(ctx, m, p.Text, old, minOldSimilarity, oldOffered,
			func(t *memory.Topic) (*[]float32, string) { return &t.Embedding, t.EmbeddedWith },
			func(t memory.Topic) string { return t.Title + "\n\n" + t.Summary },
			func(t memory.Topic, model string) error {
				return m.Store.SaveTopicEmbedding(ctx, t.ID, t.Embedding, model)
			})
		if err != nil {
			m.Log.Warn("embedding failed; old topics aren't offered", "percept", p.ID.String(), "err", err)
		}
		d.CostUSD += out.CostUSD
		for _, r := range ranked {
			topics = append(topics, r.item)
		}
	}

	described := make([]string, len(topics))
	qs := make([]system1.Question, 0, len(topics)+1)
	for i, t := range topics {
		described[i] = topicLine(t)
		q := m.TopicSort.Questions[0]
		q.Name = "in:" + t.ID
		q.Instructions = strings.ReplaceAll(q.Instructions, "{topic}", fmt.Sprintf("`topics[%d]`", i))
		qs = append(qs, q)
	}
	qs = append(qs, m.TopicSort.Questions[1])
	state := map[string]any{"message": n.message(p.Text), "topics": described}
	if len(recent) > 0 {
		last := recent[len(recent)-1]
		state["previous"] = strings.TrimSpace(n.speaker + ": " + last.Message + "\n" + n.agent + ": " + last.Reply)
	}
	resp, err := m.System1.Decide(ctx, system1.Request{State: state, Questions: qs})
	d.Provider, d.Answers, d.CostUSD = resp.Provider, resp.Answers, d.CostUSD+resp.CostUSD
	d.Latency = time.Since(start)
	if err != nil {
		m.Log.Warn("sorting into a topic failed; the message is in none", "percept", p.ID.String(), "err", err)
		d.Action = "error"
		d.Outcome = &decision.Outcome{Status: decision.OutcomeFailed, Detail: err.Error(), At: time.Now().UTC()}
		return topical{}, false, d
	}
	best, likeliest := -1, 0.0
	for i, t := range topics {
		if a := resp.Answers["in:"+t.ID]; isYes(m.TopicSort, resp.Answers, "in:"+t.ID) && a.Probability > likeliest {
			best, likeliest = i, a.Probability
		}
	}
	if best >= 0 {
		d.Action = "topic"
		t := topics[best]
		return topical{topic: t, unnamed: t.Title == ""}, false, d
	}
	starts := isYes(m.TopicSort, resp.Answers, m.TopicSort.Questions[1].Name)
	if starts {
		d.Action = "new_topic"
	}
	return topical{}, starts, d
}

// topicLine is a topic as System 1 is given it.
func topicLine(t memory.Topic) string {
	line := orDefault(t.Title, "(not named yet)")
	if about := orDefault(t.Status, t.Summary); about != "" {
		line += ": " + cutRunes(about, 300)
	}
	if t.LastSaid != "" {
		line += fmt.Sprintf(" Last said: %q", cutRunes(t.LastSaid, 200))
	}
	return line
}

// placeTopic puts p in about's topic, or a new one if starts, and returns
// what it's about now. A message the speaker answers right after a question
// stays in the question's topic, whatever the sort said.
func (m *Mind) placeTopic(ctx context.Context, p *perception.Percept, about topical, starts, answering bool, recent []memory.Exchange) topical {
	if answering && len(recent) > 0 && p.Topic == "" {
		if id := recent[len(recent)-1].Topic; id != "" && id != about.topic.ID {
			if t, ok, err := m.Store.Topic(ctx, id); err == nil && ok {
				about.topic, about.unnamed, starts = t, t.Title == "", false
			}
		}
	}
	if about.topic.ID == "" && starts {
		t := memory.Topic{ID: ulid.Make().String(), Person: p.Speaker, StartedAt: p.At.UTC()}
		if err := m.Store.StartTopic(ctx, t); err != nil {
			m.Log.Error("starting a topic failed; the message is in none", "percept", p.ID.String(), "err", err)
			return topical{open: about.open}
		}
		about.topic, about.unnamed = t, true
	}
	if about.topic.ID == "" {
		if p.Topic != "" { // chosen, but not theirs or not there
			p.Topic = ""
			if err := m.Store.SetTopic(ctx, p.ID, "", p.At.UTC()); err != nil {
				m.Log.Error("clearing a message's topic failed", "percept", p.ID.String(), "err", err)
			}
		}
		return about
	}
	p.Topic = about.topic.ID
	if err := m.Store.SetTopic(ctx, p.ID, p.Topic, p.At.UTC()); err != nil {
		m.Log.Error("keeping a message's topic failed", "percept", p.ID.String(), "err", err)
	}
	return about
}

// inTopicOf puts p, which the agent says first, in the topic of the percept
// it keeps its word to (a reminder, a promise).
func (m *Mind) inTopicOf(ctx context.Context, p perception.Percept, of ulid.ULID) perception.Percept {
	if m.TopicSort.ID == "" {
		return p
	}
	t, err := m.Store.PerceptTopic(ctx, of)
	if err != nil {
		m.Log.Warn("finding what a promise was about failed; it's said in no topic", "percept", p.ID.String(), "err", err)
	}
	p.Topic = t
	return p
}

// talkedOf is the conversation a reply is written from: in a topic, its newest
// exchanges and the chat's very newest, in order; otherwise recent.
func (m *Mind) talkedOf(ctx context.Context, p perception.Percept, recent []memory.Exchange, about topical) []memory.Exchange {
	if about.topic.ID == "" {
		return recent
	}
	inTopic, err := m.Store.TopicExchanges(ctx, about.topic.ID, time.Time{}, p.ID, topicTail)
	if err != nil {
		m.Log.Warn("loading the topic's last exchanges failed; replying from the last few minutes", "percept", p.ID.String(), "err", err)
		return recent
	}
	talk := slices.Clone(inTopic)
	for _, e := range recent[max(0, len(recent)-overallTail):] {
		if !slices.ContainsFunc(talk, func(x memory.Exchange) bool { return x.At.Equal(e.At) && x.Message == e.Message }) {
			talk = append(talk, e)
		}
	}
	slices.SortStableFunc(talk, func(a, b memory.Exchange) int { return a.At.Compare(b.At) })
	return talk
}

// topicNote tells the reply what the message is about and what it replies
// to: it changes from message to message, so it goes in the note.
func (m *Mind) topicNote(p perception.Percept, about topical, n names) string {
	var b strings.Builder
	t := about.topic
	if t.ID != "" && t.Title != "" {
		fmt.Fprintf(&b, "\n\nYou and %s are talking about %q, one of the things you have going: the conversation you see is its "+
			"newest messages, and the very newest overall.", n.speaker, t.Title)
		if t.Summary != "" {
			fmt.Fprintf(&b, " What came before, in your own notes: %s", t.Summary)
		}
		if len(t.Settled) > 0 {
			b.WriteString("\nWhat you've settled in it, which still holds:")
			for _, s := range t.Settled {
				b.WriteString("\n- " + s)
			}
		}
	}
	var others []string
	for _, o := range about.open {
		if o.ID != t.ID && o.Title != "" && len(others) < othersNamed {
			others = append(others, strconv.Quote(o.Title))
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "\n\nOther things you and %s have going, which you know only by name here: %s. Don't bring them into this "+
			"reply unless %s does.", n.speaker, strings.Join(others, ", "), n.speaker)
	}
	if about.unnamed {
		fmt.Fprintf(&b, "\n\nThis message starts something you and %s will come back to: name it in `topic`, two to four words, "+
			"like the name of a folder. Otherwise leave `topic` empty.", n.speaker)
	}
	if q := p.Quote; q != nil && strings.TrimSpace(q.Text) != "" {
		whose := "of theirs"
		if q.Agent {
			whose = "of yours"
		}
		fmt.Fprintf(&b, "\n\n%s is replying to this message %s, so that's what they mean: %q", n.speaker, whose, cutRunes(q.Text, 1500))
	}
	return b.String()
}

// sumTopics is the life loop keeping topics: the ones gone silent go quiet,
// and one the talk has paused in is summed up. Off without a TopicPrompt.
func (m *Mind) sumTopics(ctx context.Context, now time.Time) {
	if m.TopicSort.ID == "" || m.TopicPrompt.Text == "" {
		return
	}
	if err := m.Store.QuietTopics(ctx, now.Add(-quietAfter)); err != nil {
		m.Log.Warn("quieting old topics failed", "err", err)
	}
	ts, err := m.Store.TopicsToSum(ctx, now.Add(-sumAfter), 5)
	if err != nil {
		m.Log.Warn("loading topics to sum up failed", "err", err)
		return
	}
	for _, t := range ts {
		if now.Before(m.sumLater[t.ID]) {
			continue
		}
		if m.sumTopic(ctx, t) {
			delete(m.sumLater, t.ID)
			return
		}
		if m.sumLater == nil {
			m.sumLater = map[string]time.Time{}
		}
		m.sumLater[t.ID] = now.Add(sumAgainAfter) // so one that keeps failing doesn't hold up the rest
		return
	}
}

// sumTopic folds what was said in t since it was last folded, but its newest
// exchanges, into its summary, and writes its status and what's settled from
// all of it, as of its last word (a move or a merge counts). False if it
// failed: summed_at stays, so it's tried again.
func (m *Mind) sumTopic(ctx context.Context, t memory.Topic) bool {
	log := m.Log.With("topic", t.ID)
	said, err := m.Store.TopicExchanges(ctx, t.ID, t.FoldedAt, ulid.ULID{}, sumAtMost)
	if err != nil {
		log.Warn("loading what was said in a topic failed", "err", err)
		return false
	}
	t.SummedAt = t.LastAt
	if len(said) == 0 {
		if err := m.Store.SumTopic(ctx, t); err != nil {
			log.Error("keeping a topic failed", "err", err)
			return false
		}
		return true
	}
	person := m.person(ctx, t.Person)
	name := orDefault(person.Name, fallbackName)
	var b strings.Builder
	fmt.Fprintf(&b, "The topic as you last left it:\n- title: %s\n- summary: %s\n- status: %s\n- settled:",
		orDefault(t.Title, "(none yet)"), orDefault(t.Summary, "(none yet)"), orDefault(t.Status, "(none yet)"))
	if len(t.Settled) == 0 {
		b.WriteString(" (nothing yet)")
	}
	for _, s := range t.Settled {
		b.WriteString("\n  - " + s)
	}
	b.WriteString("\n\nWhat was said in it since, oldest first:")
	for _, e := range said {
		at := e.At.In(m.Zone).Format("Mon 2 Jan 15:04")
		if e.Message != "" {
			fmt.Fprintf(&b, "\n[%s] %s: %s", at, name, cutRunes(e.Message, 1500))
		}
		if e.Reply != "" {
			fmt.Fprintf(&b, "\n[%s] You: %s", at, cutRunes(e.Reply, 1500))
		}
	}

	start := time.Now()
	d := &decision.Decision{ID: ulid.Make(), At: start.UTC(), Tier: decision.TierSystem2, SetID: m.TopicPrompt.Ref,
		Action: "sum_topic", Gate: decision.GateAuto}
	str := map[string]any{"type": "string"}
	resp, err := m.summer().Reason(ctx, system2.Request{
		Instructions: strings.ReplaceAll(m.lifeInstructions(m.TopicPrompt), "{person}", name),
		Note:         b.String(),
		Format: &system2.Format{Name: "topic", Schema: map[string]any{"type": "object", "additionalProperties": false,
			"required": []string{"title", "summary", "status", "settled", "done"},
			"properties": map[string]any{"title": str, "summary": str, "status": str,
				"settled": map[string]any{"type": "array", "items": str}, "done": map[string]any{"type": "boolean"}}}},
	})
	d.Provider, d.CostUSD = resp.Provider, resp.CostUSD
	if err != nil {
		m.finishLife(ctx, d, start, decision.OutcomeFailed, err.Error())
		return false
	}
	var out struct {
		Title, Summary, Status string
		Settled                []string
		Done                   bool
	}
	if err := json.Unmarshal([]byte(resp.Text), &out); err != nil || strings.TrimSpace(out.Summary) == "" {
		m.finishLife(ctx, d, start, decision.OutcomeFailed, "no summary that can be used")
		return false
	}
	if t.Title == "" {
		t.Title = cutRunes(strings.TrimSpace(out.Title), 60)
	}
	t.Summary = cutRunes(strings.TrimSpace(out.Summary), summaryAtMost)
	t.Status = cutRunes(strings.TrimSpace(out.Status), 200)
	t.Settled = nil
	for _, s := range out.Settled {
		if s = strings.TrimSpace(s); s != "" {
			t.Settled = append(t.Settled, cutRunes(s, 200))
		}
	}
	t.Settled = t.Settled[max(0, len(t.Settled)-settledAtMost):] // the newest, if it wrote more
	if len(said) > topicTail {                                   // the newest stay word for word
		t.FoldedAt = said[len(said)-topicTail-1].At
	}
	t.Quiet = out.Done
	if err := m.Store.SumTopic(ctx, t); err != nil {
		log.Error("keeping a topic failed", "err", err)
		m.finishLife(ctx, d, start, decision.OutcomeFailed, err.Error())
		return false
	}
	m.finishLife(ctx, d, start, decision.OutcomeOK, fmt.Sprintf("summed up %q from %d exchanges", t.Title, len(said)))
	return true
}

// summer sums topics up: the light model, which is enough for notes.
func (m *Mind) summer() system2.Reasoner {
	if m.LightSystem2 != nil {
		return m.LightSystem2
	}
	return m.System2
}
