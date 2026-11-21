package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/Grandbusta/tinio/internal/secret"
)

// Store keeps connections in the platform's database (connections, a
// platform table), their grants sealed with Key.
type Store struct {
	DB  *pgxpool.Pool
	Key secret.Key
}

// Given is the connections an agent was given that still work, their
// grants opened. One that can't be opened (TINIO_KEY changed) is left out.
func (s Store) Given(ctx context.Context, agentID string) ([]Given, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT c.id, c.connector, c.label, c.access, c.secret FROM connections c
		JOIN connection_agents g ON g.connection_id = c.id
		WHERE g.agent_id = $1 AND c.state = 'ok' ORDER BY c.created_at`, agentID)
	if err != nil {
		return nil, fmt.Errorf("connections: %w", err)
	}
	defer rows.Close()
	var out []Given
	for rows.Next() {
		var g Given
		var connector string
		var sealed []byte
		if err := rows.Scan(&g.ID, &connector, &g.Label, &g.Access, &sealed); err != nil {
			return nil, err
		}
		c, ok := Find(connector)
		if !ok {
			continue
		}
		g.Connector = c
		if g.Grant, err = Open(s.Key, g.ID, sealed); err != nil {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s Store) SaveGrant(ctx context.Context, id string, g Grant) error {
	_, err := s.DB.Exec(ctx, `UPDATE connections SET secret = $2, updated_at = $3 WHERE id = $1`, id, g.Seal(s.Key, id), time.Now().UTC())
	return err
}

func (s Store) SaveAccess(ctx context.Context, id string, a Access) error {
	raw, _ := json.Marshal(a)
	_, err := s.DB.Exec(ctx, `UPDATE connections SET access = $2, updated_at = $3 WHERE id = $1`, id, raw, time.Now().UTC())
	return err
}

// SignedOut marks a connection as needing its owner to sign in again: its
// agents stop using it at their next start.
func (s Store) SignedOut(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `UPDATE connections SET state = 'needs_you', updated_at = $2 WHERE id = $1`, id, time.Now().UTC())
	return err
}

// Fresh is a working access token for connection id, refreshed (and kept)
// if it ran out: for the owner's own browser, as Google's file picker needs.
func (s Store) Fresh(ctx context.Context, id string, g Grant, hc *http.Client) (*oauth2.Token, error) {
	base := context.WithValue(ctx, oauth2.HTTPClient, hc)
	src := &saving{given: Given{ID: id, Grant: g}, saver: s, src: g.config("").TokenSource(base, g.Token), last: g.Token}
	return oauth2.ReuseTokenSource(g.Token, src).Token()
}
