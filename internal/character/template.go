package character

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Template is a character to raise: its files, with {owner}, {relation},
// {account}, {circle} and {timezone} filled in for whoever raises it.
type Template struct {
	Key       string            `yaml:"-"`
	Docs      map[string][]byte `yaml:"-"` // by file name: TemplateFiles
	Name      string            `yaml:"name"`
	About     string            `yaml:"about"`
	Relations []string          `yaml:"relations"`
}

// Files are what's rendered for the owner; TemplateFiles adds what describes
// the template.
var (
	Files         = []string{"character.yaml", "judgment.yaml"}
	TemplateFiles = append([]string{"template.yaml"}, Files...)
)

type Owner struct {
	Name     string
	Relation string // one of the template's Relations
	Account  string
	Timezone string // the owner's, like Africa/Lagos; "": UTC
}

// Templates are the characters in dir, in files.
func Templates(files fs.FS, dir string) ([]Template, error) {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, fmt.Errorf("read templates: %w", err)
	}
	var out []Template
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := LoadTemplate(files, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func LoadTemplate(files fs.FS, dir string) (Template, error) {
	docs := map[string][]byte{}
	for _, f := range TemplateFiles {
		raw, err := fs.ReadFile(files, path.Join(dir, f))
		if err != nil {
			return Template{}, fmt.Errorf("%s: %s is missing", dir, f)
		}
		docs[f] = raw
	}
	return ParseTemplate(path.Base(dir), docs)
}

// ParseTemplate is a template from its files by name, as a folder or the
// database holds them.
func ParseTemplate(key string, docs map[string][]byte) (Template, error) {
	var t Template
	dec := yaml.NewDecoder(bytes.NewReader(docs["template.yaml"]))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return Template{}, fmt.Errorf("%s/template.yaml: %w", key, err)
	}
	t.Key, t.Docs, t.Name, t.About = key, docs, strings.TrimSpace(t.Name), strings.TrimSpace(t.About)
	t.Relations = nonEmpty(t.Relations)
	if t.Name == "" || t.About == "" || len(t.Relations) == 0 {
		return Template{}, fmt.Errorf("%s/template.yaml: needs a name, about and at least one relation", key)
	}
	for _, f := range Files {
		if len(docs[f]) == 0 {
			return Template{}, fmt.Errorf("%s: %s is missing", key, f)
		}
	}
	return t, nil
}

// Render returns the folder's files by name, with the owner and the circle
// their relation is from filled in.
func (t Template) Render(o Owner) (map[string][]byte, error) {
	o.Name, o.Relation, o.Account = strings.TrimSpace(o.Name), strings.TrimSpace(o.Relation), strings.TrimSpace(o.Account)
	switch {
	case o.Name == "" || strings.ContainsAny(o.Name, "\"\\\n{}"):
		return nil, errors.New("your name can't be empty or have quotes, backslashes or braces in it")
	case !slices.Contains(t.Relations, o.Relation):
		return nil, fmt.Errorf("to %s you can be: %s", t.Name, strings.Join(t.Relations, ", "))
	case strings.ContainsAny(o.Account, "\"\\\n{} "):
		return nil, fmt.Errorf("account %q isn't one", o.Account)
	}
	if o.Timezone = strings.TrimSpace(o.Timezone); o.Timezone == "" {
		o.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(o.Timezone); err != nil || strings.ContainsAny(o.Timezone, "\"\\\n{} ") {
		return nil, fmt.Errorf("%q isn't a time zone like Africa/Lagos", o.Timezone)
	}
	fill := strings.NewReplacer("{owner}", o.Name, "{relation}", o.Relation, "{account}", o.Account, "{circle}", CircleOf(o.Relation),
		"{timezone}", o.Timezone)
	out := map[string][]byte{}
	for _, f := range Files {
		out[f] = []byte(fill.Replace(string(t.Docs[f])))
	}
	return out, nil
}
