package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/portfolio"
)

// Portfolio (prompt 09): settings, synced self-declared items and the public page JSON
// GET /api/p/{slug}; FEATURE_PORTFOLIO.
func init() { register(func(mux httpx.Router) { portfolio.Routes(mux) }) }
