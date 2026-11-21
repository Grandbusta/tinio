// Package config is what every agent shares: the platform's models, from the
// environment, and how every agent works, the same for all of them.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/action"
)

type Config struct {
	Decision      Decision  // System 1
	Reasoning     Reasoning // System 2
	WorkingMemory WorkingMemory
	Memory        Memory
	Initiative    Initiative
	DailyLife     DailyLife
	Autonomy      Autonomy
	Judgment      Judgment
	Search        Search
	Tools         Tools
	// Embedding finds the past decisions most like the one at hand; no
	// provider: the newest are used instead.
	Embedding Provider
}

type Memory struct {
	RecallCandidates int
}

type Initiative struct {
	Timezone string // empty: the one in its life story
	CheckIns []string
	Quiet    time.Duration
}

func (i Initiative) Zone() (*time.Location, error) {
	loc, err := time.LoadLocation(i.Timezone)
	if err != nil || i.Timezone == "" {
		return nil, fmt.Errorf("a time zone like Africa/Lagos is needed: timezone in their character; have %q",
			i.Timezone)
	}
	return loc, nil
}

func (i Initiative) CheckInTimes() ([]time.Duration, error) {
	out := make([]time.Duration, len(i.CheckIns))
	for n, s := range i.CheckIns {
		at, err := timeOfDay(s)
		if err != nil {
			return nil, fmt.Errorf("config: initiative.check_ins must be HH:MM times, have %q", s)
		}
		out[n] = at
	}
	return out, nil
}

type DailyLife struct {
	Enabled bool
	// Abilities nil: DefaultAbilities. Empty: none besides the journal.
	Abilities  []string
	PlanAt     string
	JournalAt  string
	ReviewAt   string
	Activities int
	// Pace is the rounds in its waking day; BudgetUSD and Web are per pace-1
	// day and are multiplied by it.
	Pace      int
	BudgetUSD float64
	// Model is System 2's model for its own day; "": reasoning's. ServiceTier
	// is asked for its own day only: "" or "flex". WorkModel makes works, for
	// itself or someone; "": reasoning's.
	Model          string
	ServiceTier    string
	WorkModel      string
	PlanPrompt     string
	ActivityPrompt string
	ReviewPrompt   string
	LearnPrompt    string
	DataPrompt     string
	ObservePrompt  string
	Web            Web
}

type Web struct {
	Searches int
	Pages    int
}

// Tools are MCP servers whose tools it may use. Only the tools listed are
// offered, each with what it can do (ToolKinds).
type Tools struct {
	CallsPerDay   int // reading calls; grows with pace, like the web caps
	ActionsPerDay int // tools that change things: real effects, so not paced
	Servers       []ToolServer
}

type ToolServer struct {
	Name string
	// About says what it is and where things are, for the planner, like
	// "Busta's shared folder, /Users/busta/shared: notes he shares".
	About string
	// Command runs a local server; URL reaches a remote one. Exactly one.
	Command []string
	URL     string
	// AuthEnv names the environment variable holding a remote server's
	// bearer token. Env names the variables a local server is given, besides
	// PATH and HOME: it never sees the rest of the environment.
	AuthEnv string
	Env     []string
	Tools   map[string]string // tool name → what it can do
	Watch   []Watch
}

// Watch calls one of the server's read tools on a schedule; each item in
// its result not seen before is something the person perceives.
type Watch struct {
	Tool  string
	Args  map[string]any
	Every time.Duration // at least a minute
	// Items is where the list is in a JSON result, as dotted keys (like
	// "messages"); "" if the result is the list, or a single item. ID is the
	// field that identifies an item; "" to tell items apart by what they say.
	Items string
	ID    string
}

// ToolRead is a tool that only reads. Every other kind is the autonomy class
// the gate checks before it is used.
const ToolRead = "read"

// ToolKinds are what a tool can do: read, or one of these autonomy classes.
var ToolKinds = []string{ToolRead, "write", "external_message", "commit", "spend"}

var serverName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (t Tools) Validate() error {
	if t.CallsPerDay < 0 || t.ActionsPerDay < 0 {
		return errors.New("config: tools.calls_per_day and actions_per_day can't be negative")
	}
	seen := map[string]bool{}
	for i, s := range t.Servers {
		switch {
		case !serverName.MatchString(s.Name):
			return fmt.Errorf("config: tools.servers[%d]: name %q must be snake_case, like notes", i, s.Name)
		case seen[s.Name]:
			return fmt.Errorf("config: tools.servers: two servers named %q", s.Name)
		case (len(s.Command) == 0) == (s.URL == ""):
			return fmt.Errorf("config: tools.servers %s: give a command or a url, not both", s.Name)
		case s.URL != "" && !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://localhost") &&
			!strings.HasPrefix(s.URL, "http://127.0.0.1"):
			return fmt.Errorf("config: tools.servers %s: url must be https (or http on localhost)", s.Name)
		case len(s.Tools) == 0:
			return fmt.Errorf("config: tools.servers %s: list the tools it may use", s.Name)
		}
		seen[s.Name] = true
		for tool, kind := range s.Tools {
			if !slices.Contains(ToolKinds, kind) {
				return fmt.Errorf("config: tools.servers %s: %s is %q; it can be %s", s.Name, tool, kind, strings.Join(ToolKinds, ", "))
			}
		}
		for _, w := range s.Watch {
			switch {
			case s.Tools[w.Tool] != ToolRead:
				return fmt.Errorf("config: tools.servers %s: watch %q must be one of its read tools", s.Name, w.Tool)
			case w.Every < time.Minute:
				return fmt.Errorf("config: tools.servers %s: watch %s every %s; at least 1m", s.Name, w.Tool, w.Every)
			}
		}
	}
	return nil
}

