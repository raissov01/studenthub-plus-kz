package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// The admin workspace ported from the kurs panel (docs/research/admin-from-kurs.md): a person's orders and the
// team's internal notes on the user page, saved filters, the support contacts of the settings page. Notes,
// orders and settings are admin only; saved filters belong to the person who saved them and need the right to the
// screen (users: admin; payments: admin or a shop manager).

func workspaceRoutes(mux httpx.Router) {
	admin := func(h auth.Handler) http.HandlerFunc { return auth.NeedRole(h, "admin") }
	mux.HandleFunc("GET /api/admin/users/{id}/orders", admin(userOrders))
	mux.HandleFunc("GET /api/admin/users/{id}/notes", admin(userNotes))
	mux.HandleFunc("POST /api/admin/users/{id}/notes", auth.Write(admin(addNote)))
	mux.HandleFunc("DELETE /api/admin/users/{id}/notes/{note}", auth.Write(admin(deleteNote)))
	mux.HandleFunc("GET /api/admin/views", auth.Need(views))
	mux.HandleFunc("POST /api/admin/views", auth.Write(auth.Need(saveView)))
	mux.HandleFunc("DELETE /api/admin/views/{id}", auth.Write(auth.Need(deleteView)))
	mux.HandleFunc("GET /api/admin/config", admin(settings))
	mux.HandleFunc("PUT /api/admin/config/support", auth.Write(admin(saveSupport)))
	mux.HandleFunc("GET /api/support", publicSupport)
	mux.HandleFunc("GET /api/admin/payments", shopsOn(auth.Need(payments)))
}

// shopsOn answers like an unknown route while both the café and the print shop are switched off.
func shopsOn(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !features.Enabled("cafe") && !features.Enabled("print") {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
		h(w, r)
	}
}

var (
	MsgNote     = httpx.Msg{KK: "Жазба 1–2000 таңба болуы керек.", RU: "Заметка должна быть от 1 до 2000 символов."}
	MsgViewName = httpx.Msg{KK: "Сүзгі атауы 1–60 таңба болуы керек.", RU: "Название фильтра — от 1 до 60 символов."}
	MsgViewMany = httpx.Msg{KK: "Сақталған сүзгі тым көп (20-дан аспайды). Біреуін өшіріңіз.", RU: "Слишком много сохранённых фильтров (не больше 20). Удалите один."}
	MsgSupport  = httpx.Msg{KK: "Байланыс деректері тым ұзын не дұрыс емес.", RU: "Контакты слишком длинные или неверные."}
)

func liveUser(ctx context.Context, tx pgx.Tx, id int64) error {
	var ok bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from users where id = $1 and deleted_at is null)`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return pgx.ErrNoRows
	}
	return nil
}

func writeRaw(w http.ResponseWriter, raw json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// userOrders: GET /api/admin/users/{id}/orders — the 30 latest café orders and print jobs of the account.
func userOrders(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var out json.RawMessage
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveUser(ctx, tx, id); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select jsonb_build_object(
			'cafe', coalesce((select jsonb_agg(x order by x->>'createdAt' desc) from (
				select jsonb_build_object('id', o.id, 'shopId', o.cafe_id, 'shopKk', c.name_kk, 'shopRu', c.name_ru, 'total', o.total,
					'paid', o.paid_amount, 'status', o.status, 'method', o.payment_method, 'payment', o.payment_status,
					'refundManual', o.refund_manual, 'createdAt', o.created_at) as x
				from cafe_orders o join cafes c on c.id = o.cafe_id where o.user_id = $1 order by o.id desc limit 30) q), '[]'),
			'print', coalesce((select jsonb_agg(x order by x->>'createdAt' desc) from (
				select jsonb_build_object('id', j.id, 'shopId', j.shop_id, 'shopKk', p.name_kk, 'shopRu', p.name_ru, 'total', j.total,
					'paid', j.paid_amount, 'status', j.status, 'method', j.payment_method, 'payment', j.payment_status,
					'refundManual', j.refund_manual, 'createdAt', j.created_at) as x
				from print_jobs j join print_shops p on p.id = j.shop_id where j.user_id = $1 order by j.id desc limit 30) q), '[]'))`, id).Scan(&out)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
		return
	}
	if err != nil {
		fail(w, r, "user orders", err)
		return
	}
	writeRaw(w, out)
}

