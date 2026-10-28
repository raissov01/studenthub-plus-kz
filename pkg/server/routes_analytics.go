package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/analytics"
)

func init() { register(func(mux *http.ServeMux) { analytics.Routes(mux) }) }
