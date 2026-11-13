// Package person is someone as their database holds them: their life, how
// they live, their owner's judgment, their skills and their secrets. Only
// DATABASE_URL and TINIO_KEY are outside it. Files only come in (tinio
// new, their old self/ folder, tinio self import) and go out (export).
package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"

	"github.com/Grandbusta/tinio/internal/agentskill"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/secret"
)

type Store interface {
	Start(ctx context.Context, name string, vs []memory.SelfVersion, secrets map[string][]byte, skills []memory.AgentSkill, at time.Time) (bool, error)
	Self(ctx context.Context, section string) (memory.SelfVersion, bool, error)
	Secrets(ctx context.Context) (map[string][]byte, error)
	AgentSkills(ctx context.Context) ([]memory.AgentSkill, error)
}

// Known are the secrets every person has: what tinio new asks for.
var Known = []string{"TELEGRAM_BOT_TOKEN", "TELEGRAM_OWNER_ID", "OPENAI_API_KEY", "OPENAI_MODEL", "CONSOLE_TOKEN"}

// machine are settings of where they run, not of who they are.
var machine = []string{"DATABASE_URL", secret.EnvKey, "TINIO_CONSOLE_ADDR", "TINIO_PROFILE_ADDR", "PATH", "HOME"}

var secretName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func CheckName(name string) error {
	switch {
	case !secretName.MatchString(name):
		return fmt.Errorf("%q isn't a secret's name: use capitals, digits and _, like DRIVE_MCP_TOKEN", name)
	case slices.Contains(machine, name):
		return fmt.Errorf("%s is about where they run, not who they are: it stays in the environment", name)
	}
	return nil
}

// Files are the sections as files, as each character in characters/ has them.
var Files = map[string]string{memory.SectionCharacter: "character.yaml", memory.SectionWork: "work.yaml", memory.SectionJudgment: "judgment.yaml"}

// oldFiles are the names before v0.4.0, still read so an old self/ folder
// or export loads.
var oldFiles = map[string]string{memory.SectionCharacter: "life.yaml", memory.SectionWork: "living.yaml"}

// BySection is files, by file name (Files, or the names before v0.4.0), by
// section; a file of neither name is left out.
func BySection(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for _, s := range memory.Sections {
		if raw, ok := files[Files[s]]; ok {
			out[s] = raw
		} else if raw, ok := files[oldFiles[s]]; ok {
			out[s] = raw
		}
	}
	return out
}

// ReadFolder is the sections in dir, by section; a missing file is left out.
func ReadFolder(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, names := range []map[string]string{Files, oldFiles} {
		for _, f := range names {
			raw, err := os.ReadFile(filepath.Join(dir, f))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			files[f] = raw
		}
	}
	return BySection(files), nil
}

var ErrNoOne = errors.New("no one lives here yet: start someone with `tinio new`")

type Agent struct {
	Life     character.Character // their character
	Work     []byte              // read over the config (config.Load)
	Judgment character.Judgment
	Skills   []agentskill.Skill
	// FromEnv are stored secrets the environment sets too: it wins.
	FromEnv []string
	// Unreadable are stored skills left out, with why.
	Unreadable []string
}

// Load reads them from their database, their secrets into the environment (Env).
func Load(ctx context.Context, st Store, key secret.Key) (Agent, error) {
	var p Agent
	doc, ok, err := st.Self(ctx, memory.SectionCharacter)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrNoOne
	}
	if p.FromEnv, err = Env(ctx, st, key); err != nil {
		return p, err
	}
	if p.Life, err = character.Parse([]byte(doc.Doc)); err != nil {
		return p, err
	}
	work, _, err := st.Self(ctx, memory.SectionWork)
	if err != nil {
		return p, err
	}
	p.Work = []byte(work.Doc)
	judgment, _, err := st.Self(ctx, memory.SectionJudgment)
	if err != nil {
		return p, err
	}
	if p.Judgment, err = character.ParseJudgment([]byte(judgment.Doc)); err != nil {
		return p, err
	}
	skills, err := st.AgentSkills(ctx)
	if err != nil {
		return p, err
	}
	for _, sk := range skills {
		s, err := agentskill.Parse(sk.Files)
		if err != nil {
			p.Unreadable = append(p.Unreadable, fmt.Sprintf("%s: %v", sk.Name, err))
			continue
		}
		p.Skills = append(p.Skills, s)
	}
	return p, nil
}

