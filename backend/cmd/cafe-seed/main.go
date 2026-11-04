// Command cafe-seed adds the demo café «Demo Coffee» with a small menu (docs/cafe.md). Local development and
// e2e only: it refuses to run without CAFE_DEMO_SEED=dev, and the café is listed only with CAFE_SHOW_DEMO=true.
//
//	cd backend && CAFE_DEMO_SEED=dev DATABASE_URL=postgres://... go run ./cmd/cafe-seed
package main

import (
	"context"
	"log"
	"os"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe"
)

func main() {
	if os.Getenv("CAFE_DEMO_SEED") != "dev" {
		log.Fatal("refusing: set CAFE_DEMO_SEED=dev (never on production)")
	}
	id, err := cafe.SeedDemo(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Demo Coffee: café %d", id)
}
