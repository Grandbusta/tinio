package system2

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The reply with what it learned (Request.CollectFacts) is the same JSON from
// every provider: ReplyWithFacts is its schema, ParseFacts reads it, and
// ReplySoFar is the reply in what's been written of it so far.

// maxAnswers is the most answers to tap a reply offers.
const maxAnswers = 4

// ReplyWithFacts is the Structured Outputs schema used with CollectFacts:
// strict mode guarantees the output matches it. The model writes the
// properties in order, which JSON gives a map alphabetically, so most of what
// it saves comes before the reply. Reply first was tried (20 Nov): its first
// words showed 0.4s sooner, but replies were blander and a fact went unsaved.
var ReplyWithFacts = map[string]any{
	"type": "object", "additionalProperties": false, "required": []string{"reply", "remember", "who_they_are", "reminders", "promises", "permissions", "corrections", "misread", "feedback", "lessons", "tastes", "send", "moves", "act", "look", "topic", "answers", "many"},
	"properties": map[string]any{
		"reply":        map[string]any{"type": "string"},
		"look":         map[string]any{"type": "array", "items": LookSchema},
		"topic":        map[string]any{"type": "string", "description": "the name the note asks for, if it does; otherwise empty"},
		"answers":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"many":         map[string]any{"type": "boolean"},
		"who_they_are": map[string]any{"type": "string"},
		"reminders": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"when", "what"},
			"properties": map[string]any{
				"when": map[string]any{"type": "string", "description": "local time, YYYY-MM-DDTHH:MM"},
				"what": map[string]any{"type": "string"},
			},
		}},
		"permissions": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"class", "verdict"},
			"properties": map[string]any{
				"class":   map[string]any{"type": "string"},
				"verdict": map[string]any{"type": "string", "enum": []string{"auto", "ask", "forbid"}},
			},
		}},
		"corrections": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"fact", "action", "text"},
			"properties": map[string]any{
				"fact":   map[string]any{"type": "string", "description": "the fact's ref from the note, e.g. f2"},
				"action": map[string]any{"type": "string", "enum": []string{"replace", "forget"}},
				"text":   map[string]any{"type": "string", "description": "the right fact, for replace; empty for forget"},
			},
		}},
		"misread": map[string]any{"type": "string"},
		"act":     map[string]any{"type": "array", "items": ActSchema},
		"moves": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"promise", "by", "as"},
			"properties": map[string]any{
				"promise": map[string]any{"type": "string", "description": "the ref of a promise in the note, e.g. p1"},
				"by":      map[string]any{"type": "string", "description": "the new local time, YYYY-MM-DDTHH:MM, or now"},
				"as":      map[string]any{"type": "string", "description": "the file it now goes as, e.g. ics; empty to keep it as it is"},
			},
		}},
		"send": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"work", "as"},
			"properties": map[string]any{
				"work": map[string]any{"type": "string", "description": "the ref of a work you made, e.g. w2, or empty"},
				"as":   map[string]any{"type": "string", "description": "the file to send it as, e.g. pdf, or empty"},
			},
		},
		"lessons": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"skill", "lesson"},
			"properties": map[string]any{
				"skill":  map[string]any{"type": "string"},
				"lesson": map[string]any{"type": "string"},
			},
		}},
		"tastes": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"kind", "likes", "taste"},
			"properties": map[string]any{
				"kind":  map[string]any{"type": "string"},
				"likes": map[string]any{"type": "boolean"},
				"taste": map[string]any{"type": "string"},
			},
		}},
		"feedback": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"skill", "level", "about"},
			"properties": map[string]any{
				"skill": map[string]any{"type": "string"},
				"level": map[string]any{"type": "integer"},
				"about": map[string]any{"type": "string"},
			},
		}},
		"promises": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"by", "what", "ability", "query", "as", "work"},
			"properties": map[string]any{
				"by":      map[string]any{"type": "string", "description": "local time, YYYY-MM-DDTHH:MM"},
				"what":    map[string]any{"type": "string"},
				"ability": map[string]any{"type": "string", "description": "an ability it needs, or empty"},
				"query":   map[string]any{"type": "string", "description": "what the ability is to look for, or empty"},
				"as":      map[string]any{"type": "string", "description": "the file to send it as, e.g. pdf, or empty for a message"},
				"work":    map[string]any{"type": "string", "description": "the ref of a work you already made that this sends, e.g. w2, or empty"},
			},
		}},
		"remember": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"about", "fact", "areas"},
			"properties": map[string]any{
				"about": map[string]any{"type": "string", "enum": []string{string(AboutSelf), string(AboutSpeaker)}},
				"fact":  map[string]any{"type": "string"},
				"areas": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
					"description": "the parts of life it's about, from the list"},
			},
		}},
	},
}

