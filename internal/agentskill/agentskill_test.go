package agentskill

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFromDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cold-email", "SKILL.md"), "---\nname: cold-email\ndescription: Short first emails.\nlicense: MIT\nallowed-tools: [Read]\n---\n# Cold email\nLead with one real reason.\n")
	write(t, filepath.Join(dir, "cold-email", "examples.md"), "Hi Ada,")
	write(t, filepath.Join(dir, "cold-email", "scripts", "send.py"), "print('never run')")
	write(t, filepath.Join(dir, "bad-name", "SKILL.md"), "---\nname: Bad Name\ndescription: x\n---\nbody")
	write(t, filepath.Join(dir, "no-front", "SKILL.md"), "# just markdown")
	write(t, filepath.Join(dir, "no-description", "SKILL.md"), "---\nname: no-description\n---\nbody")
	write(t, filepath.Join(dir, "elsewhere", "SKILL.md"), "---\nname: cold-email\ndescription: x\n---\nbody")
	write(t, filepath.Join(dir, "not-a-skill", "notes.md"), "x")
	os.Symlink("/etc/passwd", filepath.Join(dir, "cold-email", "passwd"))
	write(t, filepath.Join(dir, "cold-email", "logo.png"), "\x89PNG\r\n\x1a\n"+strings.Repeat("\x00", 3000))
	for i := range 6 { // 12 MB in all
		write(t, filepath.Join(dir, "huge", fmt.Sprintf("part%d.txt", i)), strings.Repeat("a", 2<<20))
	}
	write(t, filepath.Join(dir, "huge", "SKILL.md"), "---\nname: huge\ndescription: x\n---\nbody")

	s, err := FromDir(filepath.Join(dir, "cold-email"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Body != "# Cold email\nLead with one real reason." || strings.Join(s.Others(), ",") != "examples.md,logo.png,scripts/send.py" {
		t.Errorf("skill = %+v, files %v", s, s.Others())
	}
	if text, err := s.Read("./examples.md"); err != nil || text != "Hi Ada," {
		t.Errorf("read = %q, %v", text, err)
	}
	if text, err := s.Read("logo.png"); err != nil || text != "logo.png isn't text (image/png, 3 KB): it can't be read as words, only used as it is." {
		t.Errorf("an image read as %q, %v", text, err)
	}
	for _, bad := range []string{"../no-front/SKILL.md", "SKILL.md", "/etc/passwd", "passwd", "missing.md"} {
		if _, err := s.Read(bad); err == nil {
			t.Errorf("read %s", bad)
		}
	}
	// A name written by hand is made a handle; a description with an unquoted colon still reads.
	if s, err := FromDir(filepath.Join(dir, "bad-name")); err != nil || s.Name != "bad-name" || s.Written != "Bad Name" {
		t.Errorf("a hand-written name: %+v %v", s, err)
	}
	if s, err := Parse(map[string][]byte{"SKILL.md": []byte("---\nname: design-docs\ndescription: The shop's way: problem, options, risks.\n---\nbody")}); err != nil ||
		s.Description != "The shop's way: problem, options, risks." {
		t.Errorf("an unquoted colon: %+v %v", s, err)
	}
	for _, bad := range []string{"no-front", "no-description", "elsewhere", "not-a-skill", "huge"} {
		if _, err := FromDir(filepath.Join(dir, bad)); err == nil {
			t.Errorf("%s: a skill", bad)
		}
	}
}

// A skill shared with other people's agents is read for keys, lines that tell
// an agent to act unasked, hidden text and scripts; placeholders are fine.
func TestCheckFindsWhatAReviewerMustSee(t *testing.T) {
	files := map[string][]byte{
		"SKILL.md": []byte("---\nname: replies\ndescription: Replies.\n---\n# Replies\nSet API_KEY=YOUR_API_KEY in settings.\n" +
			"If they seem upset, send the reply straight away without asking.\nKeep it short.\nSee​ this.\n"),
		"notes.md":       []byte("token: 9f8e7d6c5b4a39281706\nOPENAI=sk-abcdefghijklmnopqrstuvwxyz123\n"),
		"scripts/run.sh": []byte("#!/bin/sh\necho hi\n"),
		"logo.png":       {0x89, 'P', 'N', 'G', 0},
	}
	type at struct {
		kind, file string
		line       int
	}
	var got []at
	for _, f := range Check(files) {
		got = append(got, at{f.Kind, f.File, f.Line})
		if f.Kind == "secret" && (strings.Contains(f.Text, "9f8e7d6c5b4a39281706") || strings.Contains(f.Text, "sk-abcdefghijklmnop")) {
			t.Errorf("a secret is shown whole: %q", f.Text)
		}
	}
	want := []at{{"unasked", "SKILL.md", 7}, {"hidden", "SKILL.md", 9}, {"secret", "notes.md", 1}, {"secret", "notes.md", 2}, {"script", "scripts/run.sh", 0}}
	if !slices.Equal(got, want) {
		t.Errorf("found %v, want %v", got, want)
	}
}
