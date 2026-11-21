package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/store"
)

const skillMD = "---\nname: brand-voice\ndescription: Write in the shop's voice.\n---\nCalm, short, we not I.\n"

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, text := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(text))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (ta *testAPI) upload(t *testing.T, path, filename string, data []byte, cookie *http.Cookie) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", filename)
	part.Write(data)
	w.Close()
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Origin", site)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	ta.router.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// Their skills climb the ladder from their scored work, born-with first; their
// owner imports know-how as a SKILL.md or a zipped folder, never one that
// reaches outside its folder, and removes it.
func TestSkillsAndImportedKnowHow(t *testing.T) {
	ta := newTestAPI(t)
	ada := ta.signUp(t)
	zara := ta.raiseZara(t, ada)
	st, ctx, now := store.New(ta.db).For(zara), context.Background(), time.Now().UTC()
	for _, s := range []memory.Skill{{Name: "social copywriting", Level: 8, ShownBy: []string{"post"}, UpdatedAt: now},
		{Name: "video editing", Level: 3, UpdatedAt: now}} {
		if err := st.AddSkill(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	path := "/v1/agents/" + zara + "/skills"

	rec, out := ta.do(t, "GET", path, nil, ada, "")
	skills, _ := data(out)["skills"].([]any)
	if rec.Code != http.StatusOK || len(skills) != 2 {
		t.Fatalf("skills: %d %v", rec.Code, out)
	}
	first, second := skills[0].(map[string]any), skills[1].(map[string]any)
	if first["name"] != "social copywriting" || first["born"] != true || first["rung"] != "pro" ||
		second["born"] != false || second["rung"] != "apprentice" {
		t.Errorf("the ladder: %v", skills)
	}

	if rec, out := ta.upload(t, path+"/import?check=1", "SKILL.md", []byte(skillMD), ada); rec.Code != http.StatusOK || data(out)["name"] != "brand-voice" {
		t.Fatalf("checking a SKILL.md: %d %v", rec.Code, out)
	}
	if _, out := ta.do(t, "GET", path, nil, ada, ""); len(data(out)["know_how"].([]any)) != 0 {
		t.Fatal("checking it kept it")
	}
	if rec, out := ta.upload(t, path+"/import", "SKILL.md", []byte(skillMD), ada); rec.Code != http.StatusCreated {
		t.Fatalf("importing a SKILL.md: %d %v", rec.Code, out)
	}
	folder := zipOf(t, map[string]string{"tea-glossary/SKILL.md": "---\nname: tea-glossary\ndescription: Name teas.\n---\nOolong is...",
		"tea-glossary/examples/one.md": "Alishan"})
	if rec, out := ta.upload(t, path+"/import", "tea-glossary.zip", folder, ada); rec.Code != http.StatusCreated {
		t.Fatalf("importing a zipped folder: %d %v", rec.Code, out)
	}
	// Written by hand: a name made a handle, a description with an unquoted colon, a title from its heading.
	hand := "---\nname: Design Docs\ndescription: The shop's way: problem, options, risks.\n---\n# Design docs, our way\nStart with the problem."
	if rec, out := ta.upload(t, path+"/import?check=1", "SKILL.md", []byte(hand), ada); rec.Code != http.StatusOK || data(out)["name"] != "design-docs" ||
		data(out)["written"] != "Design Docs" || data(out)["title"] != "Design docs, our way" {
		t.Errorf("a SKILL.md written by hand: %d %v", rec.Code, out)
	}
	for name, bad := range map[string][]byte{
		"outside.zip": zipOf(t, map[string]string{"SKILL.md": skillMD, "../evil.md": "x"}),
		"empty.zip":   zipOf(t, map[string]string{"notes.md": "no skill here"}),
		"SKILL.md":    []byte("no frontmatter"),
	} {
		if rec, _ := ta.upload(t, path+"/import", name, bad, ada); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want refused", name, rec.Code)
		}
	}
	_, out = ta.do(t, "GET", path, nil, ada, "")
	knows, _ := data(out)["know_how"].([]any)
	if len(knows) != 2 || knows[1].(map[string]any)["name"] != "tea-glossary" || knows[1].(map[string]any)["title"] != "Tea glossary" ||
		knows[1].(map[string]any)["files"] != 2.0 {
		t.Errorf("know-how: %v", knows)
	}

	if rec, _ := ta.do(t, "POST", path+"/brand-voice/remove", nil, ada, site); rec.Code != http.StatusOK {
		t.Errorf("removing: %d", rec.Code)
	}
	if rec, _ := ta.do(t, "POST", path+"/brand-voice/remove", nil, ada, site); rec.Code != http.StatusNotFound {
		t.Errorf("removing it again: %d", rec.Code)
	}
}
