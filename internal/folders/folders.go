// Package folders is the folders on this computer an owner adds for their
// agents: what's inside them that an agent may see, the paths it may name,
// and the tools it reads and makes files with.
//
// What holds, whatever an agent asks for:
//   - Only inside a folder that was added. A path is cleaned of "..", and a
//     shortcut (symlink) that leads out of the folder is refused.
//   - Never hidden files or folders (a name starting with "."), so .ssh,
//     .env and .git are never read; nor keys and password stores by their
//     names (*.pem, id_rsa, *.kdbx, ...), keychains, browser profiles, or
//     Tinio's own data folder.
//   - Making or changing a file is an action (action.Write), asked first by
//     default; nothing is ever deleted, and a changed file's old version is
//     kept in Tinio's data folder.
package folders

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Folder is a folder the owner added.
type Folder struct {
	ID    string
	Name  string
	Path  string // absolute, as picked
	Write bool   // may make and change files, each asked first
}

// ErrOutside is a path that isn't in the folder, or is one that's never read.
var ErrOutside = errors.New("not in the folder, or never read")

// Rules say what in a folder an agent never sees: Data is Tinio's own data
// folder.
type Rules struct {
	Data string
}

// never are names never read, wherever they are: keys, certificates and
// password stores.
var neverSuffix = []string{".pem", ".key", ".p12", ".pfx", ".kdbx", ".keychain", ".keychain-db", ".ovpn", ".gpg", ".asc"}

// neverPaths are places never read: keychains and browser profiles, by a
// part of their path.
var neverPaths = []string{
	"/Library/Keychains", "/Library/Cookies", "/Library/Safari", "/Library/Mail",
	"/Library/Application Support/Google/Chrome", "/Library/Application Support/Firefox",
	"/Library/Application Support/BraveSoftware", "/Library/Application Support/Microsoft Edge",
	"/AppData/Local/Google/Chrome", "/AppData/Roaming/Mozilla", "/AppData/Local/Microsoft/Edge",
	"/AppData/Roaming/Microsoft/Credentials", "/AppData/Local/Microsoft/Credentials",
	"/.config/google-chrome", "/.mozilla",
}

// Seen is whether name (one part of a path) may be seen at all.
func Seen(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
		return false
	}
	low := strings.ToLower(name)
	if strings.HasPrefix(low, "id_rsa") || strings.HasPrefix(low, "id_ed25519") || strings.HasPrefix(low, "id_ecdsa") {
		return false
	}
	for _, s := range neverSuffix {
		if strings.HasSuffix(low, s) {
			return false
		}
	}
	return true
}

// allowed is whether abs (a real, absolute path) may be seen: no part of it
// below root is hidden or never read, and it isn't in a place never read.
func (r Rules) allowed(root, abs string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if !Seen(part) {
				return false
			}
		}
	}
	slash := filepath.ToSlash(abs) + "/"
	for _, p := range neverPaths {
		if strings.Contains(slash, p+"/") {
			return false
		}
	}
	if r.Data != "" {
		if data, err := filepath.EvalSymlinks(r.Data); err == nil && within(data, abs) {
			return false
		}
	}
	return true
}

// within is whether abs is dir or inside it.
func within(dir, abs string) bool {
	return abs == dir || strings.HasPrefix(abs, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// Root is f's folder as it really is (shortcuts followed); an error if it's
// gone or isn't a folder.
func (f Folder) Root() (string, error) {
	real, err := filepath.EvalSymlinks(f.Path)
	if err != nil {
		return "", fmt.Errorf("%s isn't there any more", f.Name)
	}
	st, err := os.Stat(real)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s isn't a folder any more", f.Name)
	}
	return real, nil
}

// Resolve is rel (a path inside f, "" for f itself) as an absolute path,
// only if it's inside f and may be seen; it needn't exist yet (a file about
// to be made), but the folder it'd be in must.
func (r Rules) Resolve(f Folder, rel string) (string, error) {
	root, err := f.Root()
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(filepath.ToSlash(rel), "/") {
		return "", fmt.Errorf("%q: name a path inside %s, not from the top of the computer: %w", rel, f.Name, ErrOutside)
	}
	clean := filepath.Clean(string(filepath.Separator) + rel) // ".." can't climb above the top
	target := filepath.Join(root, clean)
	real, err := filepath.EvalSymlinks(target)
	if errors.Is(err, fs.ErrNotExist) {
		parent, perr := filepath.EvalSymlinks(filepath.Dir(target))
		if perr != nil {
			return "", fmt.Errorf("%q: no such folder in %s", rel, f.Name)
		}
		real = filepath.Join(parent, filepath.Base(target))
	} else if err != nil {
		return "", err
	}
	if !within(root, real) || !r.allowed(root, real) {
		return "", fmt.Errorf("%q: %w", rel, ErrOutside)
	}
	return real, nil
}

// Entry is a file or folder inside a folder, by its path from the folder.
type Entry struct {
	Rel  string
	Dir  bool
	Size int64
	Mod  int64 // Unix seconds
}

// Walk calls each for what may be seen in f, folders before what's in them,
// until each returns false or limit entries; it never follows a shortcut out,
// and counts what it left out (hidden, never read, or a shortcut).
func (r Rules) Walk(f Folder, limit int, each func(Entry) bool) (skipped int, more bool, err error) {
	root, err := f.Root()
	if err != nil {
		return 0, false, err
	}
	n := 0
	stop := errors.New("stop")
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if p == root {
			return werr
		}
		if werr != nil {
			skipped++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !r.allowed(root, p) {
			skipped++
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if n >= limit {
			more = true
			return stop
		}
		info, ierr := d.Info()
		if ierr != nil {
			skipped++
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		n++
		if !each(Entry{Rel: filepath.ToSlash(rel), Dir: d.IsDir(), Size: info.Size(), Mod: info.ModTime().Unix()}) {
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		err = nil
	}
	return skipped, more, err
}

// Peek is what's in the folder at path: its files and what's left out,
// counted up to limit, for the owner choosing whether to add it.
type Peek struct {
	Files   int
	Skipped int
	More    bool // more than counted
	Home    bool // it's the owner's whole home folder
}

func (r Rules) Peek(path string, limit int) (Peek, error) {
	f := Folder{Name: filepath.Base(path), Path: path}
	var p Peek
	var err error
	p.Skipped, p.More, err = r.Walk(f, limit, func(e Entry) bool {
		if !e.Dir {
			p.Files++
		}
		return true
	})
	if home, herr := os.UserHomeDir(); herr == nil {
		a, _ := filepath.EvalSymlinks(home)
		b, _ := filepath.EvalSymlinks(path)
		p.Home = a != "" && a == b
	}
	return p, err
}

// Addable is whether path may be added as a folder: an existing folder, not
// the top of a disk, not hidden or a place never read, and not Tinio's own
// data folder or one it's in.
func (r Rules) Addable(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("pick a folder: its full path")
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", errors.New("there's no folder there")
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		return "", errors.New("that isn't a folder")
	}
	top := filepath.VolumeName(real) + string(filepath.Separator)
	if real == top {
		return "", errors.New("that's the whole disk: pick the folder a job needs")
	}
	if !r.allowed(top, real) {
		return "", errors.New("that folder is one agents never see: hidden, or where keys or browser profiles are kept")
	}
	if r.Data != "" {
		if data, err := filepath.EvalSymlinks(r.Data); err == nil && within(real, data) {
			return "", errors.New("Tinio's own data is in there: pick a folder inside it, or another")
		}
	}
	return real, nil
}
