// Command json-to-csv regenerates templates/sheets/*.csv from data/*.json (one CSV per
// Google Sheets tab). Run it from the repository root: go run ./cmd/json-to-csv
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/raissov01/studenthub-plus-kz/pkg/content"
)

func main() {
	root := flag.String("root", ".", "repository root (holds data/ and templates/)")
	flag.Parse()
	outDir := filepath.Join(*root, "templates", "sheets")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, kind := range content.Kinds {
		b, err := os.ReadFile(filepath.Join(*root, "data", kind+".json"))
		if err != nil {
			log.Fatal(err)
		}
		var records []map[string]any
		if err := json.Unmarshal(b, &records); err != nil {
			log.Fatalf("data/%s.json: %v", kind, err)
		}
		csv, err := content.TemplateCSV(kind, records)
		if err != nil {
			log.Fatalf("data/%s.json: %v", kind, err)
		}
		file := filepath.Join(outDir, kind+".csv")
		if err := os.WriteFile(file, []byte(csv), 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d rows\n", filepath.ToSlash(filepath.Join("templates", "sheets", kind+".csv")), len(records))
	}
}
