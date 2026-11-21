package controllers

import (
	"errors"
	"github.com/Grandbusta/tinio/internal/db"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/connect"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/secret"
	"github.com/Grandbusta/tinio/internal/store"
)

// Connections is the accounts an owner connects (connect.Connectors) and
// which of their agents may use each. Signing in goes to the service and
// back to Callback; the tokens it leaves are sealed with Key and never
// leave the API, except a fresh access token for Google's file picker, in
// the owner's own browser. An agent's connections reach them within a
// minute (host.Changed).
type Connections struct {
	DB     *db.DB
	Key    *secret.Key // nil: nothing can be connected
	Google connect.Google
	Picker struct{ APIKey, AppID string }
	App    string // the app's origin: services send the owner back through its /api
	HTTP   *http.Client
	Log    *slog.Logger
}

const (
	signInFor   = 15 * time.Minute
	filesAtMost = 50
)

type connectionView struct {
	ID        string         `json:"id"`
	Connector string         `json:"connector"`
	Label     string         `json:"label"`
	Access    connect.Access `json:"access"`
	State     string         `json:"state"` // ok, or needs_you: sign in again
	Agents    []string       `json:"agents"`
	CreatedAt time.Time      `json:"created_at"`
}

type connectorView struct {
	connect.Connector
	Ready bool `json:"ready"` // set up on this Tinio
}

func (cn *Connections) store() connect.Store { return connect.Store{DB: cn.DB, Key: *cn.Key} }

func (cn *Connections) redirect() string { return cn.App + "/api/v1/connections/callback" }

// List is the connectors and the account's connections, each with the
// agents that may use it.
func (cn *Connections) List(c *gin.Context) error {
	mine, err := cn.connections(c, middlewares.Account(c).ID, "")
	if err != nil {
		return err
	}
	out := make([]connectorView, 0, len(connect.Connectors))
	for _, k := range connect.Connectors {
		out = append(out, connectorView{Connector: k, Ready: cn.Key != nil && (k.MCP != "" || cn.Google.ClientID != "")})
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"connectors": out, "connections": mine})
	return nil
}

// connections is account's connections, or only id's if id isn't "".
func (cn *Connections) connections(c *gin.Context, account, id string) ([]connectionView, error) {
	rows, err := cn.DB.Query(c, `
		SELECT c.id, c.connector, c.label, c.access, c.state, c.created_at,
			COALESCE(json_group_array(g.agent_id) FILTER (WHERE g.agent_id IS NOT NULL), '[]')
		FROM connections c LEFT JOIN connection_agents g ON g.connection_id = c.id
		WHERE c.account_id = $1 AND ($2 = '' OR c.id = $2)
		GROUP BY c.id ORDER BY c.created_at`, account, id)
	if err != nil {
		return nil, err
	}
	return db.CollectRows(rows, func(r db.Scanner) (connectionView, error) {
		var v connectionView
		return v, r.Scan(&v.ID, &v.Connector, &v.Label, &v.Access, &v.State, &v.CreatedAt, &v.Agents)
	})
}

func (cn *Connections) one(c *gin.Context) (connectionView, error) {
	vs, err := cn.connections(c, middlewares.Account(c).ID, c.Param("connection"))
	if err != nil {
		return connectionView{}, err
	}
	if len(vs) == 0 {
		return connectionView{}, utils.ErrNoSuchConnection
	}
	return vs[0], nil
}

