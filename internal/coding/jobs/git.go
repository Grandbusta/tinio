package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/clis"
)

// Snapshots: after each round the job's copy is kept as a commit made with an
// index of its own (new files count, and the owner's index, HEAD and log are
// untouched), under refs/tinio/jobs/<job>/<round>. A round's changes are
// between two snapshots; the job's are from the commit it started from to the
// last one.

func roundRef(job string, n int) string { return "refs/tinio/jobs/" + job + "/" + strconv.Itoa(n) }

// gitIn runs git in dir with env added, and is what it printed.
func gitIn(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// tree is the copy's files as a git tree, from an index of its own.
func tree(ctx context.Context, dir string) (string, error) {
	idx, err := os.CreateTemp("", "tinio-index-*")
	if err != nil {
		return "", err
	}
	idx.Close()
	os.Remove(idx.Name()) // git makes it
	defer os.Remove(idx.Name())
	env := []string{"GIT_INDEX_FILE=" + idx.Name()}
	if _, err := gitIn(ctx, dir, env, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := gitIn(ctx, dir, env, "add", "-A"); err != nil {
		return "", err
	}
	return gitIn(ctx, dir, env, "write-tree")
}

// checkpoint keeps j's copy as its next round, and is the round's number.
func (s *Service) checkpoint(ctx context.Context, j Job) (int, error) {
	t, err := tree(ctx, j.Dir)
	if err != nil {
		return 0, err
	}
	parent := j.BaseSHA
	if j.Rounds > 0 {
		parent = roundRef(j.ID, j.Rounds)
	}
	n := j.Rounds + 1
	c, err := gitIn(ctx, j.Dir, []string{"GIT_AUTHOR_NAME=Tinio", "GIT_AUTHOR_EMAIL=tinio@localhost", "GIT_COMMITTER_NAME=Tinio", "GIT_COMMITTER_EMAIL=tinio@localhost"},
		"commit-tree", t, "-p", parent, "-m", fmt.Sprintf("Tinio: %s, round %d", j.Title, n))
	if err != nil {
		return 0, err
	}
	if _, err := gitIn(ctx, j.Dir, nil, "update-ref", roundRef(j.ID, n), c); err != nil {
		return 0, err
	}
	return n, nil
}

// A FileChange is one file a job changed, with its diff.
type FileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Patch   string `json:"patch"` // unified, without the header
}

// Changes are j's changes: from the commit it started from to its last round,
// or round's alone (from the round before it) when round > 0.
func (s *Service) Changes(ctx context.Context, j Job, round int) ([]FileChange, error) {
	if j.Rounds == 0 {
		return []FileChange{}, nil
	}
	from, to := j.BaseSHA, roundRef(j.ID, j.Rounds)
	if round > 0 && round <= j.Rounds {
		to = roundRef(j.ID, round)
		if round > 1 {
			from = roundRef(j.ID, round-1)
		}
	}
	stat, err := gitIn(ctx, j.Dir, nil, "diff", "--numstat", from, to)
	if err != nil {
		return nil, err
	}
	out := []FileChange{}
	for _, line := range strings.Split(stat, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		c := FileChange{Path: f[2]}
		c.Added, _ = strconv.Atoi(f[0])
		c.Removed, _ = strconv.Atoi(f[1])
		patch, err := gitIn(ctx, j.Dir, nil, "diff", "--no-color", "-U3", from, to, "--", c.Path)
		if err != nil {
			return nil, err
		}
		if i := strings.Index(patch, "\n@@"); i >= 0 {
			patch = patch[i+1:]
		}
		c.Patch = patch
		out = append(out, c)
	}
	return out, nil
}

// GoBack puts j's copy back as it was before its last round, and forgets the
// round.
func (s *Service) GoBack(ctx context.Context, agent, id string) (Job, error) {
	if _, ok := s.running(agent, id); ok {
		return Job{}, errors.New("the job is still working: stop it first")
	}
	j, err := s.Get(ctx, agent, id)
	if err != nil {
		return Job{}, err
	}
	if j.Rounds == 0 || j.CommitSHA != "" {
		return Job{}, errors.New("there's no round to go back from")
	}
	if p := s.unpark(j.ID, ""); p != nil {
		p.close() // what it remembers is of the round undone
	}
	last := roundRef(j.ID, j.Rounds)
	back := j.BaseSHA
	if j.Rounds > 1 {
		back = roundRef(j.ID, j.Rounds-1)
	}
	// Files the round made go; the rest are put back as they were.
	made, err := gitIn(ctx, j.Dir, nil, "diff", "--name-only", "--diff-filter=A", back, last)
	if err != nil {
		return Job{}, err
	}
	for _, f := range strings.Split(made, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			os.Remove(filepath.Join(j.Dir, f))
		}
	}
	if _, err := gitIn(ctx, j.Dir, nil, "restore", "--source="+back, "--worktree", "--", "."); err != nil {
		return Job{}, err
	}
	gitIn(ctx, j.Dir, nil, "update-ref", "-d", last)
	j.Rounds--
	if err := s.save(ctx, j); err != nil {
		return Job{}, err
	}
	s.step(ctx, j.ID, WhoOwner, "Went back a round", "")
	s.editCard(ctx, j, func(c *Card) {
		c.Now = fmt.Sprintf("Back to how round %d left it", j.Rounds)
		if j.Rounds == 0 {
			c.Now = "Back to how it started"
		}
	})
	return j, nil
}

