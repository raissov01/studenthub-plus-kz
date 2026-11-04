// Command vapid-keys prints a fresh VAPID key pair for Web Push as env lines:
//
//	cd backend && go run ./cmd/vapid-keys >> ../.secrets/prod.env
//
// Keep VAPID_PRIVATE_KEY secret and never commit it. Changing the pair invalidates every
// browser subscription (students turn browser notifications on again).
package main

import (
	"fmt"
	"os"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/webpush"
)

func main() {
	pub, priv, err := webpush.GenerateVAPID()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vapid-keys:", err)
		os.Exit(1)
	}
	subject := "mailto:admin@studywithraissov.com"
	if len(os.Args) > 1 {
		subject = os.Args[1]
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\nVAPID_SUBJECT=%s\n", pub, priv, subject)
}
