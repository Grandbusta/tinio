package controllers

import (
	"github.com/Grandbusta/tinio/internal/db"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Growth is how an agent is getting better at helping their owner, as their
// owner sees it: what they've learned about how the owner likes things (their
// lessons and the owner's tastes, newest first), their skills, and the last
// seven days counted. Behind middlewares.OwnAgent.
type Growth struct {
	DB  *db.DB
	Now func() time.Time // nil: the clock
}

const (
	growthWeek   = 7 * 24 * time.Hour
	learnedShown = 12
)

type learnedRow struct {
	Text  string    `json:"text"`
	Skill string    `json:"skill,omitempty"` // a lesson's; "" for a taste
	From  string    `json:"from"`            // your edits (the owner), its own work, others' work, or your taste
	At    time.Time `json:"at"`
}

type skillRow struct {
	Name  string  `json:"name"`
	Level float64 `json:"level"` // 1 to 10
}

func (g *Growth) Get(c *gin.Context) error {
	st := store.New(g.DB).For(middlewares.Owned(c).ID)
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	lessons, err := st.Lessons(c, "", learnedShown)
	if err != nil {
		return err
	}
	tastes, err := st.Tastes(c, learnedShown)
	if err != nil {
		return err
	}
	learned := []learnedRow{}
	for _, l := range lessons {
		from := map[string]string{memory.LessonFromOwner: "owner", memory.LessonFromOthers: "others"}[l.Source]
		learned = append(learned, learnedRow{Text: l.Text, Skill: l.Skill, From: orElse(from, "self"), At: l.At.UTC()})
	}
	for _, t := range tastes {
		learned = append(learned, learnedRow{Text: t.Text, From: "taste", At: t.At.UTC()})
	}
	slices.SortFunc(learned, func(a, b learnedRow) int { return b.At.Compare(a.At) })
	learned = learned[:min(len(learned), learnedShown)]
	skills, err := st.Skills(c)
	if err != nil {
		return err
	}
	sk := make([]skillRow, len(skills))
	for i, s := range skills {
		sk[i] = skillRow{Name: s.Name, Level: s.Level}
	}
	week, err := st.WeekSince(c, now.Add(-growthWeek))
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"learned": learned, "skills": sk, "week": week})
	return nil
}

func orElse(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
