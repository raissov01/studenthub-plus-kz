// Package admincontent is the /admin content editor: opportunities, jobs and discounts stored in
// Postgres (admin_content) in exactly the data/*.json record shape, validated by the same rules
// as a Google Sheets row (content.ValidateRecord) and merged into /api/content as "adm-<id>" with
// source = "admin". Role admin only; every write lands in audit_log. University staff publish
// their own university's opportunities through pkg/b2b (university_posts), not here.
// See docs/api/admin-content.md.
package admincontent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Source is the "source" marker of these records in /api/content.
const Source = "admin"

// Prefix is the id prefix ("adm-<admin_content.id>"); Sheets rows may not use it.
const Prefix = content.AdminIDPrefix

var Statuses = []string{"published", "hidden"}

var (
	MsgKind     = httpx.Msg{KK: "Түрі белгісіз: opportunities, jobs немесе discounts.", RU: "Неизвестный тип: opportunities, jobs или discounts."}
	MsgStatus   = httpx.Msg{KK: "Күйі белгісіз: published немесе hidden.", RU: "Неизвестный статус: published или hidden."}
	MsgNotFound = httpx.Msg{KK: "Мұндай жазба табылмады.", RU: "Запись не найдена."}
)

// Now is the clock (tests move it).
var Now = time.Now

func today() string { return content.AlmatyToday(Now()) }

func init() { registerContent() }

// Routes wires /api/admin/content*.
func Routes(mux httpx.Router) {
	admin := func(h auth.Handler) http.HandlerFunc { return auth.NeedRole(h, "admin") }
	write := func(h auth.Handler) http.HandlerFunc { return auth.Write(auth.NeedRole(h, "admin")) }
	mux.HandleFunc("GET /api/admin/content", admin(list))
	mux.HandleFunc("POST /api/admin/content", write(create))
	mux.HandleFunc("POST /api/admin/content/validate", write(validate))
	mux.HandleFunc("GET /api/admin/content/{id}", admin(get))
	mux.HandleFunc("PUT /api/admin/content/{id}", write(update))
	mux.HandleFunc("POST /api/admin/content/{id}/hide", write(setStatus("hidden")))
	mux.HandleFunc("POST /api/admin/content/{id}/unhide", write(setStatus("published")))
	mux.HandleFunc("DELETE /api/admin/content/{id}", write(remove))
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("admincontent: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

// ---------- records ----------

// Item is one row of the admin list / editor.
type Item struct {
	ID         string          `json:"id"` // adm-<n>, the same id as in /api/content
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	NoDeadline bool            `json:"no_deadline"`
	Expired    bool            `json:"expired"` // deadline before today (Asia/Almaty): not in /api/content
	Record     json.RawMessage `json:"record"`  // exactly as /api/content serves it (with source)
	CreatedBy  *int64          `json:"created_by"`
	UpdatedBy  *int64          `json:"updated_by"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// served decodes a stored record into its typed shape, sets the id and adds source = "admin".
func served(kind string, id int64, stored []byte) (json.RawMessage, string, error) {
	rec := content.NewRecord(kind)
	if rec == nil {
		return nil, "", errors.New("unknown kind " + kind)
	}
	if err := json.Unmarshal(stored, rec); err != nil {
		return nil, "", err
	}
	rid := Prefix + strconv.FormatInt(id, 10)
	content.SetRecordID(rec, rid)
	var v any
	deadline := ""
	switch x := rec.(type) {
	case *content.Opportunity:
		x.IsDemo = false
		v = struct {
			*content.Opportunity
			Source string `json:"source"`
		}{x, Source}
		deadline = deref(x.Deadline)
	case *content.Job:
		x.IsDemo = false
		v = struct {
			*content.Job
			Source string `json:"source"`
		}{x, Source}
		deadline = deref(x.Deadline)
	case *content.Discount:
		x.IsDemo = false
		v = struct {
			*content.Discount
			Source string `json:"source"`
		}{x, Source}
		deadline = deref(x.Deadline)
	}
	raw, err := json.Marshal(v)
	return raw, deadline, err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

const cols = `id, kind, record, status, deadline::text, created_by, updated_by, created_at, updated_at`

func scan(row pgx.Row) (Item, error) {
	var it Item
	var n int64
	var stored []byte
	var deadline *string
	if err := row.Scan(&n, &it.Kind, &stored, &it.Status, &deadline, &it.CreatedBy, &it.UpdatedBy, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return it, err
	}
	raw, _, err := served(it.Kind, n, stored)
	if err != nil {
		return it, err
	}
	it.ID, it.Record = Prefix+strconv.FormatInt(n, 10), raw
	it.NoDeadline = deadline == nil
	it.Expired = deadline != nil && *deadline < today()
	return it, nil
}

// Published returns the visible records per /api/content list (expired ones are dropped by
// content.Load like every other source).
func Published(ctx context.Context) (map[string][]content.Record, error) {
	out := map[string][]content.Record{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, kind, record from admin_content where status = 'published'
			order by created_at desc, id desc limit 1000`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var kind string
			var stored []byte
			if err := rows.Scan(&id, &kind, &stored); err != nil {
				return err
			}
			raw, deadline, err := served(kind, id, stored)
			if err != nil {
				log.Printf("admincontent: record %d: %v", id, err)
				continue
			}
			out[kind] = append(out[kind], content.RawRecord(raw, deadline))
		}
		return rows.Err()
	})
	return out, err
}

