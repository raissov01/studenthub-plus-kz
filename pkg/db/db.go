// Package db owns the Postgres pool. Request code that acts for a signed-in student runs
// inside WithUser, so Row Level Security policies (app_user_id()) apply to every query.
package db

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
)

var (
	once    sync.Once
	pool    *pgxpool.Pool
	poolErr error
)

// ErrNoDB means DATABASE_URL is not set; features that need the database are off.
var ErrNoDB = errors.New("database is not configured")

// Pool returns the shared pool, created on first use (serverless cold starts stay cheap).
func Pool(ctx context.Context) (*pgxpool.Pool, error) {
	once.Do(func() {
		url := config.Str("DATABASE_URL", "")
		if url == "" {
			poolErr = ErrNoDB
			return
		}
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			poolErr = err
			return
		}
		cfg.MaxConns = int32(config.Int("DB_MAX_CONNS", 4))
		cfg.MaxConnIdleTime = 30 * time.Second
		// Supabase's transaction pooler (port 6543) does not support prepared statements.
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		pool, poolErr = pgxpool.NewWithConfig(ctx, cfg)
	})
	return pool, poolErr
}

// Tx runs fn in a transaction as the connection owner (server-side work: cron, bot, admin).
func Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	p, err := Pool(ctx)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, p, fn)
}

// WithUser runs fn as the restricted role app_user with app.user_id set, so RLS limits
// every query to the student's own rows.
func WithUser(ctx context.Context, userID int64, fn func(pgx.Tx) error) error {
	return Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "select set_config('app.user_id', $1, true)", strconv.FormatInt(userID, 10)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "set local role app_user"); err != nil {
			return err
		}
		return fn(tx)
	})
}
