package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

// HTTP side of the shared queue (06, stage 3): the admin queue for every kind and the shared
// report rule. Features plug in with OnApprove/OnReject (what a decision does), Notice (what
// the author is told) and OnReport (how a reported item is hidden / shown again).

// Refusal is an error a hook returns to refuse a decision with a user-facing message (409).
type Refusal struct {
	Code string
	Msg  httpx.Msg
}

func (e *Refusal) Error() string { return "moderation: refused: " + e.Code }

// NoticeFunc builds the direct message (HTML) sent to the author after a decision.
type NoticeFunc func(s Submission, approved bool, lang string) string

// ReportTarget lets the shared report rule check and hide items of one kind.
type ReportTarget struct {
	Exists    func(ctx context.Context, tx pgx.Tx, id string) (bool, error)
	SetHidden func(ctx context.Context, tx pgx.Tx, id string, hidden bool) error
}

var (
	extMu   sync.RWMutex
	notices = map[string]NoticeFunc{}
	targets = map[string]ReportTarget{}
)

// EditFunc validates the admin's corrected payload of a pending submission (old is the stored
// payload, in the new fields) and returns what to store; an error's text is the broken field.
type EditFunc func(old, in map[string]any, today string) (map[string]any, error)

var editors = map[string]EditFunc{}

// Editor makes pending submissions of kind editable by the admin (PATCH /api/admin/queue/{id}).
func Editor(kind string, fn EditFunc) { extMu.Lock(); editors[kind] = fn; extMu.Unlock() }

// Notice registers the author message for a kind; kinds without one get a generic message.
func Notice(kind string, fn NoticeFunc) { extMu.Lock(); notices[kind] = fn; extMu.Unlock() }

// OnReport registers a reportable target kind (market_item, job, opportunity, place, ...).
func OnReport(kind string, t ReportTarget) { extMu.Lock(); targets[kind] = t; extMu.Unlock() }

// HideAfter is N in "hide after N reports" (REPORT_HIDE_AFTER, default 3).
func HideAfter() int {
	n := config.Int("REPORT_HIDE_AFTER", 3)
	if n < 1 {
		n = 1
	}
	return n
}

var (
	MsgReason     = httpx.Msg{KK: "Қайтару себебін жазыңыз (3–500 таңба).", RU: "Укажите причину возврата (3–500 символов)."}
	MsgNotPending = httpx.Msg{KK: "Бұл ұсыныс әлдеқашан қаралған.", RU: "Эта заявка уже рассмотрена."}
	MsgNoItem     = httpx.Msg{KK: "Мұндай жазба табылмады.", RU: "Запись не найдена."}
	MsgKind       = httpx.Msg{KK: "Бұл түрге шағым қабылданбайды.", RU: "На этот тип нельзя пожаловаться."}
	MsgConsent    = httpx.Msg{KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."}
	MsgNotEdit    = httpx.Msg{KK: "Бұл түрдегі ұсынысты өңдеуге болмайды.", RU: "Заявку этого типа нельзя редактировать."}
	MsgRepReason  = httpx.Msg{KK: "Шағым себебін жазыңыз (3–300 таңба).", RU: "Укажите причину жалобы (3–300 символов)."}
)

// Routes wires the admin queue and the report endpoints; wrap applies the feature switch.
func Routes(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, wrap(fn)) }
	h("GET /api/admin/queue", auth.NeedRole(listQueue, "admin"))
	h("POST /api/admin/queue/{id}/approve", auth.Write(auth.NeedRole(decide(true), "admin")))
	h("POST /api/admin/queue/{id}/reject", auth.Write(auth.NeedRole(decide(false), "admin")))
	h("PATCH /api/admin/queue/{id}", auth.Write(auth.NeedRole(editPending, "admin")))
	h("POST /api/reports", auth.Write(auth.Need(report)))
	h("GET /api/admin/reports", auth.NeedRole(listReports, "admin"))
	h("POST /api/admin/reports/dismiss", auth.Write(auth.NeedRole(dismiss, "admin")))
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("moderation: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

// QueueItem is one row of GET /api/admin/queue.
type QueueItem struct {
	Submission
	CreatedAt  time.Time       `json:"created_at"`
	ReviewedAt *time.Time      `json:"reviewed_at"`
	Author     *Author         `json:"author"`
	Checklist  map[string]bool `json:"checklist,omitempty"`
	AgeHours   int             `json:"age_hours"`
	ReviewedBy *int64          `json:"reviewed_by"`
}

type Author struct {
	ID         int64   `json:"id"`
	Name       *string `json:"name"`
	Username   *string `json:"username"`
	Role       string  `json:"role"`
	University *string `json:"university"`
}

func listQueue(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	q := r.URL.Query()
	kind := strings.TrimSpace(q.Get("kind"))
	status := q.Get("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 200 {
		limit = 50
	}
	afterID, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	items := []QueueItem{}
	counts := map[string]int{}
	today := time.Now().UTC().Add(5 * time.Hour).Format("2006-01-02")
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		// pending decided oldest first; decided lists newest decision first
		order := "s.created_at, s.id"
		cursor := "s.id > $4"
		if status != "pending" {
			order = "s.reviewed_at desc, s.id desc"
			cursor = "($4 = 0 or s.id < $4)"
		}
		rows, err := tx.Query(ctx, `select s.id, s.kind, s.payload, s.submitted_by, s.university, s.status::text, s.reason,
				s.created_at, s.reviewed_at, s.reviewed_by, u.id, u.name, u.tg_username, u.role::text, u.university
			from submissions s left join users u on u.id = s.submitted_by
			where s.status = $1::submission_status and ($2 = '' or s.kind = $2) and `+cursor+`
			order by `+order+` limit $3`, status, kind, limit, afterID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it QueueItem
			var uid *int64
			var a Author
			var role *string
			if err := rows.Scan(&it.ID, &it.Kind, &it.Payload, &it.SubmittedBy, &it.University, &it.Status, &it.Reason,
				&it.CreatedAt, &it.ReviewedAt, &it.ReviewedBy, &uid, &a.Name, &a.Username, &role, &a.University); err != nil {
				return err
			}
			if uid != nil {
				a.ID = *uid
				if role != nil {
					a.Role = *role
				}
				it.Author = &a
			}
			it.AgeHours = int(time.Since(it.CreatedAt).Hours())
			it.Checklist = Checklist(it.Kind, it.Payload, today)
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		crows, err := tx.Query(ctx, `select kind, count(*) from submissions where status = 'pending' group by kind`)
		if err != nil {
			return err
		}
		defer crows.Close()
		for crows.Next() {
			var k string
			var n int
			if err := crows.Scan(&k, &n); err != nil {
				return err
			}
			counts[k] = n
		}
		return crows.Err()
	})
	if err != nil {
		fail(w, r, "queue", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "pendingByKind": counts})
}

