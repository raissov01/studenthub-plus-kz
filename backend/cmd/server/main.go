// Command server runs the API locally on :8080 (next dev proxies /api/* here).
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/server"
)

func main() {
	addr := config.Str("API_ADDR", ":8080")
	log.Printf("api listening on %s", addr)
	server.StartBackground(context.Background())
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
