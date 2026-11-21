// Package connect is the accounts an owner connects for their agents: the
// connectors Tinio offers, signing in to them (OAuth, with PKCE; a hosted MCP
// server's own sign-in found from the server), and the tools each gives an
// agent. A connection belongs to the account and is given to the agents the
// owner chooses.
//
// Only reviewed tools are offered: each connector names its tools and what
// each does (reads, changes, or reaches people), and a tool it doesn't name,
// new or renamed by the service, is never offered. What a hosted server says
// about its own tools is only a hint, never trusted to loosen anything.
// Sending email and anything with money are never offered.
package connect

import (
	"encoding/json"
	"fmt"
	"slices"

	"golang.org/x/oauth2"

	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/secret"
)

// Rule is what a reviewed tool does: Class "" only reads (and runs on its
// own); any other is the autonomy class it's proposed under. Destructive
// ones always ask.
type Rule struct {
	Class       action.Class
	Destructive bool
}

var (
	read      = Rule{}
	change    = Rule{Class: action.Write}
	overwrite = Rule{Class: action.Write, Destructive: true}
	reach     = Rule{Class: action.ReachOut} // others see it: a comment, a mention
)

// Part is something an owner may allow, with the scopes it needs.
type Part struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Note    string   `json:"note"`
	Default bool     `json:"default"`
	Later   bool     `json:"later,omitempty"` // shown, not offered yet
	Scopes  []string `json:"-"`
}

// Connector is a service Tinio can connect to.
type Connector struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	About string `json:"about"`
	// MCP is a hosted MCP server's address, which finds its own sign-in; ""
	// for one of Tinio's own (Google).
	MCP   string `json:"-"`
	Parts []Part `json:"parts"`
	// Tools are a hosted server's reviewed tools by name.
	Tools map[string]Rule `json:"-"`
	// Who is the tool that says which account it is, and the field of what
	// it returns that names it; "" for none.
	Who, WhoField string `json:"-"`
}

// Connectors are the services an owner can connect, in the order they're shown.
var Connectors = []Connector{
	{ID: "google", Name: "Google", About: "Your calendar, and the Drive files you pick.",
		Parts: []Part{
			{ID: "calendar", Title: "Read your calendar", Note: `Your events and free time, so they can plan around you and answer "what's on tomorrow?".`, Default: true,
				Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly"}},
			{ID: "calendar_add", Title: "Add events, with your yes", Note: "Events with no guests. They never invite anyone, move or delete your events.", Default: true,
				Scopes: []string{"https://www.googleapis.com/auth/calendar.events"}},
			{ID: "drive", Title: "Read files you pick, and make docs", Note: `Only the files you pick, and docs they make in a "Tinio" folder. Never shares or deletes.`, Default: true,
				Scopes: []string{"https://www.googleapis.com/auth/drive.file"}},
			{ID: "gmail", Title: "Gmail", Note: "Reading your email and drafting replies comes later. Sending stays never.", Later: true},
		}},
	{ID: "atlassian", Name: "Atlassian", About: "Jira issues and Confluence pages.", MCP: "https://mcp.atlassian.com/v2/mcp",
		Parts: []Part{{ID: "all", Title: "Jira and Confluence", Note: "Read issues and pages; create or change them, and comment, with your yes. Never deletes.", Default: true}},
		Who:   "atlassianUserInfo", WhoField: "email",
		Tools: map[string]Rule{
			"atlassianUserInfo": read, "getAccessibleAtlassianResources": read, "search": read, "fetch": read,
			"getJiraIssue": read, "searchJiraIssuesUsingJql": read, "getVisibleJiraProjects": read,
			"getJiraProjectIssueTypesMetadata": read, "getJiraIssueTypeMetaWithFields": read, "getTransitionsForJiraIssue": read,
			"lookupJiraAccountId": read, "getJiraIssueRemoteIssueLinks": read, "getIssueLinkTypes": read,
			"getConfluenceSpaces": read, "getConfluencePage": read, "getPagesInConfluenceSpace": read,
			"getConfluencePageFooterComments": read, "getConfluencePageInlineComments": read, "getConfluencePageDescendants": read,
			"getConfluenceCommentChildren": read, "searchConfluenceUsingCql": read, "getContentFormatGuide": read,
			"createJiraIssue": change, "transitionJiraIssue": change, "createIssueLink": change, "addWorklogToJiraIssue": change,
			"createConfluencePage": change,
			"editJiraIssue":        overwrite, "updateConfluencePage": overwrite,
			"addCommentToJiraIssue": reach, "createConfluenceFooterComment": reach, "createConfluenceInlineComment": reach,
		}},
}

func Find(id string) (Connector, bool) {
	i := slices.IndexFunc(Connectors, func(c Connector) bool { return c.ID == id })
	if i < 0 {
		return Connector{}, false
	}
	return Connectors[i], true
}

// Access is what the owner allowed a connection to do.
type Access struct {
	Parts []string `json:"parts"`
	// Files are the Drive files they picked; Folder is the "Tinio" folder
	// their agents make docs in, once made.
	Files  []File `json:"files,omitempty"`
	Folder string `json:"folder,omitempty"`
}

type File struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
}

func (a Access) Has(part string) bool { return slices.Contains(a.Parts, part) }

// Grant is what signing in left, sealed before it's stored: the client
// Tinio signed in as (registered with a hosted server, or Tinio's own for
// Google) and the tokens.
type Grant struct {
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret,omitempty"`
	AuthURL      string        `json:"auth_url"`
	TokenURL     string        `json:"token_url"`
	Resource     string        `json:"resource,omitempty"` // a hosted server's address, asked for by name
	Scopes       []string      `json:"scopes,omitempty"`
	Token        *oauth2.Token `json:"token,omitempty"`
	Verifier     string        `json:"verifier,omitempty"` // only while signing in
}

func (g Grant) config(redirect string) *oauth2.Config {
	return &oauth2.Config{ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: redirect, Scopes: g.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: g.AuthURL, TokenURL: g.TokenURL}}
}

// Seal is g sealed for connection id (or a sign-in's state).
func (g Grant) Seal(k secret.Key, id string) []byte {
	raw, _ := json.Marshal(g)
	return k.Seal("connection:"+id, string(raw))
}

func Open(k secret.Key, id string, sealed []byte) (Grant, error) {
	raw, err := k.Open("connection:"+id, sealed)
	if err != nil {
		return Grant{}, err
	}
	var g Grant
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return Grant{}, fmt.Errorf("connection %s: %w", id, err)
	}
	return g, nil
}
