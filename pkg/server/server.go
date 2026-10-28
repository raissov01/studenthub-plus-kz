// Package server builds the single HTTP handler behind every /api/* route. cmd/server runs it
// on our own server (and locally); the Next.js site proxies /api/* to it.
//
// Feature packages are wired in routes_<feature>.go files in this package:
//
//	func init() { register(func(mux *http.ServeMux) { content.Routes(mux) }) }
package server

import (
	"log"
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

var (
	routes  []func(*http.ServeMux)
	once    sync.Once
	handler http.Handler
)

func register(fn func(*http.ServeMux)) { routes = append(routes, fn) }

// Handler returns the API handler, built once per instance.
func Handler() http.Handler {
	once.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		for _, fn := range routes {
			fn(mux)
		}
		mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		})
		handler = recoverer(secure(mux))
	})
	return handler
}

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if h.Get("Cache-Control") == "" {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
				httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
