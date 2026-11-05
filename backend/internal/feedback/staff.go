package feedback

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cronrun"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/events"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// Staff: admins see every inbox; university_staff see the recipients an admin assigned them to.
// Held (flagged) tickets stay out of every inbox until an admin approves them in the queue.

func staffRole(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.Role != "admin" && u.Role != "university_staff" {
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
		return false
	}
	return true
}

const visible = `t.status <> 'held' and ($2 or exists (select 1 from feedback_recipient_staff s where s.recipient_id = t.recipient_id and s.user_id = $1))`

func withAuthor(ctx context.Context, tx pgx.Tx, t *Ticket) error {
	if t.Anonymous || t.authorID == nil {
		return nil
	}
	a := StaffMember{UserID: *t.authorID}
	if err := tx.QueryRow(ctx, `select name, role::text from users where id = $1`, *t.authorID).Scan(&a.Name, &a.Role); err != nil {
		return err
	}
	t.Author = &a
	return nil
}

func staffInbox(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !staffRole(w, r, u) {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if !slices.Contains([]string{"open", "all", "new", "in_progress", "answered", "closed"}, status) {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	admin := u.Role == "admin"
	out := []*Ticket{}
	recs := []Recipient{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select `+ticketCols+` from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id
			where `+visible+` and ($3 = 'all' or ($3 = 'open' and t.status in ('new', 'in_progress')) or t.status = $3)
			order by (t.status in ('new', 'in_progress')) desc, t.sla_due_at, t.id desc limit 200`, u.ID, admin, status)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Ticket, error) { return scanTicket(row) }); err != nil {
			return err
		}
		for _, t := range out {
			if err := withAuthor(r.Context(), tx, t); err != nil {
				return err
			}
			if t.photo != nil {
				t.Photo = "/api/feedback/photos/" + *t.photo
			}
		}
		rows, err = tx.Query(r.Context(), `select r.id, r.university, r.key, r.name_kk, r.name_ru, r.sla_hours, r.active from feedback_recipients r
			where $2 or exists (select 1 from feedback_recipient_staff s where s.recipient_id = r.id and s.user_id = $1) order by r.university, r.id`, u.ID, admin)
		if err != nil {
			return err
		}
		recs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Recipient, error) {
			var x Recipient
			return x, row.Scan(&x.ID, &x.University, &x.Key, &x.NameKK, &x.NameRU, &x.SLAHours, &x.Active)
		})
		return err
	})
	if err != nil {
		univoice.Internal(w, r, "inbox", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tickets": out, "recipients": recs})
}

func staffLoad(ctx context.Context, tx pgx.Tx, u *auth.User, id int64, lock bool) (*Ticket, error) {
	q := `select ` + ticketCols + ` from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id where t.id = $3 and ` + visible
	if lock {
		q += ` for update of t`
	}
	t, err := scanTicket(tx.QueryRow(ctx, q, u.ID, u.Role == "admin", id))
	if err != nil {
		return nil, err
	}
	if t.photo != nil {
		t.Photo = "/api/feedback/photos/" + *t.photo
	}
	return t, withAuthor(ctx, tx, t)
}

func staffTicket(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := univoice.PathID(r, "id")
	if !staffRole(w, r, u) {
		return
	}
	var t *Ticket
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		if !ok {
			return pgx.ErrNoRows
		}
		if t, err = staffLoad(r.Context(), tx, u, id, false); err != nil {
			return err
		}
		return loadMessages(r.Context(), tx, t)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		univoice.Internal(w, r, "staff ticket", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ticket": t})
}

var staffStatuses = []string{"new", "in_progress", "answered", "closed"}

func staffReply(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := univoice.PathID(r, "id")
	if !staffRole(w, r, u) {
		return
	}
	var in struct {
		Body   string `json:"body"`
		Status string `json:"status"`
	}
	if !ok || httpx.Decode(r, &in, 16<<10) != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	body, okBody := univoice.Clean(in.Body, 1, 3000, true)
	if in.Status == "" {
		in.Status = "answered"
	}
	if !okBody || !slices.Contains(staffStatuses[1:], in.Status) {
		univoice.Bad(w, r, "invalid", httpx.MsgBad)
		return
	}
	var t *Ticket
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		if t, err = staffLoad(r.Context(), tx, u, id, true); err != nil {
			return err
		}
		at := Now()
		if t.Anonymous {
			at = hourOf(at)
		}
		if _, err := tx.Exec(r.Context(), `insert into feedback_messages (ticket_id, from_staff, staff_id, body, created_at) values ($1, true, $2, $3, $4)`,
			t.ID, u.ID, body, at); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `update feedback_tickets set status = $2, student_unread = true, updated_at = $3 where id = $1`, t.ID, in.Status, at)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		univoice.Internal(w, r, "reply", err)
		return
	}
	// a signed sender hears about it in the inbox/Telegram; an anonymous one on the device (lookup)
	if !t.Anonymous && t.authorID != nil {
		ctx := notify.WithLink(context.WithoutCancel(r.Context()), "/feedback")
		_ = NotifyStudent(ctx, *t.authorID, "💬 «"+t.Recipient.NameKK+"» өтінішіңізге жауап берді / ответил на ваше обращение")
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func staffStatus(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := univoice.PathID(r, "id")
	if !staffRole(w, r, u) {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || !slices.Contains(staffStatuses, in.Status) {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		t, err := staffLoad(r.Context(), tx, u, id, true)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `update feedback_tickets set status = $2, updated_at = $3 where id = $1`, t.ID, in.Status, Now())
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		univoice.Internal(w, r, "status", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// photo: only staff who may see the ticket (or admins, including while it is held).
func photo(w http.ResponseWriter, r *http.Request, u *auth.User) {
	name := r.PathValue("name")
	if !photoRe.MatchString(name) || (u.Role != "admin" && u.Role != "university_staff") {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	var ok bool
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `select exists (select 1 from feedback_tickets t where t.photo = $3 and
			($2 or (t.status <> 'held' and exists (select 1 from feedback_recipient_staff s where s.recipient_id = t.recipient_id and s.user_id = $1))))`,
			u.ID, u.Role == "admin", name).Scan(&ok)
	})
	if err != nil {
		univoice.Internal(w, r, "photo", err)
		return
	}
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	f, err := os.Open(filepath.Join(PhotoDir(), name))
	if err != nil {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// ---------- SLA cron ----------

// SLAStats is what one cron run did.
type SLAStats struct {
	Overdue  int `json:"overdue"`
	Reminded int `json:"reminded"`
}

// RemindOverdue: tickets past their SLA (new / in_progress) remind the recipient's staff, at most
// once a day per ticket.
func RemindOverdue(ctx context.Context, now time.Time) (SLAStats, error) {
	var st SLAStats
	byRecipient := map[int64]int{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `update feedback_tickets set sla_reminded_at = $1::timestamptz
			where status in ('new', 'in_progress') and sla_due_at < $1::timestamptz and (sla_reminded_at is null or sla_reminded_at < $1::timestamptz - interval '24 hours')
			returning recipient_id`, now)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		for _, id := range ids {
			byRecipient[id]++
		}
		st.Overdue = len(ids)
		return err
	})
	if err != nil {
		return st, err
	}
	for rec, n := range byRecipient {
		ids, name, err := staffOf(ctx, rec)
		if err != nil {
			return st, err
		}
		text := "⏰ «" + name + "»: жауап мерзімі өткен өтініштер / обращения с истёкшим сроком ответа: " + strconv.Itoa(n)
		lctx := notify.WithLink(ctx, "/voice")
		for _, id := range ids {
			if NotifyStaff(lctx, id, text) == nil {
				st.Reminded++
			}
		}
	}
	return st, nil
}

func cron(w http.ResponseWriter, r *http.Request) {
	if !events.CronAuthorized(r) {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	st, err := RemindOverdue(r.Context(), Now())
	if err != nil {
		univoice.Internal(w, r, "cron", err)
		return
	}
	if err := cronrun.Success(r.Context(), "/api/cron/feedback"); err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "cron_record_failed", httpx.MsgInternal)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}