func registerContent() {
	content.RegisterExtra("admin", func(ctx context.Context) (map[string][]content.Record, error) {
		if _, err := db.Pool(ctx); errors.Is(err, db.ErrNoDB) {
			return nil, nil
		}
		return Published(ctx)
	})
}

// ---------- validation ----------

type writeBody struct {
	Kind       string         `json:"kind"`        // create only
	Status     string         `json:"status"`      // published (default on create) | hidden
	NoDeadline bool           `json:"no_deadline"` // «мерзімсіз»: exactly one of record.deadline / no_deadline
	Record     map[string]any `json:"record"`      // data/*.json shape; id and is_demo are ignored
}

// errorBody is a validation error: {"error": {"code", "message", "field", "path"}}.
func invalid(w http.ResponseWriter, r *http.Request, kind string, fe *content.FieldError) {
	msg := fe.Message
	if httpx.Lang(r) == "ru" {
		msg = "Проверьте поле «" + fe.Field + "»: " + fe.Message
	}
	httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{
		"code": "invalid_record", "message": msg, "field": fe.Field, "path": Path(kind, fe.Field)}})
}

// Path maps a sheet column (the FieldError.Field) to the JSON path in the record, for the editor
// form: title_kk -> title.kk, docs_ru -> docs[].ru, contact_email -> contact.email,
// req_gpa -> req.gpa (opportunities), req_kk -> req[].kk (jobs), no_deadline -> no_deadline.
func Path(kind, field string) string {
	switch {
	case field == "" || field == "no_deadline" || field == "kind" || field == "list":
		return field
	case strings.HasPrefix(field, "contact_"):
		return "contact." + strings.TrimPrefix(field, "contact_")
	case kind == "opportunities" && strings.HasPrefix(field, "req_"):
		return "req." + strings.TrimPrefix(field, "req_")
	}
	for _, lang := range []string{"_kk", "_ru"} {
		if base, ok := strings.CutSuffix(field, lang); ok {
			if base == "docs" || base == "steps" || (kind == "jobs" && base == "req") {
				return base + "[]." + lang[1:]
			}
			return base + "." + lang[1:]
		}
	}
	return field
}

// check validates a write body for kind; posted_at defaults to today.
func check(kind string, b writeBody) (content.Record, *content.FieldError) {
	rec, fe := content.ValidateRecord(kind, b.Record, b.NoDeadline)
	if fe != nil {
		return nil, fe
	}
	t := today()
	switch x := rec.(type) {
	case *content.Opportunity:
		if x.PostedAt == nil {
			x.PostedAt = &t
		}
	case *content.Job:
		if x.PostedAt == nil {
			x.PostedAt = &t
		}
	case *content.Discount:
		if x.PostedAt == nil {
			x.PostedAt = &t
		}
	}
	return rec, nil
}

func deadlineOf(rec content.Record) *string {
	switch x := rec.(type) {
	case *content.Opportunity:
		return x.Deadline
	case *content.Job:
		return x.Deadline
	case *content.Discount:
		return x.Deadline
	}
	return nil
}

