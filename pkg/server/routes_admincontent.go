package server

import (
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"

	"github.com/raissov01/studenthub-plus-kz/pkg/admincontent"
)

// Admin content editor: opportunities, jobs and discounts managed in /admin (admin only, not
// behind a feature switch). Importing the package also registers its /api/content extra source.
func init() { register(func(mux httpx.Router) { admincontent.Routes(mux) }) }
