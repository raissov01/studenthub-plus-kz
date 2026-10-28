// Package testdb gives integration tests a throwaway Postgres database with every
// migration applied. Only _test.go files import it.
//
//	func TestMain(m *testing.M) { os.Exit(testdb.Main(m, "auth")) }
//
// TEST_DATABASE_URL overrides the local default; when Postgres is unreachable the
// database-backed tests skip themselves via testdb.Need(t).
package testdb

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultURL = "postgres://postgres:dev@localhost:54329/studenthub"

var available bool

// Main creates studenthub_<name>_test_<pid>, applies migrations, points DATABASE_URL at it,
// runs the tests and drops the database.
func Main(m *testing.M, name string) int {
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		base = defaultURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		log.Printf("testdb: postgres unreachable, database tests skip: %v", err)
		return m.Run()
	}
	dbName := fmt.Sprintf("studenthub_%s_test_%d", name, os.Getpid())
	if _, err := admin.Exec(ctx, "create database "+dbName); err != nil {
		admin.Close(ctx)
		log.Printf("testdb: create database: %v", err)
		return m.Run()
	}
	u, _ := url.Parse(base)
	u.Path = "/" + dbName
	if err := migrate(u.String()); err != nil {
		log.Printf("testdb: migrate: %v", err)
		drop(base, dbName)
		admin.Close(ctx)
		return 1
	}
	admin.Close(ctx)
	os.Setenv("DATABASE_URL", u.String())
	available = true
	code := m.Run()
	drop(base, dbName)
	return code
}

// Need skips the test when no database is available.
func Need(t testing.TB) {
	t.Helper()
	if !available {
		t.Skip("postgres not reachable")
	}
}

func drop(base, dbName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := pgx.Connect(ctx, base)
	if err != nil {
		return
	}
	defer c.Close(ctx)
	if _, err := c.Exec(ctx, "drop database if exists "+dbName+" with (force)"); err != nil {
		log.Printf("testdb: drop %s: %v", dbName, err)
	}
}

func migrate(dsn string) error {
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	files, err := filepath.Glob(filepath.Join(repoRoot(), "supabase", "migrations", "*.sql"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no migrations found")
	}
	sort.Strings(files)
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := c.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

func repoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
