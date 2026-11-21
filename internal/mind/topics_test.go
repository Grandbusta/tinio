package mind

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system1"
)

var testTopicSort = questions.Set{
	ID: "topic", Version: 1, Primary: "starts",
	Questions: []system1.Question{
		{Name: "continues", Type: decision.QuestionPredicate, Instructions: "Does `message` carry on with {topic}?"},
		{Name: "starts", Type: decision.QuestionPredicate, Instructions: "Does `message` start one?"},
	},
	Thresholds: questions.Thresholds{Act: 0.5, Ask: 0.5},
}

func topicMind(s1 *fakeSystem1, s2 *fakeSystem2) (*Mind, *fakeStore, *fakeReplier) {
	r := &fakeReplier{}
	m, s := newMind(r, s1, s2)
	m.TopicSort, m.TopicPrompt = testTopicSort, Prompt{Ref: "topic@test", Text: "You are {name}, keeping notes with {person}."}
	return m, s, r
}

// A message in a topic is answered from the topic: its exchanges from days
// ago, word for word, its summary and what was settled, and the newest
// exchange overall. Other open topics are named, and no more. The reply is
// said in the topic too.
func TestARepliesFromItsTopic(t *testing.T) {
	s1 := &fakeSystem1{choice: "question", confidence: 0.95, inTopic: map[string]float64{"gift": 0.9}}
	s2 := &fakeSystem2{}
	m, s, r := topicMind(s1, s2)
	days := now.Add(-48 * time.Hour)
	s.topics = []memory.Topic{{ID: "gift", Person: sam, Title: "Gift guide", Summary: "Posts from 1 December.",
		Settled: []string{"Six posts, not ten"}, StartedAt: days, LastAt: days},
		{ID: "menu", Person: sam, Title: "Winter menu", Summary: "Into Mandarin by Friday.", StartedAt: days, LastAt: days}}
	s.exchanges = []memory.Exchange{
		{At: days, Message: "A gift guide this year?", Reply: "Built around who it's for.", Topic: "gift"},
		{At: now.Add(-10 * time.Minute), Message: "The kettle gave up again.", Reply: "Third time this month."},
	}

	m.handle(context.Background(), percept("1", "Which tin goes first?"))

	if got := s.percepts[0].Topic; got != "gift" {
		t.Fatalf("the message is in %q, want gift", got)
	}
	if len(r.topics) != 1 || r.topics[0] != "gift" {
		t.Fatalf("the reply was said in %v, want gift", r.topics)
	}
	req := s2.requests[len(s2.requests)-1]
	var said []string
	for _, turn := range req.History {
		said = append(said, turn.Text)
	}
	if !strings.Contains(strings.Join(said, "|"), "A gift guide this year?") || !strings.Contains(strings.Join(said, "|"), "The kettle gave up again.") {
		t.Fatalf("the reply wasn't written from the topic and the newest exchange: %q", said)
	}
	for _, want := range []string{"Gift guide", "Posts from 1 December.", "Six posts, not ten", `"Winter menu"`} {
		if !strings.Contains(req.Note, want) {
			t.Fatalf("the note doesn't say %q:\n%s", want, req.Note)
		}
	}
	if strings.Contains(req.Note, "Mandarin") {
		t.Fatalf("another topic's notes reached the reply:\n%s", req.Note)
	}
}

// Something new is started as a topic, and the reply names it.
func TestAMessageStartsATopicTheReplyNames(t *testing.T) {
	s1 := &fakeSystem1{choice: "question", confidence: 0.95, starts: 0.9}
	s2 := &fakeSystem2{topic: "Spring newsletter"}
	m, s, _ := topicMind(s1, s2)

	m.handle(context.Background(), percept("1", "I want a first newsletter out in March."))

	if len(s.topics) != 1 || s.topics[0].Title != "Spring newsletter" || s.topics[0].Person != sam {
		t.Fatalf("topics: %+v", s.topics)
	}
	if s.percepts[0].Topic != s.topics[0].ID {
		t.Fatalf("the message is in %q, not the topic it started", s.percepts[0].Topic)
	}
	if note := s2.requests[0].Note; !strings.Contains(note, "name it in `topic`") {
		t.Fatalf("the reply wasn't asked to name it:\n%s", note)
	}
}

