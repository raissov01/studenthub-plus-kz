package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/market"
)

// Market (prompt 08): free give-away items, photos on local disk; FEATURE_MARKET.
func init() { register(func(mux *http.ServeMux) { market.Routes(mux) }) }
