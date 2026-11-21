package controllers

import (
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/folders"
	"github.com/Grandbusta/tinio/internal/host"
)

// Folders are the folders on this computer the owner adds, for the agents they
// choose: read only, or read and make files. Their agents start again on a
// change, with the tools it gives them.
type Folders struct {
	DB    *db.DB
	Rules folders.Rules
}

type folderView struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Write   bool     `json:"write"`
	Agents  []string `json:"agents"`
	Files   int      `json:"files"`
	Skipped int      `json:"skipped"` // never read: hidden files, keys, shortcuts
	More    bool     `json:"more"`    // more files than counted
	Missing bool     `json:"missing"` // the folder isn't there any more
}

const countUpTo = 5000

func (fd *Folders) mine(c *gin.Context) ([]folderView, error) {
	rows, err := fd.DB.Query(c, `
		SELECT f.id, f.name, f.path, f.can_write,
			COALESCE((SELECT json_group_array(agent_id) FROM folder_agents WHERE folder_id = f.id), '[]')
		FROM folders f WHERE f.account_id = $1 ORDER BY f.created_at`, middlewares.Account(c).ID)
	if err != nil {
		return nil, err
	}
	out, err := db.CollectRows(rows, func(r db.Scanner) (folderView, error) {
		var v folderView
		return v, r.Scan(&v.ID, &v.Name, &v.Path, &v.Write, &v.Agents)
	})
	for i, v := range out {
		p, err := fd.Rules.Peek(v.Path, countUpTo)
		out[i].Files, out[i].Skipped, out[i].More, out[i].Missing = p.Files, p.Skipped, p.More, err != nil
	}
	return out, err
}

func (fd *Folders) List(c *gin.Context) error {
	out, err := fd.mine(c)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"folders": out})
	return nil
}

// Peek is what's in a folder the owner is about to add: how many files, what
// will never be read, and whether it's their whole home folder.
func (fd *Folders) Peek(c *gin.Context) error {
	path, err := fd.Rules.Addable(c.Query("path"))
	if err != nil {
		return utils.Invalid("path", capital(err.Error())+".")
	}
	p, err := fd.Rules.Peek(path, 20000)
	if err != nil {
		return utils.Invalid("path", "Tinio couldn't look inside that folder.")
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"name": filepath.Base(path), "path": path, "files": p.Files, "skipped": p.Skipped, "more": p.More, "home": p.Home})
	return nil
}

// Add adds a folder for the agents chosen; one added already gets them too.
func (fd *Folders) Add(c *gin.Context) error {
	var in struct {
		Path   string   `json:"path"`
		Write  bool     `json:"write"`
		Agents []string `json:"agents"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	path, err := fd.Rules.Addable(in.Path)
	if err != nil {
		return utils.Invalid("path", capital(err.Error())+".")
	}
	account := middlewares.Account(c).ID
	agents, err := fd.theirs(c, in.Agents)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		return utils.Invalid("agents", "Choose at least one agent to use it.")
	}
	var id string
	err = fd.DB.WithTx(c, func(tx *db.Tx) error {
		err := tx.QueryRow(c, `SELECT id FROM folders WHERE account_id = $1 AND path = $2`, account, path).Scan(&id)
		if err == db.ErrNoRows {
			id = ulid.Make().String()
			if _, err := tx.Exec(c, `INSERT INTO folders (id, account_id, name, path, can_write, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
				id, account, filepath.Base(path), path, in.Write, time.Now().UTC()); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		for _, a := range agents {
			if _, err := tx.Exec(c, `INSERT INTO folder_agents (folder_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, a); err != nil {
				return err
			}
			tx.Notify(host.Changed, a)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return fd.answer(c, id)
}

// Change sets what agents may do in a folder, and who may use it.
func (fd *Folders) Change(c *gin.Context) error {
	var in struct {
		Write  *bool     `json:"write"`
		Agents *[]string `json:"agents"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	id, before, err := fd.folderOf(c)
	if err != nil {
		return err
	}
	var after []string
	if in.Agents != nil {
		if after, err = fd.theirs(c, *in.Agents); err != nil {
			return err
		}
	}
	err = fd.DB.WithTx(c, func(tx *db.Tx) error {
		if in.Write != nil {
			if _, err := tx.Exec(c, `UPDATE folders SET can_write = $2 WHERE id = $1`, id, *in.Write); err != nil {
				return err
			}
		}
		if in.Agents != nil {
			if _, err := tx.Exec(c, `DELETE FROM folder_agents WHERE folder_id = $1`, id); err != nil {
				return err
			}
			for _, a := range after {
				if _, err := tx.Exec(c, `INSERT INTO folder_agents (folder_id, agent_id) VALUES ($1, $2)`, id, a); err != nil {
					return err
				}
			}
		} else {
			after = before
		}
		for _, a := range union(before, after) {
			tx.Notify(host.Changed, a)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return fd.answer(c, id)
}

// Remove stops every agent using a folder. The folder and its files stay as
// they are.
func (fd *Folders) Remove(c *gin.Context) error {
	id, before, err := fd.folderOf(c)
	if err != nil {
		return err
	}
	err = fd.DB.WithTx(c, func(tx *db.Tx) error {
		if _, err := tx.Exec(c, `DELETE FROM folders WHERE id = $1`, id); err != nil {
			return err
		}
		for _, a := range before {
			tx.Notify(host.Changed, a)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return fd.List(c)
}

// Use gives one agent a folder, or takes it away from them only (behind
// middlewares.OwnAgent): from their chat and their tools.
func (fd *Folders) Use(c *gin.Context) error {
	var in struct {
		Use bool `json:"use"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	id, _, err := fd.folderOf(c)
	if err != nil {
		return err
	}
	agent := c.Param("id")
	err = fd.DB.WithTx(c, func(tx *db.Tx) error {
		q := `DELETE FROM folder_agents WHERE folder_id = $1 AND agent_id = $2`
		if in.Use {
			q = `INSERT INTO folder_agents (folder_id, agent_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
		}
		if _, err := tx.Exec(c, q, id, agent); err != nil {
			return err
		}
		tx.Notify(host.Changed, agent)
		return nil
	})
	if err != nil {
		return err
	}
	return fd.answer(c, id)
}

// folderOf is the owner's folder in the path (:folder), and the agents using it.
func (fd *Folders) folderOf(c *gin.Context) (string, []string, error) {
	var id string
	var agents []string
	err := fd.DB.QueryRow(c, `SELECT f.id, COALESCE((SELECT json_group_array(agent_id) FROM folder_agents WHERE folder_id = f.id), '[]')
		FROM folders f WHERE f.id = $1 AND f.account_id = $2`, c.Param("folder"), middlewares.Account(c).ID).Scan(&id, &agents)
	if err == db.ErrNoRows {
		return "", nil, utils.ErrNoSuchFolder
	}
	return id, agents, err
}

// theirs is ids that are the owner's agents, each once.
func (fd *Folders) theirs(c *gin.Context, ids []string) ([]string, error) {
	ags, err := models.AgentsOf(c, fd.DB, middlewares.Account(c).ID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		if slices.ContainsFunc(ags, func(a models.Agent) bool { return a.ID == id }) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// answer is the folder id with the rest of the owner's, as List gives them.
func (fd *Folders) answer(c *gin.Context, id string) error {
	out, err := fd.mine(c)
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"folders": out, "folder": id})
	return nil
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, x := range b {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
