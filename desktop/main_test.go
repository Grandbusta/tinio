package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// The window's /api/... is the API's /v1/...; any other address is the app's
// page.
func TestAPIOf(t *testing.T) {
	var got string
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		io.WriteString(w, "api")
	})
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	h := apiOf(api, assets, nil)

	for _, c := range []struct{ method, path, want, body string }{
		{"GET", "/api/agents", "/v1/agents", "api"},
		{"POST", "/api/agents/a1/messages", "/v1/agents/a1/messages", "api"},
		{"GET", "/agents/a1/today", "", "<!doctype html>app"},
		{"GET", "/you", "", "<!doctype html>app"},
	} {
		got = ""
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if got != c.want || rec.Body.String() != c.body {
			t.Errorf("%s %s: API saw %q and answered %q; want %q and %q", c.method, c.path, got, rec.Body.String(), c.want, c.body)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/agents", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST to a page: %d, want 404", rec.Code)
	}
}
