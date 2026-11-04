package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/discounts"
)

// F7 discount confirmations: POST /api/discounts/{id}/check, GET /api/discounts/checks.
func init() { register(func(mux httpx.Router) { discounts.Routes(mux) }) }
