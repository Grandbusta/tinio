package store

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/internal/memory"
)

func TestABackupHoldsOnlyTheAgent(t *testing.T) {
	s := openTestStore(t)
	other := s.For(newAgent(t, s.pool))
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	life := memory.SelfVersion{ID: ulid.Make(), Section: memory.SectionCharacter, Doc: "name: Tobi\n", Source: memory.SelfNew, At: at}
	skill := memory.AgentSkill{Name: "cold-email", AddedAt: at, Files: map[string][]byte{"SKILL.md": []byte("---\n"), "logo.png": {0x89, 0x00, '\n', ','}}}
	if _, err := s.Start(ctx, "Tobi", []memory.SelfVersion{life}, map[string][]byte{"OPENAI_API_KEY": {0, '"', '\n'}}, []memory.AgentSkill{skill}, at); err != nil {
		t.Fatal(err)
	}
	if err := s.LogWeb(ctx, "search", "a \"quoted\", line\nbreak", 0.01, at); err != nil {
		t.Fatal(err)
	}
	if err := other.LogWeb(ctx, "search", "someone else's", 0, at); err != nil {
		t.Fatal(err)
	}

	var file bytes.Buffer
	made, err := s.Backup(ctx, &file, at)
	if err != nil {
		t.Fatal(err)
	}
	if made.Whose != "Tobi" || made.Version == 0 || made.Rows() != 6 {
		t.Errorf("backup = %+v (%d rows)", made, made.Rows())
	}
	var names []string
	for _, tb := range made.Tables {
		names = append(names, tb.Name)
	}
	for _, shared := range []string{"sources", "agents", "accounts", "db_migrations"} {
		if slices.Contains(names, shared) {
			t.Errorf("the backup has %s", shared)
		}
	}
	if slices.Index(names, "agent_skill_files") < slices.Index(names, "agent_skills") {
		t.Errorf("a table before the one it refers to: %v", names)
	}

	z, err := zip.NewReader(bytes.NewReader(file.Bytes()), int64(file.Len()))
	if err != nil {
		t.Fatal(err)
	}
	f, err := z.Open("web_log.csv")
	if err != nil {
		t.Fatal(err)
	}
	csv, _ := io.ReadAll(f)
	if !strings.Contains(string(csv), `"a ""quoted"", line`) || strings.Contains(string(csv), "someone else's") {
		t.Errorf("web_log.csv = %q", csv)
	}

	if _, err := New(s.pool).Backup(ctx, io.Discard, at); !errors.Is(err, errUnbound) {
		t.Errorf("an unbound backup: %v", err)
	}
}
