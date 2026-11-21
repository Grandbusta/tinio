package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Grandbusta/tinio/api/middlewares"
	"github.com/Grandbusta/tinio/api/utils"
	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

// Growth is what an agent grows towards, as their owner sees it: their dreams,
// each with its goals and any learning plan, their newest weekly review, and
// the last seven days counted. Behind middlewares.OwnAgent.
type Growth struct {
	DB  *pgxpool.Pool
	Now func() time.Time // nil: the clock
}

const growthWeek = 7 * 24 * time.Hour

type stageRow struct {
	Learn string `json:"learn"`
	Done  bool   `json:"done"`
}

type planRow struct {
	Stage  int        `json:"stage"` // the one being learnt, from 0; len(stages) once done
	Status string     `json:"status"`
	Stages []stageRow `json:"stages"`
}

type goalRow struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	DoneWhen string   `json:"done_when,omitempty"`
	Due      string   `json:"due,omitempty"`      // YYYY-MM-DD
	Status   string   `json:"status"`             // proposed (waiting for their owner), active, done
	Progress string   `json:"progress,omitempty"` // as their last review put it
	Skill    string   `json:"skill,omitempty"`
	Level    float64  `json:"level,omitempty"` // the skill's, 1 to 10
	Plan     *planRow `json:"plan,omitempty"`
}

type dreamRow struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	Source string    `json:"source"` // life (who they were raised as), self or owner
	Since  time.Time `json:"since"`
	Goals  []goalRow `json:"goals"`
}

type reviewRow struct {
	Title string    `json:"title"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// Get is their active dreams, oldest first, each with its goals that aren't
// dropped (other_goals: those of no active dream), their newest review (null
// before the first) and the week.
func (g *Growth) Get(c *gin.Context) error {
	st := store.New(g.DB).For(middlewares.Owned(c).ID)
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	dreams, err := st.Dreams(c, true)
	if err != nil {
		return err
	}
	goals, err := st.Goals(c, false)
	if err != nil {
		return err
	}
	plans, err := st.Plans(c, false)
	if err != nil {
		return err
	}
	skills, err := st.Skills(c)
	if err != nil {
		return err
	}
	levels := map[string]float64{}
	for _, sk := range skills {
		levels[sk.Name] = sk.Level
	}
	planOf := map[string]*planRow{} // by goal: its newest plan not dropped
	for _, p := range plans {
		if p.Status == memory.PlanDropped {
			continue
		}
		r := &planRow{Stage: p.Stage, Status: p.Status, Stages: make([]stageRow, len(p.Stages))}
		for i, s := range p.Stages {
			r.Stages[i] = stageRow{Learn: s.Learn, Done: i < p.Stage || p.Status == memory.PlanDone}
		}
		planOf[p.GoalID.String()] = r
	}

	out := make([]dreamRow, len(dreams))
	at := map[string]int{}
	for i, d := range dreams {
		out[i] = dreamRow{ID: d.ID.String(), Text: d.Text, Source: d.Source, Since: d.Since.UTC(), Goals: []goalRow{}}
		at[d.ID.String()] = i
	}
	others := []goalRow{}
	for _, gl := range goals {
		if gl.Status == memory.GoalDropped {
			continue
		}
		r := goalRow{ID: gl.ID.String(), Text: gl.Text, DoneWhen: gl.DoneWhen, Status: gl.Status, Progress: gl.Progress,
			Skill: gl.Skill, Level: levels[gl.Skill], Plan: planOf[gl.ID.String()]}
		if !gl.Due.IsZero() {
			r.Due = gl.Due.UTC().Format(time.DateOnly)
		}
		if i, ok := at[gl.DreamID.String()]; ok {
			out[i].Goals = append(out[i].Goals, r)
		} else {
			others = append(others, r)
		}
	}

	var review *reviewRow
	if w, ok, err := st.LatestWork(c, "review"); err != nil {
		return err
	} else if ok {
		review = &reviewRow{Title: w.Title, Text: w.Text, At: w.CreatedAt.UTC()}
	}
	week, err := st.WeekSince(c, now.Add(-growthWeek))
	if err != nil {
		return err
	}
	utils.SuccessWithData(c, http.StatusOK, gin.H{"dreams": out, "other_goals": others, "review": review, "week": week})
	return nil
}
