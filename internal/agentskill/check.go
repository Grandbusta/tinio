package agentskill

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Finding is something in a skill's files to read before it's shared with
// other people's agents.
type Finding struct {
	// Kind is secret (what looks like a key, password or token), unasked (a
	// line that may tell an agent to act without asking, or to set its rules
	// aside), hidden (text people can't see: invisible characters, HTML
	// comments) or script (a file that would run, which is never run).
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"` // from 1; 0 is the file as a whole
	Text string `json:"text"` // the line, cut short
}

var (
	secretKey   = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}|\bAKIA[0-9A-Z]{16}\b|-----BEGIN [A-Z ]*PRIVATE KEY-----|\bgh[pousr]_[A-Za-z0-9]{30,}|\bxox[abpr]-[A-Za-z0-9-]{10,}|\bAIza[0-9A-Za-z_-]{35}`)
	secretPair  = regexp.MustCompile(`(?i)\b(password|passwd|api[_ -]?key|secret|token)\b\s*[:=]\s*["']?([^\s"'<>]{8,})`)
	placeholder = regexp.MustCompile(`^[A-Z_.{}$-]+$|(?i)your|example|x{4,}|\*{3,}|\.\.\.`)
	unaskedLine = regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override|set aside)\b.{0,40}\b(instructions|rules|settings|permissions|owner|guidelines)\b|\bwithout (asking|approval|permission|checking|their (ok|yes|approval))\b|\b(don't|do not|never|no need to)\s+(ask|wait for|check with|get approval)\b|\bbypass|\bskip(ping)? (the )?(approval|owner|asking)\b|\b(straight away|right away|immediately|automatically)\b.{0,30}\b(send|post|publish|pay|transfer|buy|delete|email)\b`)
	hiddenText  = regexp.MustCompile("[\u200B-\u200F\u202A-\u202E\u2060-\u2064\u2066-\u2069\uFEFF]|[\U000E0000-\U000E007F]|<!--")
)

// secret says if l holds what looks like a real key, password or token, not
// a placeholder like YOUR_API_KEY.
func secret(l string) bool {
	if secretKey.MatchString(l) {
		return true
	}
	for _, m := range secretPair.FindAllStringSubmatch(l, -1) {
		if !placeholder.MatchString(m[2]) {
			return true
		}
	}
	return false
}

var scripts = []string{".sh", ".bash", ".zsh", ".py", ".js", ".mjs", ".ts", ".rb", ".pl", ".php", ".ps1", ".bat", ".cmd", ".exe", ".jar", ".bin"}

// Check reads every file of a skill for what a reviewer, or its author,
// should see before it reaches other people's agents; nil if there's
// nothing. It's a help, not a judge: a person still reads the skill.
func Check(files map[string][]byte) []Finding {
	var out []Finding
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		raw := files[p]
		text := utf8.Valid(raw) && !slices.Contains(raw, 0)
		if slices.Contains(scripts, strings.ToLower(path.Ext(p))) || !text && strings.HasPrefix(string(raw), "\x7fELF") || strings.HasPrefix(string(raw), "#!") {
			out = append(out, Finding{Kind: "script", File: p})
		}
		if !text {
			continue
		}
		for i, l := range strings.Split(string(raw), "\n") {
			if i == 0 {
				l = strings.TrimPrefix(l, "\uFEFF")
			}
			switch {
			case secret(l):
				out = append(out, Finding{Kind: "secret", File: p, Line: i + 1, Text: shown(l, "secret")})
			case unaskedLine.MatchString(l):
				out = append(out, Finding{Kind: "unasked", File: p, Line: i + 1, Text: shown(l, "unasked")})
			}
			if hiddenText.MatchString(l) {
				out = append(out, Finding{Kind: "hidden", File: p, Line: i + 1, Text: shown(l, "hidden")})
			}
		}
	}
	return out
}

// shown is a line as a reviewer sees it: short, a secret's value masked, and
// invisible characters made visible.
func shown(l, kind string) string {
	l = strings.TrimSpace(l)
	switch kind {
	case "secret":
		mask := func(s string) string { r := []rune(s); return string(r[:min(len(r), 6)]) + "…" }
		l = secretKey.ReplaceAllStringFunc(l, mask)
		l = secretPair.ReplaceAllStringFunc(l, func(s string) string {
			m := secretPair.FindStringSubmatchIndex(s)
			return s[:m[4]] + mask(s[m[4]:m[5]])
		})
	case "hidden":
		l = hiddenText.ReplaceAllStringFunc(l, func(s string) string {
			if s == "<!--" {
				return s
			}
			return "[hidden]"
		})
	}
	if r := []rune(l); len(r) > 160 {
		l = string(r[:160]) + "…"
	}
	return l
}
