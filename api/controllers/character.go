package controllers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/host"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Character is who an agent is, for their owner to read and change: their
// story, personality, routine and day, each change a new version they can go
// back from. A change reaches them within a minute (host.Changed). Their
// dreams, skills and family were copied into their memory when they were
// born, and live there now: they're shown, not changed, here. Behind
// middlewares.OwnAgent.
type Character struct {
	DB *pgxpool.Pool
}

const (
	versionsShown = 30
	linesAtMost   = 40
	lineRunes     = 400
)

type dayView struct {
	PlanAt    string   `json:"plan_at"` // HH:MM, "" for Tinio's default
	JournalAt string   `json:"journal_at"`
	ReviewAt  string   `json:"review_at"` // "Sun 20:00"
	CheckIns  []string `json:"check_ins"`
}

type versionView struct {
	ID     string    `json:"id"`
	Source string    `json:"source"` // new (raised), import, edit (by their owner) or rollback
	At     time.Time `json:"at"`
}

func (ch *Character) store(c *gin.Context) *store.Store {
	return store.New(ch.DB).For(middlewares.Owned(c).ID)
}

func (ch *Character) current(c *gin.Context, st *store.Store) (character.Character, error) {
	v, ok, err := st.Self(c, memory.SectionCharacter)
	if err != nil {
		return character.Character{}, err
	}
	if !ok {
		return character.Character{}, utils.ErrNoSuchAgent
	}
	return character.Parse([]byte(v.Doc))
}

func nonNilLines(ls []string) []string {
	if ls == nil {
		return []string{}
	}
	return ls
}

// Get is them as they are now, their active dreams, and their versions, newest
// first (the first is in use).
func (ch *Character) Get(c *gin.Context) error {
	st := ch.store(c)
	l, err := ch.current(c, st)
	if err != nil {
		return err
	}
	vs, err := st.SelfHistory(c, memory.SectionCharacter, versionsShown)
	if err != nil {
		return err
	}
	versions := make([]versionView, len(vs))
	for i, v := range vs {
		versions[i] = versionView{ID: v.ID.String(), Source: v.Source, At: v.At.UTC()}
	}
	ds, err := st.Dreams(c, true)
	if err != nil {
		return err
	}
	dreams := make([]string, len(ds))
	for i, d := range ds {
		dreams[i] = d.Text
	}
	makes := make([]gin.H, len(l.Makes))
	for i, m := range l.Makes {
		makes[i] = gin.H{"kind": strings.ReplaceAll(m.Kind, "_", " "), "about": m.About}
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{
		"name": l.Name, "story": nonNilLines(l.Story), "personality": nonNilLines(l.Personality), "routine": nonNilLines(l.Routine),
		"timezone": l.Timezone, "day": dayView{PlanAt: l.Day.PlanAt, JournalAt: l.Day.JournalAt, ReviewAt: l.Day.ReviewAt, CheckIns: nonNilLines(l.Day.CheckIns)},
		"makes": makes, "dreams": dreams, "versions": versions,
	})
	return nil
}

// lines is ls trimmed, the empty ones left out; false if there are too many or
// one is too long.
func lines(ls []string) ([]string, bool) {
	var out []string
	for _, l := range ls {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			if utf8.RuneCountInString(l) > lineRunes {
				return nil, false
			}
			out = append(out, l)
		}
	}
	return out, len(out) <= linesAtMost
}

// Save is their owner's version of their story, personality, routine and
// day, kept as a new version and given to them within a minute.
func (ch *Character) Save(c *gin.Context) error {
	var in struct {
		Story       []string `json:"story"`
		Personality []string `json:"personality"`
		Routine     []string `json:"routine"`
		Day         dayView  `json:"day"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return utils.ErrBadRequest
	}
	st := ch.store(c)
	l, err := ch.current(c, st)
	if err != nil {
		return err
	}
	var ok [3]bool
	l.Story, ok[0] = lines(in.Story)
	l.Personality, ok[1] = lines(in.Personality)
	l.Routine, ok[2] = lines(in.Routine)
	if ok != [3]bool{true, true, true} {
		return utils.Invalid("story", "Up to 40 lines each, of up to 400 characters.")
	}
	if len(l.Story) == 0 {
		return utils.Invalid("story", "Their story needs at least one line.")
	}
	checkIns, _ := lines(in.Day.CheckIns)
	l.Day = character.Day{PlanAt: strings.TrimSpace(in.Day.PlanAt), JournalAt: strings.TrimSpace(in.Day.JournalAt),
		ReviewAt: strings.TrimSpace(in.Day.ReviewAt), CheckIns: checkIns}
	return ch.keep(c, st, l, memory.SelfEdited)
}

// keep saves l as their newest character, checked as any character is, and
// tells whoever runs them.
func (ch *Character) keep(c *gin.Context, st *store.Store, l character.Character, source string) error {
	doc, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	if _, err := character.Parse(doc); err != nil {
		return utils.Invalid("day", strings.TrimPrefix(err.Error(), "character: "))
	}
	if err := st.SaveSelf(c, "", memory.SelfVersion{ID: ulid.Make(), Section: memory.SectionCharacter, Doc: string(doc), Source: source,
		At: time.Now().UTC()}); err != nil {
		return err
	}
	if _, err := ch.DB.Exec(c, `SELECT pg_notify($1, $2)`, host.Changed, middlewares.Owned(c).ID); err != nil {
		return err
	}
	return ch.Get(c)
}

// GoBack makes one of their earlier versions the one in use, as a new version.
func (ch *Character) GoBack(c *gin.Context) error {
	id, err := ulid.Parse(c.Param("version"))
	if err != nil {
		return utils.ErrNoSuchVersion
	}
	st := ch.store(c)
	vs, err := st.SelfHistory(c, memory.SectionCharacter, versionsShown)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if v.ID == id {
			l, err := character.Parse([]byte(v.Doc))
			if err != nil {
				return err
			}
			return ch.keep(c, st, l, memory.SelfRolledBack)
		}
	}
	return utils.ErrNoSuchVersion
}
