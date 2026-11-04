// Package feedback is «Анонимді ұсыныс және шағым / Анонимные предложения и жалобы»
// (FEATURE_FEEDBACK, docs/UNIVOICE.md). A student writes to a recipient of their university
// (Деканат, Жатақхана, ...), anonymously or signed; the recipient's assigned staff answer in an inbox.
//
// Anonymous tickets store no author at all (author_id is null, enforced by a CHECK). The student
// keeps access through a 256-bit random ticket token returned once and kept on the device; the
// server stores only SHA-256(token). Staff, admins and the database therefore never learn the
// sender; the cost is that a lost token cannot be recovered and replies cannot be pushed to an
// anonymous sender — the device polls with its tokens (POST /api/feedback/lookup).
package feedback

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/market"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// Feature is the switch: FEATURE_FEEDBACK.
const Feature = "feedback"

// Kind is the moderation queue kind of a held (flagged) ticket.
const Kind = "feedback"

// Now is replaced in tests.
var Now = time.Now

var (
	// NotifyStudent tells a signed sender about a reply (inbox + Telegram).
	NotifyStudent = notify.For(notify.CatCommunity)
	// NotifyStaff tells assigned staff / admins about new tickets, overdue SLAs and escalations.
	NotifyStaff = notify.For(notify.CatModeration)
)

var Categories = []string{"suggestion", "complaint", "question", "gratitude"}

var (
	MsgEscalate = httpx.Msg{
		KK: "Хабарламада қауіп не төтенше жағдай белгілері бар. Ол алдымен платформа әкімшісіне жіберіледі (анонимді болса да). Қазір біреудің өміріне қауіп болса — 112-ге қоңырау шалыңыз. Жалғастыру үшін ескертуді растаңыз.",
		RU: "В сообщении есть признаки угрозы или экстренной ситуации. Оно сначала уйдёт администратору платформы (даже анонимное). Если сейчас есть угроза жизни — звоните 112. Чтобы продолжить, подтвердите, что прочитали предупреждение.",
	}
	MsgRecipient = httpx.Msg{KK: "Алушы табылмады.", RU: "Получатель не найден."}
	MsgToken     = httpx.Msg{KK: "Өтініш табылмады.", RU: "Обращение не найдено."}
	MsgClosed    = httpx.Msg{KK: "Өтініш жабылған.", RU: "Обращение закрыто."}
	MsgLimit     = httpx.Msg{KK: "Бүгінгі өтініштер шегіне жеттіңіз. Ертең қайталаңыз.", RU: "Достигнут лимит обращений на сегодня. Попробуйте завтра."}
	MsgPhoto     = httpx.Msg{KK: "Фото қабылданбады: JPEG, PNG не WebP, 600 КБ-қа дейін.", RU: "Фото не принято: JPEG, PNG или WebP до 600 КБ."}
	MsgStudents  = httpx.Msg{KK: "Өтінішті студенттер жібереді.", RU: "Обращения отправляют студенты."}
	MsgConsent   = httpx.Msg{KK: "Алдымен келісім беріңіз.", RU: "Сначала дайте согласие."}
)

// PhotoDir is $UPLOAD_DIR/feedback.
func PhotoDir() string { return filepath.Join(config.Str("UPLOAD_DIR", "./uploads"), "feedback") }

var photoRe = regexp.MustCompile(`^[0-9a-f]{32}\.jpg$`)

