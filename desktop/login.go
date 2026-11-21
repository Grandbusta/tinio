package main

import (
	"os"
	"path/filepath"
	"strings"
)

// atLogin is the argument Tinio is opened with at login: it starts out of the
// way, its agents running, with no window in front of you.
const atLogin = "--at-login"

// openedAtLogin is whether this run was started at login.
func openedAtLogin() bool {
	for _, a := range os.Args[1:] {
		if a == atLogin {
			return true
		}
	}
	return false
}

// bundle is the .app the program is in on macOS ("" if it isn't in one).
func bundle(exe string) string {
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return exe[:i+len(".app")]
	}
	return ""
}

// firstOpen turns opening at login on the first time Tinio runs, so agents
// keep their days without you remembering to open it; You turns it off. It's
// done once, noted in the data folder, so turning it off sticks. A dev build
// never does it.
func firstOpen(dataDir string) error {
	if devBuild {
		return nil
	}
	mark := filepath.Join(dataDir, "login-set")
	if _, err := os.Stat(mark); err == nil {
		return nil
	}
	if err := setOpenAtLogin(true); err != nil {
		return err
	}
	return os.WriteFile(mark, []byte("Tinio set itself to open at login once; You turns it off.\n"), 0o600)
}
