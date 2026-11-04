package events

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
)

// Load reads one event (row-locked with lock) for packages built on top of events, such as the
// career-fair booths (internal/fairs). pgx.ErrNoRows when it does not exist.
func Load(ctx context.Context, tx pgx.Tx, id int64, lock bool) (*Event, error) {
	return load(ctx, tx, id, lock)
}

// Visible: published, finished or cancelled events are public; drafts only to their managers.
func Visible(ctx context.Context, u *auth.User, ev *Event) bool { return visible(ctx, u, ev) }
