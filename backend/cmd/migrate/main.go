// Command migrate applies migrations/*.sql in name order, once each.
// Usage (from backend/): DATABASE_URL=postgres://... go run ./cmd/migrate
// MIGRATIONS_DIR overrides the directory (the API image runs it from /app with /app/migrations).
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (name text primary key, applied_at timestamptz not null default now())`); err != nil {
		log.Fatal(err)
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.sql"))
	if len(files) == 0 {
		log.Fatalf("no migrations in %s", dir)
	}
	sort.Strings(files)
	for _, f := range files {
		name := filepath.Base(f)
		var done bool
		_ = conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where name = $1)`, name).Scan(&done)
		if done {
			continue
		}
		sql, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `insert into schema_migrations (name) values ($1)`, name)
			return err
		})
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		log.Printf("applied %s", name)
	}
}
