package main

import (
	"os"
	"path/filepath"
	"strings"
)

// On Linux, opening at login is a desktop entry in the user's autostart folder.

func loginFile() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "tinio.desktop"), nil
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
	quoted := `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
	entry := "[Desktop Entry]\nType=Application\nName=Tinio\nComment=Agents with lives of their own\nExec=" + quoted + " " + atLogin + "\nX-GNOME-Autostart-enabled=true\n"
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(entry), 0o644)
}
