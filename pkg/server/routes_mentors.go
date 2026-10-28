package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/mentors"
)

// Mentors (prompt 07): requests, applications, matching, pair lifecycle; FEATURE_MENTORS.
func init() { register(func(mux *http.ServeMux) { mentors.Routes(mux) }) }
