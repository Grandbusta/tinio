package host

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/ability/journal"
	"github.com/Grandbusta/tinio/internal/ability/reading"
	"github.com/Grandbusta/tinio/internal/agent"
	"github.com/Grandbusta/tinio/internal/channel/web"
	"github.com/Grandbusta/tinio/internal/config"
	"github.com/Grandbusta/tinio/internal/decision"
	"github.com/Grandbusta/tinio/internal/decision/questions"
	"github.com/Grandbusta/tinio/internal/library"
	"github.com/Grandbusta/tinio/internal/mind"
	"github.com/Grandbusta/tinio/internal/pricing"
	"github.com/Grandbusta/tinio/internal/providers/embed"
	embedopenai "github.com/Grandbusta/tinio/internal/providers/embed/openai"
	"github.com/Grandbusta/tinio/internal/providers/system1"
	s1openai "github.com/Grandbusta/tinio/internal/providers/system1/openai"
	"github.com/Grandbusta/tinio/internal/providers/system1/reasoned"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	s2openai "github.com/Grandbusta/tinio/internal/providers/system2/openai"
	"github.com/Grandbusta/tinio/internal/render"
	"github.com/Grandbusta/tinio/internal/render/archive"
	"github.com/Grandbusta/tinio/internal/render/calendar"
	"github.com/Grandbusta/tinio/internal/render/docx"
	"github.com/Grandbusta/tinio/internal/render/midi"
	"github.com/Grandbusta/tinio/internal/render/pdf"
	"github.com/Grandbusta/tinio/internal/render/table"
	"github.com/Grandbusta/tinio/internal/store"
)

// Models are what every agent thinks with, on the platform's keys.
type Models struct {
	System1 system1.DecisionModel
	System2 system2.Reasoner
	// Day is System 2 for their own day, with the day's model and service tier
	// (daily_life.model, daily_life.service_tier); nil: System2.
	Day system2.Reasoner
	// Work is System 2 for making works (OPENAI_WORK_MODEL); nil: System2.
	Work system2.Reasoner
	// Light writes replies once an account's week is used (OPENAI_LIGHT_MODEL); nil: System2.
	Light system2.Reasoner
	// Embedder nil: they take the owner's newest decision of the right type instead.
	Embedder embed.Embedder
	// Library is books and Wikipedia for them to read; nil: they don't. The
	// web search isn't given to agents yet: what it should cost is undecided.
	Library reading.Library
	// reasoner is a model by name on the reasoning API, at an effort ("low",
	// ...); nil (tests' fakes): there's none, and System1 answers every set.
	reasoner func(model, effort string) system2.Reasoner
}

// Reasoner is model on the reasoning API, at effort; nil if there's none.
func (m Models) Reasoner(model, effort string) system2.Reasoner {
	if m.reasoner == nil {
		return nil
	}
	return m.reasoner(model, effort)
}

// Routed is System 1 with each set that names a model answered by it.
func (m Models) Routed(sets ...questions.Set) system1.DecisionModel {
	if m.reasoner == nil {
		return m.System1
	}
	by := system1.ByModel{Default: m.System1, Models: map[string]system1.DecisionModel{}}
	for _, s := range sets {
		if s.Model != "" && by.Models[s.Model] == nil {
			// Low effort: a set's answers are short, and a reply may wait on them.
			by.Models[s.Model] = reasoned.Model{Reasoner: m.reasoner(s.Model, "low")}
		}
	}
	return by
}

// OpenAI is the models the environment names (see config.Load), on the
// owner's key: apiKey, or OPENAI_API_KEY if that's "".
func OpenAI(log *slog.Logger, apiKey string) (Models, error) {
	return OpenAIWith(log, apiKey, Chosen{})
}

// Chosen is the models the owner chose for each job, "" leaving a job as the
// environment or Tinio's default has it.
type Chosen struct {
	Reply, Decision, Day, Light, Work string
}