func titleOf(rec content.Record) string {
	switch x := rec.(type) {
	case *content.Opportunity:
		return x.Title.KK
	case *content.Job:
		return x.Title.KK
	case *content.Discount:
		return x.Name.KK
	}
	return ""
}

func decodeBody(w http.ResponseWriter, r *http.Request) (writeBody, bool) {
	var b writeBody
	if err := httpx.Decode(r, &b, 64<<10); err != nil || b.Record == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return b, false
	}
	if b.Status != "" && !slices.Contains(Statuses, b.Status) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_status", MsgStatus)
		return b, false
	}
	return b, true
}

func audit(ctx context.Context, tx pgx.Tx, actor int64, action string, meta map[string]any) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, meta) values ($1, $2, $3::jsonb)`, actor, action, string(raw))
	return err
}

func parseID(r *http.Request) (int64, bool) {
	s := r.PathValue("id")
	if !strings.HasPrefix(s, Prefix) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(s, Prefix), 10, 64)
	return n, err == nil && n > 0
}

// ---------- handlers ----------

// list: GET /api/admin/content?kind=&status=published|hidden|all&q=&limit=
func list(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "" && !slices.Contains(content.Kinds, kind) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_kind", MsgKind)
		return
	}
	status := q.Get("status")
	if status == "" {
		status = "all"
	}
	if status != "all" && !slices.Contains(Statuses, status) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_status", MsgStatus)
		return
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 500 {
		limit = 200
	}
	search := strings.TrimSpace(q.Get("q"))
	items := []Item{}
	counts := map[string]map[string]int{}
	for _, k := range content.Kinds {
		counts[k] = map[string]int{"published": 0, "hidden": 0}
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+cols+` from admin_content
			where ($1 = '' or kind = $1) and ($2 = 'all' or status = $2)
			  and ($3 = '' or concat_ws(' ', record #>> '{title,kk}', record #>> '{title,ru}', record #>> '{name,kk}',
			       record #>> '{name,ru}', record #>> '{org,kk}', record #>> '{org,ru}') ilike '%' || $3 || '%')
			order by updated_at desc, id desc limit $4`, kind, status, search, limit)
		if err != nil {
			return err
		}
		if items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) { return scan(row) }); err != nil {
			return err
		}
		crows, err := tx.Query(ctx, `select kind, status, count(*) from admin_content group by kind, status`)
		if err != nil {
			return err
		}
		defer crows.Close()
		for crows.Next() {
			var k, s string
			var n int
			if err := crows.Scan(&k, &s, &n); err != nil {
				return err
			}
			if counts[k] != nil {
				counts[k][s] = n
			}
		}
		return crows.Err()
	})
	if err != nil {
		fail(w, r, "list", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "counts": counts})
}

func get(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := parseID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
		return
	}
	var it Item
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		it, err = scan(tx.QueryRow(r.Context(), `select `+cols+` from admin_content where id = $1`, id))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "get", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
}

// validate: POST /api/admin/content/validate — a dry run of create (nothing is stored).
func validate(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	b, ok := decodeBody(w, r)
	if !ok {
		return
	}
	if !slices.Contains(content.Kinds, b.Kind) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_kind", MsgKind)
		return
	}
	rec, fe := check(b.Kind, b)
	if fe != nil {
		invalid(w, r, b.Kind, fe)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "record": rec})
}

func create(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	ctx := r.Context()
	b, ok := decodeBody(w, r)
	if !ok {
		return
	}
	if !slices.Contains(content.Kinds, b.Kind) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_kind", MsgKind)
		return
	}
	if b.Status == "" {
		b.Status = "published"
	}
	rec, fe := check(b.Kind, b)
	if fe != nil {
		invalid(w, r, b.Kind, fe)
		return
	}
	stored, err := json.Marshal(rec)
	if err != nil {
		fail(w, r, "create", err)
		return
	}
	var it Item
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		it, err = scan(tx.QueryRow(ctx, `insert into admin_content (kind, record, status, deadline, created_by, updated_by)
			values ($1, $2::jsonb, $3, $4::date, $5, $5) returning `+cols, b.Kind, string(stored), b.Status, deadlineOf(rec), admin.ID))
		if err != nil {
			return err
		}
		return audit(ctx, tx, admin.ID, "content_create", map[string]any{
			"id": it.ID, "kind": b.Kind, "status": b.Status, "title": titleOf(rec)})
	})
	if err != nil {
		fail(w, r, "create", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"item": it})
}

// update: PUT /api/admin/content/{id} — replaces the whole record (kind is fixed); status is
// kept unless given.
func update(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	ctx := r.Context()
	id, ok := parseID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
		return
	}
	b, ok := decodeBody(w, r)
	if !ok {
		return
	}
	var it Item
	var fe *content.FieldError
	var kind string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var oldRecord []byte
		var oldStatus string
		if err := tx.QueryRow(ctx, `select kind, record, status from admin_content where id = $1 for update`, id).
			Scan(&kind, &oldRecord, &oldStatus); err != nil {
			return err
		}
		if b.Kind != "" && b.Kind != kind {
			fe = &content.FieldError{Field: "kind", Message: `"kind" өзгертуге болмайды — жаңа жазба ретінде қосыңыз`}
			return errInvalid
		}
		rec, e := check(kind, b)
		if e != nil {
			fe = e
			return errInvalid
		}
		status := b.Status
		if status == "" {
			status = oldStatus
		}
		stored, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if it, err = scan(tx.QueryRow(ctx, `update admin_content set record = $2::jsonb, status = $3, deadline = $4::date,
				updated_by = $5, updated_at = now() where id = $1 returning `+cols,
			id, string(stored), status, deadlineOf(rec), admin.ID)); err != nil {
			return err
		}
		meta := map[string]any{"id": it.ID, "kind": kind, "title": titleOf(rec), "changed": changedKeys(oldRecord, stored)}
		if status != oldStatus {
			meta["status"] = status
		}
		return audit(ctx, tx, admin.ID, "content_update", meta)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
	case errors.Is(err, errInvalid):
		invalid(w, r, kind, fe)
	case err != nil:
		fail(w, r, "update", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
	}
}

var errInvalid = errors.New("admincontent: invalid record")

// changedKeys lists the top-level record fields that differ (for the audit row).
func changedKeys(before, after []byte) []string {
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal(before, &a)
	_ = json.Unmarshal(after, &b)
	out := []string{}
	for k, v := range b {
		if !jsonEqual(a[k], v) {
			out = append(out, k)
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func jsonEqual(x, y json.RawMessage) bool {
	var a, b any
	if json.Unmarshal(x, &a) != nil || json.Unmarshal(y, &b) != nil {
		return string(x) == string(y)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// setStatus: POST /api/admin/content/{id}/hide | /unhide (idempotent).
func setStatus(status string) auth.Handler {
	action := "content_hide"
	if status == "published" {
		action = "content_unhide"
	}
	return func(w http.ResponseWriter, r *http.Request, admin *auth.User) {
		ctx := r.Context()
		id, ok := parseID(r)
		if !ok {
			httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
			return
		}
		var it Item
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			if it, err = scan(tx.QueryRow(ctx, `update admin_content set status = $2, updated_by = $3, updated_at = now()
				where id = $1 returning `+cols, id, status, admin.ID)); err != nil {
				return err
			}
			return audit(ctx, tx, admin.ID, action, map[string]any{"id": it.ID, "kind": it.Kind})
		})
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
			return
		}
		if err != nil {
			fail(w, r, action, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
	}
}

// remove: DELETE /api/admin/content/{id} — the audit row keeps a copy of the deleted record.
func remove(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	ctx := r.Context()
	id, ok := parseID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
		return
	}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var kind, status string
		var stored json.RawMessage
		if err := tx.QueryRow(ctx, `delete from admin_content where id = $1 returning kind, status, record`, id).
			Scan(&kind, &status, &stored); err != nil {
			return err
		}
		return audit(ctx, tx, admin.ID, "content_delete", map[string]any{
			"id": Prefix + strconv.FormatInt(id, 10), "kind": kind, "status": status, "record": stored})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "delete", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": Prefix + strconv.FormatInt(id, 10)})
}
