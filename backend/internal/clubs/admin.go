package clubs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// AdminClub is a catalogue row for the site admin: archived clubs too, with their admins.
type AdminClub struct {
	Club
	Admins  []ClubAdmin `json:"admins"`
	Pending int         `json:"pending_requests"`
}

type ClubAdmin struct {
	UserID   int64   `json:"user_id"`
	Name     *string `json:"name"`
	Username *string `json:"username"`
}

func adminList(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	out := []*AdminClub{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "select "+clubCols+" from clubs c order by c.status, c.name_kk limit 1000")
		if err != nil {
			return err
		}
		for rows.Next() {
			c, err := scanClub(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, &AdminClub{Club: *c, Admins: []ClubAdmin{}})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range out {
			rows, err := tx.Query(ctx, `select u.id, u.name, u.tg_username from club_admins a join users u on u.id = a.user_id
				where a.club_id = $1 and u.deleted_at is null order by a.granted_at`, c.ID)
			if err != nil {
				return err
			}
			admins, err := pgx.CollectRows(rows, func(x pgx.CollectableRow) (ClubAdmin, error) {
				var a ClubAdmin
				return a, x.Scan(&a.UserID, &a.Name, &a.Username)
			})
			if err != nil {
				return err
			}
			c.Admins = admins
			if err := tx.QueryRow(ctx, `select count(*) from club_members where club_id = $1 and status = 'pending'`, c.ID).Scan(&c.Pending); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, r, "admin list", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"clubs": out})
}

func audit(ctx context.Context, tx pgx.Tx, actor int64, action string, target *int64, meta map[string]any) error {
	raw, _ := json.Marshal(meta)
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, meta) values ($1, $2, $3, $4::jsonb)`, actor, action, target, string(raw))
	return err
}

func adminCreate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	var in map[string]any
	if err := httpx.Decode(r, &in, 32<<10); err != nil {
		invalid(w, r, "")
		return
	}
	if _, ok := in["university"]; !ok {
		in["university"] = University()
	}
	if s, _ := in["name_kk"].(string); strings.TrimSpace(s) == "" {
		invalid(w, r, "name_kk")
		return
	}
	cols, args, bad := sets(in, true, 0)
	if bad != "" {
		invalid(w, r, bad)
		return
	}
	names := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		name, mark, _ := strings.Cut(c, " = ")
		names[i], marks[i] = name, mark
	}
	args = append(args, u.ID)
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "insert into clubs ("+strings.Join(names, ", ")+", created_by) values ("+strings.Join(marks, ", ")+
			", $"+strconv.Itoa(len(args))+") returning id", args...).Scan(&id)
		if err != nil {
			return err
		}
		return audit(ctx, tx, u.ID, "club_create", nil, map[string]any{"club": id})
	})
	if pgCode(err) == "23514" || pgCode(err) == "23502" {
		invalid(w, r, "")
		return
	}
	if err != nil {
		fail(w, r, "admin create", err)
		return
	}
	var c *Club
	if err := db.Tx(ctx, func(tx pgx.Tx) error { var e error; c, e = load(ctx, tx, id); return e }); err != nil {
		fail(w, r, "admin create load", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"club": c})
}

func adminEdit(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	var in map[string]any
	if err := httpx.Decode(r, &in, 32<<10); err != nil || len(in) == 0 {
		invalid(w, r, "")
		return
	}
	cols, args, bad := sets(in, true, 1)
	if bad != "" {
		invalid(w, r, bad)
		return
	}
	cols = append(cols, "updated_at = now()")
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "update clubs set "+strings.Join(cols, ", ")+" where id = $1", append([]any{id}, args...)...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
		keys := make([]string, 0, len(in))
		for k := range in {
			keys = append(keys, k)
		}
		return audit(ctx, tx, u.ID, "club_edit", nil, map[string]any{"club": id, "fields": keys})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	writeEdit(w, r, id, err)
}

func adminGrant(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	var body struct {
		UserID int64 `json:"userId"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || !ok || body.UserID < 1 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from users where id = $1 and deleted_at is null)
			and exists (select 1 from clubs where id = $2)`, body.UserID, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		if _, err := tx.Exec(ctx, `insert into club_admins (club_id, user_id, granted_by) values ($1, $2, $3) on conflict do nothing`, id, body.UserID, u.ID); err != nil {
			return err
		}
		return audit(ctx, tx, u.ID, "club_admin_grant", &body.UserID, map[string]any{"club": id})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	if err != nil {
		fail(w, r, "grant", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func adminRevoke(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok1 := pathInt(r, "id")
	uid, ok2 := pathInt(r, "userId")
	if !ok1 || !ok2 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var n int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from club_admins where club_id = $1 and user_id = $2`, id, uid)
		if err != nil {
			return err
		}
		if n = tag.RowsAffected(); n == 0 {
			return nil
		}
		return audit(ctx, tx, u.ID, "club_admin_revoke", &uid, map[string]any{"club": id})
	})
	if err != nil {
		fail(w, r, "revoke", err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoReq)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
