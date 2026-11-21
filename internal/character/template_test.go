package character

import (
	"os"
	"strings"
	"testing"
)

func TestCharacters(t *testing.T) {
	ts, err := Templates(os.DirFS("../.."), "characters")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 6 {
		t.Errorf("%d characters; want 6", len(ts))
	}
	for _, tmpl := range ts {
		for _, rel := range tmpl.Relations {
			for _, account := range []string{"telegram:42", ""} {
				files, err := tmpl.Render(Owner{Name: "Sam", Relation: rel, Account: account, Timezone: "Africa/Lagos"})
				if err != nil {
					t.Fatalf("%s as %s: %v", tmpl.Key, rel, err)
				}
				for name, raw := range files {
					text := string(raw)
					for _, p := range []string{"{owner}", "{relation}", "{account}", "{circle}", "{timezone}"} {
						if strings.Contains(text, p) {
							t.Errorf("%s/%s: %s is left", tmpl.Key, name, p)
						}
					}
					if strings.Contains(text, "—") {
						t.Errorf("%s/%s: an em dash", tmpl.Key, name)
					}
				}
				l, err := Parse(files["character.yaml"])
				if err != nil {
					t.Fatalf("%s as %s: %v", tmpl.Key, rel, err)
				}
				if l.Name != tmpl.Name || l.Timezone != "Africa/Lagos" || len(l.Dreams) == 0 || len(l.Skills) == 0 || len(l.Makes) == 0 {
					t.Errorf("%s: life = %+v", tmpl.Key, l)
				}
				if j, err := ParseJudgment(files["judgment.yaml"]); err != nil || len(j.Principles) < 3 || len(j.Cases) < 3 ||
					len(j.Priorities) < 3 || len(j.Boundaries) < 1 {
					t.Errorf("%s: judgment = %d principles, %d cases, %v; want a real start", tmpl.Key, len(j.Principles), len(j.Cases), err)
				} else if !strings.Contains(string(files["judgment.yaml"]), "Sam") {
					t.Errorf("%s: judgment doesn't name its owner", tmpl.Key)
				}
				if d := l.Day; d.PlanAt == "" || d.JournalAt == "" || d.ReviewAt == "" || len(d.CheckIns) == 0 {
					t.Errorf("%s: day = %+v; want all of it", tmpl.Key, d)
				}
				owner := l.Family[0]
				if owner.Name != "Sam" || owner.Relation != rel || owner.Account != account || owner.Circle != CircleOf(rel) {
					t.Errorf("%s as %s: owner = %+v", tmpl.Key, rel, owner)
				}
				if !strings.Contains(strings.Join(l.Story, "\n"), "Sam is "+l.Name+"'s "+rel+".") {
					t.Errorf("%s: the story doesn't say who Sam is", tmpl.Key)
				}
			}
		}
	}
}

func TestRenderRefuses(t *testing.T) {
	tmpl, err := LoadTemplate(os.DirFS("../.."), "characters/zara")
	if err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]Owner{
		"a quote":        {Name: `Sam "the man"`, Relation: "boss"},
		"bad account":    {Name: "Sam", Relation: "boss", Account: "telegram: 42"},
		"bad time zone":  {Name: "Sam", Relation: "boss", Timezone: "Lagos"},
		"not a relation": {Name: "Sam", Relation: "father"},
	} {
		if _, err := tmpl.Render(o); err == nil {
			t.Errorf("%s: rendered", name)
		}
	}
}
