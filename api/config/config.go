// Package config is the API's settings, read from the environment once at
// start, and its database.
package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/Grandbusta/tinio/internal/connect"
	"github.com/Grandbusta/tinio/internal/secret"
)

type Config struct {
	Port           string   // PORT, default 8081
	DatabaseURL    string   // DATABASE_URL: the platform's one database
	AppOrigin      string   // APP_ORIGIN: the app (app.heytinio.com), the only site that may change anything
	CookieSecure   bool     // COOKIE_SECURE: false only for plain http on localhost
	SigninSecret   []byte   // SIGNIN_SECRET: keys the sign-in codes' hashes (32+ characters); the API's alone
	TrustedProxies []string // TRUSTED_PROXIES: whose X-Forwarded-For is believed
	// DevCode (SIGNIN_DEV_CODE=true) lets 000000 stand in for any code that was
	// sent, for development. Only with the app on localhost over plain http.
	DevCode bool
	// Key (TINIO_KEY) seals the tokens of the accounts owners connect; without
	// it, nothing can be connected.
	Key *secret.Key
	// Google is Tinio's own Google client (GOOGLE_CLIENT_ID,
	// GOOGLE_CLIENT_SECRET); GooglePicker is what the app's file picker needs
	// (GOOGLE_API_KEY, GOOGLE_APP_ID). Without them, Google can't be connected.
	Google       connect.Google
	GooglePicker struct{ APIKey, AppID string }
}

func Load() (Config, error) {
	c := Config{
		Port:           orDefault(os.Getenv("PORT"), "8081"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		AppOrigin:      orDefault(os.Getenv("APP_ORIGIN"), "http://localhost:3001"),
		CookieSecure:   os.Getenv("COOKIE_SECURE") != "false",
		SigninSecret:   []byte(os.Getenv("SIGNIN_SECRET")),
		TrustedProxies: strings.Split(orDefault(os.Getenv("TRUSTED_PROXIES"), "127.0.0.1,::1"), ","),
		DevCode:        os.Getenv("SIGNIN_DEV_CODE") == "true",
		Google:         connect.Google{ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET")},
	}
	c.GooglePicker.APIKey, c.GooglePicker.AppID = os.Getenv("GOOGLE_API_KEY"), os.Getenv("GOOGLE_APP_ID")
	if k := os.Getenv(secret.EnvKey); k != "" {
		key, err := secret.ParseKey(k)
		if err != nil {
			return c, err
		}
		c.Key = &key
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL isn't set: the platform's database")
	}
	if c.DevCode && (c.CookieSecure || !local(c.AppOrigin)) {
		return c, errors.New("SIGNIN_DEV_CODE is for development only: it needs COOKIE_SECURE=false and APP_ORIGIN on localhost")
	}
	return c, nil
}

func local(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
