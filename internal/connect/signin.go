package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// Google is Tinio's own Google client (GOOGLE_CLIENT_ID,
// GOOGLE_CLIENT_SECRET); without it, Google can't be connected.
type Google struct {
	ClientID, ClientSecret string
}

var ErrNotSetUp = errors.New("this connection isn't set up on this Tinio")

const (
	googleAuth     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleToken    = "https://oauth2.googleapis.com/token"
	googleUserInfo = "https://openidconnect.googleapis.com/v1/userinfo"
)

// Begin starts signing in to c for the parts the owner chose: the grant to
// keep (sealed) until the service sends them back to redirect, and where to
// send them now. For a hosted MCP server, its sign-in is found from the
// server and Tinio registers itself there.
func Begin(ctx context.Context, c Connector, parts []string, g Google, redirect, state string, hc *http.Client) (Grant, string, error) {
	var grant Grant
	if c.MCP == "" {
		if g.ClientID == "" {
			return Grant{}, "", ErrNotSetUp
		}
		grant = Grant{ClientID: g.ClientID, ClientSecret: g.ClientSecret, AuthURL: googleAuth, TokenURL: googleToken, Scopes: []string{"openid", "email"}}
		for _, p := range c.Parts {
			if !p.Later && slices.Contains(parts, p.ID) {
				grant.Scopes = append(grant.Scopes, p.Scopes...)
			}
		}
	} else {
		var err error
		if grant, err = discover(ctx, c.MCP, redirect, hc); err != nil {
			return Grant{}, "", fmt.Errorf("finding %s's sign-in: %w", c.Name, err)
		}
	}
	grant.Verifier = oauth2.GenerateVerifier()
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(grant.Verifier), oauth2.AccessTypeOffline}
	if c.MCP == "" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.SetAuthURLParam("include_granted_scopes", "true"))
	}
	if grant.Resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", grant.Resource))
	}
	return grant, grant.config(redirect).AuthCodeURL(state, opts...), nil
}

// discover finds a hosted MCP server's sign-in, as the MCP spec has it: the
// server's protected-resource metadata, then its authorization server's,
// then registering Tinio as a client there.
func discover(ctx context.Context, server, redirect string, hc *http.Client) (Grant, error) {
	metaURL, err := resourceMetadataURL(ctx, server, hc)
	if err != nil {
		return Grant{}, err
	}
	prm, err := oauthex.GetProtectedResourceMetadata(ctx, metaURL, server, hc)
	if err != nil {
		return Grant{}, err
	}
	if len(prm.AuthorizationServers) == 0 {
		return Grant{}, errors.New("it names no authorization server")
	}
	asm, err := auth.GetAuthServerMetadata(ctx, prm.AuthorizationServers[0], hc)
	if err != nil {
		return Grant{}, err
	}
	if asm == nil || asm.RegistrationEndpoint == "" {
		return Grant{}, errors.New("its authorization server doesn't let Tinio register")
	}
	reg, err := oauthex.RegisterClient(ctx, asm.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{
		RedirectURIs: []string{redirect}, ClientName: "Tinio", ClientURI: "https://github.com/Grandbusta/tinio",
		GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none",
	}, hc)
	if err != nil {
		return Grant{}, err
	}
	return Grant{ClientID: reg.ClientID, ClientSecret: reg.ClientSecret, AuthURL: asm.AuthorizationEndpoint, TokenURL: asm.TokenEndpoint,
		Resource: prm.Resource, Scopes: prm.ScopesSupported}, nil
}

// resourceMetadataURL is where server says its metadata is (in the
// WWW-Authenticate of its answer to a request without a token), or else the
// well-known address beside it.
func resourceMetadataURL(ctx context.Context, server string, hc *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if cs, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate")); err == nil {
		for _, c := range cs {
			if u := c.Params["resource_metadata"]; u != "" {
				return u, nil
			}
		}
	}
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource" + strings.TrimSuffix(u.Path, "/"), nil
}

// Finish trades the code the service sent back for tokens, and says which
// outside account it is ("" if it can't tell).
func Finish(ctx context.Context, c Connector, grant Grant, code, redirect string, hc *http.Client) (Grant, string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, hc)
	opts := []oauth2.AuthCodeOption{oauth2.VerifierOption(grant.Verifier)}
	if grant.Resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", grant.Resource))
	}
	tok, err := grant.config(redirect).Exchange(ctx, code, opts...)
	if err != nil {
		return Grant{}, "", fmt.Errorf("signing in to %s: %w", c.Name, err)
	}
	grant.Token, grant.Verifier = tok, ""
	return grant, label(ctx, c, grant, hc), nil
}

// label is the outside account's email or name, best effort.
func label(ctx context.Context, c Connector, g Grant, hc *http.Client) string {
	client := oauth2.NewClient(context.WithValue(ctx, oauth2.HTTPClient, hc), oauth2.StaticTokenSource(g.Token))
	if c.MCP == "" {
		resp, err := client.Get(googleUserInfo)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		var who struct {
			Email string `json:"email"`
		}
		json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&who)
		return who.Email
	}
	if c.Who == "" {
		return ""
	}
	text, err := callOnce(ctx, c.MCP, client, c.Who)
	if err != nil {
		return ""
	}
	var who map[string]any
	if json.Unmarshal([]byte(text), &who) != nil {
		return ""
	}
	s, _ := who[c.WhoField].(string)
	return s
}