// userNotes: GET /api/admin/users/{id}/notes — newest first, with the author.
func userNotes(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var out json.RawMessage
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveUser(ctx, tx, id); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select jsonb_build_object('notes', coalesce(jsonb_agg(jsonb_build_object('id', n.id, 'body', n.body,
				'createdAt', n.created_at, 'author', case when a.id is null then null else jsonb_build_object('id', a.id, 'name', a.name, 'username', a.tg_username) end)
				order by n.id desc), '[]'))
			from admin_user_notes n left join users a on a.id = n.author_id where n.user_id = $1`, id).Scan(&out)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
		return
	}
	if err != nil {
		fail(w, r, "notes", err)
		return
	}
	writeRaw(w, out)
}

// addNote: POST /api/admin/users/{id}/notes {"body"} — audited note_add (the length, never the text).
func addNote(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	var b struct {
		Body string `json:"body"`
	}
	if err := httpx.Decode(r, &b, 8<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	body := strings.TrimSpace(b.Body)
	if n := utf8.RuneCountInString(body); n < 1 || n > 2000 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_note", MsgNote)
		return
	}
	ctx := r.Context()
	var noteID int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := liveUser(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into admin_user_notes (user_id, author_id, body) values ($1, $2, $3) returning id`, id, actor.ID, body).Scan(&noteID); err != nil {
			return err
		}
		return audit(ctx, tx, actor.ID, "note_add", &id, "", map[string]any{"note": noteID, "length": utf8.RuneCountInString(body)})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "user_not_found", MsgNoUser)
		return
	}
	if err != nil {
		fail(w, r, "note add", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": noteID})
}

// deleteNote: DELETE /api/admin/users/{id}/notes/{note} — audited note_delete.
func deleteNote(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	id, ok := pathUser(w, r)
	if !ok {
		return
	}
	note, err := strconv.ParseInt(r.PathValue("note"), 10, 64)
	if err != nil || note <= 0 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	found := false
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from admin_user_notes where id = $1 and user_id = $2`, note, id)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		found = true
		return audit(ctx, tx, actor.ID, "note_delete", &id, "", map[string]any{"note": note})
	})
	if err != nil {
		fail(w, r, "note delete", err)
		return
	}
	if !found {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- saved filters ----------

// managedShops: the café and print shop ids where u is the manager.
func managedShops(ctx context.Context, tx pgx.Tx, uid int64) (cafes, prints []int64, err error) {
	if err = tx.QueryRow(ctx, `select coalesce(array_agg(cafe_id order by cafe_id), '{}') from cafe_staff where user_id = $1 and role = 'manager'`, uid).Scan(&cafes); err != nil {
		return
	}
	err = tx.QueryRow(ctx, `select coalesce(array_agg(shop_id order by shop_id), '{}') from print_staff where user_id = $1 and role = 'manager'`, uid).Scan(&prints)
	return
}

// mayUseScreen: users needs the admin role; payments the admin role or a manager seat at a café / print shop.
func mayUseScreen(ctx context.Context, u *auth.User, screen string) (bool, error) {
	switch screen {
	case "users":
		return u.Role == "admin", nil
	case "payments":
		if u.Role == "admin" {
			return true, nil
		}
		var cafes, prints []int64
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			cafes, prints, err = managedShops(ctx, tx, u.ID)
			return err
		})
		return len(cafes)+len(prints) > 0, err
	}
	return false, nil
}

func screenGate(w http.ResponseWriter, r *http.Request, u *auth.User, screen string) bool {
	ok, err := mayUseScreen(r.Context(), u, screen)
	switch {
	case err != nil:
		fail(w, r, "screen gate", err)
	case !ok:
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
	}
	return err == nil && ok
}

type savedView struct {
	ID        int64     `json:"id"`
	Screen    string    `json:"screen"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"createdAt"`
}

// views: GET /api/admin/views?screen=users|payments — the caller's own saved filters.
func views(w http.ResponseWriter, r *http.Request, u *auth.User) {
	screen := r.URL.Query().Get("screen")
	if !screenGate(w, r, u, screen) {
		return
	}
	ctx := r.Context()
	out := []savedView{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, screen, name, query, created_at from admin_saved_views where owner_id = $1 and screen = $2 order by name`, u.ID, screen)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[savedView])
		return err
	})
	if err != nil {
		fail(w, r, "views", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"views": out})
}

