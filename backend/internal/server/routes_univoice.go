package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/feedback"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/surveys"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/wellbeing"
)

// «University voice» (docs/UNIVOICE.md): surveys (FEATURE_SURVEYS), anonymous feedback tickets with
// the staff inbox and SLA cron (FEATURE_FEEDBACK), wellbeing contacts (FEATURE_WELLBEING).
func init() {
	register(func(mux httpx.Router) {
		surveys.Routes(mux)
		feedback.Routes(mux)
		wellbeing.Routes(mux)
	})
}
