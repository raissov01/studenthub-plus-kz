package server

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/events"
	"github.com/raissov01/studenthub-plus-kz/pkg/mentors"
)

// Cross-feature wiring that neither package can do without importing the other.
func init() {
	// 05 × 07: an approved mentor manages only the mentoring group meetings they created.
	events.ExtraManager = func(ctx context.Context, u *auth.User, ev *events.Event) bool {
		if u == nil || ev == nil || ev.Cat != "mentoring" {
			return false
		}
		if ev.CreatedBy != nil && *ev.CreatedBy != u.ID {
			return false
		}
		ok := false
		err := db.Tx(ctx, func(tx pgx.Tx) error { ok = mentors.IsApprovedMentor(ctx, tx, u.ID); return nil })
		return err == nil && ok
	}

	// 07: when no mentor is free, offer the university's next mentoring group meetings.
	mentors.GroupMeetings = func(ctx context.Context, university string, limit int) ([]mentors.GroupMeeting, error) {
		var out []mentors.GroupMeeting
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select id::text, title_kk, to_char(starts_at at time zone 'Asia/Almaty', 'YYYY-MM-DD"T"HH24:MI'), coalesce(place_text, '')
				from events where cat = 'mentoring' and status = 'published' and starts_at > now() and university = $1
				order by starts_at limit $2`, university, limit)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var g mentors.GroupMeeting
				if err := rows.Scan(&g.ID, &g.Title, &g.StartsAt, &g.Place); err != nil {
					return err
				}
				g.URL = "/event/" + g.ID
				out = append(out, g)
			}
			return rows.Err()
		})
		if err != nil {
			log.Printf("glue: group meetings: %v", err)
		}
		return out, err
	}

	// 06 × 07: only the university's ambassador team lead runs the mentor matching screen.
	mentors.AmbassadorCanMatch = func(ctx context.Context, userID int64, university string) (bool, error) {
		ok := false
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select exists (select 1 from ambassador_terms where user_id = $1 and university = $2
				and status = 'active' and role_in_team = 'lead')`, userID, university).Scan(&ok)
		})
		return ok, err
	}
}
