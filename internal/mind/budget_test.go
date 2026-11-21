package mind

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/Grandbusta/tinio"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/memory"
)

var updateBudget = flag.Bool("update", false, "record the prompts' sizes as their budget")

const budgetFile = "testdata/prompt_budget.json"

// System 2's instructions are paid for on every call, so they're held to a
// budget in bytes and only change size on purpose: growing or shrinking, the
// test fails until the new size is recorded with
//
//	go test ./internal/mind -run TestPromptBudget -update
//
// They're measured as Mei (a ready character) has them, with a test mind's
// abilities, so the numbers move with the prompts and the help the mind adds.
func TestPromptBudget(t *testing.T) {
	read := func(name string) Prompt {
		raw, err := fs.ReadFile(tinio.Files, "prompts/"+name+".md")
		if err != nil {
			t.Fatal(err)
		}
		return Prompt{Ref: name, Text: string(raw)}
	}
	tpl, err := character.LoadTemplate(tinio.Files, "characters/zara")
	if err != nil {
		t.Fatal(err)
	}
	files, err := tpl.Render(character.Owner{Name: "Busta", Relation: tpl.Relations[0]})
	if err != nil {
		t.Fatal(err)
	}
	life, err := character.Parse(files["character.yaml"])
	if err != nil {
		t.Fatal(err)
	}
	m, s, _ := gatedMind(&fakeReplier{}, &fakeSystem2{})
	m.Life, m.ReplyPrompt = life, read("reply")
	ctx := context.Background()

	got := map[string]int{
		"reply to its owner":    len(m.instructions(ctx, s.people[owner], names{life.Name, "Busta"})),
		"reply to someone else": len(m.instructions(ctx, memory.Person{Key: sam, Relation: "a friend"}, names{life.Name, "Sam"})),
	}
	for _, name := range []string{"plan", "activity", "review", "learn", "data", "observe"} {
		got[name] = len(m.lifeInstructions(read(name)))
	}

	if *updateBudget {
		raw, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile(budgetFile, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(budgetFile)
	if err != nil {
		t.Fatal(err)
	}
	var budget map[string]int
	if err := json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		if b, ok := budget[name]; !ok || got[name] != b {
			t.Errorf("%s: %d bytes, budget %d. If that's meant, record it with -update", name, got[name], b)
		}
	}
	for name := range budget {
		if _, ok := got[name]; !ok {
			t.Errorf("%s is in the budget but not measured: record the budget again with -update", name)
		}
	}
}
