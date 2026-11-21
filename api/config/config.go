// Package config is the API's settings, read from the environment once at
// start, and where Tinio keeps its data.
package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Grandbusta/tinio/internal/connect"
	"github.com/Grandbusta/tinio/internal/secret"
)

type Config struct {
	Port           string   // PORT, default 8081
	DataDir        string   // TINIO_DATA: the folder with the database and the key; DefaultDataDir if not set
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
		DataDir:        os.Getenv("TINIO_DATA"),
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
	if c.DataDir == "" {
		dir, err := DefaultDataDir()
		if err != nil {
			return c, err
		}
		c.DataDir = dir
	}
	if c.DevCode && (c.CookieSecure || !local(c.AppOrigin)) {
		return c, errors.New("SIGNIN_DEV_CODE is for development only: it needs COOKIE_SECURE=false and APP_ORIGIN on localhost")
	}
	return c, nil
}

// DefaultDataDir is where Tinio keeps its data unless TINIO_DATA says:
// Application Support on macOS, %AppData% on Windows, the XDG data folder
// elsewhere.
func DefaultDataDir() (string, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "Tinio"), nil
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "tinio"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "tinio"), nil
}

// DatabasePath is the database's file in the data folder.
func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "tinio.db") }

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
