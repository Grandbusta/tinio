package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Open opens a fresh database in a test's own folder.
func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(context.Background(), filepath.Join(t.TempDir(), "tinio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// What's kept comes back as it went in: times (and in order, as text), lists,
// vectors, JSON and flags.
func TestValuesComeBackAsTheyWent(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 21, 9, 15, 0, 500, time.FixedZone("EAT", 3*3600))
	if _, err := d.Exec(ctx, `INSERT INTO accounts (id, email, terms_accepted_at, adult_at, created_at) VALUES ($1, $2, $3, $3, $3)`,
		"a1", "ada@example.com", at); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO agents (id, account_id, name, owner_name, relation, created_at) VALUES ($1, $2, 'Zara', 'Ada', 'friend', $3)`,
		"g1", "a1", at); err != nil {
		t.Fatal(err)
	}
	vec := []float32{0.25, -1.5, 3}
	if _, err := d.Exec(ctx, `INSERT INTO topics (agent_id, id, person, settled, started_at, last_at, embedding) VALUES ($1, $2, $3, $4, $5, $5, $6)`,
		"g1", "t1", "account:a1", []string{"Six posts", "Under £30"}, at, vec); err != nil {
		t.Fatal(err)
	}
	var (
		settled []string
		got     []float32
		last    time.Time
		folded  *time.Time
	)
	if err := d.QueryRow(ctx, `SELECT settled, embedding, last_at, folded_at FROM topics WHERE agent_id = $1 AND id = $2`, "g1", "t1").
		Scan(&settled, &got, &last, &folded); err != nil {
		t.Fatal(err)
	}
	if len(settled) != 2 || settled[1] != "Under £30" || len(got) != 3 || got[1] != -1.5 || !last.Equal(at) || folded != nil {
		t.Fatalf("got %q %v %v %v", settled, got, last, folded)
	}
	var n int
	if err := d.QueryRow(ctx, `SELECT count(*) FROM topics WHERE last_at > $1 AND last_at < $2`, at.Add(-time.Nanosecond), at.Add(time.Second)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("times don't compare as text: %d %v", n, err)
	}
}

// No rows is an empty list, as the platform's Postgres gave: in JSON, [] and
// never null, which the app reads as a list.
func TestNoRowsIsAnEmptyList(t *testing.T) {
	d := openTest(t)
	rows, err := d.Query(context.Background(), `SELECT name FROM keys`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CollectRows(rows, func(r Scanner) (string, error) {
		var s string
		return s, r.Scan(&s)
	})
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("%#v, %v", got, err)
	}
}
