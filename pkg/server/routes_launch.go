package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/launch"
)

func init() { register(func(mux httpx.Router) { launch.Routes(mux) }) }
