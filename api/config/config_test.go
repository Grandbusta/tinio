package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Grandbusta/tinio/internal/secret"
)

// The key is made the first time, readable only by its owner, and the same
// one is used every time after; TINIO_KEY wins when it's set.
func TestTheKeyIsMadeOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINIO_DATA", dir)
	t.Setenv(secret.EnvKey, "")
	load := func() *secret.Key {
		t.Helper()
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.LoadKey(); err != nil {
			t.Fatal(err)
		}
		return c.Key
	}
	first := load()
	info, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key file: %v %v", info, err)
	}
	sealed := first.Seal("test", "kept")
	if v, err := load().Open("test", sealed); err != nil || v != "kept" {
		t.Errorf("a second start has another key: %q %v", v, err)
	}
	t.Setenv(secret.EnvKey, secret.NewKey())
	if _, err := load().Open("test", sealed); err == nil {
		t.Error("TINIO_KEY wasn't used")
	}
}

// A passcode for other devices is long enough to stand up to guessing.
func TestAShortPasscodeIsRefused(t *testing.T) {
	t.Setenv("TINIO_DATA", t.TempDir())
	t.Setenv("TINIO_PASSCODE", "too short")
	if _, err := Load(); err == nil {
		t.Error("a short passcode was taken")
	}
}
