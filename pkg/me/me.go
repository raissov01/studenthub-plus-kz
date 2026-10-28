// Package me is the signed-in student's own account: profile, consent, data export and
// deletion. Every query runs through db.WithUser, so RLS limits it to the student's rows.
//
// New features that store personal data plug in here instead of editing this package:
//   - rows in Postgres: user_id references users(id) on delete cascade (deleted with the
//     account) and an RLS select policy, then me.RegisterExport for the JSON download;
//   - data outside Postgres (Storage photos, PostHog person): me.RegisterDeleteHook.
package me

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// ExportFunc returns one section of the export; tx is scoped to the user (RLS on).
type ExportFunc func(ctx context.Context, tx pgx.Tx, userID int64) (any, error)

// DeleteHook cleans up data about the user that a cascade cannot reach. It runs as the
// connection owner inside the deletion transaction, before the users row goes; returning an
// error aborts the deletion. Hooks must be idempotent.
type DeleteHook func(ctx context.Context, tx pgx.Tx, userID int64) error

var (
	mu          sync.Mutex
	exports     = map[string]ExportFunc{}
	exportOrder []string
	deleteHooks []DeleteHook
)

// RegisterExport adds a section to GET /api/me/export.
func RegisterExport(section string, fn ExportFunc) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := exports[section]; !dup {
		exportOrder = append(exportOrder, section)
	}
	exports[section] = fn
}

// RegisterDeleteHook adds cleanup that runs when a student deletes the account.
func RegisterDeleteHook(fn DeleteHook) {
	mu.Lock()
	defer mu.Unlock()
	deleteHooks = append(deleteHooks, fn)
}

// Rows returns the result of query as a JSON array of row objects; for ExportFuncs.
func Rows(ctx context.Context, tx pgx.Tx, query string, args ...any) (json.RawMessage, error) {
	var out json.RawMessage
	err := tx.QueryRow(ctx, "select coalesce(jsonb_agg(to_jsonb(t)), '[]'::jsonb) from ("+query+") t", args...).Scan(&out)
	return out, err
}

func init() {
	RegisterExport("reminders", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return Rows(ctx, tx, "select * from reminders where user_id = app_user_id() order by deadline")
	})
	RegisterExport("botDailySends", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return Rows(ctx, tx, "select * from bot_daily_sends where user_id = app_user_id() order by day")
	})
	RegisterExport("botEvents", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return Rows(ctx, tx, "select * from bot_events where user_id = app_user_id() order by at")
	})
	RegisterExport("auditLog", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return Rows(ctx, tx, `select id, action, reason, created_at,
			actor_id = app_user_id() as by_me, target_id = app_user_id() as about_me
			from audit_log where actor_id = app_user_id() or target_id = app_user_id() order by id`)
	})
}

var (
	MsgConsent = httpx.Msg{KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."}
	MsgConfirm = httpx.Msg{KK: "Өшіруді растау үшін DELETE деп жазыңыз.", RU: "Для подтверждения удаления введите DELETE."}
	MsgNoStore = httpx.Msg{KK: "GPA мен телефон серверде сақталмайды — олар тек құрылғыңызда.", RU: "GPA и телефон не хранятся на сервере — только на вашем устройстве."}
)

// Routes wires /api/me*.
func Routes(mux httpx.Router) {
	mux.HandleFunc("GET /api/me", get)
	mux.HandleFunc("POST /api/me/consent", auth.Write(auth.Need(consent)))
	mux.HandleFunc("PATCH /api/me", auth.Write(auth.Need(patch)))
	mux.HandleFunc("GET /api/me/export", auth.Need(export))
	mux.HandleFunc("DELETE /api/me", auth.Write(auth.Need(remove)))
}

// InboxUnread counts the user's unread in-site notifications; pkg/notify sets it. tx is
// scoped to the user (RLS on).
var InboxUnread func(ctx context.Context, tx pgx.Tx, userID int64) (int, error)

// get: GET /api/me. It also slides the session (auth.Touch): the app calls it on start, so
// an active student's session never runs out.
func get(w http.ResponseWriter, r *http.Request) {
	r, u := auth.Touch(w, r)
	if u == nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"user": nil, "needsConsent": false, "inboxUnread": 0})
		return
	}
	unread := 0
	if InboxUnread != nil {
		err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
			var err error
			unread, err = InboxUnread(r.Context(), tx, u.ID)
			return err
		})
		if err != nil {
			log.Printf("me: inbox unread %d: %v", u.ID, err) // the profile still loads
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": u, "needsConsent": u.NeedsConsent(), "inboxUnread": unread})
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("me: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func consent(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		IsAdult *bool `json:"isAdult"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil && !errors.Is(err, io.EOF) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var out *auth.User
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		var err error
		out, err = auth.ScanUser(tx.QueryRow(r.Context(), `update users
			set consent_at = coalesce(consent_at, now()), is_adult = coalesce($1, is_adult)
			where id = app_user_id() returning `+auth.UserColumns, body.IsAdult))
		return err
	})
	if err != nil {
		fail(w, r, "consent", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": out, "needsConsent": false})
}

// field validators for PATCH /api/me; each returns the SQL value (nil = clear).
var editable = map[string]func(json.RawMessage) (any, bool){
	"name":       text(80),
	"university": text(120),
	"faculty":    text(120),
	"city":       text(80),
	"lang": func(raw json.RawMessage) (any, bool) {
		var s string
		if json.Unmarshal(raw, &s) != nil || (s != "kk" && s != "ru") {
			return nil, false
		}
		return s, true
	},
	"course": func(raw json.RawMessage) (any, bool) {
		if string(raw) == "null" {
			return nil, true
		}
		var n int
		if json.Unmarshal(raw, &n) != nil || n < 1 || n > 6 {
			return nil, false
		}
		return n, true
	},
}

// never stored server-side, even if a client sends them
var refused = map[string]bool{"gpa": true, "phone": true}

func text(max int) func(json.RawMessage) (any, bool) {
	return func(raw json.RawMessage) (any, bool) {
		if string(raw) == "null" {
			return nil, true
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, false
		}
		s = strings.TrimSpace(s)
		if utf8.RuneCountInString(s) > max || strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return nil, false
		}
		if s == "" {
			return nil, true
		}
		return s, true
	}
}

