package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Building the handler panics on conflicting ServeMux patterns (e.g. two packages claiming
// "POST /api/events"); the feature routes answer 404 while their switch is off.
func TestHandlerBuildsAndSwitchesHide(t *testing.T) {
	t.Setenv("RATE_WRITE_PER_MIN", "10000") // earlier tests in this package share the in-memory limiter
	t.Setenv("FEATURE_EVENTS", "false")
	t.Setenv("FEATURE_AMBASSADOR", "false")
	h := Handler()
	for _, p := range []string{"/api/events/1", "/api/ambassador/rating", "/api/certificates/abcdefgh"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s with switches off: %d", p, w.Code)
		}
	}
	// the shared moderation queue serves every feature, so it stays on (admin only); students'
	// suggestions feed it and the admin content editor is not behind a switch either
	for _, c := range [][2]string{{"GET", "/api/admin/queue"}, {"PATCH", "/api/admin/queue/1"}, {"POST", "/api/submissions"},
		{"GET", "/api/submissions/my"}, {"GET", "/api/admin/content"}, {"POST", "/api/admin/content"}, {"DELETE", "/api/admin/content/adm-1"}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(c[0], c[1], nil)
		r.Header.Set("Origin", "https://studenthub-plus-kz.vercel.app")
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous with switches off: %d, want 401", c[0], c[1], w.Code)
		}
	}
}
