// Package config is the API's settings, read from the environment once at
// start, and where Tinio keeps its data.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/Grandbusta/tinio/internal/connect"
	"github.com/Grandbusta/tinio/internal/secret"
)

type Config struct {
	Addr           string   // TINIO_ADDR: where the API listens; this machine alone unless it says otherwise
	DataDir        string   // TINIO_DATA: the folder with the database and the key; DefaultDataDir if not set
	AppOrigin      string   // APP_ORIGIN: the app, the only site that may change anything
	CookieSecure   bool     // COOKIE_SECURE=true: the session cookie only over https
	TrustedProxies []string // TRUSTED_PROXIES: whose X-Forwarded-For is believed
	// Passcode (TINIO_PASSCODE, 16 characters or more) lets another device in;
	// "": only this machine.
	Passcode string
	// Key seals every key and token kept: TINIO_KEY if it's set, or else the
	// one Tinio made in the data folder the first time it ran (LoadKey).
	Key *secret.Key
	// Google is Tinio's own Google client (GOOGLE_CLIENT_ID,
	// GOOGLE_CLIENT_SECRET); GooglePicker is what the app's file picker needs
	// (GOOGLE_API_KEY, GOOGLE_APP_ID). Without them, Google can't be connected.
	Google       connect.Google
	GooglePicker struct{ APIKey, AppID string }
}

func Load() (Config, error) {
	c := Config{
		Addr:           orDefault(os.Getenv("TINIO_ADDR"), "127.0.0.1:8081"),
		DataDir:        os.Getenv("TINIO_DATA"),
		AppOrigin:      orDefault(os.Getenv("APP_ORIGIN"), "http://localhost:3001"),
		CookieSecure:   os.Getenv("COOKIE_SECURE") == "true",
		TrustedProxies: strings.Split(orDefault(os.Getenv("TRUSTED_PROXIES"), "127.0.0.1,::1"), ","),
		Passcode:       os.Getenv("TINIO_PASSCODE"),
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
	if c.Passcode != "" && utf8.RuneCountInString(c.Passcode) < 16 {
		return c, errors.New("TINIO_PASSCODE must be at least 16 characters")
	}
	return c, nil
}

// keyFile is where the key Tinio made is kept, in the data folder.
const keyFile = "key"

// LoadKey is the key in the data folder, made (32 random bytes) and kept
// there, readable only by its owner, the first time; TINIO_KEY wins if it's
// set. Lose it and every key and token kept can't be opened again.
func (c *Config) LoadKey() error {
	if c.Key != nil {
		return nil
	}
	path := filepath.Join(c.DataDir, keyFile)
	text, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		text = []byte(secret.NewKey())
		if err := os.WriteFile(path, text, 0o600); err != nil {
			return fmt.Errorf("keep the key: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read the key: %w", err)
	}
	key, err := secret.ParseKey(strings.TrimSpace(string(text)))
	if err != nil {
		return fmt.Errorf("the key in %s: %w", path, err)
	}
	c.Key = &key
	return nil
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

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