// One person's topic never reaches another's reply: not when it's chosen for
// their message, and not among the topics their message is sorted into.
func TestOnePersonsTopicNeverReachesAnother(t *testing.T) {
	s1 := &fakeSystem1{choice: "question", confidence: 0.95, inTopic: map[string]float64{"ada": 0.99}}
	s2 := &fakeSystem2{}
	m, s, _ := topicMind(s1, s2)
	s.topics = []memory.Topic{{ID: "ada", Person: "test:ada", Title: "Ada's surgery", Summary: "Booked for the 12th.", LastAt: now}}

	chosen := percept("1", "How is it going?")
	chosen.Topic = "ada"
	m.handle(context.Background(), chosen)
	m.handle(context.Background(), percept("2", "Any news?"))

	for _, p := range s.percepts {
		if p.Topic != "" {
			t.Fatalf("%q was put in %q", p.Text, p.Topic)
		}
	}
	for _, req := range s2.requests {
		if strings.Contains(req.Note, "surgery") || strings.Contains(req.Note, "12th") {
			t.Fatalf("Ada's topic reached Sam's reply:\n%s", req.Note)
		}
	}
	for _, call := range s1.topicCalls {
		if strings.Contains(strings.Join(call.State.(map[string]any)["topics"].([]string), "|"), "surgery") {
			t.Fatalf("Ada's topic was offered for Sam's message")
		}
	}
}

// A topic quiet for months is still brought up again by what it was about:
// the old ones closest to the message are offered with the recent ones, and
// the one it's sorted into is open again. One about something else isn't offered.
func TestAnOldTopicIsBroughtUpAgain(t *testing.T) {
	s1 := &fakeSystem1{choice: "question", confidence: 0.95, inTopic: map[string]float64{"song": 0.9}}
	m, s, _ := topicMind(s1, &fakeSystem2{})
	m.Embedder = wordEmbedder{}
	months := now.Add(-90 * 24 * time.Hour)
	s.topics = []memory.Topic{
		{ID: "song", Person: sam, Title: "Wedding song", Summary: "A song for Ada's wedding; the second verse is left.", Quiet: true, StartedAt: months, LastAt: months},
		{ID: "money", Person: sam, Title: "Trip budget", Summary: "Money for the summer trip.", Quiet: true, StartedAt: months, LastAt: months},
	}

	m.handle(context.Background(), percept("1", "Can we pick the song back up?"))

	offered := strings.Join(s1.topicCalls[0].State.(map[string]any)["topics"].([]string), "|")
	if !strings.Contains(offered, "Wedding song") || strings.Contains(offered, "Trip budget") {
		t.Fatalf("offered: %s", offered)
	}
	if s.percepts[0].Topic != "song" || s.topics[0].Quiet {
		t.Fatalf("the message is in %q, and the topic is quiet: %v", s.percepts[0].Topic, s.topics[0].Quiet)
	}
	if s.topics[0].EmbeddedWith != "fake/words" {
		t.Fatal("the old topic's embedding wasn't kept")
	}
}

// Once the talk in a topic pauses, the life loop sums it up: what came
// before its newest exchanges is folded into the summary, and what was
// settled is kept. A finished one goes quiet.
func TestATopicIsSummedUpOnceTheTalkPauses(t *testing.T) {
	s2 := &fakeSystem2{formats: map[string]string{
		"topic": `{"title": "", "summary": "Six posts from 1 December.", "status": "The first post waits for a yes.", "settled": ["Six posts, not ten"], "done": false}`,
	}}
	m, s, _ := topicMind(&fakeSystem1{}, s2)
	start := now.Add(-time.Hour)
	for i := range 8 {
		s.exchanges = append(s.exchanges, memory.Exchange{At: start.Add(time.Duration(i) * time.Minute), Message: "more", Reply: "yes", Topic: "gift"})
	}
	last := s.exchanges[7].At
	moved := last.Add(time.Minute) // a message moved into it: its last word, after anything said
	s.topics = []memory.Topic{{ID: "gift", Person: sam, Title: "Gift guide", StartedAt: start, LastAt: moved}}

	m.sumTopics(context.Background(), moved.Add(time.Minute))
	if len(s2.requests) != 0 {
		t.Fatal("it summed up a topic still being talked in")
	}
	m.sumTopics(context.Background(), moved.Add(sumAfter))
	m.sumTopics(context.Background(), moved.Add(sumAfter+time.Minute))
	if len(s2.requests) != 1 {
		t.Fatalf("summed up %d times, want once", len(s2.requests))
	}
	got := s.topics[0]
	if got.Summary != "Six posts from 1 December." || got.Status == "" || len(got.Settled) != 1 || got.Title != "Gift guide" {
		t.Fatalf("summed up as %+v", got)
	}
	if !got.SummedAt.Equal(moved) || !got.FoldedAt.Equal(s.exchanges[1].At) {
		t.Fatalf("summed to %v and folded to %v; want %v and the exchange before the newest %d", got.SummedAt, got.FoldedAt, moved, topicTail)
	}
	if got.Quiet {
		t.Fatal("an unfinished topic went quiet")
	}

	s2.formats["topic"] = `{"title": "", "summary": "Six posts, all sent.", "status": "Done.", "done": true,
		"settled": ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "Six posts, not ten"]}`
	s.topics[0].LastAt = moved.Add(time.Minute)
	s.exchanges = append(s.exchanges, memory.Exchange{At: moved.Add(time.Minute), Message: "All sent, thanks", Reply: "Glad it went out.", Topic: "gift"})
	m.sumTopics(context.Background(), moved.Add(time.Hour))
	if !s.topics[0].Quiet {
		t.Fatal("a finished topic stayed open")
	}
	if got := s.topics[0].Settled; len(got) != settledAtMost || got[len(got)-1] != "Six posts, not ten" {
		t.Fatalf("settled kept: %q, want the newest %d", got, settledAtMost)
	}
}

