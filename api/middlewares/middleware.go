// Package middlewares is what runs before the controllers.
package middlewares

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
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

// SignedIn lets a request through as owner when it comes from this machine,
// and otherwise only with a live session (from the passcode), keeping its
// account for the controller (Account).
func SignedIn(db models.DB, owner models.Account, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if FromThisMachine(c.Request) {
			c.Set(accountKey, owner)
			return
		}
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

// FromThisMachine is a request from a browser on this machine to this machine
// by name: it came over loopback, through nothing that forwarded it from
// elsewhere, and its Host is localhost or a loopback address, so a page that
// rebinds its own name to 127.0.0.1 isn't taken for the owner.
func FromThisMachine(r *http.Request) bool {
	if !loopback(r.RemoteAddr) || !loopback(r.Host) {
		return false
	}
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		for _, v := range r.Header.Values(h) {
			for _, part := range strings.Split(v, ",") {
				part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "for="))
				if part != "" && !loopback(strings.Trim(part, `"[]`)) {
					return false
				}
			}
		}
	}
	return true
}

// loopback is whether hostport (or a bare host) names this machine.
func loopback(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