// ParseFacts is the reply and what it learned, from text in ReplyWithFacts'
// shape; Provider, Usage and CostUSD are left for the caller.
func ParseFacts(text string) (Response, error) {
	var (
		remember   []Memory
		whoTheyAre string
		reminders  []Reminder
		promises   []Promise
		perms      []Permission
		fixes      []Correction
		misread    string
		topic      string
		answers    []string
		many       bool
		send       Send
		moves      []Move
		acts       []Act
		looks      []Look
		feedback   []Feedback
		lessons    []LessonLearned
		tastes     []Taste
	)
	reply := text
	var out struct {
		Reply      string `json:"reply"`
		WhoTheyAre string `json:"who_they_are"`
		Remember   []struct {
			About string   `json:"about"`
			Fact  string   `json:"fact"`
			Areas []string `json:"areas"`
		} `json:"remember"`
		Reminders []struct {
			When string `json:"when"`
			What string `json:"what"`
		} `json:"reminders"`
		Promises []struct {
			By      string `json:"by"`
			What    string `json:"what"`
			Ability string `json:"ability"`
			Query   string `json:"query"`
			As      string `json:"as"`
			Work    string `json:"work"`
		} `json:"promises"`
		Permissions []struct {
			Class   string `json:"class"`
			Verdict string `json:"verdict"`
		} `json:"permissions"`
		Corrections []struct {
			Fact   string `json:"fact"`
			Action string `json:"action"`
			Text   string `json:"text"`
		} `json:"corrections"`
		Misread string   `json:"misread"`
		Topic   string   `json:"topic"`
		Answers []string `json:"answers"`
		Many    bool     `json:"many"`
		Act     []struct {
			Tool string `json:"tool"`
			Args string `json:"args"`
			Why  string `json:"why"`
		} `json:"act"`
		Look []struct {
			Tool  string `json:"tool"`
			Query string `json:"query"`
		} `json:"look"`
		Moves []struct {
			Promise string `json:"promise"`
			By      string `json:"by"`
			As      string `json:"as"`
		} `json:"moves"`
		Send struct {
			Work string `json:"work"`
			As   string `json:"as"`
		} `json:"send"`
		Feedback []struct {
			Skill string `json:"skill"`
			Level int    `json:"level"`
			About string `json:"about"`
		} `json:"feedback"`
		Lessons []struct {
			Skill  string `json:"skill"`
			Lesson string `json:"lesson"`
		} `json:"lessons"`
		Tastes []struct {
			Kind  string `json:"kind"`
			Likes bool   `json:"likes"`
			Taste string `json:"taste"`
		} `json:"tastes"`
	}
	if err := json.Unmarshal([]byte(reply), &out); err != nil {
		return Response{}, fmt.Errorf("the reply is not the JSON asked for: %w", err)
	}
	reply = strings.TrimSpace(out.Reply)
	whoTheyAre = strings.TrimSpace(out.WhoTheyAre)
	for _, f := range out.Remember {
		about := About(f.About)
		if text := strings.TrimSpace(f.Fact); text != "" && (about == AboutSelf || about == AboutSpeaker) {
			var areas []string
			for _, a := range f.Areas {
				if a = strings.ToLower(strings.TrimSpace(a)); a != "" {
					areas = append(areas, a)
				}
			}
			remember = append(remember, Memory{About: about, Text: text, Areas: areas})
		}
	}
	for _, r := range out.Reminders {
		if when, what := strings.TrimSpace(r.When), strings.TrimSpace(r.What); when != "" && what != "" {
			reminders = append(reminders, Reminder{When: when, What: what})
		}
	}
	for _, p := range out.Permissions {
		if class := strings.TrimSpace(p.Class); class != "" {
			perms = append(perms, Permission{Class: class, Verdict: strings.TrimSpace(p.Verdict)})
		}
	}
	for _, c := range out.Corrections {
		fact, text := strings.TrimSpace(c.Fact), strings.TrimSpace(c.Text)
		if fact != "" && (c.Action == "forget" || text != "") {
			fixes = append(fixes, Correction{Fact: fact, Forget: c.Action == "forget", Text: text})
		}
	}
	misread = strings.TrimSpace(out.Misread)
	topic = strings.TrimSpace(out.Topic)
	for _, a := range out.Answers {
		if a = strings.TrimSpace(a); a != "" && len(answers) < maxAnswers {
			answers = append(answers, a)
		}
	}
	many = out.Many && len(answers) > 1
	for _, l := range out.Look {
		if tool := strings.TrimSpace(l.Tool); tool != "" {
			looks = append(looks, Look{Tool: tool, Query: strings.TrimSpace(l.Query)})
		}
	}
	for _, a := range out.Act {
		if tool := strings.TrimSpace(a.Tool); tool != "" {
			acts = append(acts, Act{Tool: tool, Args: strings.TrimSpace(a.Args), Why: strings.TrimSpace(a.Why)})
		}
	}
	for _, mv := range out.Moves {
		if ref, by := strings.Trim(strings.TrimSpace(mv.Promise), "[]"), strings.ToLower(strings.TrimSpace(mv.By)); ref != "" && by != "" {
			moves = append(moves, Move{Promise: ref, By: by, As: strings.ToLower(strings.TrimSpace(mv.As))})
		}
	}
	if work := strings.Trim(strings.TrimSpace(out.Send.Work), "[]"); work != "" {
		send = Send{Work: work, As: strings.ToLower(strings.TrimSpace(out.Send.As))}
	}
	for _, l := range out.Lessons {
		if skill, text := strings.TrimSpace(l.Skill), strings.TrimSpace(l.Lesson); skill != "" && text != "" {
			lessons = append(lessons, LessonLearned{Skill: skill, Text: text})
		}
	}
	for _, t := range out.Tastes {
		if text := strings.TrimSpace(t.Taste); text != "" {
			tastes = append(tastes, Taste{Kind: strings.TrimSpace(t.Kind), Likes: t.Likes, Text: text})
		}
	}
	for _, f := range out.Feedback {
		if skill := strings.TrimSpace(f.Skill); skill != "" && f.Level >= 1 && f.Level <= 10 {
			feedback = append(feedback, Feedback{Skill: skill, Level: f.Level, About: strings.TrimSpace(f.About)})
		}
	}
	for _, p := range out.Promises {
		if by, what := strings.TrimSpace(p.By), strings.TrimSpace(p.What); by != "" && what != "" {
			promises = append(promises, Promise{By: by, What: what, Ability: strings.TrimSpace(p.Ability),
				Query: strings.TrimSpace(p.Query), As: strings.ToLower(strings.TrimSpace(p.As)),
				Work: strings.Trim(strings.TrimSpace(p.Work), "[]")})
		}
	}
	return Response{
		Text: reply, Remember: remember, WhoTheyAre: whoTheyAre, Reminders: reminders, Promises: promises,
		Permissions: perms, Corrections: fixes, Misread: misread, Topic: topic, Answers: answers, Many: many,
		Feedback: feedback, Lessons: lessons, Tastes: tastes, Send: send, Moves: moves, Acts: acts, Looks: looks,
	}, nil
}