// OpenAIWith is OpenAI with the models the owner chose.
func OpenAIWith(log *slog.Logger, apiKey string, chosen Chosen) (Models, error) {
	cfg := config.Load()
	if apiKey != "" {
		cfg.Decision.APIKey, cfg.Reasoning.APIKey, cfg.Embedding.APIKey = apiKey, apiKey, apiKey
	}
	set := func(to *string, model string) {
		if model != "" {
			*to = model
		}
	}
	set(&cfg.Reasoning.Model, chosen.Reply)
	set(&cfg.Decision.Model, chosen.Decision)
	set(&cfg.DailyLife.Model, chosen.Day)
	set(&cfg.Reasoning.LightModel, chosen.Light)
	set(&cfg.DailyLife.WorkModel, chosen.Work)
	d, r := cfg.Decision, cfg.Reasoning
	if r.APIKey == "" || r.Model == "" {
		return Models{}, fmt.Errorf("host: set your OpenAI key, and OPENAI_MODEL, to run agents")
	}
	s1model := d.Model
	if s1model == "" {
		s1model = s1openai.DefaultModel
	}
	if err := Priced(s1model, r.Model, r.LightModel, cfg.DailyLife.Model, cfg.DailyLife.WorkModel, cfg.Embedding.Model); err != nil {
		return Models{}, err
	}
	price := func(model string) pricing.Price { return pricing.OpenAI[model] }
	m := Models{
		System1: s1openai.New(d.APIKey, s1model, d.BaseURL, price(s1model)),
		System2: system2.WithoutEmDashes(s2openai.New(r.APIKey, r.Model, r.BaseURL, price(r.Model), r.Effort, "")),
	}
	if day := cfg.DailyLife; day.Model != "" || day.ServiceTier != "" {
		model := r.Model
		if day.Model != "" {
			model = day.Model
		}
		m.Day = system2.WithoutEmDashes(s2openai.New(r.APIKey, model, r.BaseURL, price(model), r.Effort, day.ServiceTier))
	}
	if model := r.LightModel; model != "" {
		m.Light = system2.WithoutEmDashes(s2openai.New(r.APIKey, model, r.BaseURL, price(model), r.Effort, ""))
	}
	if model := cfg.DailyLife.WorkModel; model != "" {
		m.Work = system2.WithoutEmDashes(s2openai.New(r.APIKey, model, r.BaseURL, price(model), r.Effort, ""))
	}
	m.reasoner = func(model, effort string) system2.Reasoner {
		return s2openai.New(r.APIKey, model, r.BaseURL, price(model), effort, "")
	}
	e := cfg.Embedding
	m.Embedder = embedopenai.New(e.APIKey, e.Model, e.BaseURL, price(e.Model))
	m.Library = library.New("", "")
	return m, nil
}

