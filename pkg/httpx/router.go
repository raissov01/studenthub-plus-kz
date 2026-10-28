package httpx

import "net/http"

// Router is implemented by ServeMux and by the auditing collector. Every feature uses
// this same registration surface, so the inventory walks the actual registered routes.
type Router interface {
	Handle(string, http.Handler)
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}
