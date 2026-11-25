package folders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/mind"
	"github.com/Grandbusta/tinio/internal/tools"
)

// Store is the folders added, in the database, and where a changed file's
// old version is kept (Versions, in Tinio's data folder).
type Store struct {
	DB       *db.DB
	Rules    Rules
	Versions string
}

// Given is the folders an agent was given, oldest first.
func (s Store) Given(ctx context.Context, agentID string) ([]Folder, error) {
	rows, err := s.DB.Query(ctx, `SELECT f.id, f.name, f.path, f.can_write FROM folders f
		JOIN folder_agents g ON g.folder_id = f.id WHERE g.agent_id = $1 ORDER BY f.created_at`, agentID)
	if err != nil {
		return nil, fmt.Errorf("folders: %w", err)
	}
	return db.CollectRows(rows, func(r db.Scanner) (Folder, error) {
		var f Folder
		return f, r.Scan(&f.ID, &f.Name, &f.Path, &f.Write)
	})
}

const (
	readRunes   = 30000 // read at a time; "from" reads on
	listMax     = 200
	searchMax   = 30
	searchFiles = 2000
	searchSize  = 2 << 20
	writeRunes  = 200000
)

// Tools is what fs give an agent: listing, searching and reading as
// abilities, limited to limit calls a day with the rest (st logs them); and,
// for folders they may write in, making and changing files, only ever
// proposed. With coding, they have coding jobs: a folder that's a git
// repository is changed by one, on its own branch and reviewed, never directly.
func (s Store) Tools(fs []Folder, st tools.Store, limit int, coding bool) (reads []ability.Ability, doers []mind.Doer) {
	if len(fs) == 0 {
		return nil, nil
	}
	k := kit{s: s, fs: fs, st: st, limit: limit, coding: coding}
	repos := ""
	if coding {
		repos = " Not in a folder that's a git repository: there, start a coding job (code.start) instead, even for one line."
	}
	names := k.names()
	reads = []ability.Ability{
		read{k: k, name: "folders.list", about: "List what's in the owner's folders on this computer: " + names + `. Query: {"folder": "its name", "path": "a folder inside it, or nothing for the top"}.`, run: k.list},
		read{k: k, name: "folders.search", about: `Search the owner's folders for files whose name or words have all of the query's words, in any order; it returns the lines that match, which often answer a question without reading the file. Query: {"query": "a few words", "folder": "its name, or nothing for all"}.`, run: k.search},
		read{k: k, name: "folders.read", about: `Read a file in the owner's folders: text, Markdown, Word, spreadsheets and PDFs. Query: {"folder": "its name", "file": "its path inside the folder", "from": "where to read on from, if it said there's more"}.`, run: k.read},
	}
	for _, f := range fs {
		if f.Write {
			doers = []mind.Doer{
				do{k: k, name: "folders.make_file", about: `Make a new text file (.md, .txt, .csv, .json, .html, ...) in one of the owner's folders you may make files in. Arguments: {"folder": "its name", "file": "its path inside the folder", "text": "all its words"}. It never replaces a file that's there.` + repos, check: k.checkMake, run: k.makeFile, explain: k.explainMake},
				do{k: k, name: "folders.change_file", about: `Change a text file in one of the owner's folders you may make files in, by replacing one passage that appears once. Arguments: {"folder": "its name", "file": "its path inside the folder", "find": "the exact words to replace", "replace": "what goes there instead"}. The old version is kept.` + repos, check: k.checkChange, run: k.change, explain: k.explainChange},
			}
			break
		}
	}
	return reads, doers
}

type kit struct {
	s      Store
	fs     []Folder
	st     tools.Store
	limit  int
	coding bool // a git repository is changed by a coding job, not here
}

func (k kit) names() string {
	var out []string
	for _, f := range k.fs {
		may := "read"
		if f.Write {
			may = "read, and make files with the owner's yes"
		}
		out = append(out, fmt.Sprintf("%q (%s)", f.Name, may))
	}
	return strings.Join(out, ", ")
}

