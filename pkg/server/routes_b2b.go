package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/b2b"
)

// Employer and university cabinets (prompt 10): employers, jobs, applications, official
// university posts and aggregate stats, cron /api/cron/b2b; FEATURE_B2B.
func init() { register(func(mux *http.ServeMux) { b2b.Routes(mux) }) }
