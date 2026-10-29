// Command e2etest owns a disposable local database and the real HTTP API for Playwright.
// It is never imported by the application and adds no HTTP routes or auth bypasses.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/server"
)

func run() error {
	name := os.Getenv("E2E_DB_NAME")
	if !regexp.MustCompile(`^studenthub_browser_[0-9]+_[0-9a-f]+$`).MatchString(name) {
		return fmt.Errorf("refusing non-test database name %q", name)
	}
	// Deliberately fixed to local Postgres. DATABASE_URL / production .env are ignored.
	base := "postgres://postgres:dev@127.0.0.1:54329/postgres"
	u, _ := url.Parse(base)
	u.Path = "/" + name
	dsn := u.String()
	ctx := context.Background()
	if len(os.Args) == 2 && os.Args[1] == "sql" {
		var q struct {
			SQL  string
			Args []any
		}
		if err := json.NewDecoder(os.Stdin).Decode(&q); err != nil {
			return err
		}
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return err
		}
		defer c.Close(ctx)
		rows, err := c.Query(ctx, q.SQL, q.Args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			row := map[string]any{}
			for i, f := range rows.FieldDescriptions() {
				row[f.Name] = values[i]
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return err
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		return err
	}
	defer func() {
		if _, err := admin.Exec(ctx, "drop database "+name+" with (force)"); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}()
	if err := os.Setenv("DATABASE_URL", dsn); err != nil {
		return err
	}
	if err := os.MkdirAll(".fe-deps", 0700); err != nil {
		return err
	}
	uploads, err := os.MkdirTemp(".fe-deps", "real-uploads-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(uploads)
	if err := os.Setenv("UPLOAD_DIR", uploads); err != nil {
		return err
	}
	stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	migrate := exec.CommandContext(stop, "go", "run", "./cmd/migrate")
	migrate.Stdout, migrate.Stderr = os.Stdout, os.Stderr
	if err := migrate.Run(); err != nil {
		return err
	}
	srv := &http.Server{Addr: os.Getenv("API_ADDR"), Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-stop.Done():
		shutdown, end := context.WithTimeout(ctx, 10*time.Second)
		defer end()
		return srv.Shutdown(shutdown)
	}
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
