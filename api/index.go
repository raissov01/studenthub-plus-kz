// Package handler is the only Vercel Go function: vercel.json rewrites every /api/* path
// here and pkg/server routes it.
package handler

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/server"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	server.Handler().ServeHTTP(w, r)
}
