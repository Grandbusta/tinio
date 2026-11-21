// Package agentskill loads know-how in the Agent Skills format: a folder
// with a SKILL.md (YAML frontmatter with a name and a description, then the
// instructions in Markdown) and any files it points to. A skill is trusted
// like a prompt, since its owner installed it; it grants no permissions, and
// scripts in it are never run, only read.
package agentskill

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxFiles    = 100
	maxStored   = 2 << 20  // bytes of one file kept
	maxTotal    = 10 << 20 // bytes of a whole skill kept
	maxFileSize = 50_000   // bytes of a file it reads
)

type Skill struct {
	Name        string // its handle (Handle)
	Written     string // its name as its SKILL.md wrote it, when that wasn't already a handle; "" if it was
	Description string
	Body        string
	// Files are all of it by slash-separated path, SKILL.md included.
	Files map[string][]byte
}

// FromDir reads a skill's folder, which must be named like the skill.
func FromDir(dir string) (Skill, error) {
	files, total := map[string][]byte{}, 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 { // never a way out of the folder
			return nil
		}
		if len(files) == maxFiles {
			return fmt.Errorf("more than %d files", maxFiles)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if total += len(raw); len(raw) > maxStored || total > maxTotal {
			return fmt.Errorf("too big: at most %d MB a file and %d MB in all", maxStored>>20, maxTotal>>20)
		}
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		return Skill{}, err
	}
	s, err := Parse(files)
	if err != nil {
		return Skill{}, err
	}
	if s.Name != filepath.Base(dir) {
		return Skill{}, fmt.Errorf("name %q must match its folder, %q", s.Name, filepath.Base(dir))
	}
	return s, nil
}

// FromUpload reads a skill someone uploaded: a SKILL.md alone, or a .zip of its
// folder (with the folder at its root, or its files there). The same limits as
// FromDir hold; paths that would leave the folder, and links, are refused.
func FromUpload(filename string, data []byte) (Skill, error) {
	if !strings.EqualFold(path.Ext(filename), ".zip") {
		if len(data) > maxStored {
			return Skill{}, fmt.Errorf("too big: at most %d MB", maxStored>>20)
		}
		return Parse(map[string][]byte{"SKILL.md": data})
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Skill{}, fmt.Errorf("not a zip file: %w", err)
	}
	files, total := map[string][]byte{}, 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(path.Base(f.Name), ".") || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		if f.Mode()&fs.ModeSymlink != 0 {
			return Skill{}, fmt.Errorf("%s is a link; a skill's files must be files", f.Name)
		}
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return Skill{}, fmt.Errorf("%s points outside the skill", f.Name)
		}
		if len(files) == maxFiles {
			return Skill{}, fmt.Errorf("more than %d files", maxFiles)
		}
		if f.UncompressedSize64 > maxStored {
			return Skill{}, fmt.Errorf("%s is too big: at most %d MB a file", f.Name, maxStored>>20)
		}
		rc, err := f.Open()
		if err != nil {
			return Skill{}, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, maxStored+1))
		rc.Close()
		if err != nil {
			return Skill{}, err
		}
		if total += len(raw); len(raw) > maxStored || total > maxTotal {
			return Skill{}, fmt.Errorf("too big: at most %d MB a file and %d MB in all", maxStored>>20, maxTotal>>20)
		}
		files[name] = raw
	}
	// A zipped folder: its files are inside one top folder.
	if _, ok := files["SKILL.md"]; !ok {
		top := ""
		for name := range files {
			dir, _, found := strings.Cut(name, "/")
			if !found || (top != "" && dir != top) {
				top = ""
				break
			}
			top = dir
		}
		if top != "" {
			inner := make(map[string][]byte, len(files))
			for name, raw := range files {
				inner[strings.TrimPrefix(name, top+"/")] = raw
			}
			files = inner
		}
	}
	return Parse(files)
}

