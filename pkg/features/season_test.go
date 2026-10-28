package features_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/server"
)

func TestMarketSeason(t *testing.T) {
	now := time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC)
	t.Setenv("MARKET_SEASON", "")
	if s := features.MarketSeason(now); s != nil {
		t.Fatalf("unset: %+v", s)
	}
	t.Setenv("MARKET_SEASON", "bad, 2026-01-01..2026-02-01, 2027-05-20..2027-06-30, 2026-12-15..2027-01-31")
	s := features.MarketSeason(now)
	if s == nil || !s.Active || s.From != "2026-12-15" || s.Key() != "2026-12-15..2027-01-31" {
		t.Fatalf("current: %+v", s)
	}
	if s := features.MarketSeason(now.AddDate(0, 2, 0)); s == nil || s.Active || s.From != "2027-05-20" {
		t.Fatalf("upcoming: %+v", s)
	}
	// /api/config exposes it next to the existing keys
	t.Setenv("MARKET_SEASON", "2000-01-01..2999-12-31")
	w := get(server.Handler(), "/api/config")
	var out map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out["marketSeason"]), `"active":true`) || out["features"] == nil || out["botUsername"] == nil {
		t.Fatalf("config: %s", w.Body)
	}
}
