package controllers

import (
	"context"
	"fmt"
	"github.com/Grandbusta/tinio/internal/db"
	"io/fs"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/character"
)

// CharacterInfo is a character as the app shows it before you pick.
type CharacterInfo struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	About       string   `json:"about"`
	Personality []string `json:"personality"`
	BornWith    []string `json:"born_with"` // their skills' names
	Relations   []string `json:"relations"` // who their owner can be to them
}

// Characters are what each account may raise: Tinio's ready ones and their
// own, from the database (models.CharactersFor).
type Characters struct {
	DB *db.DB

	mu     sync.Mutex
	parsed map[string]parsedCharacter // by key@version: a version never changes
}

type parsedCharacter struct {
	template character.Template
	info     CharacterInfo
}

// LoadCharacters brings the ready characters in the database up to date with
// those in files (characters/), at start.
func LoadCharacters(ctx context.Context, db *db.DB, files fs.FS) (*Characters, error) {
	ts, err := character.Templates(files, "characters")
	if err != nil {
		return nil, err
	}
	if err := models.SyncReady(ctx, db, ts, time.Now().UTC()); err != nil {
		return nil, err
	}
	return &Characters{DB: db}, nil
}

func (ch *Characters) List(c *gin.Context) error {
	cs, err := models.CharactersFor(c, ch.DB, middlewares.Account(c).ID)
	if err != nil {
		return err
	}
	list := []CharacterInfo{}
	for _, x := range cs {
		p, err := ch.parse(x)
		if err != nil {
			return err
		}
		list = append(list, p.info)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"characters": list})
	return nil
}

// Template is the newest version of the character key, if account may raise it.
func (ch *Characters) Template(ctx context.Context, account, key string) (character.Template, int, error) {
	x, ok, err := models.CharacterFor(ctx, ch.DB, account, key)
	if err != nil {
		return character.Template{}, 0, err
	}
	if !ok {
		return character.Template{}, 0, utils.ErrNoSuchCharacter
	}
	p, err := ch.parse(x)
	return p.template, x.Version, err
}

func (ch *Characters) parse(x models.Character) (parsedCharacter, error) {
	id := x.Key + "@" + strconv.Itoa(x.Version)
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if p, ok := ch.parsed[id]; ok {
		return p, nil
	}
	t, err := character.ParseTemplate(x.Key, x.Docs)
	if err != nil {
		return parsedCharacter{}, fmt.Errorf("character %s: %w", id, err)
	}
	// Rendered for someone, only to read what's always true of them.
	rendered, err := t.Render(character.Owner{Name: "you", Relation: t.Relations[0]})
	if err != nil {
		return parsedCharacter{}, fmt.Errorf("character %s: %w", id, err)
	}
	life, err := character.Parse(rendered["character.yaml"])
	if err != nil {
		return parsedCharacter{}, fmt.Errorf("character %s: %w", id, err)
	}
	info := CharacterInfo{Key: t.Key, Name: t.Name, About: t.About, Personality: life.Personality, BornWith: []string{}, Relations: t.Relations}
	for _, s := range life.Skills {
		info.BornWith = append(info.BornWith, s.Name)
	}
	if ch.parsed == nil {
		ch.parsed = map[string]parsedCharacter{}
	}
	p := parsedCharacter{template: t, info: info}
	ch.parsed[id] = p
	return p, nil
}
