package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/clubs"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Clubs: /api/clubs/*, /api/admin/clubs/*, behind the "clubs" switch (docs/clubs.md).
func init() { register(func(mux httpx.Router) { clubs.Routes(mux) }) }
