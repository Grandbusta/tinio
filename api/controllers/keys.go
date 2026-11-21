package controllers

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/models"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/db"
	"github.com/Grandbusta/tinio/internal/secret"
)

// Keys are the owner's own keys for the services Tinio uses: the only thing
// they bring. Each is sealed with the key in the data folder, and only ever
// shown masked. One set in the environment (OPENAI_API_KEY) is used instead,
// and can't be changed from here.
type Keys struct {
	DB  *db.DB
	Key secret.Key
}

type keyView struct {
	Set   bool   `json:"set"`
	Shown string `json:"shown,omitempty"` // its last few characters, masked
	From  string `json:"from,omitempty"`  // "app" or "environment"
}

func (k *Keys) Get(c *gin.Context) error {
	v := keyView{From: "app"}
	value, err := models.OpenKey(c, k.DB, k.Key, models.KeyOpenAI)
	if err != nil {
		return err
	}
	if env := os.Getenv("OPENAI_API_KEY"); env != "" {
		value, v.From = env, "environment"
	}
	if value != "" {
		v.Set, v.Shown = true, secret.Mask(value)
	} else {
		v.From = ""
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"openai": v})
	return nil
}

// Set keeps the owner's OpenAI key ("" removes it), and their agents start
// again on it.
func (k *Keys) Set(c *gin.Context) error {
	var in struct {
		OpenAI *string `json:"openai"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.OpenAI == nil {
		return utils.ErrBadRequest
	}
	value := strings.TrimSpace(*in.OpenAI)
	if value != "" && (len(value) < 20 || len(value) > 300 || strings.ContainsAny(value, " \t\n")) {
		return utils.Invalid("openai", "That doesn't look like an OpenAI key: it starts sk- and is one long word.")
	}
	if err := models.SaveKey(c, k.DB, k.Key, models.KeyOpenAI, value, time.Now().UTC()); err != nil {
		return err
	}
	k.DB.Notify(models.KeysChanged, models.KeyOpenAI)
	return k.Get(c)
}
