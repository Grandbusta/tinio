package utils

import (
	"strings"
	"testing"
)

// Every email renders on the template, in HTML and plain text, with what
// people wrote escaped.
func TestAMessageRenders(t *testing.T) {
	html, text, err := Message{To: "a@example.com", Subject: "S", Heading: "Your sign-in code", Paras: []string{"Enter it."}, Code: "428193",
		Note: "<script>x</script>", NoteTitle: "What the reviewer said", Fine: "Ignore it.", Why: "You asked."}.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "428193") || strings.Contains(html, "<script>x") || !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("html:\n%s", html)
	}
	if !strings.Contains(text, "428193") || !strings.Contains(text, "You asked.") {
		t.Errorf("text:\n%s", text)
	}
}