// folder is the one named (by name, any case, or id); with one folder given,
// "" is it.
func (k kit) folder(name string) (Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" && len(k.fs) == 1 {
		return k.fs[0], nil
	}
	for _, f := range k.fs {
		if strings.EqualFold(f.Name, name) || f.ID == name {
			return f, nil
		}
	}
	return Folder{}, fmt.Errorf("no folder %q: yours are %s", name, k.names())
}

// --- Reading ---

type read struct {
	k     kit
	name  string
	about string
	run   func(args map[string]string) (string, error)
}

func (r read) Name() string       { return r.name }
func (r read) Describe() string   { return r.about }
func (r read) Risk() ability.Risk { return ability.UsesTools }

func (r read) Prepare(ctx context.Context, a ability.Act) (ability.Material, error) {
	args := map[string]string{}
	if q := strings.TrimSpace(a.Query()); q != "" && q != "{}" {
		if err := json.Unmarshal([]byte(q), &args); err != nil {
			return ability.Material{}, fmt.Errorf("%s: its query isn't a JSON object of strings: %w", r.name, err)
		}
	}
	if err := tools.Under(ctx, r.k.st, a, r.k.limit); err != nil {
		return ability.Material{}, err
	}
	text, err := r.run(args)
	if logErr := tools.Log(ctx, r.k.st, r.name, a.Query(), a.Now); logErr != nil {
		return ability.Material{}, logErr
	}
	if err != nil {
		return ability.Material{}, err
	}
	note := fmt.Sprintf("\n\nYou used %s (the owner's folders on this computer) with %s; it returned this.%s", r.name, orNothing(a.Query()), ability.Fence(text))
	return ability.Material{Note: note, Project: a.Project}, nil
}

func (k kit) list(args map[string]string) (string, error) {
	if args["folder"] == "" && len(k.fs) > 1 {
		return "The folders: " + k.names() + ". List one by its name.", nil
	}
	f, err := k.folder(args["folder"])
	if err != nil {
		return "", err
	}
	dir, err := k.s.Rules.Resolve(f, args["path"])
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("%q isn't a folder in %s", args["path"], f.Name)
	}
	root, _ := f.Root()
	var b strings.Builder
	shown, left := 0, 0
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.Type()&os.ModeSymlink != 0 || !k.s.Rules.allowed(root, p) {
			left++
			continue
		}
		if shown == listMax {
			fmt.Fprintf(&b, "... and more: list a folder inside it\n")
			break
		}
		shown++
		if e.IsDir() {
			fmt.Fprintf(&b, "%s/\n", e.Name())
			continue
		}
		if info, err := e.Info(); err == nil {
			fmt.Fprintf(&b, "%s (%s, %s)\n", e.Name(), size(info.Size()), info.ModTime().Format("2 Jan 2006"))
		}
	}
	where := f.Name
	if p := strings.Trim(filepath.ToSlash(args["path"]), "/"); p != "" {
		where += " › " + strings.ReplaceAll(p, "/", " › ")
	}
	if shown == 0 {
		b.WriteString("(nothing here it may see)\n")
	}
	if left > 0 {
		fmt.Fprintf(&b, "(%d left out: hidden files, keys and shortcuts are never shown)\n", left)
	}
	return where + ":\n" + b.String(), nil
}

