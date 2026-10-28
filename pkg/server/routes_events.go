package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/events"
)

// Events (05): /api/events/*, behind the "events" switch.
func init() { register(func(mux *http.ServeMux) { events.Routes(mux) }) }