func patch(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body map[string]json.RawMessage
	if err := httpx.Decode(r, &body, 4<<10); err != nil || len(body) == 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var sets []string
	var args []any
	for k, raw := range body {
		if refused[strings.ToLower(k)] {
			httpx.Error(w, r, http.StatusBadRequest, "not_stored", MsgNoStore)
			return
		}
		check, ok := editable[k]
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "unknown_field", httpx.MsgBad)
			return
		}
		v, ok := check(raw)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_"+k, httpx.MsgBad)
			return
		}
		args = append(args, v)
		sets = append(sets, k+" = $"+strconv.Itoa(len(args)))
	}
	var out *auth.User
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		var err error
		out, err = auth.ScanUser(tx.QueryRow(r.Context(),
			"update users set "+strings.Join(sets, ", ")+" where id = app_user_id() returning "+auth.UserColumns, args...))
		return err
	})
	if err != nil {
		fail(w, r, "patch", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": out, "needsConsent": false})
}

func export(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := map[string]any{"exportedAt": time.Now().UTC(), "format": "studenthub-export-v1"}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		user, err := Rows(ctx, tx, "select * from users where id = app_user_id()")
		if err != nil {
			return err
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(user, &rows); err != nil || len(rows) != 1 {
			return fmt.Errorf("user row: %v", err)
		}
		out["user"] = rows[0]
		mu.Lock()
		order := append([]string(nil), exportOrder...)
		mu.Unlock()
		for _, name := range order {
			v, err := exports[name](ctx, tx, u.ID)
			if err != nil {
				return fmt.Errorf("export %s: %w", name, err)
			}
			out[name] = v
		}
		return nil
	})
	if err != nil {
		fail(w, r, "export", err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="studenthub-data.json"`)
	httpx.JSON(w, http.StatusOK, out)
}

func remove(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Confirm != "DELETE" {
		httpx.Error(w, r, http.StatusBadRequest, "confirm_required", MsgConfirm)
		return
	}
	if err := Delete(r.Context(), u.ID); err != nil {
		fail(w, r, "delete", err)
		return
	}
	auth.ClearCookie(w, r)
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// Delete removes the account: hooks first, then the users row as app_user (RLS allows only
// the own row; foreign keys cascade to every personal table and null out authorship), then
// one audit_log row that names nobody. All in one transaction.
func Delete(ctx context.Context, userID int64) error {
	mu.Lock()
	hooks := append([]DeleteHook(nil), deleteHooks...)
	mu.Unlock()
	return db.Tx(ctx, func(tx pgx.Tx) error {
		for _, h := range hooks {
			if err := h(ctx, tx, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "select set_config('app.user_id', $1, true)", strconv.FormatInt(userID, 10)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "set local role app_user"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "delete from users where id = app_user_id()")
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("delete user %d: %d rows", userID, tag.RowsAffected())
		}
		if _, err := tx.Exec(ctx, "reset role"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values (null, 'account_delete', null, null, '{}')`)
		return err
	})
}