func init() {
	me.RegisterExport("feedbackTickets", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select t.id, r.name_kk as recipient, t.category, t.body, t.status, t.created_at,
			(select coalesce(jsonb_agg(jsonb_build_object('fromStaff', m.from_staff, 'body', m.body, 'createdAt', m.created_at) order by m.id), '[]')
			 from feedback_messages m where m.ticket_id = t.id) as messages
			from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id where t.author_id = app_user_id() order by t.id`)
	})
	// signed tickets cascade with the account; their photos go first
	me.RegisterDeleteHook(func(ctx context.Context, tx pgx.Tx, userID int64) error {
		rows, err := tx.Query(ctx, `select photo from feedback_tickets where author_id = $1 and photo is not null`, userID)
		if err != nil {
			return err
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, n := range names {
			if err := removePhoto(n); err != nil {
				return err
			}
		}
		return nil
	})
	moderation.OnApprove(Kind, func(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
		id, err := heldID(s)
		if err != nil {
			return err
		}
		var rec int64
		err = tx.QueryRow(ctx, `update feedback_tickets set status = 'new', updated_at = $2 where id = $1 and status = 'held' returning recipient_id`, id, Now()).Scan(&rec)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		tellStaff(context.WithoutCancel(ctx), rec, "new")
		return nil
	})
	moderation.OnReject(Kind, func(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
		id, err := heldID(s)
		if err != nil {
			return err
		}
		reason := ""
		if s.Reason != nil {
			reason = *s.Reason
		}
		tag, err := tx.Exec(ctx, `update feedback_tickets set status = 'closed', student_unread = true, updated_at = $2 where id = $1 and status = 'held'`, id, Now())
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		body := "Модерациядан өтпеді / Не прошло модерацию"
		if reason != "" {
			body += ": " + reason
		}
		_, err = tx.Exec(ctx, `insert into feedback_messages (ticket_id, from_staff, body, created_at) values ($1, true, $2, $3)`, id, clip(body, 3000), hourOf(Now()))
		return err
	})
}

func heldID(s moderation.Submission) (int64, error) {
	var p struct {
		TicketID int64 `json:"ticketId"`
	}
	if err := json.Unmarshal(s.Payload, &p); err != nil || p.TicketID == 0 {
		return 0, fmt.Errorf("feedback: bad held payload %d", s.ID)
	}
	return p.TicketID, nil
}

// Routes wires the student, staff, admin and cron endpoints behind FEATURE_FEEDBACK.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	h("GET /api/feedback/recipients", auth.Need(recipients))
	h("POST /api/feedback/tickets", auth.Write(auth.Need(create)))
	h("GET /api/feedback/mine", auth.Need(mine))
	h("POST /api/feedback/lookup", auth.Write(lookup))
	h("POST /api/feedback/tickets/{id}/messages", auth.Write(studentMessage))
	h("POST /api/feedback/tickets/{id}/read", auth.Write(studentRead))
	h("GET /api/feedback/photos/{name}", auth.Need(photo))

	h("GET /api/staff/feedback", auth.Need(staffInbox))
	h("GET /api/staff/feedback/{id}", auth.Need(staffTicket))
	h("POST /api/staff/feedback/{id}/reply", auth.Write(auth.Need(staffReply)))
	h("POST /api/staff/feedback/{id}/status", auth.Write(auth.Need(staffStatus)))

	h("GET /api/admin/feedback/recipients", auth.NeedRole(adminRecipients, "admin"))
	h("POST /api/admin/feedback/recipients", auth.Write(auth.NeedRole(adminCreate, "admin")))
	h("PATCH /api/admin/feedback/recipients/{id}", auth.Write(auth.NeedRole(adminPatch, "admin")))
	h("PUT /api/admin/feedback/recipients/{id}/staff", auth.Write(auth.NeedRole(adminStaff, "admin")))

	h("GET /api/cron/feedback", cron)
}

// ---------- helpers ----------

func hourOf(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func pick(r *http.Request, m httpx.Msg) string {
	if httpx.Lang(r) == "ru" {
		return m.RU
	}
	return m.KK
}

// NewToken returns a fresh ticket token (base64url of 32 random bytes) and its stored hash.
func NewToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	return tok, TokenHash(tok), nil
}

// TokenHash is SHA-256 of the token: the token has 256 bits of entropy, so no slow hash is needed.
func TokenHash(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

var reToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func removePhoto(name string) error {
	if !photoRe.MatchString(name) {
		return nil
	}
	if err := os.Remove(filepath.Join(PhotoDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func savePhoto(data []byte) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%x.jpg", b)
	if err := os.MkdirAll(PhotoDir(), 0o750); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(PhotoDir(), ".up-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(PhotoDir(), name)); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return name, nil
}

// ---------- views ----------

type Recipient struct {
	ID         int64         `json:"id"`
	University string        `json:"university,omitempty"`
	Key        string        `json:"key"`
	NameKK     string        `json:"nameKk"`
	NameRU     string        `json:"nameRu"`
	SLAHours   int           `json:"slaHours"`
	Active     bool          `json:"active"`
	Staff      []StaffMember `json:"staff,omitempty"`
}

type StaffMember struct {
	UserID int64   `json:"userId"`
	Name   *string `json:"name"`
	Role   string  `json:"role"`
}

type Message struct {
	FromStaff bool      `json:"fromStaff"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// Ticket is what both sides see. Author is filled only for signed tickets and only for staff.
type Ticket struct {
	ID        int64        `json:"id"`
	Recipient Recipient    `json:"recipient"`
	Category  string       `json:"category"`
	Body      string       `json:"body"`
	Photo     string       `json:"photo,omitempty"` // staff only: URL
	HasPhoto  bool         `json:"hasPhoto"`
	Anonymous bool         `json:"anonymous"`
	Status    string       `json:"status"`
	Flag      *string      `json:"flag,omitempty"`
	Unread    bool         `json:"unread"`
	SLADueAt  time.Time    `json:"slaDueAt"`
	Overdue   bool         `json:"overdue"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
	Author    *StaffMember `json:"author,omitempty"`
	Messages  []Message    `json:"messages"`
	photo     *string
	authorID  *int64
}

const ticketCols = `t.id, t.recipient_id, r.key, r.name_kk, r.name_ru, r.sla_hours, r.active, t.category, t.body, t.photo, t.anonymous, t.status,
	t.flag, t.student_unread, t.sla_due_at, t.created_at, t.updated_at, t.author_id`

func scanTicket(row pgx.Row) (*Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.Recipient.ID, &t.Recipient.Key, &t.Recipient.NameKK, &t.Recipient.NameRU, &t.Recipient.SLAHours, &t.Recipient.Active,
		&t.Category, &t.Body, &t.photo, &t.Anonymous, &t.Status, &t.Flag, &t.Unread, &t.SLADueAt, &t.CreatedAt, &t.UpdatedAt, &t.authorID)
	if err != nil {
		return nil, err
	}
	t.HasPhoto = t.photo != nil
	t.Overdue = (t.Status == "new" || t.Status == "in_progress") && Now().After(t.SLADueAt)
	t.Messages = []Message{}
	return &t, nil
}

func loadMessages(ctx context.Context, tx pgx.Tx, ts ...*Ticket) error {
	for _, t := range ts {
		rows, err := tx.Query(ctx, `select from_staff, body, created_at from feedback_messages where ticket_id = $1 order by id`, t.ID)
		if err != nil {
			return err
		}
		t.Messages, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Message, error) {
			var m Message
			return m, row.Scan(&m.FromStaff, &m.Body, &m.CreatedAt)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// forStudent: a held ticket reads «review», nothing staff-internal leaks.
func forStudent(t *Ticket) *Ticket {
	if t.Status == "held" {
		t.Status = "review"
	}
	t.Flag, t.Overdue = nil, false
	return t
}

func student(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return false
	}
	if u.Role != "student" && u.Role != "ambassador" {
		httpx.Error(w, r, http.StatusForbidden, "students_only", MsgStudents)
		return false
	}
	return true
}

// ---------- student ----------

func recipients(w http.ResponseWriter, r *http.Request, u *auth.User) {
	out := []Recipient{}
	univ := ""
	if u.University != nil {
		univ = *u.University
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select r.id, r.key, r.name_kk, r.name_ru, r.sla_hours from feedback_recipients r
			where r.university = $1 and r.active and exists (select 1 from feedback_recipient_staff s where s.recipient_id = r.id)
			order by r.id`, univ)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Recipient, error) {
			x := Recipient{Active: true}
			return x, row.Scan(&x.ID, &x.Key, &x.NameKK, &x.NameRU, &x.SLAHours)
		})
		return err
	})
	if err != nil {
		univoice.Internal(w, r, "recipients", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recipients": out, "categories": Categories, "university": univ})
}

type createInput struct {
	RecipientID int64  `json:"recipientId"`
	Category    string `json:"category"`
	Body        string `json:"body"`
	Anonymous   bool   `json:"anonymous"`
	Ack         bool   `json:"ack"` // the student saw the escalation notice
}

// readCreate accepts JSON, or multipart/form-data with a "data" JSON part and an optional "photo".
func readCreate(w http.ResponseWriter, r *http.Request) (createInput, []byte, error) {
	var in createInput
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" {
		return in, nil, httpx.Decode(r, &in, 32<<10)
	}
	if params["boundary"] == "" {
		return in, nil, errors.New("no boundary")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		return in, nil, err
	}
	var photo []byte
	gotData := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return in, nil, err
		}
		switch part.FormName() {
		case "data":
			if err := json.NewDecoder(io.LimitReader(part, 32<<10)).Decode(&in); err != nil {
				return in, nil, err
			}
			gotData = true
		case "photo":
			if photo, err = io.ReadAll(io.LimitReader(part, 1<<20)); err != nil {
				return in, nil, err
			}
		}
		part.Close()
	}
	if !gotData {
		return in, nil, errors.New("no data")
	}
	return in, photo, nil
}