// Commit commits j's copy on its branch, as the owner, with message: on their
// yes. Only on this computer: pushing is another yes.
func (s *Service) Commit(ctx context.Context, agent, id, message string) (Job, error) {
	if r, ok := s.running(agent, id); ok {
		if !r.isChecking() {
			return Job{}, errors.New("the job is still working: let it finish first")
		}
		r.endCheck() // ready, and only being checked: the owner's yes needn't wait for it
	}
	j, err := s.Get(ctx, agent, id)
	if err != nil {
		return Job{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = j.Title
	}
	if _, err := gitIn(ctx, j.Dir, nil, "add", "-A"); err != nil {
		return Job{}, err
	}
	if out, _ := gitIn(ctx, j.Dir, nil, "diff", "--cached", "--name-only"); out == "" {
		return Job{}, errors.New("there's nothing to commit")
	}
	if _, err := gitIn(ctx, j.Dir, nil, "commit", "-q", "-m", message); err != nil {
		return Job{}, err
	}
	if j.CommitSHA, err = gitIn(ctx, j.Dir, nil, "rev-parse", "HEAD"); err != nil {
		return Job{}, err
	}
	if err := s.save(ctx, j); err != nil {
		return Job{}, err
	}
	s.step(ctx, j.ID, WhoOwner, "Committed it", strings.SplitN(message, "\n", 2)[0])
	go s.learn(s.ctx, j) // what the job taught, for the next in this folder
	s.editCard(ctx, j, func(c *Card) { c.Now = "Committed on " + orDefault(j.Branch, j.Base) + ", on this computer" })
	return j, nil
}

// HasOrigin says whether j's repository has an origin to push to.
func (s *Service) HasOrigin(ctx context.Context, j Job) bool {
	dir := j.Dir
	if dir == "" {
		dir = j.Repo
	}
	_, err := gitIn(ctx, dir, nil, "remote", "get-url", "origin")
	return err == nil
}

// Push pushes j's branch to the repository's origin and opens a pull request
// into the branch it started from, through the owner's gh: on their second yes.
func (s *Service) Push(ctx context.Context, agent, id, body string) (Job, error) {
	j, err := s.Get(ctx, agent, id)
	if err != nil {
		return Job{}, err
	}
	switch {
	case j.CommitSHA == "":
		return Job{}, errors.New("commit it first")
	case j.Branch == "":
		return Job{}, errors.New("it worked in the repository itself, on " + j.Base + ": push it yourself")
	}
	gh := s.Finder.GHNow(ctx)
	if !gh.SignedIn {
		return Job{}, errors.New("GitHub's gh isn't signed in on this computer: run gh auth login")
	}
	if _, err := gitIn(ctx, j.Dir, nil, "remote", "get-url", "origin"); err != nil {
		return Job{}, errors.New("the repository has no origin to push to")
	}
	if _, err := gitIn(ctx, j.Dir, nil, "push", "-u", "origin", j.Branch); err != nil {
		return Job{}, err
	}
	s.step(ctx, j.ID, WhoOwner, "Pushed "+j.Branch, "")
	if j.PRURL != "" { // its pull request has the new commits now
		s.editCard(ctx, j, func(c *Card) { c.Now = "Pushed: the pull request has it" })
		return j, nil
	}
	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := exec.CommandContext(pctx, gh.Path, "pr", "create", "--title", j.Title, "--body", body, "--head", j.Branch, "--base", j.Base)
	cmd.Dir = j.Dir
	cmd.Env = clis.Env(os.Environ(), gh.Path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Job{}, fmt.Errorf("opening the pull request: %s", strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	j.PRURL = strings.TrimSpace(lines[len(lines)-1])
	if err := s.save(ctx, j); err != nil {
		return Job{}, err
	}
	s.step(ctx, j.ID, WhoOwner, "Opened a pull request", j.PRURL)
	s.editCard(ctx, j, func(c *Card) { c.Now = "Pull request open" })
	return j, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
