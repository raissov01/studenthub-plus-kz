package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/fairs"
)

// Career-fair booths (spec 10): exhibitors, booth QR scans, employer list, next-day summary;
// FEATURE_FAIRS on top of FEATURE_EVENTS.
func init() { register(func(mux httpx.Router) { fairs.Routes(mux) }) }
