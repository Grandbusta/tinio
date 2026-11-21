//go:build tools

// Package tools pins the wails command the desktop app is built with: Wails
// v2.12.0's own, on current golang.org/x/tools, which reads what Go 1.27
// compiles (the one Wails ships with can't). Build it with
//
//	go build -o ../../bin/wails github.com/wailsapp/wails/v2/cmd/wails
package tools

import _ "github.com/wailsapp/wails/v2/cmd/wails"
