package connect

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/mind"
	"github.com/Grandbusta/tinio/internal/tools"
)

// Given is a connection given to an agent, as their engine uses it.
type Given struct {
	ID        string
	Connector Connector
	Label     string
	Access    Access
	Grant     Grant
}

// Saver keeps what using a connection changes: tokens refreshed (sealed by
// whoever saves them), access gained (the folder docs are made in), or the
// service having signed Tinio out.
type Saver interface {
	SaveGrant(ctx context.Context, id string, g Grant) error
	SaveAccess(ctx context.Context, id string, a Access) error
	SignedOut(ctx context.Context, id string) error
}

// Tools is what g gives an agent: reading tools as abilities, and tools
// that change things, only ever proposed. limit is the reading calls a day,
// across every tool; st is where calls are logged. close ends any
// connection it made.
func (g Given) Tools(ctx context.Context, s Saver, st tools.Store, limit int, hc *http.Client, log *slog.Logger) (reads []ability.Ability, doers []mind.Doer, close func()) {
	client := g.client(s, hc)
	if g.Connector.MCP == "" {
		gt := &google{given: g, client: client, saver: s, store: st, limit: limit}
		return gt.reads(), gt.doers(), func() {}
	}
	kinds, alwaysAsk := map[string]string{}, map[string]bool{}
	for name, r := range g.Connector.Tools {
		kinds[name] = config.ToolRead
		if r.Class != "" {
			kinds[name] = string(r.Class)
		}
		alwaysAsk[name] = r.Destructive
	}
	about := g.Connector.Name + " (" + g.Connector.About + ")"
	if g.Label != "" {
		about = g.Connector.Name + ", " + g.Label + " (" + g.Connector.About + ")"
	}
	c := tools.Remote(ctx, g.Connector.ID, about, g.Connector.MCP, client, kinds, alwaysAsk, limit, st)
	if c.Err != nil {
		log.Warn("connection not used", "connector", g.Connector.ID, "err", c.Err)
		c.Server.Close()
		return nil, nil, func() {}
	}
	for _, t := range c.Tools {
		reads = append(reads, t)
	}
	for _, a := range c.Actions {
		doers = append(doers, a)
	}
	return reads, doers, c.Server.Close
}

// client signs requests as the owner, refreshing the token when it runs out
// and keeping the new one; a refusal to refresh means the service signed
// Tinio out.
func (g Given) client(s Saver, hc *http.Client) *http.Client {
	base := context.WithValue(context.Background(), oauth2.HTTPClient, hc)
	src := &saving{given: g, saver: s, src: g.Grant.config("").TokenSource(base, g.Grant.Token), last: g.Grant.Token}
	return oauth2.NewClient(base, oauth2.ReuseTokenSource(g.Grant.Token, src))
}

type saving struct {
	given Given
	saver Saver
	mu    sync.Mutex
	src   oauth2.TokenSource
	last  *oauth2.Token
}

func (s *saving) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, err := s.src.Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.ErrorCode == "invalid_grant" || re.Response != nil && re.Response.StatusCode == http.StatusUnauthorized) {
			s.saver.SignedOut(ctx, s.given.ID)
		}
		return nil, err
	}
	if s.last == nil || tok.AccessToken != s.last.AccessToken {
		g := s.given.Grant
		if tok.RefreshToken == "" && s.last != nil {
			tok.RefreshToken = s.last.RefreshToken
		}
		g.Token, s.last = tok, tok
		if err := s.saver.SaveGrant(ctx, s.given.ID, g); err != nil {
			return nil, err
		}
	}
	return tok, nil
}

// callOnce calls one tool of a hosted server with client, for its text.
func callOnce(ctx context.Context, server string, client *http.Client, tool string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "tinio", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server, HTTPClient: client}, nil)
	if err != nil {
		return "", err
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), nil
}
