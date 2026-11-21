// Package perception defines what the agent perceives from the world.
package perception

import (
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

type Percept struct {
	ID          ulid.ULID
	Source      string
	ExternalID  string // source's own ID, used to drop duplicates; unique per source
	Thread      string
	Speaker     string // stable key of the person who said it, e.g. "telegram:123"; "" if none
	SpeakerName string
	Kind        string
	Text        string
	Payload     json.RawMessage
	At          time.Time
	// Topic is what it's about, by its id: as the speaker chose it (writing in
	// a topic, or starting one), or once the mind has sorted it. "": none yet.
	Topic string
	// Quote is what it replies to, when the speaker chose that; nil if nothing.
	Quote *Quote
}

// Quote is a message replied to: the channel's own id for it, whether the
// agent wrote it, and what it said.
type Quote struct {
	ID    string
	Agent bool
	Text  string
}

// KindMessage is someone writing to the agent.
const KindMessage = "message"

// Kinds of percept the agent makes itself when it speaks first, so a reply to
// it reads as an answer. What it said is on the decision.
const (
	KindReminder      = "reminder"
	KindCheckIn       = "check_in"
	KindDelivery      = "delivery"
	KindAction        = "action"      // what came of a tool it used
	KindObservation   = "observation" // something new a watched tool returned
	KindReviewRequest = "review_request"
)

// KindApproval is the owner answering the autonomy gate. Its Payload is an
// Answer; it has no Text, so it is never part of the conversation.
const KindApproval = "approval"

// KindHello is their owner opening their chat for the first time: they
// introduce themselves.
const KindHello = "hello"

// KindCall is the owner's own choice on one of the agent's calls. Its Payload
// is a Pick; it has no Text.
const KindCall = "call"

type Pick struct {
	Call   string `json:"call"`
	Option int    `json:"option"` // an index into its options; -1 something else
}

type Answer struct {
	Approval string `json:"approval"`
	Approve  bool   `json:"approve"`
	// Text is the owner's version of what was asked, approved instead of the
	// agent's; "" for theirs. Only for a request whose text may be changed.
	Text string `json:"text,omitempty"`
}
