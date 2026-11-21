package config

import "testing"

// The development code can't be turned on anywhere but localhost over plain http.
func TestTheDevelopmentCodeIsOnlyForLocalhost(t *testing.T) {
	t.Setenv("TINIO_DATA", t.TempDir())
	t.Setenv("SIGNIN_DEV_CODE", "true")
	for _, c := range []struct {
		secure, origin string
		ok             bool
	}{
		{"false", "http://localhost:3001", true},
		{"false", "http://127.0.0.1:3001", true},
		{"", "http://localhost:3001", false},
		{"false", "https://app.heytinio.com", false},
		{"false", "http://localhost.heytinio.com", false},
	} {
		t.Setenv("COOKIE_SECURE", c.secure)
		t.Setenv("APP_ORIGIN", c.origin)
		if cfg, err := Load(); (err == nil) != c.ok || (c.ok && !cfg.DevCode) {
			t.Errorf("secure %q, origin %s: %v", c.secure, c.origin, err)
		}
	}
}
