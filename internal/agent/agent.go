// Package agent is an agent as the platform's database holds them: their
// character and their owner's judgment (the newest of each in agent_versions),
// and the skills they've been taught.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/Grandbusta/tinio/internal/agentskill"
	"github.com/Grandbusta/tinio/internal/character"
	"github.com/Grandbusta/tinio/internal/memory"
)

type Store interface {
	Self(ctx context.Context, section string) (memory.SelfVersion, bool, error)
	AgentSkills(ctx context.Context) ([]memory.AgentSkill, error)
}

var ErrNoOne = errors.New("agent: no character yet")

type Agent struct {
	Life     character.Character
	Judgment character.Judgment
	Skills   []agentskill.Skill
	// Unreadable are stored skills left out, with why.
	Unreadable []string
}

// Load reads them from their database. Their secrets stay sealed there: many
// agents share one process, so nothing of theirs goes in its environment.
func Load(ctx context.Context, st Store) (Agent, error) {
	var p Agent
	doc, ok, err := st.Self(ctx, memory.SectionCharacter)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrNoOne
	}
	if p.Life, err = character.Parse([]byte(doc.Doc)); err != nil {
		return p, err
	}
	judgment, _, err := st.Self(ctx, memory.SectionJudgment)
	if err != nil {
		return p, err
	}
	if p.Judgment, err = character.ParseJudgment([]byte(judgment.Doc)); err != nil {
		return p, err
	}
	skills, err := st.AgentSkills(ctx)
	if err != nil {
		return p, err
	}
	for _, sk := range skills {
		s, err := agentskill.Parse(sk.Files)
		if err != nil {
			p.Unreadable = append(p.Unreadable, fmt.Sprintf("%s: %v", sk.Name, err))
			continue
		}
		p.Skills = append(p.Skills, s)
	}
	return p, nil
}
