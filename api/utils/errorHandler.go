package utils

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Every error answer has one shape:
//
//	{"status": "error", "code": "wrong_code", "message": "...", "data": {...}}
//
// code is for the app to act on; message is plain words for people; data
// carries anything more (a wrong code's tries left).

// Problem is an error the API answers with as it is. Any other error a
// controller returns is logged and answered as something_went_wrong, saying
// nothing of what.
type Problem struct {
	Status  int
	Code    string
	Message string
	Data    any // nil: none
}

func (p Problem) Error() string { return p.Code + ": " + p.Message }

func (p Problem) With(data any) Problem {
	p.Data = data
	return p
}

// The API's errors, all in one place.
var (
	ErrSomethingWentWrong = Problem{http.StatusInternalServerError, "something_went_wrong", "Something went wrong on our side. Try again in a minute.", nil}
	ErrBadRequest         = Problem{http.StatusBadRequest, "bad_request", "That request isn't one we understand.", nil}
	ErrWrongOrigin        = Problem{http.StatusForbidden, "wrong_origin", "This request has to come from Tinio's own site.", nil}
	ErrSignedOut          = Problem{http.StatusUnauthorized, "signed_out", "You're not signed in.", nil}
	ErrNotEmail           = Problem{http.StatusBadRequest, "not_email", "That doesn't look like an email address.", nil}
	ErrSlowDown           = Problem{http.StatusTooManyRequests, "slow_down", "You've asked several times in a short time. Wait a few minutes, then try again.", nil}
	ErrWrongCode          = Problem{http.StatusBadRequest, "wrong_code", "That code isn't right. Check the email and try again.", nil}
	ErrExpired            = Problem{http.StatusBadRequest, "expired", "That code has expired. Codes work for 10 minutes; we can send you a new one.", nil}
	ErrTooManyTries       = Problem{http.StatusBadRequest, "too_many_tries", "Too many wrong tries, so this code no longer works. Ask for a new one.", nil}
	ErrNoSuchCharacter    = Problem{http.StatusNotFound, "no_such_character", "We don't have that character.", nil}
	ErrNoSuchAgent        = Problem{http.StatusNotFound, "no_such_agent", "You don't have that agent.", nil}
	ErrNoSuchFile         = Problem{http.StatusNotFound, "no_such_file", "That file isn't here.", nil}
	ErrNoSuchTopic        = Problem{http.StatusNotFound, "no_such_topic", "That isn't one of the topics you have going.", nil}
	ErrNoSuchMessage      = Problem{http.StatusNotFound, "no_such_message", "That message isn't in this chat.", nil}
	ErrNoSuchWork         = Problem{http.StatusNotFound, "no_such_work", "That isn't among what they've made.", nil}
	ErrNoSuchDecision     = Problem{http.StatusNotFound, "no_such_decision", "That isn't one of their decisions.", nil}
	ErrNoSuchFact         = Problem{http.StatusNotFound, "no_such_fact", "That isn't something they remember.", nil}
	ErrNoSuchVersion      = Problem{http.StatusNotFound, "no_such_version", "That isn't one of their versions.", nil}
	ErrNoSuchSkill        = Problem{http.StatusNotFound, "no_such_skill", "They don't have that know-how.", nil}
	ErrNoSuchMarketSkill  = Problem{http.StatusNotFound, "no_such_market_skill", "That skill isn't in the market.", nil}
	ErrNoSuchPage         = Problem{http.StatusNotFound, "no_such_page", "There's nothing here.", nil}
	ErrNoSuchPrinciple    = Problem{http.StatusNotFound, "no_such_principle", "That isn't one of their principles.", nil}
	ErrNoSuchConnection   = Problem{http.StatusNotFound, "no_such_connection", "That isn't one of your connections.", nil}
	ErrConnectionsOff     = Problem{http.StatusServiceUnavailable, "connections_off", "Connecting that isn't set up on this Tinio yet.", nil}
	ErrServiceDown        = Problem{http.StatusBadGateway, "service_down", "We couldn't reach its sign-in just now. Try again in a minute.", nil}
	ErrSignInAgain        = Problem{http.StatusConflict, "sign_in_again", "The service signed Tinio out. Connect it again to carry on.", nil}
	ErrNoSuchInvite       = Problem{http.StatusNotFound, "no_such_invite", "There's no invite for that email waiting to be used.", nil}
	ErrNotInvited         = Problem{http.StatusForbidden, "not_invited", "Tinio is invite-only for now, and this email isn't invited yet.", nil}
	ErrNotWaiting         = Problem{http.StatusConflict, "not_waiting", "That's no longer waiting for an answer.", nil}
	ErrTooManyAgents      = Problem{http.StatusConflict, "too_many_agents", "Your plan has room for the agents you have. A bigger plan has room for more.", nil}
	ErrDailyLimit         = Problem{http.StatusTooManyRequests, "daily_limit", "That's as many messages as you can send them in a day for now. Try again tomorrow.", nil}
)

// Invalid is a field that isn't right, with what to do about it.
func Invalid(field, message string) Problem {
	return Problem{http.StatusBadRequest, "invalid", message, map[string]string{"field": field}}
}

// ServerResponse answers err and stops the request.
func ServerResponse(c *gin.Context, log *slog.Logger, err error) {
	var p Problem
	if !errors.As(err, &p) {
		log.Error("request failed", "method", c.Request.Method, "path", c.FullPath(), "err", err)
		p = ErrSomethingWentWrong
	}
	body := gin.H{"status": "error", "code": p.Code, "message": p.Message}
	if p.Data != nil {
		body["data"] = p.Data
	}
	c.AbortWithStatusJSON(p.Status, body)
}

// Handle runs a controller that answers itself on success and returns any
// error, so every error is answered in one place.
func Handle(log *slog.Logger, fn func(c *gin.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := fn(c); err != nil {
			ServerResponse(c, log, err)
		}
	}
}
