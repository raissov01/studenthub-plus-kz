package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/ambassador"
)

// Ambassadors (06): applications, cabinet, suggestions, rating, certificates, the shared admin
// queue and reports — behind the "ambassador" switch. Importing the package also registers its
// moderation hooks, the /api/content extra source and the data-export sections.
func init() { register(func(mux *http.ServeMux) { ambassador.Routes(mux) }) }
