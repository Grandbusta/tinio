package controllers

import (
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/config"
	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/host"
)

// Agents raises agents from the ready characters, and lists an account's own.
// Both are behind middlewares.SignedIn.
type Agents struct {
	DB         *db.DB
	Characters *Characters
	Now        func() time.Time // nil: the clock
}

type newAgentInput struct {
	Character string `json:"character"`
	OwnerName string `json:"owner_name"`
	Relation  string `json:"relation"`
	Timezone  string `json:"timezone"` // the owner's, from their browser; "": UTC
}

// Create raises an agent from a character the signed-in account may raise:
// their character and judgment are the newest version's, with the owner's
// name and who they are to them filled in.
func (a *Agents) Create(c *gin.Context) error {
	var in newAgentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	account := middlewares.Account(c).ID
	t, version, err := a.Characters.Template(c, account, in.Character)
	if err != nil {
		return err
	}
	owner := strings.TrimSpace(in.OwnerName)
	switch {
	case owner == "" || len([]rune(owner)) > 60:
		return utils.Invalid("owner_name", "Tell "+t.Name+" what to call you: up to 60 letters.")
	case strings.ContainsAny(owner, "\"\\\n{}"):
		return utils.Invalid("owner_name", "Your name can't have quotes, backslashes or braces in it.")
	case !slices.Contains(t.Relations, in.Relation):
		return utils.Invalid("relation", "Pick who you are to "+t.Name+".")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil || strings.ContainsAny(in.Timezone, "\"\\\n{} ") {
		return utils.Invalid("timezone", "That isn't a time zone like Africa/Lagos.")
	}
	// The owner is known to the mind as the speaker the host gives their words,
	// and the agent works in the owner's time zone.
	files, err := t.Render(character.Owner{Name: owner, Relation: in.Relation, Account: "account:" + account, Timezone: in.Timezone})
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	ag := models.Agent{AccountID: account, CharacterKey: t.Key, CharacterVersion: version, Name: t.Name, OwnerName: owner,
		Relation: in.Relation, Pace: config.Pace, CreatedAt: now}
	tx, err := a.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	if err := ag.Save(c, tx); err != nil {
		return err
	}
	for _, f := range character.Files {
		v := models.AgentVersion{AgentID: ag.ID, Section: strings.TrimSuffix(f, ".yaml"), Doc: string(files[f]),
			Source: fmt.Sprintf("character:%s@%d", t.Key, version), CreatedAt: now}
		if err := v.Save(c, tx); err != nil {
			return err
		}
	}
	// A host starts them as soon as this commits.
	tx.Notify(host.Raised, ag.ID)
	if err := tx.Commit(c); err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusCreated, gin.H{"agent": ag})
	return nil
}

// List is the signed-in account's agents, oldest first.
func (a *Agents) List(c *gin.Context) error {
	ags, err := models.AgentsOf(c, a.DB, middlewares.Account(c).ID)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"agents": ags})
	return nil
}

// One is an agent the signed-in account owns (behind middlewares.OwnAgent).
func (a *Agents) One(c *gin.Context) error {
	utils.SuccessWithData(c, http.StatusOK, gin.H{"agent": middlewares.Owned(c)})
	return nil
}