// Start begins signing in to a connector for the parts the owner chose
// (and, if from an agent's page, gives it to that agent once connected):
// the address to send them to.
func (cn *Connections) Start(c *gin.Context) error {
	var in struct {
		Connector string   `json:"connector"`
		Parts     []string `json:"parts"`
		Agent     string   `json:"agent"`
	}
	if c.ShouldBindJSON(&in) != nil {
		return utils.ErrBadRequest
	}
	k, ok := connect.Find(in.Connector)
	if !ok {
		return utils.ErrNoSuchConnection
	}
	if cn.Key == nil {
		return utils.ErrConnectionsOff
	}
	var parts []string
	for _, p := range k.Parts {
		if !p.Later && slices.Contains(in.Parts, p.ID) {
			parts = append(parts, p.ID)
		}
	}
	if len(parts) == 0 {
		return utils.Invalid("parts", "Choose at least one thing they may do.")
	}
	acc := middlewares.Account(c)
	if in.Agent != "" {
		if _, ok, err := models.AgentOf(c, cn.DB, acc.ID, in.Agent); err != nil {
			return err
		} else if !ok {
			return utils.ErrNoSuchAgent
		}
	}
	state := utils.NewToken()
	grant, to, err := connect.Begin(c.Request.Context(), k, parts, cn.Google, cn.redirect(), state, cn.HTTP)
	if errors.Is(err, connect.ErrNotSetUp) {
		return utils.ErrConnectionsOff
	}
	if err != nil {
		cn.Log.Warn("couldn't begin signing in", "connector", k.ID, "err", err)
		return utils.ErrServiceDown.With(gin.H{"name": k.Name})
	}
	now := time.Now().UTC()
	if _, err := cn.DB.Exec(c, `DELETE FROM connect_starts WHERE created_at < $1`, now.Add(-signInFor)); err != nil {
		return err
	}
	if _, err := cn.DB.Exec(c, `INSERT INTO connect_starts (state, account_id, connector, agent_id, access, secret, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, state, acc.ID, k.ID, in.Agent, connect.Access{Parts: parts}, grant.Seal(*cn.Key, "start:"+state), now); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"url": to})
	return nil
}

// Callback is where a service sends the owner back after signing in: the
// sign-in they began (in this browser, as this account, in the last 15
// minutes) is finished and kept, and they go back to the app.
func (cn *Connections) Callback(c *gin.Context) error {
	acc := middlewares.Account(c)
	var connector, agentID string
	var access connect.Access
	var sealed []byte
	err := cn.DB.QueryRow(c, `DELETE FROM connect_starts WHERE state = $1 AND account_id = $2 AND created_at > $3
		RETURNING connector, agent_id, access, secret`, c.Query("state"), acc.ID, time.Now().UTC().Add(-signInFor)).
		Scan(&connector, &agentID, &access, &sealed)
	back := func(query string) error {
		c.Redirect(http.StatusSeeOther, cn.App+"/connections/"+url.PathEscape(connector)+"?"+query)
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		c.Redirect(http.StatusSeeOther, cn.App+"/connections?failed=expired")
		return nil
	}
	if err != nil {
		return err
	}
	k, ok := connect.Find(connector)
	if !ok || cn.Key == nil || c.Query("error") != "" || c.Query("code") == "" {
		return back("failed=declined")
	}
	grant, err := connect.Open(*cn.Key, "start:"+c.Query("state"), sealed)
	if err != nil {
		return err
	}
	grant, label, err := connect.Finish(c.Request.Context(), k, grant, c.Query("code"), cn.redirect(), cn.HTTP)
	if err != nil {
		cn.Log.Warn("couldn't finish signing in", "connector", k.ID, "err", err)
		return back("failed=signin")
	}
	now := time.Now().UTC()
	id := ulid.Make().String()
	// Signing in again keeps the files they picked and the folder made before.
	var before connect.Access
	err = cn.DB.QueryRow(c, `SELECT access FROM connections WHERE account_id = $1 AND connector = $2`, acc.ID, k.ID).Scan(&before)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return err
	}
	if before.Files != nil {
		access.Files = before.Files
	}
	if before.Folder != "" {
		access.Folder = before.Folder
	}
	err = cn.DB.QueryRow(c, `
		INSERT INTO connections (id, account_id, connector, label, access, secret, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, '', 'ok', $6, $6)
		ON CONFLICT (account_id, connector) DO UPDATE SET label = excluded.label, state = 'ok', updated_at = excluded.updated_at,
			access = excluded.access
		RETURNING id`, id, acc.ID, k.ID, label, access, now).Scan(&id)
	if err != nil {
		return err
	}
	if _, err := cn.DB.Exec(c, `UPDATE connections SET secret = $2 WHERE id = $1`, id, grant.Seal(*cn.Key, id)); err != nil {
		return err
	}
	if agentID != "" {
		if _, err := cn.DB.Exec(c, `INSERT INTO connection_agents (connection_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, agentID); err != nil {
			return err
		}
	}
	if err := cn.tell(c, id, nil); err != nil {
		return err
	}
	q := url.Values{"connected": {id}}
	if agentID != "" {
		q.Set("for", agentID)
	}
	return back(q.Encode())
}

// tell has the agents of connection id, and those in also, load their
// connections again.
func (cn *Connections) tell(c *gin.Context, id string, also []string) error {
	rows, err := cn.DB.Query(c, `SELECT agent_id FROM connection_agents WHERE connection_id = $1
		UNION SELECT value FROM json_each($2)`, id, also)
	if err != nil {
		return err
	}
	agents, err := db.CollectRows(rows, func(r db.Scanner) (string, error) {
		var a string
		return a, r.Scan(&a)
	})
	for _, a := range agents {
		cn.DB.Notify(host.Changed, a)
	}
	return err
}

