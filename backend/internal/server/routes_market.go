package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/market"
)

// Market (prompt 08): free give-away items, photos on local disk; FEATURE_MARKET.
func init() { register(func(mux httpx.Router) { market.Routes(mux) }) }