// ReplySoFar is the reply in the start of the JSON replyWithFacts asks for,
// its escapes decoded: "" until it begins.
func ReplySoFar(js string) string {
	_, rest, ok := strings.Cut(js, `"reply":`)
	if rest = strings.TrimLeft(rest, " \t\r\n"); !ok || !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end, escape := len(rest), -1
	for i := 0; i < len(rest) && end == len(rest); i++ {
		switch rest[i] {
		case '\\':
			escape = i
			i++
		case '"':
			end = i
		}
	}
	s := rest[:end]
	if end == len(rest) {
		// What's cut off at the end waits for the rest of it: an escape (\n,
		// \u00e9), or the first half of a surrogate pair (\ud83d), so no
		// half of a character shows.
		if need := 2; escape >= 0 {
			if strings.HasPrefix(s[escape:], `\u`) {
				need = 6
			}
			if escape+need > len(s) {
				s = s[:escape]
			}
		}
		if n := len(s); n >= 6 && s[n-6:n-4] == `\u` {
			if hi := strings.ToLower(s[n-4 : n-2]); hi >= "d8" && hi < "dc" {
				s = s[:n-6]
			}
		}
	}
	var text string
	if json.Unmarshal([]byte(`"`+s+`"`), &text) != nil {
		return ""
	}
	return text
}
