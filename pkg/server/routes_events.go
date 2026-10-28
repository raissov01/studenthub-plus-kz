package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/events"
)

// Events (05): /api/events/*, behind the "events" switch.
func init() { register(func(mux httpx.Router) { events.Routes(mux) }) }
