package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Scoped admin roles (admin_grants) and the "who manages what" overview of /admin/roles. Shop staff, club
// admins, university staff and feedback recipients keep their own grant screens (/admin/cafe, /admin/print,
// /admin/clubs, /admin/staff, /voice); this page lists them together so an admin sees every right at once.

// Scopes lists the grantable scopes.
var Scopes = []string{auth.ScopeModerator}

var almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

func validScope(s string) bool {
	for _, x := range Scopes {
		if x == s {
			return true
		}
	}
	return false
}

// grants: GET /api/admin/grants.
func grants(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	var out json.RawMessage
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select jsonb_build_object(
			'scopes', $1::text[],
			'grants', coalesce((select jsonb_agg(jsonb_build_object('userId', u.id, 'name', u.name, 'username', u.tg_username,
					'university', u.university, 'role', u.role::text, 'scope', g.scope, 'reason', g.reason, 'createdAt', g.created_at,
					'grantedBy', case when gb.id is null then null else jsonb_build_object('id', gb.id, 'name', gb.name) end) order by g.created_at desc)
				from admin_grants g join users u on u.id = g.user_id left join users gb on gb.id = g.granted_by), '[]'),
			'admins', coalesce((select jsonb_agg(jsonb_build_object('userId', id, 'name', name, 'username', tg_username) order by id)
				from users where role = 'admin' and deleted_at is null), '[]'),
			'cafes', coalesce((select jsonb_agg(jsonb_build_object('shopId', c.id, 'nameKk', c.name_kk, 'nameRu', c.name_ru,
					'userId', u.id, 'name', u.name, 'username', u.tg_username, 'role', s.role) order by c.id, s.role, u.id)
				from cafe_staff s join cafes c on c.id = s.cafe_id join users u on u.id = s.user_id), '[]'),
			'printShops', coalesce((select jsonb_agg(jsonb_build_object('shopId', p.id, 'nameKk', p.name_kk, 'nameRu', p.name_ru,
					'userId', u.id, 'name', u.name, 'username', u.tg_username, 'role', s.role) order by p.id, s.role, u.id)
				from print_staff s join print_shops p on p.id = s.shop_id join users u on u.id = s.user_id), '[]'),
			'clubs', coalesce((select jsonb_agg(jsonb_build_object('clubId', c.id, 'nameKk', c.name_kk, 'nameRu', c.name_ru,
					'userId', u.id, 'name', u.name, 'username', u.tg_username) order by c.name_kk, u.id)
				from club_admins a join clubs c on c.id = a.club_id join users u on u.id = a.user_id), '[]'),
			'universityStaff', coalesce((select jsonb_agg(jsonb_build_object('university', s.university,
					'userId', u.id, 'name', u.name, 'username', u.tg_username) order by s.university, u.id)
				from university_staff s join users u on u.id = s.user_id where u.deleted_at is null), '[]'),
			'feedbackStaff', coalesce((select jsonb_agg(jsonb_build_object('recipientId', fr.id, 'university', fr.university,
					'nameKk', fr.name_kk, 'nameRu', fr.name_ru, 'userId', u.id, 'name', u.name, 'username', u.tg_username) order by fr.university, fr.id, u.id)
				from feedback_recipient_staff s join feedback_recipients fr on fr.id = s.recipient_id join users u on u.id = s.user_id), '[]')
		)`, Scopes).Scan(&out)
	})
	if err != nil {
		fail(w, r, "grants", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

type grantBody struct {
	UserID int64  `json:"userId"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

func readGrant(w http.ResponseWriter, r *http.Request) (grantBody, bool) {
	var b grantBody
	if err := httpx.Decode(r, &b, 2<<10); err != nil || b.UserID <= 0 || !validScope(b.Scope) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return b, false
	}
	reason, ok := cleanReason(b.Reason)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
		return b, false
	}
	b.Reason = reason
	return b, true
}

// grantScope: POST /api/admin/grants {"userId", "scope", "reason"} — audited as scope_grant.
func grantScope(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	b, ok := readGrant(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	changed := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveTarget(ctx, tx, actor.ID, b.UserID, false); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `insert into admin_grants (user_id, scope, granted_by, reason) values ($1, $2, $3, $4)
			on conflict (user_id, scope) do nothing`, b.UserID, b.Scope, actor.ID, b.Reason)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		changed = true
		return audit(ctx, tx, actor.ID, "scope_grant", &b.UserID, b.Reason, map[string]any{"scope": b.Scope})
	})
	if targetErr(w, r, "grant", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"changed": changed})
}

// revokeScope: POST /api/admin/grants/revoke {"userId", "scope", "reason"} — audited as scope_revoke.
func revokeScope(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	b, ok := readGrant(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	changed := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveTarget(ctx, tx, actor.ID, b.UserID, false); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `delete from admin_grants where user_id = $1 and scope = $2`, b.UserID, b.Scope)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		changed = true
		return audit(ctx, tx, actor.ID, "scope_revoke", &b.UserID, b.Reason, map[string]any{"scope": b.Scope})
	})
	if targetErr(w, r, "revoke", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"changed": changed})
}
