// Package middlewares is what runs before the controllers.
package middlewares

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
)

// FromOurSite refuses a request that changes anything unless the browser says
// it comes from the app (origin): another site can't sign someone in or out.
func FromOurSite(origin string, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Origin") != origin {
			utils.ServerResponse(c, log, utils.ErrWrongOrigin)
		}
	}
}

// LimitByAddress refuses requests from an address past l.
func LimitByAddress(l *utils.Limiter, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(c.ClientIP()) {
			utils.ServerResponse(c, log, utils.ErrSlowDown)
		}
	}
}

const accountKey = "account"

// SignedIn lets a request through only with a live session, and keeps its
// account for the controller (Account).
func SignedIn(db models.DB, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, _ := c.Cookie(config.SessionCookie)
		acc, ok, err := models.SessionAccount(c, db, utils.TokenHash(token), time.Now().UTC())
		switch {
		case err != nil:
			utils.ServerResponse(c, log, err)
		case !ok:
			utils.ServerResponse(c, log, utils.ErrSignedOut)
		default:
			c.Set(accountKey, acc)
		}
	}
}

// Account is the signed-in account; only behind SignedIn.
func Account(c *gin.Context) models.Account {
	return c.MustGet(accountKey).(models.Account)
}

const agentKey = "agent"

// OwnAgent lets a request through only for an agent (the :id in its path) the
// signed-in account owns, and keeps them for the controller (Owned). Anyone
// else's is not found, as if it weren't there. Only behind SignedIn.
func OwnAgent(db models.DB, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ag, ok, err := models.AgentOf(c, db, Account(c).ID, c.Param("id"))
		switch {
		case err != nil:
			utils.ServerResponse(c, log, err)
		case !ok:
			utils.ServerResponse(c, log, utils.ErrNoSuchAgent)
		default:
			c.Set(agentKey, ag)
		}
	}
}

// Owned is the agent the request is for; only behind OwnAgent.
func Owned(c *gin.Context) models.Agent {
	return c.MustGet(agentKey).(models.Agent)
}
