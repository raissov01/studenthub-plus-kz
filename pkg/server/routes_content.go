package server

import (
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/chat"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
)

func init() { register(func(mux *http.ServeMux) { content.Routes(mux); chat.Routes(mux) }) }
