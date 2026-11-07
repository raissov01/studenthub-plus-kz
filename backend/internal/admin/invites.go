package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Role invites (admin_role_invites): the super admin reserves "admin" or "moderator" for an email that has not
// signed in yet; auth.bootstrapAdmin applies it on the first sign-in with that email verified. Admin only, audited.

var (
	MsgInviteEmail  = httpx.Msg{KK: "Email дұрыс емес.", RU: "Неверный email."}
	MsgInviteExists = httpx.Msg{KK: "Бұл email-мен аккаунт бар: оны «Қолданушылар» бөлімінен тауып, рөлді тікелей беріңіз.", RU: "С этим email уже есть аккаунт: найдите его в «Пользователях» и выдайте роль напрямую."}
)

type invite struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Grant     string    `json:"grant"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
	By        *person   `json:"by"`
}

// invites: GET /api/admin/invites.
func invites(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	out := []invite{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select i.id, i.email, i.grant_kind, i.reason, i.created_at, u.id, u.name, u.tg_username
			from admin_role_invites i left join users u on u.id = i.invited_by order by i.id desc limit 200`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (invite, error) {
			var v invite
			var id *int64
			var name, username *string
			err := row.Scan(&v.ID, &v.Email, &v.Grant, &v.Reason, &v.CreatedAt, &id, &name, &username)
			v.By = scanPerson(id, name, username)
			return v, err
		})
		return err
	})
	if err != nil {
		fail(w, r, "invites", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invites": out})
}

// createInvite: POST /api/admin/invites {"email", "grant": "admin"|"moderator", "reason"} — audited invite_create.
// 409 user_exists when the email already has an account.
func createInvite(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	var b struct {
		Email  string `json:"email"`
		Grant  string `json:"grant"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &b, 2<<10); err != nil || (b.Grant != "admin" && b.Grant != auth.ScopeModerator) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email, ok := auth.NormalizeEmail(b.Email)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "bad_email", MsgInviteEmail)
		return
	}
	reason, ok := cleanReason(b.Reason)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
		return
	}
	ctx := r.Context()
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select id from users where lower(email) = $1 and deleted_at is null limit 1`, email).Scan(new(int64))
		if err == nil {
			return errExists
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into admin_role_invites (email, grant_kind, reason, invited_by) values ($1, $2, $3, $4)
			on conflict (email) do update set grant_kind = excluded.grant_kind, reason = excluded.reason, invited_by = excluded.invited_by,
				created_at = now() returning id`, email, b.Grant, reason, actor.ID).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, actor.ID, "invite_create", nil, reason, map[string]any{"email": email, "grant": b.Grant})
	})
	if errors.Is(err, errExists) {
		httpx.Error(w, r, http.StatusConflict, "user_exists", MsgInviteExists)
		return
	}
	if err != nil {
		fail(w, r, "invite", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "email": email, "grant": b.Grant})
}

var errExists = errors.New("admin: email has an account")

// deleteInvite: DELETE /api/admin/invites/{id} — audited invite_delete.
func deleteInvite(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	found := false
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var email, kind string
		err := tx.QueryRow(ctx, `delete from admin_role_invites where id = $1 returning email, grant_kind`, id).Scan(&email, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return audit(ctx, tx, actor.ID, "invite_delete", nil, "", map[string]any{"email": email, "grant": kind})
	})
	if err != nil {
		fail(w, r, "invite delete", err)
		return
	}
	if !found {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}