func create(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !student(w, r, u) {
		return
	}
	in, photoData, err := readCreate(w, r)
	if err != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	body, ok := univoice.Clean(in.Body, 10, 3000, true)
	valid := false
	for _, c := range Categories {
		valid = valid || c == in.Category
	}
	if !ok || !valid || in.RecipientID <= 0 {
		univoice.Bad(w, r, "invalid", httpx.Msg{KK: "Санатты таңдап, мәтінді жазыңыз (10–3000 таңба).", RU: "Выберите категорию и напишите текст (10–3000 символов)."})
		return
	}
	flag := Detect(body)
	if flag == FlagThreat && !in.Ack {
		httpx.Error(w, r, http.StatusConflict, "escalation_notice", MsgEscalate)
		return
	}
	univ := ""
	if u.University != nil {
		univ = *u.University
	}
	var rec Recipient
	err = db.Tx(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `select r.id, r.name_kk, r.name_ru, r.sla_hours from feedback_recipients r
			where r.id = $1 and r.university = $2 and r.active and exists (select 1 from feedback_recipient_staff s where s.recipient_id = r.id)`,
			in.RecipientID, univ).Scan(&rec.ID, &rec.NameKK, &rec.NameRU, &rec.SLAHours)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "no_recipient", MsgRecipient)
		return
	}
	if err != nil {
		univoice.Internal(w, r, "create recipient", err)
		return
	}
	// abuse limit per account (the rate_limits counter expires with its window)
	if okRate, retry := ratelimit.Allow(r.Context(), fmt.Sprintf("fb:t:%d", u.ID), config.Int("FEEDBACK_PER_DAY", 5), 24*time.Hour); !okRate {
		w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "feedback_limit", MsgLimit)
		return
	}
	var photoName *string
	if len(photoData) > 0 {
		jpg, _, _, err := market.Process(r.Context(), photoData) // re-encode: EXIF/GPS and all metadata dropped
		if err != nil {
			univoice.Bad(w, r, "photo", MsgPhoto)
			return
		}
		name, err := savePhoto(jpg)
		if err != nil {
			univoice.Internal(w, r, "photo save", err)
			return
		}
		photoName = &name
	}
	now := Now()
	created := now
	var tok string
	var tokHash []byte
	var author *int64
	if in.Anonymous {
		created = hourOf(now)
		if tok, tokHash, err = NewToken(); err != nil {
			univoice.Internal(w, r, "token", err)
			return
		}
	} else {
		author = &u.ID
	}
	status := "new"
	var flagCol *string
	if flag != "" {
		status, flagCol = "held", &flag
	}
	var id int64
	err = db.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `insert into feedback_tickets (recipient_id, category, body, photo, anonymous, author_id, token_hash, status, flag,
			sla_due_at, created_at, updated_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11) returning id`,
			rec.ID, in.Category, body, photoName, in.Anonymous, author, tokHash, status, flagCol,
			now.Add(time.Duration(rec.SLAHours)*time.Hour), created).Scan(&id); err != nil {
			return err
		}
		if flag != "" {
			// the moderation item never names the sender, whatever the mode
			_, err := moderation.Submit(r.Context(), tx, Kind, map[string]any{
				"ticketId": id, "recipient": rec.NameKK + " / " + rec.NameRU, "category": in.Category, "flag": flag,
				"text": body, "hasPhoto": photoName != nil,
			}, nil, univ)
			return err
		}
		return nil
	})
	if err != nil {
		if photoName != nil {
			_ = removePhoto(*photoName)
		}
		univoice.Internal(w, r, "create ticket", err)
		return
	}
	bg := context.WithoutCancel(r.Context())
	switch flag {
	case FlagThreat:
		escalate(bg, id, rec)
	case "":
		tellStaff(bg, rec.ID, "new")
	}
	out := map[string]any{"id": id, "status": status, "held": flag != "", "flag": flag}
	if status == "held" {
		out["status"] = "review"
	}
	if in.Anonymous {
		out["token"] = tok
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func mine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	out := []*Ticket{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select `+ticketCols+` from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id
			where t.author_id = $1 order by t.updated_at desc limit 100`, u.ID)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Ticket, error) { return scanTicket(row) }); err != nil {
			return err
		}
		return loadMessages(r.Context(), tx, out...)
	})
	if err != nil {
		univoice.Internal(w, r, "mine", err)
		return
	}
	for _, t := range out {
		forStudent(t)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// lookup: POST {tokens:[...]} → the anonymous tickets those device tokens open. No sign-in needed:
// the token is the credential. Unknown tokens are simply absent from the answer.
func lookup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tokens []string `json:"tokens"`
	}
	if err := httpx.Decode(r, &in, 8<<10); err != nil || len(in.Tokens) > 20 {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	type found struct {
		Token  string  `json:"token"`
		Ticket *Ticket `json:"ticket"`
	}
	out := []found{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		for _, tok := range in.Tokens {
			if !reToken.MatchString(tok) {
				continue
			}
			t, err := scanTicket(tx.QueryRow(r.Context(), `select `+ticketCols+` from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id
				where t.token_hash = $1`, TokenHash(tok)))
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if err := loadMessages(r.Context(), tx, t); err != nil {
				return err
			}
			out = append(out, found{Token: tok, Ticket: forStudent(t)})
		}
		return nil
	})
	if err != nil {
		univoice.Internal(w, r, "lookup", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// studentTicket finds the ticket the caller may act on: by device token (anonymous) or as its
// signed author.
func studentTicket(ctx context.Context, tx pgx.Tx, r *http.Request, id int64, tok string) (*Ticket, error) {
	if tok != "" {
		if !reToken.MatchString(tok) {
			return nil, pgx.ErrNoRows
		}
		return scanTicket(tx.QueryRow(ctx, `select `+ticketCols+` from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id
			where t.id = $1 and t.token_hash = $2 for update of t`, id, TokenHash(tok)))
	}
	u := auth.Current(r)
	if u == nil {
		return nil, pgx.ErrNoRows
	}
	return scanTicket(tx.QueryRow(ctx, `select `+ticketCols+` from feedback_tickets t join feedback_recipients r on r.id = t.recipient_id
		where t.id = $1 and t.author_id = $2 and not t.anonymous for update of t`, id, u.ID))
}

func studentMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := univoice.PathID(r, "id")
	var in struct {
		Token string `json:"token"`
		Body  string `json:"body"`
	}
	if !ok || httpx.Decode(r, &in, 16<<10) != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	body, ok := univoice.Clean(in.Body, 1, 3000, true)
	if !ok {
		univoice.Bad(w, r, "invalid", httpx.MsgBad)
		return
	}
	var rec int64
	var closed bool
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		t, err := studentTicket(r.Context(), tx, r, id, in.Token)
		if err != nil {
			return err
		}
		if t.Status == "closed" || t.Status == "held" {
			closed = true
			return nil
		}
		at := Now()
		if t.Anonymous {
			at = hourOf(at)
		}
		if _, err := tx.Exec(r.Context(), `insert into feedback_messages (ticket_id, from_staff, body, created_at) values ($1, false, $2, $3)`, t.ID, body, at); err != nil {
			return err
		}
		// the ball is back with staff: a new SLA window
		_, err = tx.Exec(r.Context(), `update feedback_tickets set status = case when status = 'answered' then 'in_progress' else status end,
			sla_due_at = $2, sla_reminded_at = null, updated_at = $3 where id = $1`,
			t.ID, Now().Add(time.Duration(t.Recipient.SLAHours)*time.Hour), at)
		rec = t.Recipient.ID
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgToken)
	case err != nil:
		univoice.Internal(w, r, "student message", err)
	case closed:
		httpx.Error(w, r, http.StatusConflict, "closed", MsgClosed)
	default:
		tellStaff(context.WithoutCancel(r.Context()), rec, "message")
		httpx.JSON(w, http.StatusCreated, map[string]any{"ok": true})
	}
}

func studentRead(w http.ResponseWriter, r *http.Request) {
	id, ok := univoice.PathID(r, "id")
	var in struct {
		Token string `json:"token"`
	}
	if !ok || httpx.Decode(r, &in, 4<<10) != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		t, err := studentTicket(r.Context(), tx, r, id, in.Token)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `update feedback_tickets set student_unread = false where id = $1`, t.ID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgToken)
		return
	}
	if err != nil {
		univoice.Internal(w, r, "student read", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- notifications (never name an anonymous sender: the server does not know it) ----------

func staffOf(ctx context.Context, recipientID int64) ([]int64, string, error) {
	var ids []int64
	var name string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select name_kk from feedback_recipients where id = $1`, recipientID).Scan(&name); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select s.user_id from feedback_recipient_staff s join users u on u.id = s.user_id
			where s.recipient_id = $1 and u.deleted_at is null and u.role in ('university_staff', 'admin')`, recipientID)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	return ids, name, err
}

func tellStaff(ctx context.Context, recipientID int64, what string) {
	ids, name, err := staffOf(ctx, recipientID)
	if err != nil {
		log.Printf("feedback: staff of %d: %v", recipientID, err)
		return
	}
	text := "📨 «" + html.EscapeString(name) + "»: жаңа өтініш / новое обращение"
	if what == "message" {
		text = "💬 «" + html.EscapeString(name) + "»: өтінішке жаңа хабарлама / новое сообщение в обращении"
	}
	ctx = notify.WithLink(ctx, "/voice")
	for _, id := range ids {
		if err := NotifyStaff(ctx, id, text); err != nil {
			log.Printf("feedback: notify staff %d: %v", id, err)
		}
	}
}

// escalate tells every admin about a threat/emergency ticket immediately.
func escalate(ctx context.Context, ticketID int64, rec Recipient) {
	var ids []int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id from users where role = 'admin' and deleted_at is null`)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	if err != nil {
		log.Printf("feedback: escalate admins: %v", err)
		return
	}
	text := fmt.Sprintf("🚨 Қауіп/төтенше белгісі бар өтініш / Обращение с признаками угрозы: #%d («%s»). Тексеріңіз / Проверьте: %s/admin/queue",
		ticketID, html.EscapeString(rec.NameKK), config.SiteURL())
	ctx = notify.WithLink(ctx, "/admin/queue")
	for _, id := range ids {
		if err := NotifyStaff(ctx, id, text); err != nil {
			log.Printf("feedback: escalate %d: %v", id, err)
		}
	}
}
