// Command server runs the API locally on :8080 (next dev proxies /api/* here).
package main

import (
	"log"
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/server"
)

func main() {
	addr := config.Str("API_ADDR", ":8080")
	log.Printf("api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