// Env puts their secrets in this process's environment where it doesn't
// already set them, so the config's ${VAR}s and their tools' env find them.
// It returns those the environment sets too, which win.
func Env(ctx context.Context, st Store, key secret.Key) ([]string, error) {
	sealed, err := st.Secrets(ctx)
	if err != nil {
		return nil, err
	}
	var fromEnv []string
	for name, v := range sealed {
		value, err := key.Open(name, v)
		if err != nil {
			return nil, err
		}
		if _, set := os.LookupEnv(name); set {
			fromEnv = append(fromEnv, name)
			continue
		}
		os.Setenv(name, value)
	}
	sort.Strings(fromEnv)
	return fromEnv, nil
}

// Start makes the database theirs, all at once: sections are their YAML by
// section (BySection), checked first. It returns false, changing nothing, if
// someone lives there already.
func Start(ctx context.Context, st Store, key secret.Key, sections map[string][]byte, secrets map[string]string, skills []agentskill.Skill,
	source string, now time.Time) (string, bool, error) {
	life, err := character.Parse(sections[memory.SectionCharacter])
	if err != nil {
		return "", false, err
	}
	if _, err := character.ParseJudgment(sections[memory.SectionJudgment]); err != nil {
		return "", false, err
	}
	var vs []memory.SelfVersion
	for i, section := range memory.Sections {
		if raw, ok := sections[section]; ok {
			at := now.Add(time.Duration(i) * time.Millisecond)
			vs = append(vs, memory.SelfVersion{ID: ulid.Make(), Section: section, Doc: string(raw), Source: source, At: at})
		}
	}
	sealed := map[string][]byte{}
	for name, v := range secrets {
		if err := CheckName(name); err != nil {
			return "", false, err
		}
		sealed[name] = key.Seal(name, v)
	}
	stored := make([]memory.AgentSkill, len(skills))
	for i, s := range skills {
		stored[i] = memory.AgentSkill{Name: s.Name, Description: s.Description, Files: s.Files, AddedAt: now}
	}
	ok, err := st.Start(ctx, life.Name, vs, sealed, stored, now)
	return life.Name, ok, err
}

// Import moves someone from their folder into their database, once: dir's
// sections and skills/, and the secrets the
// environment sets (Known, and those their tools name). Nothing is
// imported (Name "") if no one lives in dir or someone lives in the
// database already.
func Import(ctx context.Context, st Store, key secret.Key, dir string, now time.Time) (Imported, error) {
	var im Imported
	sections, err := ReadFolder(dir)
	if err != nil {
		return im, err
	}
	if _, ok := sections[memory.SectionCharacter]; !ok {
		return im, nil
	}
	if _, ok, err := st.Self(ctx, memory.SectionCharacter); err != nil || ok {
		return im, err
	}
	secrets := map[string]string{}
	for _, name := range append(slices.Clone(Known), toolSecrets(sections[memory.SectionWork])...) {
		if v := os.Getenv(name); v != "" && CheckName(name) == nil {
			secrets[name] = v
			im.Secrets = append(im.Secrets, name)
		}
	}
	skills, skipped := SkillsIn(filepath.Join(dir, "skills"))
	im.Skipped = skipped
	for _, s := range skills {
		im.Skills = append(im.Skills, s.Name)
	}
	sort.Strings(im.Secrets)
	name, ok, err := Start(ctx, st, key, sections, secrets, skills, memory.SelfImported, now)
	if err != nil || !ok {
		return Imported{}, err
	}
	im.Name = name
	return im, nil
}

type Imported struct {
	Name    string // "" if no one was
	Secrets []string
	Skills  []string
	Skipped []string // folders that aren't skills, with why
}

// SkillsIn reads every skill folder in dir; a missing dir has none.
func SkillsIn(dir string) ([]agentskill.Skill, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	var out []agentskill.Skill
	var skipped []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := agentskill.FromDir(filepath.Join(dir, e.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue // a folder without a SKILL.md
		}
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", filepath.Join(dir, e.Name()), err))
			continue
		}
		out = append(out, s)
	}
	return out, skipped
}

// toolSecrets are the environment variables work's tool servers name.
func toolSecrets(work []byte) []string {
	var doc struct {
		Tools struct {
			Servers []struct {
				AuthEnv string   `yaml:"auth_env"`
				Env     []string `yaml:"env"`
			} `yaml:"servers"`
		} `yaml:"tools"`
	}
	yaml.Unmarshal(work, &doc)
	var out []string
	for _, s := range doc.Tools.Servers {
		if s.AuthEnv != "" {
			out = append(out, s.AuthEnv)
		}
		out = append(out, s.Env...)
	}
	return out
}