// saveView: POST /api/admin/views {"screen", "name", "query"} — the same name replaces the old filter. Audited view_save.
func saveView(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var b struct{ Screen, Name, Query string }
	if err := httpx.Decode(r, &b, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if !screenGate(w, r, u, b.Screen) {
		return
	}
	name := strings.TrimSpace(b.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 60 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_name", MsgViewName)
		return
	}
	q := strings.TrimPrefix(strings.TrimSpace(b.Query), "?")
	if _, err := url.ParseQuery(q); err != nil || len(q) > 1000 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from admin_saved_views where owner_id = $1 and screen = $2 and name <> $3`, u.ID, b.Screen, name).Scan(&n); err != nil {
			return err
		}
		if n >= 20 {
			return errTooMany
		}
		if err := tx.QueryRow(ctx, `insert into admin_saved_views (owner_id, screen, name, query) values ($1, $2, $3, $4)
			on conflict (owner_id, screen, name) do update set query = excluded.query returning id`, u.ID, b.Screen, name, q).Scan(&id); err != nil {
			return err
		}
		return audit(ctx, tx, u.ID, "view_save", nil, "", map[string]any{"screen": b.Screen, "name": name})
	})
	if errors.Is(err, errTooMany) {
		httpx.Error(w, r, http.StatusConflict, "too_many", MsgViewMany)
		return
	}
	if err != nil {
		fail(w, r, "view save", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
}

var errTooMany = errors.New("admin: too many saved views")

// deleteView: DELETE /api/admin/views/{id} — only the owner's own filter. Audited view_delete.
func deleteView(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	found := false
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var screen, name string
		err := tx.QueryRow(ctx, `delete from admin_saved_views where id = $1 and owner_id = $2 returning screen, name`, id, u.ID).Scan(&screen, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return audit(ctx, tx, u.ID, "view_delete", nil, "", map[string]any{"screen": screen, "name": name})
	})
	if err != nil {
		fail(w, r, "view delete", err)
		return
	}
	if !found {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- settings ----------

// Support is what students see under «Қолдау қызметі»: every field optional, plain text.
type Support struct {
	Email    string `json:"email"`
	Telegram string `json:"telegram"`
	Phone    string `json:"phone"`
	Address  string `json:"address"`
	HoursKK  string `json:"hoursKk"`
	HoursRU  string `json:"hoursRu"`
}

func (s *Support) clean() bool {
	for _, f := range []struct {
		p   *string
		max int
	}{{&s.Email, 254}, {&s.Telegram, 64}, {&s.Phone, 40}, {&s.Address, 200}, {&s.HoursKK, 200}, {&s.HoursRU, 200}} {
		*f.p = strings.TrimSpace(*f.p)
		if utf8.RuneCountInString(*f.p) > f.max || strings.ContainsAny(*f.p, "\n\r<>") {
			return false
		}
	}
	if s.Email != "" {
		e, ok := auth.NormalizeEmail(s.Email)
		if !ok {
			return false
		}
		s.Email = e
	}
	s.Telegram = strings.TrimPrefix(s.Telegram, "@")
	return true
}

func loadSupport(ctx context.Context, tx pgx.Tx) (Support, *time.Time, *person, error) {
	var s Support
	var raw []byte
	var at *time.Time
	var id *int64
	var name, username *string
	err := tx.QueryRow(ctx, `select a.value, a.updated_at, u.id, u.name, u.tg_username from admin_settings a left join users u on u.id = a.updated_by
		where a.key = 'support'`).Scan(&raw, &at, &id, &name, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil, nil, nil
	}
	if err != nil {
		return s, nil, nil, err
	}
	_ = json.Unmarshal(raw, &s)
	return s, at, scanPerson(id, name, username), nil
}

// settings: GET /api/admin/config — the feature switches (read only: the server's environment decides them) and
// the support contacts with who changed them last.
func settings(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	var s Support
	var at *time.Time
	var by *person
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		s, at, by, err = loadSupport(ctx, tx)
		return err
	})
	if err != nil {
		fail(w, r, "settings", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"features": features.All(), "featureOrder": features.Names, "support": s, "updatedAt": at, "updatedBy": by})
}

// saveSupport: PUT /api/admin/config/support {Support} — audited settings_update with the changed field names.
func saveSupport(w http.ResponseWriter, r *http.Request, actor *auth.User) {
	var s Support
	if err := httpx.Decode(r, &s, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if !s.clean() {
		httpx.Error(w, r, http.StatusBadRequest, "bad_support", MsgSupport)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		old, _, _, err := loadSupport(ctx, tx)
		if err != nil {
			return err
		}
		changed := []string{}
		for k, pair := range map[string][2]string{"email": {old.Email, s.Email}, "telegram": {old.Telegram, s.Telegram}, "phone": {old.Phone, s.Phone},
			"address": {old.Address, s.Address}, "hoursKk": {old.HoursKK, s.HoursKK}, "hoursRu": {old.HoursRU, s.HoursRU}} {
			if pair[0] != pair[1] {
				changed = append(changed, k)
			}
		}
		if len(changed) == 0 {
			return nil
		}
		raw, _ := json.Marshal(s)
		if _, err := tx.Exec(ctx, `insert into admin_settings (key, value, updated_by, updated_at) values ('support', $1::jsonb, $2, now())
			on conflict (key) do update set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`, string(raw), actor.ID); err != nil {
			return err
		}
		return audit(ctx, tx, actor.ID, "settings_update", nil, "", map[string]any{"group": "support", "fields": sortStrings(changed)})
	})
	if err != nil {
		fail(w, r, "settings save", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"support": s})
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

// publicSupport: GET /api/support — the contacts for every visitor (cached a few minutes).
func publicSupport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var s Support
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		s, _, _, err = loadSupport(ctx, tx)
		return err
	})
	if err != nil {
		fail(w, r, "support", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.JSON(w, http.StatusOK, map[string]any{"support": s})
}