// Checklist is the short pre-approval list the admin sees for opportunities (06): an official
// https source link, no fee asked, a sane deadline. Other kinds get none.
func Checklist(kind string, payload json.RawMessage, today string) map[string]bool {
	if kind != "opportunity" {
		return nil
	}
	var p struct {
		SourceURL string `json:"source_url"`
		Free      *bool  `json:"free"`
		Deadline  string `json:"deadline"`
	}
	_ = json.Unmarshal(payload, &p)
	return map[string]bool{
		"hasOfficialSource": strings.HasPrefix(strings.ToLower(p.SourceURL), "https://"),
		"noFee":             p.Free != nil && *p.Free,
		"deadlineOk":        p.Deadline == "" || p.Deadline >= today,
	}
}

func decide(approve bool) auth.Handler {
	return func(w http.ResponseWriter, r *http.Request, admin *auth.User) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = httpx.Decode(r, &body, 4<<10)
		reason := strings.TrimSpace(body.Reason)
		n := utf8.RuneCountInString(reason)
		if (!approve && n < 3) || n > 500 {
			httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
			return
		}
		var s Submission
		err = db.Tx(r.Context(), func(tx pgx.Tx) error {
			var err error
			if s, err = Review(r.Context(), tx, id, admin.ID, approve, reason); err != nil {
				return err
			}
			action := "moderation_approve"
			if !approve {
				action = "moderation_reject"
			}
			_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, target_id, reason, meta)
				values ($1, $2, $3, nullif($4, ''), jsonb_build_object('submission', $5::bigint, 'kind', $6::text))`,
				admin.ID, action, s.SubmittedBy, reason, s.ID, s.Kind)
			return err
		})
		var ref *Refusal
		switch {
		case errors.Is(err, ErrNotPending):
			httpx.Error(w, r, http.StatusConflict, "not_pending", MsgNotPending)
			return
		case errors.Is(err, ErrNeedReason):
			httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
			return
		case errors.As(err, &ref):
			httpx.Error(w, r, http.StatusConflict, ref.Code, ref.Msg)
			return
		case err != nil:
			fail(w, r, "review", err)
			return
		}
		notified := false
		if s.SubmittedBy != nil {
			if err := notifyAuthor(r.Context(), s, approve); err != nil {
				log.Printf("moderation: notify %d: %v", *s.SubmittedBy, err)
			} else {
				notified = true
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"submission": s, "notified": notified})
	}
}

// editPending: PATCH /api/admin/queue/{id} {"payload": {...}} — the admin corrects a pending
// suggestion (typo, missing ru text, wrong deadline) before approving it. Validated by the kind's
// EditFunc with the same rules as the original submission; recorded in audit_log.
func editPending(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var body struct {
		Payload map[string]any `json:"payload"`
	}
	if err := httpx.Decode(r, &body, 16<<10); err != nil || body.Payload == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	today := time.Now().UTC().Add(5 * time.Hour).Format("2006-01-02")
	var s Submission
	var fieldErr error
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select id, kind, payload, submitted_by, university, status::text, reason
			from submissions where id = $1 for update`, id).
			Scan(&s.ID, &s.Kind, &s.Payload, &s.SubmittedBy, &s.University, &s.Status, &s.Reason)
		if err != nil {
			return err
		}
		if s.Status != "pending" {
			return ErrNotPending
		}
		extMu.RLock()
		fn := editors[s.Kind]
		extMu.RUnlock()
		if fn == nil {
			return errNotEditable
		}
		var old map[string]any
		if err := json.Unmarshal(s.Payload, &old); err != nil {
			old = map[string]any{}
		}
		next, err := fn(old, body.Payload, today)
		if err != nil {
			fieldErr = err
			return err
		}
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		changed := []string{}
		for k, v := range next {
			a, _ := json.Marshal(v)
			b, _ := json.Marshal(old[k])
			if string(a) != string(b) {
				changed = append(changed, k)
			}
		}
		sort.Strings(changed)
		if _, err := tx.Exec(ctx, `update submissions set payload = $2::jsonb where id = $1`, id, string(raw)); err != nil {
			return err
		}
		s.Payload = raw
		ch, _ := json.Marshal(changed)
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, meta)
			values ($1, 'moderation_edit', $2, jsonb_build_object('submission', $3::bigint, 'kind', $4::text, 'changed', $5::jsonb))`,
			admin.ID, s.SubmittedBy, s.ID, s.Kind, string(ch))
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoItem)
	case errors.Is(err, ErrNotPending):
		httpx.Error(w, r, http.StatusConflict, "not_pending", MsgNotPending)
	case errors.Is(err, errNotEditable):
		httpx.Error(w, r, http.StatusBadRequest, "not_editable", MsgNotEdit)
	case fieldErr != nil:
		f := fieldErr.Error()
		httpx.Error(w, r, http.StatusBadRequest, "invalid_"+f, httpx.Msg{
			KK: "«" + f + "» өрісі дұрыс толтырылмаған.", RU: "Поле «" + f + "» заполнено неверно."})
	case err != nil:
		fail(w, r, "edit", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"submission": s})
	}
}

var errNotEditable = errors.New("moderation: kind is not editable")

// Notify is the sender; tests replace it. Decisions about the author's own submission and
// report alerts to admins are transactional: always sent, immediately.
var Notify = notify.For(notify.CatModeration)

func notifyAuthor(ctx context.Context, s Submission, approved bool) error {
	lang := "kk"
	if p, err := db.Pool(ctx); err == nil {
		_ = p.QueryRow(ctx, `select lang from users where id = $1`, *s.SubmittedBy).Scan(&lang)
	}
	extMu.RLock()
	fn := notices[s.Kind]
	extMu.RUnlock()
	text := ""
	if fn != nil {
		text = fn(s, approved, lang)
	}
	if text == "" {
		text = GenericNotice(s, approved, lang)
	}
	return Notify(ctx, *s.SubmittedBy, text)
}

// GenericNotice is the default author message: published, or returned with the reason.
func GenericNotice(s Submission, approved bool, lang string) string {
	reason := ""
	if s.Reason != nil {
		reason = html.EscapeString(*s.Reason)
	}
	site := config.SiteURL()
	if lang == "ru" {
		if approved {
			return fmt.Sprintf("✅ Ваша заявка №%d одобрена и опубликована.\n%s", s.ID, site)
		}
		return fmt.Sprintf("↩️ Ваша заявка №%d возвращена.\nПричина: %s", s.ID, reason)
	}
	if approved {
		return fmt.Sprintf("✅ №%d ұсынысыңыз бекітілді және жарияланды.\n%s", s.ID, site)
	}
	return fmt.Sprintf("↩️ №%d ұсынысыңыз қайтарылды.\nСебебі: %s", s.ID, reason)
}

// ---------- reports ----------

func report(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || body.Kind == "" || body.ID == "" || len(body.ID) > 64 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 300 {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgRepReason)
		return
	}
	extMu.RLock()
	t, ok := targets[body.Kind]
	extMu.RUnlock()
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "unknown_kind", MsgKind)
		return
	}
	res, err := Report(r.Context(), t, body.Kind, body.ID, u.ID, reason)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoItem)
		return
	}
	if err != nil {
		fail(w, r, "report", err)
		return
	}
	if res.First {
		go notifyAdmins(context.WithoutCancel(r.Context()), body.Kind, body.ID, reason)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "hidden": res.Hidden})
}

// ReportResult tells what one report changed.
type ReportResult struct {
	First   bool // first open report on this target: admins get a direct message
	Counted int  // reports that count under the shared rule
	Hidden  bool // the target is hidden now
}

// Report records a report (idempotent per reporter) and applies the shared rule: hide after
// HideAfter() distinct reporters with accounts at least 7 days old.
func Report(ctx context.Context, t ReportTarget, kind, id string, reporter int64, reason string) (ReportResult, error) {
	var res ReportResult
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		ok, err := t.Exists(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return pgx.ErrNoRows
		}
		tag, err := tx.Exec(ctx, `insert into reports (target_kind, target_id, reporter_id, reason) values ($1, $2, $3, $4)
			on conflict (target_kind, target_id, reporter_id) do nothing`, kind, id, reporter, reason)
		if err != nil {
			return err
		}
		var open int
		if err := tx.QueryRow(ctx, `select count(*) from reports where target_kind = $1 and target_id = $2 and not dismissed`,
			kind, id).Scan(&open); err != nil {
			return err
		}
		res.First = tag.RowsAffected() == 1 && open == 1
		if res.Counted, err = CountedReports(ctx, tx, kind, id); err != nil {
			return err
		}
		if res.Counted >= HideAfter() {
			res.Hidden = true
			return t.SetHidden(ctx, tx, id, true)
		}
		return nil
	})
	return res, err
}

func notifyAdmins(ctx context.Context, kind, id, reason string) {
	p, err := db.Pool(ctx)
	if err != nil {
		return
	}
	rows, err := p.Query(ctx, `select id from users where role = 'admin' and deleted_at is null`)
	if err != nil {
		log.Printf("moderation: admins: %v", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return
	}
	text := fmt.Sprintf("⚠️ Жаңа шағым / Новая жалоба: %s #%s\n%s\n%s/admin/queue",
		html.EscapeString(kind), html.EscapeString(id), html.EscapeString(reason), config.SiteURL())
	for _, a := range ids {
		if err := Notify(ctx, a, text); err != nil {
			log.Printf("moderation: notify admin %d: %v", a, err)
		}
	}
}

type reportGroup struct {
	Kind     string    `json:"kind"`
	ID       string    `json:"id"`
	Total    int       `json:"total"`
	Counted  int       `json:"counted"`
	FirstAt  time.Time `json:"first_at"`
	Reasons  []string  `json:"reasons"`
	HideAt   int       `json:"hide_after"`
	IsHidden bool      `json:"hidden"`
}

func listReports(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	out := []reportGroup{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select target_kind, target_id, count(*), min(created_at),
				(array_agg(reason order by created_at desc))[1:5]
			from reports where not dismissed group by target_kind, target_id order by min(created_at) limit 200`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var g reportGroup
			if err := rows.Scan(&g.Kind, &g.ID, &g.Total, &g.FirstAt, &g.Reasons); err != nil {
				rows.Close()
				return err
			}
			out = append(out, g)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out {
			n, err := CountedReports(ctx, tx, out[i].Kind, out[i].ID)
			if err != nil {
				return err
			}
			out[i].Counted, out[i].HideAt, out[i].IsHidden = n, HideAfter(), n >= HideAfter()
		}
		return nil
	})
	if err != nil {
		fail(w, r, "reports", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"reports": out})
}

// dismiss: the admin found the reports groundless; they stop counting and the item shows again.
func dismiss(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	var body struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &body, 2<<10); err != nil || body.Kind == "" || body.ID == "" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	extMu.RLock()
	t, ok := targets[body.Kind]
	extMu.RUnlock()
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "unknown_kind", MsgKind)
		return
	}
	var n int64
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update reports set dismissed = true where target_kind = $1 and target_id = $2 and not dismissed`,
			body.Kind, body.ID)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		if err := t.SetHidden(r.Context(), tx, body.ID, false); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, reason, meta)
			values ($1, 'report_dismiss', nullif($2, ''), jsonb_build_object('kind', $3::text, 'id', $4::text, 'reports', $5::bigint))`,
			admin.ID, strings.TrimSpace(body.Reason), body.Kind, body.ID, n)
		return err
	})
	if err != nil {
		fail(w, r, "dismiss", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"dismissed": n, "hidden": false})
}
