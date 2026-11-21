// Package controllers is the API's handlers. Each returns an error instead of
// answering it, and utils.Handle answers every error the same way.
package controllers

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/db"
)

// Auth is who may use Tinio. On this machine it's its owner, with no sign-in
// (middlewares.SignedIn). From another device (a phone on the same Wi-Fi),
// only with the passcode the owner set (TINIO_PASSCODE), which gives that
// browser a session; with no passcode, nothing but this machine gets in.
type Auth struct {
	DB       *db.DB
	Owner    models.Account // this machine's owner
	Passcode string         // config.Passcode; "": this machine only
	Secure   bool           // the session cookie only over https
	Log      *slog.Logger
	Now      func() time.Time // nil: the clock
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// SignIn is another device giving the passcode; right, it's signed in as the
// owner. A wrong one, or any when there's no passcode, says only that.
func (a *Auth) SignIn(c *gin.Context) error {
	var in struct {
		Passcode string `json:"passcode"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	if a.Passcode == "" || subtle.ConstantTimeCompare([]byte(in.Passcode), []byte(a.Passcode)) != 1 {
		return utils.ErrWrongPasscode
	}
	now := a.now()
	token := utils.NewToken()
	s := models.Session{AccountID: a.Owner.ID, TokenHash: utils.TokenHash(token), CreatedAt: now, ExpiresAt: now.Add(config.SessionTTL)}
	if err := s.Save(c, a.DB); err != nil {
		return err
	}
	a.setSession(c, token, s.ExpiresAt)
	utils.SuccessWithData(c, http.StatusOK, gin.H{"account": a.Owner})
	return nil
}

// Me is the owner (behind middlewares.SignedIn).
func (a *Auth) Me(c *gin.Context) error {
	utils.SuccessWithData(c, http.StatusOK, gin.H{"account": middlewares.Account(c)})
	return nil
}

// SignOut ends this browser's session and clears the cookie. On this machine
// the owner is still let in.
func (a *Auth) SignOut(c *gin.Context) error {
	if token, err := c.Cookie(config.SessionCookie); err == nil {
		if err := models.DeleteSession(c, a.DB, utils.TokenHash(token)); err != nil {
			return err
		}
	}
	a.setSession(c, "", time.Unix(0, 0))
	utils.SuccessWithData(c, http.StatusOK, gin.H{"signed_out": true})
	return nil
}

// setSession sets the session cookie; an empty token clears it.
func (a *Auth) setSession(c *gin.Context, token string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: config.SessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteLaxMode})
}
