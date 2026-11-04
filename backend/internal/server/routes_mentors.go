package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/mentors"
)

// Mentors (prompt 07): requests, applications, matching, pair lifecycle; FEATURE_MENTORS.
func init() { register(func(mux httpx.Router) { mentors.Routes(mux) }) }
