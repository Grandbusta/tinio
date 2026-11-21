# Tinio for your desktop

Tinio as an app: the agents and the API run inside it, and its window is the app you
raise them and talk to them in. Nothing listens on a port, and there's no sign-in: the
window is yours. Your data is in the same folder as the `tinio` command's (see
[SETUP](../docs/SETUP.md)), so use one or the other at a time, not both.

It's built with [Wails](https://wails.io) v2. The window is a React app (`frontend/`); its
calls to `/api/...` go straight to the API in the same program (`main.go`), and what's
said in a chat comes to it as the app's events rather than a stream.

## Build it

You need Go 1.27+, Node 20.19+, and on Linux the WebKitGTK development packages
(see Wails' [installation guide](https://wails.io/docs/gettingstarted/installation)).

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
cd desktop
wails build -skipbindings
```

The app is in `build/bin` (`Tinio.app` on macOS). Open it, give it your OpenAI key, and
raise your first agent.

## While it runs

Your agents live their days only while Tinio runs, so it keeps running:

- **Closing the window doesn't quit.** On macOS Tinio hides, like Cmd+H, and clicking it
  in the Dock brings it back; elsewhere the window goes to the taskbar. Quit from the
  menu (macOS) or with Quit Tinio on You. Opening Tinio again while it runs brings back
  its window.
- **It opens when you log in**, out of the way (minimised), set the first time it runs.
  Turn it off on You. It's a launch agent on macOS
  (`~/Library/LaunchAgents/app.tinio.desktop.plist`), a value under the Run key on
  Windows, and `~/.config/autostart/tinio.desktop` on Linux. A `wails dev` build never
  sets it.
- **What an agent says while you're elsewhere is a notification**: their name and what
  they said, once, and clicking it opens their chat. macOS asks the first time; allow
  Tinio in System Settings, Notifications, if you said no.
- **Their files** (a PDF, a spreadsheet) are saved where you choose: the window can't
  download, so the app asks.

If `wails` stops with "package ... without types was imported", your Go is newer than the
Wails CLI's own tools. Build the CLI with current ones instead:

```bash
mkdir /tmp/wails-cli && cd /tmp/wails-cli && go mod init wails-cli
printf '//go:build tools\npackage tools\nimport _ "github.com/wailsapp/wails/v2/cmd/wails"\n' > tools.go
go get github.com/wailsapp/wails/v2@v2.12.0 golang.org/x/tools@latest && go mod tidy
go build -o "$(go env GOPATH)/bin/wails" github.com/wailsapp/wails/v2/cmd/wails
```

## Work on it

`wails dev -skipbindings` runs the app with the window reloading as you change the
frontend; the same window is at http://localhost:34115 in a browser.

The screens are a copy of the web app's, kept apart and synced by hand. Their `next/link`
and `next/navigation` imports are small shims over the router (`frontend/src/shims`), so a
screen copied across needs no changes for that. To work on them against a `tinio` command
instead:

```bash
cd desktop/frontend
TINIO_API=http://127.0.0.1:8081 APP_ORIGIN=http://localhost:5173 npm run dev
```

with `APP_ORIGIN=http://localhost:5173` set where `tinio` runs too.
