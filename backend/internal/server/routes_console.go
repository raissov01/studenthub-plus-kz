package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/console"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// The role-scoped admin console (/admin): the panels of the signed-in person and the shop workspace.
func init() { register(func(mux httpx.Router) { console.Routes(mux) }) }