func (s *fakeStore) Topics(_ context.Context, person string, quietSince time.Time, limit int) ([]memory.Topic, error) {
	return keep(s.topics, limit, func(t memory.Topic) bool {
		return t.Person == person && (!t.Quiet || !t.LastAt.Before(quietSince))
	}), nil
}

func (s *fakeStore) OldTopics(_ context.Context, person string, before time.Time, limit int) ([]memory.Topic, error) {
	return keep(s.topics, limit, func(t memory.Topic) bool { return t.Person == person && t.Quiet && t.LastAt.Before(before) }), nil
}

func (s *fakeStore) SaveTopicEmbedding(_ context.Context, id string, v []float32, model string) error {
	for i := range s.topics {
		if s.topics[i].ID == id {
			s.topics[i].Embedding, s.topics[i].EmbeddedWith = v, model
		}
	}
	return nil
}

func (s *fakeStore) Topic(_ context.Context, id string) (memory.Topic, bool, error) {
	t, ok := find(s.topics, func(t memory.Topic) bool { return t.ID == id })
	return t, ok, nil
}

func (s *fakeStore) StartTopic(_ context.Context, t memory.Topic) error {
	t.LastAt = t.StartedAt
	s.topics = append(s.topics, t)
	return nil
}

func (s *fakeStore) SetTopic(_ context.Context, percept ulid.ULID, topic string, at time.Time) error {
	for i := range s.percepts {
		if s.percepts[i].ID == percept {
			s.percepts[i].Topic = topic
		}
	}
	for i := range s.topics {
		if s.topics[i].ID == topic && at.After(s.topics[i].LastAt) {
			s.topics[i].LastAt, s.topics[i].Quiet = at, false
		}
	}
	return nil
}

func (s *fakeStore) NameTopic(_ context.Context, id, title string) error {
	for i := range s.topics {
		if s.topics[i].ID == id && s.topics[i].Title == "" {
			s.topics[i].Title = title
		}
	}
	return nil
}

// TopicExchanges is the exchanges kept with that topic, newest limit, oldest first.
func (s *fakeStore) TopicExchanges(_ context.Context, topic string, after time.Time, _ ulid.ULID, limit int) ([]memory.Exchange, error) {
	in := keep(s.exchanges, -1, func(e memory.Exchange) bool { return e.Topic == topic && e.At.After(after) })
	return in[max(0, len(in)-limit):], nil
}

func (s *fakeStore) TopicsToSum(_ context.Context, before time.Time, limit int) ([]memory.Topic, error) {
	return keep(s.topics, limit, func(t memory.Topic) bool { return t.LastAt.After(t.SummedAt) && !t.LastAt.After(before) }), nil
}

func (s *fakeStore) SumTopic(_ context.Context, t memory.Topic) error {
	i := slices.IndexFunc(s.topics, func(x memory.Topic) bool { return x.ID == t.ID })
	t.Quiet = t.Quiet && !s.topics[i].LastAt.After(t.SummedAt)
	if s.topics[i].Title != "" {
		t.Title = s.topics[i].Title
	}
	t.LastAt = s.topics[i].LastAt
	s.topics[i] = t
	return nil
}

func (s *fakeStore) QuietTopics(_ context.Context, before time.Time) error {
	for i := range s.topics {
		if s.topics[i].LastAt.Before(before) {
			s.topics[i].Quiet = true
		}
	}
	return nil
}

func (s *fakeStore) PerceptTopic(_ context.Context, id ulid.ULID) (string, error) {
	p, _ := find(s.percepts, func(p perception.Percept) bool { return p.ID == id })
	return p.Topic, nil
}
