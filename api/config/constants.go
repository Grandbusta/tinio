package config

import "time"

const (
	SessionCookie = "tinio_session"
	SessionTTL    = 30 * 24 * time.Hour

	// A guard against abuse, beside the plan's weekly allowance (internal/usage).
	MessagesPerDay = 200 // to one agent, in any 24 hours
)

// Pace is the rounds of its own work an agent does a day; 1 is like a person.
// It comes with the account's plan, never from the owner's request: until there
// are plans, every agent works like a person.
const Pace = 1
