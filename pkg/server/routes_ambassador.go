package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/ambassador"
)

// Ambassadors (06): applications, cabinet, rating, certificates — behind the "ambassador" switch.
// Suggestions (/api/submissions) and the shared queue are wired in routes_moderation.go. Importing the package also registers its
// moderation hooks, the /api/content extra source and the data-export sections.
func init() { register(func(mux httpx.Router) { ambassador.Routes(mux) }) }
