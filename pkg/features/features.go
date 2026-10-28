// Package features holds the feature switches. Each one is off unless the environment
// says otherwise (FEATURE_EVENTS=true, FEATURE_MARKET=on, ...), so turning a feature on or
// off is an env change plus redeploy, never a code change.
package features

import (
	"net/http"
	"strings"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Names lists every switch in display order.
var Names = []string{"events", "ambassador", "mentors", "market", "portfolio", "b2b", "quiz"}

// Enabled reports whether the named feature is on. Unknown names are off.
func Enabled(name string) bool {
	for _, n := range Names {
		if n == name {
			return config.Bool("FEATURE_"+strings.ToUpper(name), false)
		}
	}
	return false
}

// All returns the state of every switch.
func All() map[string]bool {
	out := make(map[string]bool, len(Names))
	for _, n := range Names {
		out[n] = Enabled(n)
	}
	return out
}

// Require serves h only while the feature is on; otherwise it answers exactly like an
// unknown route, so a switched-off feature is indistinguishable from a missing one.
func Require(name string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Enabled(name) {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// RequireFunc is Require for a handler function.
func RequireFunc(name string, h http.HandlerFunc) http.Handler { return Require(name, h) }

// Routes wires GET /api/config: public settings the frontend needs at runtime.
func Routes(mux httpx.Router) {
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		httpx.JSON(w, http.StatusOK, map[string]any{
			"features":       All(),
			"botUsername":    strings.TrimPrefix(config.Str("BOT_USERNAME", ""), "@"),
			"reportUrl":      config.Str("REPORT_URL", ""),
			"suggestFormUrl": config.Str("SUGGEST_FORM_URL", ""),
			// null unless MARKET_SEASON is set and the range is current or upcoming
			"marketSeason": MarketSeason(time.Now()),
		})
	})
}