// Parse makes a skill of its files; fs.ErrNotExist if there's no SKILL.md.
// It forgives what people writing one by hand get wrong: frontmatter that
// isn't quite YAML (an unquoted colon in a description) is read line by line,
// and a name like "Brand Voice" is made the handle brand-voice (Written keeps
// it as they wrote it). Errors are said for people, not programmers.
func Parse(files map[string][]byte) (Skill, error) {
	raw, ok := files["SKILL.md"]
	if !ok {
		return Skill{}, fmt.Errorf("SKILL.md: %w", fs.ErrNotExist)
	}
	front, body, ok := frontmatter(raw)
	if !ok {
		return Skill{}, errors.New("the top of its SKILL.md needs a name: line and a description: line, between two --- lines")
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if yaml.Unmarshal(front, &meta) != nil {
		meta.Name, meta.Description = line(front, "name"), line(front, "description")
	}
	written := strings.TrimSpace(meta.Name)
	handle := Handle(written)
	desc := strings.TrimSpace(meta.Description)
	switch {
	case handle == "":
		return Skill{}, errors.New("its SKILL.md needs a name: line, with letters or numbers in it")
	case desc == "":
		return Skill{}, errors.New("its SKILL.md needs a description: line, saying what it's for and when to use it")
	case utf8.RuneCountInString(desc) > 1024:
		return Skill{}, errors.New("its description is too long: at most 1024 characters")
	}
	s := Skill{Name: handle, Description: desc, Body: strings.TrimSpace(string(body)), Files: files}
	if written != handle {
		s.Written = written
	}
	return s, nil
}

// line is the value after key: on its own line in front, quotes taken off; ""
// if there's none. For frontmatter that isn't valid YAML.
func line(front []byte, key string) string {
	for _, l := range strings.Split(string(front), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

var notHandle = regexp.MustCompile(`[^a-z0-9]+`)

// Handle makes name a skill's handle: lowercase words joined by hyphens, at
// most 64 characters ("Brand Voice" and "brand_voice" are brand-voice). It's
// how the agent asks for the skill, and what a skill of the same name replaces.
func Handle(name string) string {
	h := strings.Trim(notHandle.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(h) > 64 {
		h = strings.TrimRight(h[:64], "-")
	}
	return h
}

// Title is how a skill is named for people: its instructions' first heading,
// or else its handle in words ("brand-voice" is "Brand voice").
func Title(name, body string) string {
	for _, l := range strings.Split(body, "\n") {
		if t, ok := strings.CutPrefix(strings.TrimSpace(l), "# "); ok && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t)
		}
		if strings.TrimSpace(l) != "" {
			break
		}
	}
	words := strings.ReplaceAll(name, "-", " ")
	if words == "" {
		return ""
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// Others are its files besides SKILL.md, sorted.
func (s Skill) Others() []string {
	var out []string
	for f := range s.Files {
		if f != "SKILL.md" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// frontmatter splits a file that starts with a --- line into what's
// between it and the next --- line, and the rest.
func frontmatter(raw []byte) (front, body []byte, ok bool) {
	raw = bytes.TrimPrefix(raw, []byte("\uFEFF"))
	rest, ok := bytes.CutPrefix(raw, []byte("---\n"))
	if !ok {
		rest, ok = bytes.CutPrefix(raw, []byte("---\r\n"))
	}
	if !ok {
		return nil, nil, false
	}
	front, body, ok = bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return nil, nil, false
	}
	_, body, _ = bytes.Cut(body, []byte("\n"))
	return front, body, true
}

// Read is one of its files besides SKILL.md. One that isn't text (an
// image, a PDF, a font) is only described: its bytes would mean nothing
// read as words.
func (s Skill) Read(file string) (string, error) {
	file = path.Clean(strings.TrimPrefix(strings.TrimSpace(file), "./"))
	raw, ok := s.Files[file]
	if !ok || file == "SKILL.md" {
		return "", fmt.Errorf("%s has no file %q (it has %s)", s.Name, file, strings.Join(s.Others(), ", "))
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		kind, _, _ := strings.Cut(http.DetectContentType(raw), ";")
		return fmt.Sprintf("%s isn't text (%s, %s): it can't be read as words, only used as it is.", file, kind, size(len(raw))), nil
	}
	if len(raw) > maxFileSize {
		raw = raw[:maxFileSize]
	}
	return strings.ToValidUTF8(string(raw), ""), nil
}

func size(n int) string {
	if n < 1<<10 {
		return fmt.Sprintf("%d bytes", n)
	}
	return fmt.Sprintf("%d KB", (n+1<<10-1)>>10)
}
