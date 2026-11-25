// Package character loads who the agent is: their background, personality and
// what they make, written by whoever made them, and who their owner is to them.
package character

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Character is who the agent is, for their voice and know-how: a short
// background (Story) they never contradict, their personality, and what they
// make. They work for their owner; they have no life of their own.
type Character struct {
	Name        string   `yaml:"name"`
	Story       []string `yaml:"story"`
	Timezone    string   `yaml:"timezone"`
	Personality []string `yaml:"personality"`
	Makes       []Craft  `yaml:"makes"`
	// Hello is how they introduce themselves the first time they meet their
	// owner, in place of saying plainly that they're an AI; "" for that.
	Hello string `yaml:"hello"`
	// Skills and Family are copied into memory the first time; after that
	// memory wins. Family is who the agent knows from the start: their owner.
	Skills []Skill    `yaml:"skills"`
	Family []Relative `yaml:"family"`
}

type Craft struct {
	Kind  string `yaml:"kind"` // snake_case; works are filed under it
	About string `yaml:"about"`
	// Data is what each one carries beside its words, so it can be made into
	// a file from it, like "score" for music (sent as MIDI); "" if none.
	Data string `yaml:"data"`
}

var BuiltIn = []Craft{
	{Kind: "notes", About: "notes on something you're learning or thinking through"},
	{Kind: "writing", About: "a short piece of writing of any other kind"},
	{Kind: "table", About: "a table in rows and columns: a list, a comparison, a budget", Data: "table"},
	{Kind: "schedule", About: "a plan of dated events, to put in a calendar", Data: "events"},
}

// Reserved are kinds an ability sets itself (reading notes).
var Reserved = []string{"reading_notes"}

var kindName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (l Character) Crafts() []Craft { return append(append([]Craft{}, BuiltIn...), l.Makes...) }

// Carries is whether works of c carry data of kind (memory.Work.Carries).
func (c Craft) Carries(kind string) bool {
	return kind == "" || kind == c.Data
}

// Carrying is the kinds of work it makes that carry data of kind.
func (l Character) Carrying(kind string) []string {
	var kinds []string
	for _, c := range l.Crafts() {
		if c.Carries(kind) {
			kinds = append(kinds, c.Kind)
		}
	}
	return kinds
}

type Skill struct {
	Name    string   `yaml:"name"`
	Level   int      `yaml:"level"` // 1 (beginner) to 10 (master)
	ShownBy []string `yaml:"shown_by"`
}

type Relative struct {
	Name     string `yaml:"name"`
	Relation string `yaml:"relation"`
	// Account, when set, is how the agent knows them, not by name.
	Account string `yaml:"account"`
	Circle  string `yaml:"circle"` // empty: family
}

var Circles = []string{"family", "friend", "school", "work", "community"}

var workRelations = []string{"boss", "client", "co-founder", "colleague", "manager", "employer"}

func CircleOf(relation string) string {
	switch r := strings.ToLower(relation); {
	case strings.Contains(r, "friend"):
		return "friend"
	case strings.Contains(r, "mentor"), strings.Contains(r, "teacher"), strings.Contains(r, "tutor"):
		return "school"
	case slices.Contains(workRelations, r):
		return "work"
	}
	return "family"
}

// Parse reads and checks a character (character.yaml).
func Parse(raw []byte) (Character, error) {
	const section = "character"
	var l Character
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return Character{}, fmt.Errorf("%s: %w", section, err)
	}
	l.Name, l.Timezone = strings.TrimSpace(l.Name), strings.TrimSpace(l.Timezone)
	if l.Timezone != "" {
		if _, err := time.LoadLocation(l.Timezone); err != nil {
			return Character{}, fmt.Errorf("%s: timezone must be a time zone like Africa/Lagos, have %q", section, l.Timezone)
		}
	}
	l.Story = nonEmpty(l.Story)
	l.Personality = nonEmpty(l.Personality)
	if l.Name == "" || len(l.Story) == 0 {
		return Character{}, errors.New(section + ": write the agent's name and at least one line of its life story first")
	}
	kinds := map[string]bool{}
	for _, k := range Reserved {
		kinds[k] = true
	}
	for _, c := range BuiltIn {
		kinds[c.Kind] = true
	}
	for i, c := range l.Makes {
		c.Kind, c.About = strings.TrimSpace(c.Kind), strings.TrimSpace(c.About)
		switch {
		case !kindName.MatchString(c.Kind) || c.About == "":
			return Character{}, fmt.Errorf("%s: makes %d needs a kind in snake_case (like beat_idea) and what it is (about)", section, i+1)
		case kinds[c.Kind]:
			return Character{}, fmt.Errorf("%s: makes %q is already a kind of work", section, c.Kind)
		}
		kinds[c.Kind] = true
		l.Makes[i] = c
	}
	skills := map[string]bool{}
	for i, sk := range l.Skills {
		sk.Name = strings.TrimSpace(sk.Name)
		switch {
		case sk.Name == "" || sk.Level < 1 || sk.Level > 10:
			return Character{}, fmt.Errorf("%s: skill %d needs a name and a level from 1 to 10", section, i+1)
		case skills[strings.ToLower(sk.Name)]:
			return Character{}, fmt.Errorf("%s: skill %q is listed twice", section, sk.Name)
		}
		skills[strings.ToLower(sk.Name)] = true
		for j, k := range sk.ShownBy {
			k = strings.TrimSpace(k)
			if !kinds[k] {
				return Character{}, fmt.Errorf("%s: skill %q is shown by %q, which isn't a kind of work it makes", section, sk.Name, k)
			}
			sk.ShownBy[j] = k
		}
		l.Skills[i] = sk
	}
	accounts := map[string]bool{}
	for i, r := range l.Family {
		r.Name, r.Relation, r.Account, r.Circle = strings.TrimSpace(r.Name), strings.TrimSpace(r.Relation), strings.TrimSpace(r.Account),
			strings.ToLower(strings.TrimSpace(r.Circle))
		if r.Circle == "" {
			r.Circle = "family"
		}
		if !slices.Contains(Circles, r.Circle) {
			return Character{}, fmt.Errorf("%s: %s's circle must be one of %s, have %q", section, r.Name, strings.Join(Circles, ", "), r.Circle)
		}
		if r.Name == "" || r.Relation == "" {
			return Character{}, fmt.Errorf("%s: family member %d needs a name and a relation", section, i+1)
		}
		if r.Account != "" {
			if accounts[r.Account] {
				return Character{}, fmt.Errorf("%s: account %s belongs to two family members", section, r.Account)
			}
			accounts[r.Account] = true
		}
		l.Family[i] = r
	}
	return l, nil
}

func nonEmpty(lines []string) []string {
	var out []string
	for _, s := range lines {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
