package server

import (
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/feeds"
)

// Opportunity feed (docs/feeds.md): admin sources / items, behind the "feeds" switch. Importing the
// package also registers its /api/content extra source and the feed_item moderation hooks.
func init() { register(func(mux httpx.Router) { feeds.Routes(mux) }) }
