package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/discounts"
)

// F7 discount confirmations: POST /api/discounts/{id}/check, GET /api/discounts/checks.
func init() { register(func(mux *http.ServeMux) { discounts.Routes(mux) }) }
