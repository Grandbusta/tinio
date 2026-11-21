package store

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
)

// A topic's messages are its exchanges, however old; once the talk pauses
// it's due to be summed up, and summing it up keeps what was settled. Two
// topics merge only when they're with the same person.
func TestTopics(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	gift := memory.Topic{ID: ulid.Make().String(), Person: "account:ada", StartedAt: now.Add(-72 * time.Hour)}
	theirs := memory.Topic{ID: ulid.Make().String(), Person: "telegram:42", Title: "Someone else's", StartedAt: now}
	for _, tp := range []memory.Topic{gift, theirs} {
		if err := s.StartTopic(ctx, tp); err != nil {
			t.Fatal(err)
		}
	}
	hear := func(text string, at time.Time, topic string) perception.Percept {
		t.Helper()
		p := perception.Percept{ID: ulid.Make(), Source: memory.ChatSource, ExternalID: ulid.Make().String(), Thread: memory.ChatSource,
			Speaker: "account:ada", Kind: perception.KindMessage, Text: text, At: at, Topic: topic}
		if _, err := s.SavePercept(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := hear("A gift guide this year?", now.Add(-72*time.Hour), gift.ID)
	hear("The kettle broke.", now.Add(-time.Hour), "")
	latest := hear("Six posts, not ten.", now.Add(-10*time.Minute), "")
	if err := s.SetTopic(ctx, latest.ID, gift.ID, latest.At); err != nil {
		t.Fatal(err)
	}

	said, err := s.TopicExchanges(ctx, gift.ID, time.Time{}, ulid.ULID{}, 6)
	if err != nil || len(said) != 2 || said[0].Message != old.Text || said[1].Message != latest.Text {
		t.Fatalf("the topic's exchanges: %+v %v", said, err)
	}
	due, err := s.TopicsToSum(ctx, now.Add(-3*time.Minute), 5)
	if err != nil || len(due) != 1 || due[0].ID != gift.ID {
		t.Fatalf("due to sum up: %+v %v", due, err)
	}
	summed := due[0]
	summed.Title, summed.Summary, summed.Status, summed.Settled, summed.SummedAt = "Gift guide", "Six posts.", "Under way.", []string{"Six posts, not ten"}, latest.At
	if err := s.SumTopic(ctx, summed); err != nil {
		t.Fatal(err)
	}
	if due, err := s.TopicsToSum(ctx, now, 5); err != nil || len(due) != 1 || due[0].ID == gift.ID {
		t.Fatalf("summed up, but still due: %+v %v", due, err)
	}
	got, _, err := s.Topic(ctx, gift.ID)
	if err != nil || got.Title != "Gift guide" || len(got.Settled) != 1 || got.Quiet {
		t.Fatalf("after summing up: %+v %v", got, err)
	}

	if ok, err := s.MergeTopic(ctx, theirs.ID, gift.ID); err != nil || ok {
		t.Fatalf("someone else's topic merged into Ada's: %v %v", ok, err)
	}
	if _, ok, _ := s.Topic(ctx, theirs.ID); !ok {
		t.Fatal("a refused merge dropped the topic")
	}
	more := memory.Topic{ID: ulid.Make().String(), Person: "account:ada", Title: "Posts", StartedAt: now}
	if err := s.StartTopic(ctx, more); err != nil {
		t.Fatal(err)
	}
	hear("The first post goes Tuesday.", now.Add(-5*time.Minute), more.ID)
	if ok, err := s.MergeTopic(ctx, more.ID, gift.ID); err != nil || !ok {
		t.Fatalf("merging: %v %v", ok, err)
	}
	if said, err := s.TopicExchanges(ctx, gift.ID, time.Time{}, ulid.ULID{}, 6); err != nil || len(said) != 3 {
		t.Fatalf("after the merge, the topic's exchanges: %+v %v", said, err)
	}
	topics, err := s.Topics(ctx, "account:ada", now.Add(-time.Hour), 10)
	if err != nil || len(topics) != 1 || topics[0].ID != gift.ID {
		t.Fatalf("Ada's topics: %+v %v", topics, err)
	}

	// Quiet for months, it's still found with its embedding, until it's summed up again.
	if _, err := s.db.Exec(ctx, `UPDATE topics SET state = 'quiet', last_at = $3 WHERE agent_id = $1 AND id = $2`,
		s.agent, gift.ID, now.AddDate(0, -3, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTopicEmbedding(ctx, gift.ID, []float32{0.1, 0.2}, "test/model"); err != nil {
		t.Fatal(err)
	}
	quiet, err := s.OldTopics(ctx, "account:ada", now.AddDate(0, 0, -30), 10)
	if err != nil || len(quiet) != 1 || len(quiet[0].Embedding) != 2 || quiet[0].EmbeddedWith != "test/model" {
		t.Fatalf("old topics: %+v %v", quiet, err)
	}
	if err := s.SumTopic(ctx, quiet[0]); err != nil {
		t.Fatal(err)
	}
	if quiet, err := s.OldTopics(ctx, "account:ada", now.AddDate(0, 0, -30), 10); err != nil || len(quiet[0].Embedding) != 0 {
		t.Fatalf("summed up again, it kept the old embedding: %+v %v", quiet, err)
	}
}
