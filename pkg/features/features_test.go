package features_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/server"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestRequireOffLooksLikeUnknownRoute(t *testing.T) {
	for _, n := range features.Names {
		t.Setenv("FEATURE_"+strings.ToUpper(n), "")
	}
	gated := features.RequireFunc("market", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("market list")) })
	unknown := get(server.Handler(), "/api/definitely-not-a-route")

	off := get(gated, "/api/market")
	if off.Code != 404 || off.Body.String() != unknown.Body.String() || unknown.Code != 404 {
		t.Fatalf("off: %d %q vs unknown %d %q", off.Code, off.Body, unknown.Code, unknown.Body)
	}
	for _, v := range []string{"true", "on", "1"} {
		t.Setenv("FEATURE_MARKET", v)
		if on := get(gated, "/api/market"); on.Code != 200 || on.Body.String() != "market list" {
			t.Fatalf("FEATURE_MARKET=%s: %d %s", v, on.Code, on.Body)
		}
	}
	t.Setenv("FEATURE_MARKET", "off")
	if get(gated, "/api/market").Code != 404 {
		t.Fatal("FEATURE_MARKET=off still routed")
	}
	if features.Enabled("nonexistent") {
		t.Fatal("unknown feature enabled")
	}
}

func TestConfigEndpoint(t *testing.T) {
	t.Setenv("FEATURE_EVENTS", "true")
	t.Setenv("FEATURE_B2B", "")
	t.Setenv("BOT_USERNAME", "@studenthub_kz_bot")
	t.Setenv("REPORT_URL", "https://forms.gle/report")
	t.Setenv("SUGGEST_FORM_URL", "https://forms.gle/suggest")
	w := get(server.Handler(), "/api/config")
	var out struct {
		Features       map[string]bool
		BotUsername    string
		ReportURL      string `json:"reportUrl"`
		SuggestFormURL string `json:"suggestFormUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("config: %d %s", w.Code, w.Body)
	}
	if len(out.Features) != 6 || !out.Features["events"] || out.Features["b2b"] || out.Features["market"] {
		t.Fatalf("features: %v", out.Features)
	}
	if out.BotUsername != "studenthub_kz_bot" || out.ReportURL == "" || out.SuggestFormURL == "" {
		t.Fatalf("config: %+v", out)
	}
}
