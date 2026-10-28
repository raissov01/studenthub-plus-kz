package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Building the handler panics on conflicting ServeMux patterns (e.g. two packages claiming
// "POST /api/events"); the feature routes answer 404 while their switch is off.
func TestHandlerBuildsAndSwitchesHide(t *testing.T) {
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
	// the shared moderation queue serves every feature, so it stays on (admin only)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/queue", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/admin/queue anonymous: %d, want 401", w.Code)
	}
}
