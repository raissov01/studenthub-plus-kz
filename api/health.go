package handler

import (
	"encoding/json"
	"net/http"
)

// Handler reports that the Go runtime is reachable next to the Next.js app.
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "runtime": "go"})
}
