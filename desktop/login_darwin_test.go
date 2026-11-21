package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Opening at login is a launch agent that opens Tinio with atLogin; turning it
// off removes it. (In a home folder of the test's own.)
func TestOpenAtLoginOnMac(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if on, err := opensAtLogin(); err != nil || on {
		t.Fatalf("before: %v, %v", on, err)
	}
	if err := setOpenAtLogin(true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", "app.tinio.desktop.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>app.tinio.desktop</string>", "<string>" + atLogin + "</string>", "<key>RunAtLoad</key>"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the launch agent has no %s:\n%s", want, b)
		}
	}
	if on, _ := opensAtLogin(); !on {
		t.Error("on, but not said to be")
	}
	if err := setOpenAtLogin(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := opensAtLogin(); on {
		t.Error("off, but still on")
	}
	if err := setOpenAtLogin(false); err != nil {
		t.Errorf("turning it off twice: %v", err)
	}
}

func TestBundle(t *testing.T) {
	if got := bundle("/Applications/Tinio.app/Contents/MacOS/Tinio"); got != "/Applications/Tinio.app" {
		t.Errorf("in a bundle: %q", got)
	}
	if got := bundle("/usr/local/bin/tinio-desktop"); got != "" {
		t.Errorf("not in one: %q", got)
	}
}
