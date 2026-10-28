// Package admin serves the admin-only endpoints: overview, role changes (always with a
// reason, always audited) and a minimal user search. The admin check reads the role from
// the database on every request, so revoking admin takes effect immediately.
package admin

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/analytics"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Roles mirrors the user_role enum.
var Roles = []string{"student", "ambassador", "admin", "university_staff", "employer"}

var (
	MsgReason   = httpx.Msg{KK: "Себебін жазыңыз (3–500 таңба).", RU: "Укажите причину (3–500 символов)."}
	MsgNoUser   = httpx.Msg{KK: "Мұндай қолданушы табылмады.", RU: "Пользователь не найден."}
	MsgSelfRole = httpx.Msg{KK: "Өз рөліңізді өзгерте алмайсыз.", RU: "Нельзя менять собственную роль."}
)

// Routes wires /api/admin/*.
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/summary", auth.NeedRole(summary, "admin"))
	mux.HandleFunc("GET /api/admin/users", auth.NeedRole(users, "admin"))
	mux.HandleFunc("GET /api/admin/analytics", auth.NeedRole(analyticsReport, "admin"))
	// Write runs first (origin, rate limit), then the role check.
	mux.HandleFunc("POST /api/admin/role", auth.Write(auth.NeedRole(setRole, "admin")))
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("admin: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func summary(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	byRole := map[string]int{}
	for _, role := range Roles {
		byRole[role] = 0
	}
	var total, consented, reminders int
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select role::text, count(*) from users where deleted_at is null group by role`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var role string
			var n int
			if err := rows.Scan(&role, &n); err != nil {
				return err
			}
			byRole[role] = n
			total += n
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select
			(select count(*) from users where deleted_at is null and consent_at is not null),
			(select count(*) from reminders where status = 'active')`).Scan(&consented, &reminders)
	})
	if err != nil {
		fail(w, r, "summary", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"users":           map[string]any{"total": total, "byRole": byRole, "withConsent": consented},
		"activeReminders": reminders,
		"features":        features.All(),
	})
}

type userRow struct {
	ID         int64      `json:"id"`
	TelegramID *int64     `json:"telegramId"` // null for an email-only account
	Email      *string    `json:"email"`
	Username   *string    `json:"username"`
	Name       *string    `json:"name"`
	Role       string     `json:"role"`
	University *string    `json:"university"`
	ConsentAt  *time.Time `json:"consentAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// users: ?q= matches name, username or email (substring) or an exact id / telegram id. Max 20 rows.
// analyticsReport returns anonymous usage numbers for the last ?days= days (default 30).
func analyticsReport(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 365 {
		days = 30
	}
	rep, err := analytics.Report(r.Context(), days)
	if err != nil {
		fail(w, r, "analytics", err)
		return
	}
	httpx.JSON(w, http.StatusOK, rep)
}

func users(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 64 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	num, _ := strconv.ParseInt(strings.TrimPrefix(q, "@"), 10, 64)
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimPrefix(q, "@")) + "%"
	out := []userRow{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, telegram_id, email, tg_username, name, role::text, university, consent_at, created_at
			from users
			where deleted_at is null and ($1 = '' or id = $2 or telegram_id = $2
				or name ilike $3 or tg_username ilike $3 or email ilike $3)
			order by consent_at is null, created_at desc limit 20`, q, num, like)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u userRow
			if err := rows.Scan(&u.ID, &u.TelegramID, &u.Email, &u.Username, &u.Name, &u.Role, &u.University, &u.ConsentAt, &u.CreatedAt); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "users", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out})
}

func setRole(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	var body struct {
		UserID     int64  `json:"userId"`
		TelegramID int64  `json:"telegramId"`
		Role       string `json:"role"`
		Reason     string `json:"reason"`
	}
	if err := httpx.Decode(r, &body, 2<<10); err != nil || (body.UserID == 0) == (body.TelegramID == 0) || !validRole(body.Role) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
		return
	}
	var target struct {
		ID         int64  `json:"id"`
		TelegramID *int64 `json:"telegramId"`
		Role       string `json:"role"`
		Previous   string `json:"previousRole"`
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		return changeRole(r.Context(), tx, actor.ID, body.UserID, body.TelegramID, body.Role, reason,
			&target.ID, &target.TelegramID, &target.Previous)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
		return
	case errors.Is(err, errSelf):
		httpx.Error(w, r, http.StatusBadRequest, "self_role", MsgSelfRole)
		return
	case err != nil:
		fail(w, r, "role", err)
		return
	}
	target.Role = body.Role
	httpx.JSON(w, http.StatusOK, map[string]any{"user": target, "changed": target.Previous != body.Role})
}

var errSelf = errors.New("admin: cannot change own role")

func changeRole(ctx context.Context, tx pgx.Tx, actorID, userID, telegramID int64, role, reason string,
	id *int64, tgID **int64, prev *string) error {
	err := tx.QueryRow(ctx, `select id, telegram_id, role::text from users
		where deleted_at is null and (id = $1 or telegram_id = $2) for update`, userID, telegramID).Scan(id, tgID, prev)
	if err != nil {
		return err
	}
	if *id == actorID {
		return errSelf
	}
	if *prev == role {
		return nil
	}
	if _, err := tx.Exec(ctx, `update users set role = $2::user_role where id = $1`, *id, role); err != nil {
		return err
	}
	action := "role_grant"
	if role == "student" {
		action = "role_revoke"
	}
	_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values ($1, $2, $3, $4, jsonb_build_object('from', $5::text, 'to', $6::text))`,
		actorID, action, *id, reason, *prev, role)
	return err
}

func validRole(s string) bool {
	for _, r := range Roles {
		if r == s {
			return true
		}
	}
	return false
}
