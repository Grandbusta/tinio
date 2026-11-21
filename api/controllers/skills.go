package controllers

import (
	"errors"
	"github.com/Grandbusta/tinio/internal/db"
	"io"
	"io/fs"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/agentskill"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Skills is what an agent can do and how well: their skills on the ladder
// (Apprentice, then Pro once their scored work holds a level of proLevel), and
// the know-how their owner imported in the Agent Skills format, which they
// read when a task fits. Know-how is trusted like a prompt but grants no
// permission, and its scripts are never run. Behind middlewares.OwnAgent.
type Skills struct {
	DB *db.DB
}

const (
	proLevel     = 6.0
	scoresShown  = 5
	uploadAtMost = 12 << 20 // a skill's 10 MB, and room for the zip around it
)

type skillView struct {
	Name    string    `json:"name"`
	Level   float64   `json:"level"` // 1 to 10, from their scored work
	Rung    string    `json:"rung"`  // apprentice, pro (certified comes with a skill's tests)
	Born    bool      `json:"born"`  // in their character from the start
	ShownBy []string  `json:"shown_by"`
	Scores  []float64 `json:"scores"` // newest first
}

type knowHowView struct {
	Name        string    `json:"name"`  // its handle
	Title       string    `json:"title"` // for people
	Description string    `json:"description"`
	Files       int       `json:"files"`
	AddedAt     time.Time `json:"added_at"`
	From        string    `json:"from"` // "market" when taught from it; "" imported
}

func (sk *Skills) store(c *gin.Context) *store.Store {
	return store.New(sk.DB).For(middlewares.Owned(c).ID)
}

// List is their skills, born with first, and their imported know-how, by name.
func (sk *Skills) List(c *gin.Context) error {
	st := sk.store(c)
	skills, err := st.Skills(c)
	if err != nil {
		return err
	}
	life, err := (&Character{DB: sk.DB}).current(c, st)
	if err != nil {
		return err
	}
	born := map[string]bool{}
	for _, s := range life.Skills {
		born[s.Name] = true
	}
	out := make([]skillView, 0, len(skills))
	for _, s := range skills {
		v := skillView{Name: s.Name, Level: math.Round(s.Level*10) / 10, Rung: "apprentice", Born: born[s.Name],
			ShownBy: s.ShownBy, Scores: []float64{}}
		if s.Level >= proLevel {
			v.Rung = "pro"
		}
		ev, err := st.RecentEvidence(c, s.Name, scoresShown)
		if err != nil {
			return err
		}
		for _, e := range ev {
			v.Scores = append(v.Scores, math.Round(e.Score*10)/10)
		}
		out = append(out, v)
	}
	slices.SortStableFunc(out, func(a, b skillView) int {
		if a.Born != b.Born {
			if a.Born {
				return -1
			}
			return 1
		}
		return 0
	})
	knows, err := st.AgentSkills(c)
	if err != nil {
		return err
	}
	kv := make([]knowHowView, 0, len(knows))
	for _, k := range knows {
		var body string
		if s, err := agentskill.Parse(k.Files); err == nil {
			body = s.Body
		}
		v := knowHowView{Name: k.Name, Title: agentskill.Title(k.Name, body), Description: k.Description,
			Files: len(k.Files), AddedAt: k.AddedAt.UTC()}
		if strings.HasPrefix(k.Source, "market:") {
			v.From = "market"
		}
		kv = append(kv, v)
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"skills": out, "know_how": kv})
	return nil
}

// Import keeps an uploaded skill (a SKILL.md, or a .zip of its folder) as
// their know-how, replacing any of the same name; it reaches them within a
// minute. With ?check=1 it only reads it, for their owner to see what's in it.
func (sk *Skills) Import(c *gin.Context) error {
	s, err := uploadedSkill(c)
	if err != nil {
		return err
	}
	if c.Query("check") == "1" {
		utils.SuccessWithData(c, http.StatusOK, gin.H{"name": s.Name, "title": agentskill.Title(s.Name, s.Body), "written": s.Written,
			"description": s.Description, "files": slices.Sorted(maps.Keys(s.Files))})
		return nil
	}
	ag := middlewares.Owned(c)
	if err := store.New(sk.DB).For(ag.ID).SaveAgentSkill(c, memory.AgentSkill{Name: s.Name, Description: s.Description,
		Files: s.Files, AddedAt: time.Now().UTC()}); err != nil {
		return err
	}
	sk.DB.Notify(host.Changed, ag.ID)
	utils.SuccessWithData(c, http.StatusCreated, gin.H{"imported": knowHowView{Name: s.Name, Title: agentskill.Title(s.Name, s.Body),
		Description: s.Description, Files: len(s.Files), AddedAt: time.Now().UTC()}})
	return nil
}

// Remove takes away imported know-how; what they made with it stays.
func (sk *Skills) Remove(c *gin.Context) error {
	ag := middlewares.Owned(c)
	ok, err := store.New(sk.DB).For(ag.ID).RemoveAgentSkill(c, c.Param("name"))
	if err != nil {
		return err
	}
	if !ok {
		return utils.ErrNoSuchSkill
	}
	sk.DB.Notify(host.Changed, ag.ID)
	utils.SuccessWithData(c, http.StatusOK, gin.H{"removed": c.Param("name")})
	return nil
}

// uploadedSkill is the skill in the request's file: a SKILL.md, or a .zip of
// its folder.
func uploadedSkill(c *gin.Context) (agentskill.Skill, error) {
	fh, err := c.FormFile("file")
	if err != nil {
		return agentskill.Skill{}, utils.Invalid("file", "Choose a SKILL.md or a .zip of a skill's folder.")
	}
	if fh.Size > uploadAtMost {
		return agentskill.Skill{}, utils.Invalid("file", "That's too big: a skill is at most 10 MB.")
	}
	f, err := fh.Open()
	if err != nil {
		return agentskill.Skill{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, uploadAtMost+1))
	if err != nil {
		return agentskill.Skill{}, err
	}
	s, err := agentskill.FromUpload(fh.Filename, data)
	if errors.Is(err, fs.ErrNotExist) {
		return agentskill.Skill{}, utils.Invalid("file", "There's no SKILL.md in it. A skill's folder has one at its top.")
	}
	if err != nil {
		return agentskill.Skill{}, utils.Invalid("file", "We can't read that skill: "+err.Error()+".")
	}
	return s, nil
}
