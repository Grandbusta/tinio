package main

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
)

// On macOS, opening at login is a launch agent of the user's: a file in
// ~/Library/LaunchAgents that opens the app (by its bundle, so it's the app
// macOS knows) with atLogin.

const loginLabel = "app.tinio.desktop"

func loginFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", loginLabel+".plist"), nil
}

func opensAtLogin() (bool, error) {
	f, err := loginFile()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(f)
	return err == nil, nil
}

func setOpenAtLogin(on bool) error {
	f, err := loginFile()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	args := []string{exe, atLogin}
	if b := bundle(exe); b != "" {
		args = []string{"/usr/bin/open", "-a", b, "--args", atLogin}
	}
	var list bytes.Buffer
	for _, a := range args {
		list.WriteString("\t\t<string>")
		xml.EscapeText(&list, []byte(a))
		list.WriteString("</string>\n")
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + loginLabel + `</string>
	<key>ProgramArguments</key>
	<array>
` + list.String() + `	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(plist), 0o644)
}
