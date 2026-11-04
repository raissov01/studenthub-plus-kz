// Command server runs the API locally on :8080 (next dev proxies /api/* here).
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/clubs"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/server"
)

func main() {
	addr := config.Str("API_ADDR", ":8080")
	// CLUBS_SEED=1: import the verified KazATU clubs once (idempotent, keyed by seed_key; docs/clubs-seed.md)
	if config.Bool("CLUBS_SEED", false) {
		if n, err := clubs.Seed(context.Background()); err != nil {
			log.Printf("clubs seed: %v", err)
		} else {
			log.Printf("clubs seed: %d added", n)
		}
	}
	log.Printf("api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
