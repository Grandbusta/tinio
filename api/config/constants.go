package config

import "time"

const (
	SessionCookie = "tinio_session"
	SessionTTL    = 30 * 24 * time.Hour

	// A guard against a loop running up the owner's bill, beside the week's cap (internal/usage).
	MessagesPerDay = 200 // to one agent, in any 24 hours
)