func (k kit) search(args map[string]string) (string, error) {
	words := strings.Fields(strings.ToLower(args["query"]))
	if len(words) == 0 {
		return "", errors.New(`search needs {"query": "words"}`)
	}
	fs := k.fs
	if args["folder"] != "" {
		f, err := k.folder(args["folder"])
		if err != nil {
			return "", err
		}
		fs = []Folder{f}
	}
	var hits []string
	for _, f := range fs {
		root, err := f.Root()
		if err != nil {
			continue
		}
		scanned := 0
		_, _, err = k.s.Rules.Walk(f, 20000, func(e Entry) bool {
			if len(hits) >= searchMax {
				return false
			}
			name := filepath.Base(e.Rel)
			if all(strings.ToLower(name), words) {
				hits = append(hits, fmt.Sprintf("%s › %s (its name)", f.Name, e.Rel))
				return true
			}
			if e.Dir || e.Size > searchSize || scanned >= searchFiles || !searchable(name) {
				return true
			}
			scanned++
			text, err := Text(filepath.Join(root, filepath.FromSlash(e.Rel)))
			if err != nil || !all(strings.ToLower(text), words) {
				return true
			}
			hits = append(hits, fmt.Sprintf("%s › %s:\n%s", f.Name, e.Rel, matching(text, words)))
			return true
		})
		if err != nil {
			return "", err
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("Nothing in %s has all of %q, in its name or its words. Try fewer words, or list the folder.", folderNames(fs), args["query"]), nil
	}
	return strings.Join(hits, "\n"), nil
}

// all is whether every word is in text, in any order.
func all(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// matching is the lines of text with the most of words, at most three, each
// cut short: what answers a search without reading the whole file.
func matching(text string, words []string) string {
	type line struct {
		text string
		n    int
	}
	var best []line
	for _, l := range strings.Split(text, "\n") {
		low, n := strings.ToLower(l), 0
		for _, w := range words {
			if strings.Contains(low, w) {
				n++
			}
		}
		if n > 0 {
			best = append(best, line{strings.Join(strings.Fields(l), " "), n})
		}
	}
	sort.SliceStable(best, func(i, j int) bool { return best[i].n > best[j].n })
	var out []string
	for _, l := range best[:min(3, len(best))] {
		out = append(out, "  "+cut(l.text, 200))
	}
	return strings.Join(out, "\n")
}

// searchable is a file whose words are searched (a PDF only when read).
func searchable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf", ".png", ".jpg", ".jpeg", ".gif", ".heic", ".mp4", ".mov", ".mp3", ".zip", ".dmg", ".exe", ".app":
		return false
	}
	return true
}

func (k kit) read(args map[string]string) (string, error) {
	f, err := k.folder(args["folder"])
	if err != nil {
		return "", err
	}
	if args["file"] == "" {
		return "", errors.New(`read needs {"file": "its path inside the folder"}`)
	}
	p, err := k.s.Rules.Resolve(f, args["file"])
	if err != nil {
		return "", err
	}
	text, err := Text(p)
	if err != nil {
		return "", fmt.Errorf("%s › %s: %w", f.Name, args["file"], err)
	}
	from, _ := strconv.Atoi(args["from"])
	runes := []rune(text)
	if from < 0 || from > len(runes) {
		from = 0
	}
	end := min(from+readRunes, len(runes))
	out := string(runes[from:end])
	if end < len(runes) {
		out += fmt.Sprintf("\n\n(That's up to %d of %d characters; read on with \"from\": \"%d\".)", end, len(runes), end)
	}
	return fmt.Sprintf("%s › %s:\n%s", f.Name, filepath.ToSlash(args["file"]), out), nil
}

// --- Making and changing files ---

type do struct {
	k       kit
	name    string
	about   string
	check   func(args map[string]string) error
	run     func(args map[string]string, at time.Time) (string, error)
	explain func(args map[string]string) (title, where, quote string)
}

func (d do) Name() string        { return d.name }
func (d do) Describe() string    { return d.about }
func (d do) Class() action.Class { return action.Write }
func (d do) Destructive() bool   { return false } // they only add and change, keeping the old version

func (d do) Check(raw string) error {
	args, err := strArgs(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", d.name, err)
	}
	return d.check(args)
}

func (d do) Do(ctx context.Context, raw string, at time.Time) (string, error) {
	args, err := strArgs(raw)
	if err == nil {
		err = d.check(args)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", d.name, err)
	}
	text, err := d.run(args, at)
	if logErr := d.k.st.LogWeb(ctx, "action", d.name+" "+cut(raw, 500), 0, at.UTC()); logErr != nil {
		return "", logErr
	}
	return text, err
}

// Explain is what the owner's asked, for its card.
func (d do) Explain(raw string) (title, where, quote string) {
	args, err := strArgs(raw)
	if err != nil {
		return "", "", ""
	}
	return d.explain(args)
}

