package server_test

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// TestStaticFilesServedWithContentType checks that the embedded UI (web.FS)
// is served at the paths the frontend references, with the content
// types browsers need: ES modules must be text/javascript or module loading
// fails, not merely misbehaves.
func TestStaticFilesServedWithContentType(t *testing.T) {
	srv := server.New(server.Options{State: state.New()})

	cases := []struct {
		path        string
		contentType string
	}{
		{"/", "text/html"},
		{"/css/app.css", "text/css"},
		{"/js/app.js", "text/javascript"},
		{"/js/api.js", "text/javascript"},
		{"/js/live.js", "text/javascript"},
		{"/js/i18n.js", "text/javascript"},
		{"/js/views/islands.js", "text/javascript"},
		{"/js/views/history.js", "text/javascript"},
		{"/js/views/efficiency.js", "text/javascript"},
		{"/js/views/help.js", "text/javascript"},
		{"/vendor/uplot/uPlot.esm.js", "text/javascript"},
		{"/vendor/uplot/uPlot.min.css", "text/css"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200", tc.path, rec.Code)
			}
			got := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(got, tc.contentType) {
				t.Errorf("GET %s: Content-Type = %q, want prefix %q", tc.path, got, tc.contentType)
			}
			if rec.Body.Len() == 0 {
				t.Errorf("GET %s: empty body", tc.path)
			}
		})
	}
}

// TestStaticContentTypesArePinned checks the UI is served with the types
// server.go registers rather than the ones the machine happens to know.
//
// The prefix test above can pass on Linux and on one Windows PC while a
// GitHub Windows runner serves every ES module as "application/javascript":
// mime.TypeByExtension reads the registry there, and the same key is one
// other installers write - a PC where it says "text/plain" gets a blank page,
// because a browser refuses a module with that type. Pinning is only worth
// anything if it is checked, so this asserts the exact strings.
func TestStaticContentTypesArePinned(t *testing.T) {
	srv := server.New(server.Options{State: state.New()})

	for path, want := range map[string]string{
		"/":                          "text/html; charset=utf-8",
		"/css/app.css":               "text/css; charset=utf-8",
		"/js/app.js":                 "text/javascript; charset=utf-8",
		"/js/views/island-header.js": "text/javascript; charset=utf-8",
		"/vendor/uplot/uPlot.esm.js": "text/javascript; charset=utf-8",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Type"); got != want {
			t.Errorf("GET %s: Content-Type = %q, want exactly %q", path, got, want)
		}
	}

	// http.FileServerFS asks this function, so it is the one that has to
	// answer the same on every machine.
	if got := mime.TypeByExtension(".js"); got != "text/javascript; charset=utf-8" {
		t.Errorf("mime.TypeByExtension(\".js\") = %q; the registry is still deciding what the UI is served as", got)
	}
}

// TestStaticFileNotFound checks that a missing static file is a plain 404,
// not the API's JSON error body - the static handler and the API handler are
// different mux trees (server.go routes()).
func TestStaticFileNotFound(t *testing.T) {
	srv := server.New(server.Options{State: state.New()})

	req := httptest.NewRequest(http.MethodGet, "/does/not/exist.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
