// Package coding runs coding jobs with the coding CLI on the owner's computer
// (Claude Code first): a job is started in a folder with a brief, what the CLI
// does comes back as events, and each thing it asks to do is answered, by the
// job's rules or by the owner.
//
// A job follows the repository's own rules (its CLAUDE.md and .claude
// settings), never the owner's personal setup: none of their hooks or
// permission rules can answer what Tinio should ask.
package coding

import (
	"context"
	"errors"
)

// A Job is what to start.
type Job struct {
	Dir      string // where it works: the job's copy of the repository
	Brief    string // the first message: what to do
	System   string // added to the CLI's own system prompt; "" for none
	Model    string // e.g. claude-opus-5-5; "" for the CLI's default
	MaxTurns int    // the most steps it may take; 0 for the CLI's own limit
	Resume   string // a session to carry on, instead of a new one
}

// Event kinds.
const (
	Started = "started" // SessionID is the CLI's session, for Resume
	Said    = "said"    // Text is what it wrote
	Saying  = "saying"  // Text is what it's writing, so far: Said follows with all of it
	Step    = "step"    // Tool and Input: something it did that didn't need asking
	Changed = "changed" // Path: a file it changed (also a Step)
	Ask     = "ask"     // AskID, Tool, Input: it wants to do this; answer it
	Done    = "done"    // Status, Text (its last words), CostUSD, Turns
)

// Statuses of a Done event.
const (
	Completed = "completed" // it finished what it was asked
	Stopped   = "stopped"   // it ran out of steps
	Failed    = "failed"    // an error ended it
)

// An Event is one thing from a running job.
type Event struct {
	Kind      string
	SessionID string
	Text      string
	Tool      string
	Input     map[string]any
	Path      string
	AskID     string
	Status    string
	CostUSD   float64 // what it reports a turn would cost on a key: on a plan, model time
	Turns     int
}

// A Session is a running job. After a Done it waits: Send carries it on (say,
// fixes sent back), Close ends it.
type Session interface {
	Events() <-chan Event // closed when the CLI has ended
	Send(text string) error
	// Answer says yes or no to an Ask; why is what the CLI is told with a no.
	// stop, with a no, also ends its turn there, as a No in Claude Code does:
	// it waits to be told what to do instead rather than working round it.
	Answer(askID string, allow bool, why string, stop bool) error
	// Choose answers the CLI's own question (AskUserQuestion): each question's
	// text, with what was chosen.
	Choose(askID string, answers map[string]string) error
	Interrupt() error
	Close() error
	// Alive is whether it can still take more: the CLI hasn't ended.
	Alive() bool
}

// An Adapter starts jobs with one CLI.
type Adapter interface {
	Start(ctx context.Context, job Job) (Session, error)
}

// ErrEnded is from a Session the CLI has left.
var ErrEnded = errors.New("coding: the job has ended")