// Search with the openai provider uses the System 2 key (reasoning.api_key).
type Search struct {
	Provider string
	Model    string // empty: the System 2 model
}

func (d DailyLife) ReviewTime() (time.Weekday, time.Duration, error) {
	day, at, ok := strings.Cut(strings.TrimSpace(d.ReviewAt), " ")
	days := map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
		"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}
	wd, known := days[strings.ToLower(day)]
	t, err := timeOfDay(strings.TrimSpace(at))
	if !ok || !known || err != nil {
		return 0, 0, fmt.Errorf("config: daily_life.review_at must be a day and HH:MM, like \"Sun 20:00\", have %q", d.ReviewAt)
	}
	return wd, t, nil
}

var DefaultAbilities = []string{"read_book", "read_article"}

func (d DailyLife) AbilityNames() []string {
	if d.Abilities != nil {
		return d.Abilities
	}
	return DefaultAbilities
}

type Judgment struct {
	Calls       bool
	AsksPerDay  int // day-plan calls only; goal calls are always asked
	ReplyWindow time.Duration
}

type Autonomy struct {
	Classes    map[string]string
	AskExpires time.Duration
}

// Policy is the defaults with the config's overrides, with no owner yet.
func (c *Config) Policy() (action.Policy, error) {
	classes := action.Defaults()
	overrides := map[action.Class]action.Verdict{}
	for k, v := range c.Autonomy.Classes {
		overrides[action.Class(k)] = action.Verdict(strings.TrimSpace(v))
	}
	if err := action.Validate(overrides); err != nil {
		return action.Policy{}, fmt.Errorf("config: %w", err)
	}
	for k, v := range overrides {
		classes[k] = v
	}
	if c.Autonomy.AskExpires <= 0 {
		return action.Policy{}, errors.New("config: autonomy.ask_expires must be positive, e.g. 24h")
	}
	return action.Policy{Classes: classes, AskExpires: c.Autonomy.AskExpires}, nil
}

func (d DailyLife) Times() (plan, journal time.Duration, err error) {
	if plan, err = timeOfDay(d.PlanAt); err != nil {
		return 0, 0, fmt.Errorf("config: daily_life.plan_at must be HH:MM, have %q", d.PlanAt)
	}
	if journal, err = timeOfDay(d.JournalAt); err != nil || journal <= plan {
		return 0, 0, fmt.Errorf("config: daily_life.journal_at must be HH:MM after plan_at, have %q", d.JournalAt)
	}
	return plan, journal, nil
}

func timeOfDay(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

type WorkingMemory struct {
	Turns  int // 0 turns recall off
	Window time.Duration
}

type Provider struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

type Decision struct {
	Provider
	QuestionSets string
}

type Reasoning struct {
	Provider
	Prompt      string
	TopicPrompt string
	Effort      string // empty: the model's default
	LightModel  string
}

// Load is how every agent works, on the models the environment names:
// OPENAI_API_KEY for all of them, OPENAI_MODEL for System 2, and
// OPENAI_DECISION_MODEL, OPENAI_DAY_MODEL and OPENAI_WORK_MODEL (empty: System
// 1's default, and OPENAI_MODEL). Nothing leaves the chat yet, so messages outside it are
// forbidden.
func Load() *Config {
	key := os.Getenv("OPENAI_API_KEY")
	return &Config{
		Decision: Decision{Provider: Provider{Provider: "openai", Model: os.Getenv("OPENAI_DECISION_MODEL"), APIKey: key}, QuestionSets: "decisions"},
		Reasoning: Reasoning{Provider: Provider{Provider: "openai", Model: os.Getenv("OPENAI_MODEL"), APIKey: key}, Prompt: "prompts/reply.md", TopicPrompt: "prompts/topic.md", Effort: "low",
			LightModel: cmp.Or(os.Getenv("OPENAI_LIGHT_MODEL"), "gpt-6-luna")},
		WorkingMemory: WorkingMemory{Turns: 8, Window: 30 * time.Minute},
		Memory:        Memory{RecallCandidates: 30},
		Initiative:    Initiative{Quiet: 4 * time.Hour},
		DailyLife: DailyLife{Enabled: true, Pace: 1, PlanAt: "08:00", JournalAt: "22:00", Activities: 3, BudgetUSD: 0.05,
			Model:      os.Getenv("OPENAI_DAY_MODEL"),
			WorkModel:  os.Getenv("OPENAI_WORK_MODEL"),
			Abilities:  []string{"read_book", "read_article", "web_search", "read_page"},
			PlanPrompt: "prompts/plan.md", ActivityPrompt: "prompts/activity.md",
			ReviewAt: "Sun 20:00", ReviewPrompt: "prompts/review.md", LearnPrompt: "prompts/learn.md",
			DataPrompt: "prompts/data.md", ObservePrompt: "prompts/observe.md",
			Web: Web{Searches: 10, Pages: 15}},
		Search:    Search{Provider: "openai"},
		Tools:     Tools{CallsPerDay: 50, ActionsPerDay: 10},
		Autonomy:  Autonomy{Classes: map[string]string{string(action.ExternalMessage): string(action.Forbid)}, AskExpires: 24 * time.Hour},
		Judgment:  Judgment{Calls: true, AsksPerDay: 1, ReplyWindow: 30 * time.Minute},
		Embedding: Provider{Provider: "openai", Model: "text-embedding-3-small", APIKey: key},
	}
}
