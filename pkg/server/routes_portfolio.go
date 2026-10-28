package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/portfolio"
)

// Portfolio (prompt 09): settings, synced self-declared items and the public page JSON
// GET /api/p/{slug}; FEATURE_PORTFOLIO.
func init() { register(func(mux *http.ServeMux) { portfolio.Routes(mux) }) }
