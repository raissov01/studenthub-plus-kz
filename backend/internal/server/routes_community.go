package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/community"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Community: lost & found (FEATURE_LOSTFOUND), course Q&A (FEATURE_COURSEQA) and roommates
// (FEATURE_ROOMMATES), each behind its own switch; docs/api/community.md.
func init() { register(func(mux httpx.Router) { community.Routes(mux) }) }
