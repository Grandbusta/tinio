package config

import "time"

const (
	SessionCookie = "tinio_session"

	// Site is Tinio's own site: emails link to it and show its icon.
	Site       = "https://heytinio.com"
	SessionTTL = 30 * 24 * time.Hour

	CodeTTL  = 10 * time.Minute // how long a one-time code works
	MaxTries = 5                // wrong tries before a code stops working

	// One-time code types: a code is only ever good for its own type.
	OTPSignIn = "sign_in"

	// Ways to sign in (identities.provider).
	ProviderEmail  = "email"
	ProviderGoogle = "google"

	// A guard against abuse, beside the plan's weekly allowance (internal/usage).
	MessagesPerDay = 200 // to one agent, in any 24 hours
)

// Pace is the rounds of its own work an agent does a day; 1 is like a person.
// It comes with the account's plan, never from the owner's request: until there
// are plans, every agent works like a person.
const Pace = 1
