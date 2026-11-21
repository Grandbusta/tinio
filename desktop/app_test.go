package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Saving takes only an agent's files, from the API in the program.
func TestSaveTakesOnlyAgentsFiles(t *testing.T) {
	a := newApp(nil, nil)
	for _, url := range []string{"/api/keys", "/api/agents/../keys", "https://example.com/x"} {
		rec := httptest.NewRecorder()
		a.serve(rec, httptest.NewRequest("POST", "/api/app/save", strings.NewReader(`{"url":"`+url+`"}`)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", url, rec.Code)
		}
	}
}

func TestClip(t *testing.T) {
	if got := clip("short", 10); got != "short" {
		t.Errorf("short: %q", got)
	}
	if got := clip("ábcdéfghij and more", 6); got != "ábcdé…" {
		t.Errorf("long: %q", got)
	}
}
