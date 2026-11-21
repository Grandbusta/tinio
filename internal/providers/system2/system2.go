// Package system2 defines the provider-neutral interface to slow, deliberate
// reasoning models. Each provider lives in its own subpackage.
package system2

import (
	"context"
	"errors"
)

// Reasoner answers a request. On an error, the Response holds only what was
// spent getting it (Provider, Usage, CostUSD), so the caller can record it.
type Reasoner interface {
	Reason(ctx context.Context, req Request) (Response, error)
}

type Request struct {
	Instructions    string
	History         []Turn // oldest first
	Note            string
	Message         string // "" when the agent speaks first
	MaxOutputTokens int    // the most it may write, reasoning included; 0: the provider's default
	// CollectFacts asks for the structured fields of Response alongside the reply.
	CollectFacts bool
	// Format asks for JSON in Response.Text. Not used with CollectFacts.
	Format *Format
	// Draft, if set, is told the reply so far while it's written, all of it
	// each time, and may be told it again from the start if it's tried again.
	// It runs on the call's goroutine, so it should be quick. Not used with Format.
	Draft func(text string)
}

type Format struct {
	Name   string
	Schema map[string]any
}

type About string

const (
	AboutSelf    About = "self" // the agent's own life, including things it just decided
	AboutSpeaker About = "speaker"
)

type Memory struct {
	About About
	Text  string
	Areas []string // the parts of life it's about, as the prompt lists them; nil if none were given
}

type Reminder struct {
	When string // local time, "2006-01-02T15:04"
	What string
}

type Promise struct {
	By      string // when it will send it, local time, "2006-01-02T15:04"
	What    string
	Ability string // the ability it needs, e.g. "read_article"; "" if it only writes
	Query   string
	As      string // the file it sends it as, e.g. "pdf"; "" for a message
	Work    string // the ref in the note of a work it already made that this sends, e.g. "w2"; "" to make one
}

// Act is a tool that changes things, proposed: run only once allowed.
type Act struct {
	Tool string // e.g. "notes.write_file"
	Args string // its arguments, a JSON object
	Why  string
}

// ActSchema is one tool it proposes to use that changes things.
var ActSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"tool", "args", "why"},
	"properties": map[string]any{
		"tool": map[string]any{"type": "string", "description": "the tool's name, as listed"},
		"args": map[string]any{"type": "string", "description": "its arguments, a JSON object"},
		"why":  map[string]any{"type": "string", "description": "why, in a few words"},
	}}

// Look is a reading tool it wants to use before it writes its reply, with
// what to give it. A response with looks has no reply yet: it's asked again
// with what they returned.
type Look struct {
	Tool  string // e.g. "google.calendar"
	Query string
}

// LookSchema is one look.
var LookSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"tool", "query"},
	"properties": map[string]any{
		"tool":  map[string]any{"type": "string", "description": "the reading tool's name, as listed"},
		"query": map[string]any{"type": "string", "description": "what to give it, as its description says"},
	}}

// Move is a new time for a promise it already made.
type Move struct {
	Promise string // its ref in the note, e.g. "p1"
	By      string // local time, "2006-01-02T15:04", or "now"
}

// Send is a work it already made, sent with the reply as a file.
type Send struct {
	Work string // its ref in the note, e.g. "w2"; "" for none
	As   string // e.g. "pdf"
}

type Permission struct {
	Class   string
	Verdict string
}

type Correction struct {
	Fact   string // the fact's ref in the note, e.g. "f2"
	Forget bool   // forget it; otherwise replace it with Text
	Text   string
}

type Feedback struct {
	Skill string
	Level int // 1 to 10
	About string
}

var ErrCutOff = errors.New("the answer was cut off before it was finished")

type Taste struct {
	Kind  string // "" for any kind of work
	Likes bool
	Text  string
}

type GoalTaken struct {
	Skill    string
	Level    int // a new skill's starting level, 1 to 10; 0 for one it has
	Text     string
	DoneWhen string
	Due      string // YYYY-MM-DD
	DreamID  string // "" for a new one
	NewDream string // third person with its name; "" for an existing one
}

type LessonLearned struct {
	Skill string
	Text  string
}

type Role string

const (
	RolePerson Role = "person"
	RoleAgent  Role = "agent"
)

type Turn struct {
	Role Role
	Text string
}

type Response struct {
	Provider string
	Text     string
	// Remember to Goals are filled only with CollectFacts. Permissions,
	// Feedback, Lessons, Tastes and Goals count only from the owner.
	Remember []Memory
	// WhoTheyAre is one sentence with names ("Ada is Daniel's cousin."); "" if not told.
	WhoTheyAre  string
	Reminders   []Reminder
	Promises    []Promise
	Permissions []Permission
	Corrections []Correction
	// Misread is the intent choice the speaker's previous message really was,
	// when they said it was misread; "" if not.
	Misread  string
	Feedback []Feedback
	Lessons  []LessonLearned
	Tastes   []Taste
	Goals    []GoalTaken
	Send     Send
	Moves    []Move
	Acts     []Act
	// Looks, if any, are what it wants to read before it replies; Text is
	// then empty, and only Looks counts.
	Looks []Look
	// Topic is the name it gave what the message starts, when the note asked
	// for one; "" otherwise.
	Topic   string
	Usage   Usage
	CostUSD float64 // 0 when no price is configured
}

type Usage struct {
	InputTokens     int
	CachedTokens    int // part of InputTokens: read from the provider's prompt cache, at a lower price
	OutputTokens    int
	ReasoningTokens int // part of OutputTokens
}