// Agents sets which of the account's agents may use a connection.
func (cn *Connections) Agents(c *gin.Context) error {
	var in struct {
		Agents []string `json:"agents"`
	}
	if c.ShouldBindJSON(&in) != nil {
		return utils.ErrBadRequest
	}
	v, err := cn.one(c)
	if err != nil {
		return err
	}
	acc := middlewares.Account(c)
	for _, a := range in.Agents {
		if _, ok, err := models.AgentOf(c, cn.DB, acc.ID, a); err != nil {
			return err
		} else if !ok {
			return utils.ErrNoSuchAgent
		}
	}
	tx, err := cn.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	if _, err := tx.Exec(c, `DELETE FROM connection_agents WHERE connection_id = $1`, v.ID); err != nil {
		return err
	}
	for _, a := range in.Agents {
		if _, err := tx.Exec(c, `INSERT INTO connection_agents (connection_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, v.ID, a); err != nil {
			return err
		}
	}
	if err := tx.Commit(c); err != nil {
		return err
	}
	if err := cn.tell(c, v.ID, v.Agents); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"agents": in.Agents})
	return nil
}

// Remove disconnects: its agents stop using it, and its tokens are deleted.
func (cn *Connections) Remove(c *gin.Context) error {
	v, err := cn.one(c)
	if err != nil {
		return err
	}
	if _, err := cn.DB.Exec(c, `DELETE FROM connections WHERE id = $1`, v.ID); err != nil {
		return err
	}
	if err := cn.tell(c, "", v.Agents); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"removed": v.ID})
	return nil
}

// PickFiles is what Google's file picker needs in the owner's browser: a fresh
// access token (only for files this app may open), the API key and app id.
func (cn *Connections) PickFiles(c *gin.Context) error {
	v, err := cn.one(c)
	if err != nil {
		return err
	}
	if v.Connector != "google" || !v.Access.Has("drive") || cn.Key == nil || cn.Picker.APIKey == "" {
		return utils.ErrConnectionsOff
	}
	var sealed []byte
	if err := cn.DB.QueryRow(c, `SELECT secret FROM connections WHERE id = $1`, v.ID).Scan(&sealed); err != nil {
		return err
	}
	grant, err := connect.Open(*cn.Key, v.ID, sealed)
	if err != nil {
		return err
	}
	tok, err := cn.store().Fresh(c.Request.Context(), v.ID, grant, cn.HTTP)
	if err != nil {
		return utils.ErrSignInAgain
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"token": tok.AccessToken, "api_key": cn.Picker.APIKey, "app_id": cn.Picker.AppID})
	return nil
}

// Files sets the Drive files the owner picked for their agents to read.
func (cn *Connections) Files(c *gin.Context) error {
	var in struct {
		Files []connect.File `json:"files"`
	}
	if c.ShouldBindJSON(&in) != nil || len(in.Files) > filesAtMost {
		return utils.ErrBadRequest
	}
	v, err := cn.one(c)
	if err != nil {
		return err
	}
	if v.Connector != "google" {
		return utils.ErrBadRequest
	}
	v.Access.Files = in.Files
	if err := cn.store().SaveAccess(c, v.ID, v.Access); err != nil {
		return err
	}
	if err := cn.tell(c, v.ID, nil); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"files": in.Files})
	return nil
}

type agentConnection struct {
	connectionView
	Name  string         `json:"name"`
	Parts []connect.Part `json:"parts"` // what it may do, of the parts allowed
}

// AgentTools is an agent's tools (behind middlewares.OwnAgent): the
// connections they were given and those of the account they weren't, what
// each may do, whether changes ask and the web is theirs as their settings
// say, and what they used lately (tools, searches, pages), newest first.
func (cn *Connections) AgentTools(c *gin.Context) error {
	ag := middlewares.Owned(c)
	all, err := cn.connections(c, middlewares.Account(c).ID, "")
	if err != nil {
		return err
	}
	given, offer := []agentConnection{}, []agentConnection{}
	for _, v := range all {
		k, ok := connect.Find(v.Connector)
		if !ok {
			continue
		}
		ac := agentConnection{connectionView: v, Name: k.Name, Parts: []connect.Part{}}
		for _, p := range k.Parts {
			if v.Access.Has(p.ID) {
				ac.Parts = append(ac.Parts, p)
			}
		}
		if slices.Contains(v.Agents, ag.ID) {
			given = append(given, ac)
		} else {
			offer = append(offer, ac)
		}
	}
	st := store.New(cn.DB).For(ag.ID)
	p, err := (&Settings{}).policy(c, st)
	if err != nil {
		return err
	}
	lately := []store.WebEntry{}
	for kind, said := range map[string]string{"tool": "Looked with ", "action": "Did ", "search": "Searched the web for ", "page": "Read "} {
		es, err := st.WebLog(c, kind, 12)
		if err != nil {
			return err
		}
		for _, e := range es {
			e.What = said + e.What
			lately = append(lately, e)
		}
	}
	slices.SortFunc(lately, func(a, b store.WebEntry) int { return b.At.Compare(a.At) })
	lately = lately[:min(len(lately), 12)]
	utils.SuccessWithData(c, http.StatusOK, gin.H{"given": given, "offer": offer, "lately": lately,
		"changes": verdictOf(p, action.Write), "comments": verdictOf(p, action.ReachOut), "web": verdictOf(p, action.ReadWeb)})
	return nil
}

// Use gives a connection of the account to this agent, or takes it away.
func (cn *Connections) Use(c *gin.Context) error {
	var in struct {
		Use bool `json:"use"`
	}
	if c.ShouldBindJSON(&in) != nil {
		return utils.ErrBadRequest
	}
	v, err := cn.one(c)
	if err != nil {
		return err
	}
	ag := middlewares.Owned(c)
	q := `DELETE FROM connection_agents WHERE connection_id = $1 AND agent_id = $2`
	if in.Use {
		q = `INSERT INTO connection_agents (connection_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	}
	if _, err := cn.DB.Exec(c, q, v.ID, ag.ID); err != nil {
		return err
	}
	if err := cn.tell(c, "", []string{ag.ID}); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"use": in.Use, "connection": v.ID})
	return nil
}
