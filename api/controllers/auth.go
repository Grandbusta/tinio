// Package controllers is the API's handlers. Each returns an error instead of
// answering it, and utils.Handle answers every error the same way.
package controllers

import (
	"crypto/hmac"
	"github.com/Grandbusta/tinio/internal/db"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
)

// Auth signs people in with an emailed code (Google comes later). Signing up
// and signing in are one flow: the account is made the first time a code
// works. Nothing it answers says whether an email has an account, or an
// invite while the beta is on: an email that may not join is told so by email.
type Auth struct {
	DB     *db.DB
	Mail   utils.Mailer
	Secret []byte // keys the codes' hashes: config.SigninSecret
	Secure bool   // the session cookie only over https
	// DevCode lets 000000 stand in for the code sent: config.DevCode, for
	// development on localhost only.
	DevCode bool
	Log     *slog.Logger
	// CodesPerEmail limits how often one email is sent a code.
	CodesPerEmail *utils.Limiter
	Now           func() time.Time // nil: the clock
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

type emailInput struct {
	Email string `json:"email"`
}

type verifyInput struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// SendCode emails a sign-in code, whether or not the email has an account.
func (a *Auth) SendCode(c *gin.Context) error {
	var in emailInput
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	email, err := utils.Email(in.Email)
	if err != nil {
		return err
	}
	if !a.CodesPerEmail.Allow(email) {
		return utils.ErrSlowDown
	}
	code, err := utils.NewCode()
	if err != nil {
		return err
	}
	now := a.now()
	o := models.OTP{Type: config.OTPSignIn, Target: email, CodeHash: utils.CodeHash(a.Secret, config.OTPSignIn, email, code),
		ExpiresAt: now.Add(config.CodeTTL), CreatedAt: now}
	if err := o.Save(c, a.DB); err != nil {
		return err
	}
	err = a.Mail.Send(c.Request.Context(), utils.Message{To: email, Subject: "Your Tinio code: " + code, Preview: "It works once, for 10 minutes.",
		Heading: "Your sign-in code", Paras: []string{"Enter it on the page you came from. It works once, for 10 minutes."}, Code: code,
		Fine: "Tinio never asks for your code in a message, an email or a call. Your agents won't either. " +
			"If you didn't ask for it, ignore this email: nothing happens without the code.",
		Why: "You asked to sign in to Tinio with this email."})
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusAccepted, gin.H{"sent": true})
	return nil
}

// Verify signs in with a code, making the account the first time, and sets
// the session cookie. A wrong try is counted even though it fails.
func (a *Auth) Verify(c *gin.Context) error {
	var in verifyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	email, err := utils.Email(in.Email)
	if err != nil {
		return err
	}
	code := strings.Join(strings.Fields(in.Code), "")
	now := a.now()

	tx, err := a.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	o, ok, err := models.LiveOTP(c, tx, config.OTPSignIn, email)
	switch {
	case err != nil:
		return err
	case !ok || !now.Before(o.ExpiresAt): // also no code, or one already used
		return utils.ErrExpired
	case o.Attempts >= config.MaxTries:
		return utils.ErrTooManyTries
	}
	if !hmac.Equal(o.CodeHash, utils.CodeHash(a.Secret, config.OTPSignIn, email, code)) && !(a.DevCode && code == "000000") {
		if err := o.WrongTry(c, tx); err != nil {
			return err
		}
		if err := tx.Commit(c); err != nil {
			return err
		}
		if left := config.MaxTries - o.Attempts; left > 0 {
			return utils.ErrWrongCode.With(gin.H{"left": left})
		}
		return utils.ErrTooManyTries
	}
	if err := o.Use(c, tx, now); err != nil {
		return err
	}
	acc, isNew, err := a.accountFor(c, tx, email, now)
	if err != nil {
		return err
	}
	token := utils.NewToken()
	s := models.Session{AccountID: acc.ID, TokenHash: utils.TokenHash(token), CreatedAt: now, ExpiresAt: now.Add(config.SessionTTL)}
	if err := s.Save(c, tx); err != nil {
		return err
	}
	if err := tx.Commit(c); err != nil {
		return err
	}
	a.setSession(c, token, s.ExpiresAt)
	utils.SuccessWithData(c, http.StatusOK, gin.H{"account": acc, "new": isNew})
	return nil
}

// accountFor is the account email signs in to. An account that writes to it
// but signs in another way (Google) gains email as a way in; otherwise it's a
// new account.
func (a *Auth) accountFor(c *gin.Context, db models.DB, email string, now time.Time) (models.Account, bool, error) {
	acc, ok, err := models.SignInWith(c, db, config.ProviderEmail, email, now)
	if err != nil || ok {
		return acc, false, err
	}
	acc, ok, err = models.FindAccountByEmail(c, db, email)
	if err != nil {
		return acc, false, err
	}
	if !ok {
		acc = models.Account{Email: email, CreatedAt: now}
		if err := acc.Save(c, db); err != nil {
			return acc, false, err
		}
	}
	i := models.Identity{AccountID: acc.ID, Provider: config.ProviderEmail, Subject: email, CreatedAt: now}
	return acc, !ok, i.Save(c, db)
}

// Me is the signed-in account (behind middlewares.SignedIn).
func (a *Auth) Me(c *gin.Context) error {
	acc := middlewares.Account(c)
	utils.SuccessWithData(c, http.StatusOK, gin.H{"account": acc})
	return nil
}

// SignOut ends the session and clears the cookie.
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