// writable is the folder named, only if files may be made in it, and the
// file's path in it, only if it's one that may be made or changed.
func (k kit) writable(args map[string]string) (Folder, string, error) {
	f, err := k.folder(args["folder"])
	if err != nil {
		return Folder{}, "", err
	}
	if !f.Write {
		return Folder{}, "", fmt.Errorf("%s is read only: the owner can let you make files there in Settings", f.Name)
	}
	if k.coding && isRepo(f.Path) {
		return Folder{}, "", fmt.Errorf("%s is a git repository: change it with a coding job (code.start), on its own branch, which the owner reviews; even one line", f.Name)
	}
	if !Writable(args["file"]) {
		return Folder{}, "", fmt.Errorf("%q: only text files (.md, .txt, .csv, .json, .html, ...) are made or changed", args["file"])
	}
	p, err := k.s.Rules.Resolve(f, args["file"])
	return f, p, err
}

func (k kit) checkMake(args map[string]string) error {
	_, p, err := k.writable(args)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(p); err == nil {
		return fmt.Errorf("%q is there already: change it instead", args["file"])
	}
	if strings.TrimSpace(args["text"]) == "" || utf8.RuneCountInString(args["text"]) > writeRunes {
		return fmt.Errorf("its text is empty or over %d characters", writeRunes)
	}
	return nil
}

func (k kit) makeFile(args map[string]string, _ time.Time) (string, error) {
	f, p, err := k.writable(args)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("couldn't make %s: %w", args["file"], err)
	}
	if _, err := file.WriteString(args["text"]); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return fmt.Sprintf("Made %s in %s.", filepath.ToSlash(args["file"]), f.Name), nil
}

func (k kit) explainMake(args map[string]string) (string, string, string) {
	f, _ := k.folder(args["folder"])
	return "Make " + filepath.Base(args["file"]) + " in " + f.Name + "?", where(f, args["file"]), cut(args["text"], 1500)
}

func (k kit) checkChange(args map[string]string) error {
	_, p, err := k.writable(args)
	if err != nil {
		return err
	}
	if args["find"] == "" {
		return errors.New(`it needs "find": the exact words to replace`)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("%q isn't there", args["file"])
	}
	switch n := strings.Count(string(b), args["find"]); n {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("%q isn't in %s", cut(args["find"], 80), args["file"])
	default:
		return fmt.Errorf("%q is in %s %d times: find a longer passage, there once", cut(args["find"], 80), args["file"], n)
	}
}

func (k kit) change(args map[string]string, at time.Time) (string, error) {
	f, p, err := k.writable(args)
	if err != nil {
		return "", err
	}
	old, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if k.s.Versions != "" { // the old version first, so a change never loses it
		dir := filepath.Join(k.s.Versions, f.ID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		name := at.UTC().Format("20060102-150405") + "-" + strings.ReplaceAll(filepath.ToSlash(args["file"]), "/", "_")
		if err := os.WriteFile(filepath.Join(dir, name), old, 0o600); err != nil {
			return "", err
		}
	}
	info, _ := os.Stat(p)
	mode := os.FileMode(0o644)
	if info != nil {
		mode = info.Mode().Perm()
	}
	changed := strings.Replace(string(old), args["find"], args["replace"], 1)
	if err := os.WriteFile(p, []byte(changed), mode); err != nil {
		return "", err
	}
	return fmt.Sprintf("Changed %s in %s; its old version is kept.", filepath.ToSlash(args["file"]), f.Name), nil
}

func (k kit) explainChange(args map[string]string) (string, string, string) {
	f, _ := k.folder(args["folder"])
	return "Change " + filepath.Base(args["file"]) + "?", where(f, args["file"]),
		"− " + cut(args["find"], 700) + "\n+ " + cut(args["replace"], 700)
}

// --- Small things ---

func where(f Folder, file string) string {
	dir := filepath.ToSlash(filepath.Dir(filepath.Clean("/" + file)))
	if dir == "/" {
		return f.Name
	}
	return f.Name + " › " + strings.ReplaceAll(strings.Trim(dir, "/"), "/", " › ")
}

func strArgs(raw string) (map[string]string, error) {
	args := map[string]string{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &args); err != nil {
		return nil, fmt.Errorf("its arguments aren't a JSON object of strings: %w", err)
	}
	return args, nil
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func orNothing(q string) string {
	if q = strings.TrimSpace(q); q == "" || q == "{}" {
		return "nothing"
	}
	return q
}

func folderNames(fs []Folder) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return strings.Join(out, ", ")
}

// isRepo is whether dir is a git repository (its .git a folder, or a file for a worktree).
func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
