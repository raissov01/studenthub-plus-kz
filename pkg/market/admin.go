package market

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

type adminItemOut struct {
	itemOut
	OwnerID        int64    `json:"ownerId"`
	Reports        int      `json:"reports"`        // all open (not dismissed) reports
	CountedReports int      `json:"countedReports"` // distinct reporters with 7+ day old accounts
	ReportReasons  []string `json:"reportReasons"`
	SubmissionID   *int64   `json:"submissionId"` // pending market_report / market_item submission
}

// adminItems: GET /api/admin/market/items?status=reported|hidden|pending|active&university=
// "reported" (default) = items with open reports, any state.
func adminItems(w http.ResponseWriter, r *http.Request, u *auth.User) {
	st := r.URL.Query().Get("status")
	if st == "" {
		st = "reported"
	}
	uni := strings.TrimSpace(r.URL.Query().Get("university"))
	if !contains([]string{"reported", "hidden", "pending", "active"}, st) || utf8.RuneCountInString(uni) > 120 {
		bad(w, r)
		return
	}
	ctx := r.Context()
	out := []adminItemOut{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+itemCols+`, i.user_id,
				(select count(*) from reports x where x.target_kind = 'market_item' and x.target_id = i.id::text and not x.dismissed),
				(select count(distinct x.reporter_id) from reports x join users ru on ru.id = x.reporter_id
					where x.target_kind = 'market_item' and x.target_id = i.id::text and not x.dismissed
					and ru.created_at <= now() - interval '7 days'),
				coalesce((select array_agg(x.reason order by x.id) from reports x
					where x.target_kind = 'market_item' and x.target_id = i.id::text and not x.dismissed), '{}'),
				(select s.id from submissions s where s.status = 'pending' and s.kind in ('market_report', 'market_item')
					and (s.payload->>'itemId')::bigint = i.id order by s.id limit 1)
			from market_items i join users u on u.id = i.user_id
			where ($2 = '' or i.university = $2) and case $1
				when 'reported' then exists (select 1 from reports x where x.target_kind = 'market_item'
					and x.target_id = i.id::text and not x.dismissed)
				else i.status = $1 end
			order by i.id desc limit 200`, st, uni)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a adminItemOut
			var pickup, name string
			var photos []string
			var owner int64
			var username *string
			if err := rows.Scan(&a.ID, &a.Title, &a.Desc, &a.Category, &a.Condition, &a.University, &pickup, &photos, &a.Status,
				&a.HiddenReason, &a.CreatedAt, &a.ExpiresAt, &a.GivenAt, &owner, &name, &username,
				&a.OwnerID, &a.Reports, &a.CountedReports, &a.ReportReasons, &a.SubmissionID); err != nil {
				return err
			}
			a.Photos = make([]string, 0, len(photos))
			for _, p := range photos {
				a.Photos = append(a.Photos, photoURL(p))
			}
			a.Pickup = &pickup
			a.PickupHint = "university"
			a.Author = &authorOut{Name: name, Username: username}
			a.DaysAgo = int(time.Since(a.CreatedAt).Hours() / 24)
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "admin items", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

func reasonOK(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 3 && n <= 500
}

func pendingReport(ctx context.Context, tx pgx.Tx, id int64) (int64, error) {
	var sub int64
	err := tx.QueryRow(ctx, `select id from submissions where kind = $1 and status = 'pending'
		and (payload->>'itemId')::bigint = $2 order by id limit 1`, KindReport, id).Scan(&sub)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return sub, err
}

func audit(ctx context.Context, tx pgx.Tx, actor int64, action string, target int64, reason string, itemID int64) error {
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values ($1, $2, $3, $4, jsonb_build_object('itemId', $5::bigint))`, actor, action, target, reason, itemID)
	return err
}

// adminDecide resolves the item's pending report through the shared queue (so the
// submission is closed and the hooks run) or, without one, applies the action directly.
func adminDecide(w http.ResponseWriter, r *http.Request, u *auth.User, hide bool) {
	id, ok := pathID(r, "id")
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok {
		bad(w, r)
		return
	}
	reason, ok := reasonOK(in.Reason)
	if !ok {
		fail(w, r, "reason_required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	found := true
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var owner int64
		err := tx.QueryRow(ctx, `select user_id from market_items where id = $1 for update`, id).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		sub, err := pendingReport(ctx, tx, id)
		if err != nil {
			return err
		}
		switch {
		case sub != 0:
			_, err = moderation.Review(ctx, tx, sub, u.ID, hide, reason)
		case hide:
			_, _, err = hideByAdmin(ctx, tx, id)
		default:
			err = dismissReports(ctx, tx, id)
		}
		if err != nil {
			return err
		}
		action := "market_restore"
		if hide {
			action = "market_hide"
		}
		return audit(ctx, tx, u.ID, action, owner, reason, id)
	})
	if err != nil {
		internal(w, r, "admin decide", err)
		return
	}
	if !found {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminHide: POST /api/admin/market/items/{id}/hide {reason} — hides for good, deletes photos.
func adminHide(w http.ResponseWriter, r *http.Request, u *auth.User) { adminDecide(w, r, u, true) }

// adminRestore: POST /api/admin/market/items/{id}/restore {reason} — the reports were
// groundless: they are dismissed and the item is visible again.
func adminRestore(w http.ResponseWriter, r *http.Request, u *auth.User) { adminDecide(w, r, u, false) }

// adminBan: POST /api/admin/market/bans {userId, days (1–365), reason} — temporary publishing
// ban, audited.
func adminBan(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in struct {
		UserID int64  `json:"userId"`
		Days   int    `json:"days"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || in.UserID <= 0 || in.Days < 1 || in.Days > 365 {
		bad(w, r)
		return
	}
	reason, ok := reasonOK(in.Reason)
	if !ok {
		fail(w, r, "reason_required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var until time.Time
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into market_bans (user_id, until, reason, created_by)
			select id, now() + make_interval(days => $2), $3, $4 from users where id = $1 and deleted_at is null
			on conflict (user_id) do update set until = excluded.until, reason = excluded.reason,
				created_by = excluded.created_by, created_at = now()
			returning until`, in.UserID, in.Days, reason, u.ID).Scan(&until)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'market_ban', $2, $3, jsonb_build_object('days', $4::int))`, u.ID, in.UserID, reason, in.Days)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "user_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		internal(w, r, "ban", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"userId": in.UserID, "until": until})
}

// adminUnban: DELETE /api/admin/market/bans/{userId} {reason}
func adminUnban(w http.ResponseWriter, r *http.Request, u *auth.User) {
	uid, ok := pathID(r, "userId")
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok {
		bad(w, r)
		return
	}
	reason, ok := reasonOK(in.Reason)
	if !ok {
		fail(w, r, "reason_required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var n int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from market_bans where user_id = $1`, uid)
		if err != nil {
			return err
		}
		if n = tag.RowsAffected(); n == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason) values ($1, 'market_unban', $2, $3)`,
			u.ID, uid, reason)
		return err
	})
	if err != nil {
		internal(w, r, "unban", err)
		return
	}
	if n == 0 {
		fail(w, r, "not_banned", http.StatusNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"userId": uid, "lifted": true})
}
