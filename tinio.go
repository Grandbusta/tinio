// Package tinio is what every agent shares, built into the binary: the prompts,
// the question sets, and the characters to start from.
package tinio

import (
	"embed"
	"errors"
	"io/fs"
	"os"
)

//go:embed prompts decisions characters
var built embed.FS

// Files is the shared files, a file or folder on disk (relative to where it
// runs) in place of the built-in one at the same path, so they can be
// changed without a rebuild. A path that is absolute or leaves the folder
// (..) is only ever on disk.
var Files fs.FS = files{}

type files struct{}

func (files) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return os.Open(name)
	}
	f, err := os.DirFS(".").Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return built.Open(name)
	}
	return f, err
}
