package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/ambassador"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

// The shared review queue and reports serve every feature (events, mentors, market, employers),
// so they are not tied to any single feature switch. Admin-only routes still require role admin.
// Students' suggestions («Мүмкіндік ұсыну», POST /api/submissions) feed that queue and are
// always available too, whatever the "ambassador" switch says.
func init() {
	register(func(mux httpx.Router) {
		moderation.Routes(mux, func(h http.Handler) http.Handler { return h })
		ambassador.SubmissionRoutes(mux)
	})
}
