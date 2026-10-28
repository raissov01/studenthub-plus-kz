// Package settings reads the server-owned launch controls.
package settings

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

func ShowDemo(ctx context.Context) (bool, error) {
	var show bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select show_demo from app_settings where singleton`).Scan(&show)
	})
	return show, err
}

func SetShowDemo(ctx context.Context, show bool) error {
	return db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into app_settings (singleton, show_demo) values (true, $1)
   on conflict (singleton) do update set show_demo = excluded.show_demo`, show)
		return err
	})
}