// Priced is an error naming any of models ("" is none) with no price in
// internal/pricing: its cost would be recorded as $0, and no budget or cap
// could hold.
func Priced(models ...string) error {
	var missing []string
	for _, m := range models {
		if _, ok := pricing.OpenAI[m]; m != "" && !ok {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("host: no price for %s in internal/pricing: its cost would count as $0", strings.Join(missing, ", "))
	}
	return nil
}

// shared is what the host reads once, for every agent.
type shared struct {
	cfg    *config.Config // read only: each agent's is a copy
	models Models

	incoming, followUp, lifeArea, personCircle questions.Set
	factRelevance, openThreads, topicSort      questions.Set
	skillLevel, stageCheck                     questions.Set
	reply, topic                               mind.Prompt
	day                                        mind.Day // only its prompts: the rest is each agent's
	formats                                    *render.Set
}

// Formats are the files a work can be made into, in order of preference.
func Formats() *render.Set {
	return render.NewSet(pdf.Format{}, docx.Format{}, table.XLSX{}, table.CSV{}, calendar.Format{}, midi.Format{}, archive.Format{})
}

func loadShared(files fs.FS, models Models) (shared, error) {
	sh := shared{models: models, formats: Formats()}
	cfg := config.Load()
	sh.cfg = cfg
	sets, err := questions.LoadDir(files, cfg.Decision.QuestionSets)
	if err != nil {
		return sh, err
	}
	for _, s := range sets {
		if err := Priced(s.Model); err != nil {
			return sh, fmt.Errorf("%w (question set %s)", err, s.Ref())
		}
	}
	sh.models.System1 = models.Routed(slices.Collect(maps.Values(sets))...)
	// The mind leans on each set's shape, so a wrong one stops the host here
	// rather than an agent mid-reply.
	for _, s := range []struct {
		to    *questions.Set
		name  string
		check func(questions.Set) bool
	}{
		{&sh.incoming, "incoming_message", nil},
		{&sh.followUp, "follow_up", nil},
		{&sh.personCircle, "person_circle", nil},
		{&sh.lifeArea, "life_area", lifeAreaShaped},
		{&sh.factRelevance, "fact_relevance", factTemplate},
		{&sh.openThreads, "open_threads", factTemplate},
		{&sh.topicSort, "topic", topicShaped},
		{&sh.skillLevel, "skill_level", one(decision.QuestionScore)},
		{&sh.stageCheck, "stage_check", one(decision.QuestionPredicate)},
	} {
		set, ok := sets[s.name]
		if !ok {
			return sh, fmt.Errorf("host: question set %s not found in %s", s.name, cfg.Decision.QuestionSets)
		}
		if s.check != nil && !s.check(set) {
			return sh, fmt.Errorf("host: question set %s isn't the shape the mind needs", set.Ref())
		}
		*s.to = set
	}
	dl := cfg.DailyLife
	for _, p := range []struct {
		to   *mind.Prompt
		path string
	}{{&sh.reply, cfg.Reasoning.Prompt}, {&sh.topic, cfg.Reasoning.TopicPrompt}, {&sh.day.PlanPrompt, dl.PlanPrompt}, {&sh.day.ActivityPrompt, dl.ActivityPrompt},
		{&sh.day.ReviewPrompt, dl.ReviewPrompt}, {&sh.day.LearnPrompt, dl.LearnPrompt}, {&sh.day.DataPrompt, dl.DataPrompt},
		{&sh.day.ObservePrompt, dl.ObservePrompt}} {
		text, err := fs.ReadFile(files, p.path)
		if err != nil {
			return sh, fmt.Errorf("host: read prompt: %w", err)
		}
		// The ref is the file's name and a short hash of its text, so every edit
		// is a new version in the decision log.
		sum := sha256.Sum256(text)
		name := strings.TrimSuffix(filepath.Base(p.path), filepath.Ext(p.path))
		*p.to = mind.Prompt{Ref: fmt.Sprintf("%s@%x", name, sum[:4]), Text: string(text)}
	}
	return sh, nil
}

func one(t decision.QuestionType) func(questions.Set) bool {
	return func(s questions.Set) bool { return len(s.Questions) == 1 && s.Questions[0].Type == t }
}

func factTemplate(s questions.Set) bool {
	return one(decision.QuestionPredicate)(s) && strings.Contains(s.Questions[0].Instructions, "{fact}")
}

// topicShaped is two yes/no questions: whether a message carries on with
// {topic}, asked of each, then whether it starts one.
func topicShaped(s questions.Set) bool {
	return len(s.Questions) == 2 && s.Questions[0].Type == decision.QuestionPredicate &&
		strings.Contains(s.Questions[0].Instructions, "{topic}") && s.Questions[1].Type == decision.QuestionPredicate
}

func lifeAreaShaped(s questions.Set) bool {
	want := map[string]decision.QuestionType{
		"area": decision.QuestionChoice, "personal": decision.QuestionPredicate, "broad": decision.QuestionPredicate,
	}
	for _, q := range s.Questions {
		if t, ok := want[q.Name]; ok && t == q.Type {
			delete(want, q.Name)
		}
	}
	return len(want) == 0 && s.Thresholds.Label > 0
}

// mind is r's agent as their database holds them now, on the platform's
// defaults with their character's day, which they live only if h.Days.
// mind builds an agent's mind; done ends what it connected to for them
// (their connections' servers), once it stops.
func (h *Host) mind(ctx context.Context, r *runner) (m *mind.Mind, done func(), err error) {
	st := store.New(h.pool).For(r.row.id)
	ag, err := agent.Load(ctx, st)
	if err != nil {
		return nil, nil, err
	}
	cfg := *h.shared.cfg // a copy: only its strings and the check-ins' slice change below
	day := ag.Life.Day
	cfg.Initiative.Timezone, cfg.Initiative.CheckIns = ag.Life.Timezone, day.CheckIns
	for _, s := range []struct {
		to   *string
		from string
	}{
		{&cfg.DailyLife.PlanAt, day.PlanAt}, {&cfg.DailyLife.JournalAt, day.JournalAt}, {&cfg.DailyLife.ReviewAt, day.ReviewAt},
	} {
		if s.from != "" {
			*s.to = s.from
		}
	}
	zone, err := cfg.Initiative.Zone()
	if err != nil {
		return nil, nil, err
	}
	checkIns, err := cfg.Initiative.CheckInTimes()
	if err != nil {
		return nil, nil, err
	}
	var quiet mind.QuietHours
	if r.row.quietFrom != "" {
		from, err1 := time.Parse("15:04", r.row.quietFrom)
		until, err2 := time.Parse("15:04", r.row.quietUntil)
		if err := errors.Join(err1, err2); err != nil {
			return nil, nil, fmt.Errorf("quiet hours: %w", err)
		}
		midnight := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
		quiet = mind.QuietHours{From: from.Sub(midnight), Until: until.Sub(midnight)}
	}
	policy, err := cfg.Policy()
	if err != nil {
		return nil, nil, err
	}
	policy.Owner = r.row.speaker()
	for _, c := range ag.Life.Makes {
		if _, ok := h.shared.formats.Data(c.Data); c.Data != "" && !ok {
			return nil, nil, fmt.Errorf("character: makes %q carries data %q, which no file is made from", c.Kind, c.Data)
		}
	}
	var kept mind.Kept
	if len(r.row.kept) > 0 {
		if err := json.Unmarshal(r.row.kept, &kept); err != nil {
			return nil, nil, fmt.Errorf("what they kept: %w", err)
		}
	}
	sh := h.shared
	var lived mind.Day // zero: no day of their own
	if h.Days {
		lived = sh.day
		dl := cfg.DailyLife
		if lived.PlanAt, lived.JournalAt, err = dl.Times(); err != nil {
			return nil, nil, err
		}
		if lived.ReviewDay, lived.ReviewAt, err = dl.ReviewTime(); err != nil {
			return nil, nil, err
		}
		pace := max(r.row.pace, 1)
		lived.MaxActivities, lived.BudgetUSD, lived.Pace = dl.Activities, dl.BudgetUSD*float64(pace), pace
	}
	log := h.log.With("agent", r.row.id)
	abilities := []ability.Ability{journal.Journal{Store: st}}
	if sh.models.Library != nil {
		abilities = append(abilities, reading.Article{Library: sh.models.Library, Store: st}, reading.Book{Library: sh.models.Library, Store: st})
	}
	var doers []mind.Doer
	var closes []func()
	if h.Connections != nil {
		given, err := h.Connections.Given(ctx, r.row.id)
		if err != nil {
			log.Warn("their connections weren't loaded", "err", err)
		}
		for _, g := range given {
			reads, ds, end := g.Tools(ctx, *h.Connections, st, cfg.Tools.CallsPerDay, h.http, log)
			abilities, doers, closes = append(abilities, reads...), append(doers, ds...), append(closes, end)
		}
	}
	done = func() {
		for _, end := range closes {
			end()
		}
	}
	chat := web.Channel{Store: st}
	return mind.New(mind.Config{
		RestAfter: h.RestAfter, Kept: kept,
		Life: ag.Life, Judgment: ag.Judgment, JudgmentApproved: r.row.ready, Skills: ag.Skills,
		Percepts: r.percepts, Ticks: r.ticks, Store: st, Replier: chat, Drafter: chat, Asker: chat, Filer: chat,
		System1: sh.models.System1, System2: sh.models.System2, DaySystem2: sh.models.Day, WorkSystem2: sh.models.Work,
		LightSystem2: sh.models.Light,
		Embedder:     sh.models.Embedder,
		Incoming:     sh.incoming, FollowUp: sh.followUp, LifeArea: sh.lifeArea, PersonCircle: sh.personCircle,
		FactRelevance: sh.factRelevance, OpenThreads: sh.openThreads, TopicSort: sh.topicSort, TopicPrompt: sh.topic, SkillLevel: sh.skillLevel, StageCheck: sh.stageCheck,
		ReplyPrompt: sh.reply, RecallCandidates: cfg.Memory.RecallCandidates,
		Working:   mind.WorkingMemory{Turns: cfg.WorkingMemory.Turns, Window: cfg.WorkingMemory.Window},
		Zone:      zone,
		Day:       lived,
		CheckIns:  mind.CheckIns{Times: checkIns, Quiet: cfg.Initiative.Quiet},
		Quiet:     quiet,
		Abilities: ability.NewSet(abilities...),
		Doers:     doers, ActionsPerDay: cfg.Tools.ActionsPerDay,
		Autonomy: policy,
		Formats:  sh.formats,
		Calls:    mind.Calls{On: cfg.Judgment.Calls, AsksPerDay: cfg.Judgment.AsksPerDay, ReplyWindow: cfg.Judgment.ReplyWindow},
		Log:      log,
	}), done, nil
}
