// Package content serves GET /api/content: opportunities, jobs and discounts from Google
// Sheets (published CSV links in env), with in-memory and data/*.json fallbacks.
// See docs/CONTENT.md. Ported from legacy/api/content.js and legacy/js/lib/content.
package content

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Default is the loader used by the HTTP handler; its memory lives as long as the instance.
var Default = &Loader{}

func Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/content", Handler(Default))
}

// Handler serves GET/HEAD with the given loader.
func Handler(l *Loader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"error": map[string]string{"code": "method", "message": "Тек GET сұранысы қабылданады."},
			})
			return
		}
		// A client hanging up must not abort the Sheets fetch (and wipe the memory refresh).
		body := safeLoad(l, context.WithoutCancel(r.Context()))
		w.Header().Set("Cache-Control", "public, s-maxage=600, stale-while-revalidate=86400")
		writeJSON(w, http.StatusOK, body)
	}
}

func safeLoad(l *Loader, ctx context.Context) (c Content) {
	defer func() {
		if v := recover(); v != nil {
			// Load already isolates failures per list; this is a last resort.
			fmt.Fprintf(os.Stderr, "[content] күтпеген қате: %v\n", v)
			c = Content{Opportunities: []Record{}, Jobs: []Record{}, Discounts: []Record{}, Source: "fallback"}
		}
	}()
	return l.Load(ctx, time.Now())
}

// writeJSON matches JSON.stringify output: no HTML escaping of <, > and &.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
