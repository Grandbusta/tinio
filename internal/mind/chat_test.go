package mind

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
)

// In a chat that shows them, a reply carries its answers to tap and what it
// read to write it; in one that doesn't (Telegram), it's only its words.
func TestAReplyCarriesItsAnswersAndWhatItRead(t *testing.T) {
	for _, rich := range []bool{true, false} {
		r := &sayingReplier{}
		s2 := &fakeSystem2{answers: []string{"Simplified", "Traditional"},
			looks: [][]system2.Look{{{Tool: "read_article", Query: "Mandarin menus"}}}}
		m, s := newMind(r, &fakeSystem1{choice: "question", confidence: 0.95}, s2)
		m.RichChat = rich
		m.Autonomy.Owner = sam
		s.people[sam] = memory.Person{Key: sam, Name: "Sam", Circle: "family"}
		m.Abilities = ability.NewSet(&fakeAbility{name: "read_article", describe: "read an article", risk: ability.UsesTools, source: "Wikipedia: Chinese characters",
			found: map[string]string{"Mandarin menus": "Menus in mainland China use simplified characters."}})

		m.handle(context.Background(), percept("1", "Can you translate the menu into Mandarin?"))

		if !rich {
			if len(r.said) != 0 || len(r.sent) != 1 {
				t.Fatalf("plain chat: said %+v, sent %q", r.said, r.sent)
			}
			continue
		}
		if len(r.said) != 1 || !slices.Equal(r.said[0].Answers, []string{"Simplified", "Traditional"}) ||
			!slices.Equal(r.said[0].Read, []string{"Wikipedia: Chinese characters"}) {
			t.Fatalf("said %+v", r.said)
		}
	}
}

// Only the owner's thumb on a work counts, as their feedback on each skill
// that kind of work shows; down takes what was off with it.
func TestTheOwnersThumbOnAWorkCounts(t *testing.T) {
	m, s, _ := gatedMind(&fakeReplier{}, &fakeSystem2{})
	s.skills = []memory.Skill{{Name: "copywriting", Level: 5, ShownBy: []string{"caption"}}, {Name: "music", Level: 5, ShownBy: []string{"beat_idea"}}}
	w := addWork(s, "Gift 1: the travel tin", "caption", "For the commuter...", "", now)
	thumb := func(who string, good bool, note string) {
		p := from(who)
		p.Kind, p.Text = perception.KindReaction, ""
		p.Payload, _ = json.Marshal(perception.Reaction{Work: w.ID.String(), Good: good, Note: note})
		m.handle(context.Background(), p)
	}

	thumb(sam, true, "")
	if len(s.evidence) != 0 {
		t.Fatal("someone who isn't the owner moved a skill")
	}
	thumb(owner, false, "Too long for a caption")
	if len(s.evidence) != 1 || s.evidence[0].Skill != "copywriting" || s.evidence[0].Score != thumbDown ||
		s.evidence[0].Source != memory.EvidenceOwner || s.evidence[0].Note != "Gift 1: the travel tin: Too long for a caption" {
		t.Fatalf("evidence %+v", s.evidence)
	}
	if s.skills[1].Level != 5 {
		t.Fatal("a skill the work doesn't show moved")
	}
}
