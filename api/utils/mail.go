package utils

import (
	"bytes"
	"context"
	_ "embed"
	htmltemplate "html/template"
	"log/slog"
	texttemplate "text/template"

	"github.com/Grandbusta/tinio/api/config"
)

// Message is an email in Tinio's one template: every email the platform
// sends fills in these parts and looks the same (the design's Email). It's
// sent as HTML, with a plain-text twin made from the same parts.
type Message struct {
	To      string
	Subject string
	Preview string // what the inbox shows after the subject
	Heading string
	Paras   []string
	Code    string // a sign-in code, in its box; never with a Button
	// Note is someone's words in a box of their own, under NoteTitle (a
	// reviewer's note to a skill's author).
	Note      string
	NoteTitle string
	Button    string // "": none
	Link      string // where Button goes
	Fine      string // the small print: what to do if it wasn't for them
	Why       string // why they got this email, in the footer
}

var (
	//go:embed letter.html
	letterHTML string
	//go:embed letter.txt
	letterText string

	htmlLetter = htmltemplate.Must(htmltemplate.New("letter").Parse(letterHTML))
	textLetter = texttemplate.Must(texttemplate.New("letter").Parse(letterText))
)

// Render is m as HTML and as plain text.
func (m Message) Render() (html, text string, err error) {
	data := struct {
		Message
		Site string
	}{m, config.Site}
	var h, t bytes.Buffer
	if err := htmlLetter.Execute(&h, data); err != nil {
		return "", "", err
	}
	if err := textLetter.Execute(&t, data); err != nil {
		return "", "", err
	}
	return h.String(), t.String(), nil
}

// Mailer sends the platform's emails.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// LogMailer writes each message to the log instead of sending it, for running
// locally. A sign-in code is in the log, so it must never run in production.
type LogMailer struct{ Log *slog.Logger }

func (l LogMailer) Send(_ context.Context, m Message) error {
	_, text, err := m.Render()
	if err != nil {
		return err
	}
	l.Log.Info("email (not sent: local)", "to", m.To, "subject", m.Subject, "text", text)
	return nil
}
