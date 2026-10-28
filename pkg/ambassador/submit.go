package ambassador

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

// PlaceCats are the "nearby" place categories.
var PlaceCats = []string{"canteen", "cafe", "print", "copy", "library", "shop", "pharmacy", "sport", "cowork", "other"}

// ContentIDPrefix marks database records in /api/content ("amb-<community_content.id>").
const ContentIDPrefix = "amb-"

var reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var reBuilding = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

type fieldErr string

func (e fieldErr) Error() string { return string(e) }

func text(m map[string]any, key string, min, max int) (string, error) {
	v, ok := m[key]
	if !ok || v == nil {
		if min > 0 {
			return "", fieldErr(key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fieldErr(key)
	}
	s = strings.TrimSpace(s)
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 {
		return "", fieldErr(key)
	}
	if n := utf8.RuneCountInString(s); n > max || (n < min) {
		return "", fieldErr(key)
	}
	return s, nil
}

func flag(m map[string]any, key string) (bool, error) {
	v, ok := m[key]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fieldErr(key)
	}
	return b, nil
}

func httpsURL(m map[string]any, key string, required bool) (string, error) {
	min := 0
	if required {
		min = 9
	}
	s, err := text(m, key, min, 500)
	if err != nil || s == "" {
		return s, err
	}
	if !strings.HasPrefix(strings.ToLower(s), "https://") || strings.ContainsAny(s, " <>\"") {
		return "", fieldErr(key)
	}
	return s, nil
}

// CleanPayload validates a suggestion and returns the normalised payload (only known fields).
func CleanPayload(kind string, in map[string]any, today string) (map[string]any, error) {
	out := map[string]any{}
	set := func(k string, v any, err error) error {
		if err != nil {
			return err
		}
		out[k] = v
		return nil
	}
	var err error
	switch kind {
	case KindOpportunity:
		t, e := text(in, "type", 1, 20)
		if e != nil || !slices.Contains(content.OppTypes, t) {
			return nil, fieldErr("type")
		}
		out["type"] = t
		for _, f := range []struct {
			k        string
			min, max int
		}{{"title_kk", 3, 140}, {"title_ru", 0, 140}, {"org_kk", 2, 140}, {"org_ru", 0, 140}, {"desc_kk", 10, 2000}, {"desc_ru", 0, 2000}} {
			v, e := text(in, f.k, f.min, f.max)
			if err = set(f.k, v, e); err != nil {
				return nil, err
			}
		}
		src, e := httpsURL(in, "source_url", true) // checklist: an official source link
		if err = set("source_url", src, e); err != nil {
			return nil, err
		}
		city, e := text(in, "city", 0, 20)
		if e != nil || (city != "" && !slices.Contains(content.Cities, city)) {
			return nil, fieldErr("city")
		}
		if city == "" {
			city = "other"
		}
		out["city"] = city
		d, e := text(in, "deadline", 0, 10)
		if e != nil || (d != "" && (!reDate.MatchString(d) || !content.IsValidDate(d) || d < today)) {
			return nil, fieldErr("deadline")
		}
		out["deadline"] = d
		for _, k := range []string{"funded", "free"} {
			b, e := flag(in, k)
			if err = set(k, b, e); err != nil {
				return nil, err
			}
		}
	case KindPlace:
		c, e := text(in, "cat", 1, 20)
		if e != nil || !slices.Contains(PlaceCats, c) {
			return nil, fieldErr("cat")
		}
		out["cat"] = c
		for _, f := range []struct {
			k        string
			min, max int
		}{{"name_kk", 2, 120}, {"name_ru", 0, 120}, {"address", 3, 200}, {"desc_kk", 0, 1000}, {"desc_ru", 0, 1000}, {"hours", 0, 100}} {
			v, e := text(in, f.k, f.min, f.max)
			if err = set(f.k, v, e); err != nil {
				return nil, err
			}
		}
		b, e := text(in, "building_id", 0, 32)
		if e != nil || (b != "" && !reBuilding.MatchString(b)) {
			return nil, fieldErr("building_id")
		}
		out["building_id"] = b
		src, e := httpsURL(in, "source_url", false)
		if err = set("source_url", src, e); err != nil {
			return nil, err
		}
	default:
		return nil, fieldErr("kind")
	}
	return out, nil
}

// submit: POST /api/submissions — an opportunity or a place for the admin queue. Students and
// ambassadors alike; nothing is published without approval. An ambassador's suggestion is
// always for their own university.
func submit(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body struct {
		Kind       string         `json:"kind"`
		University *string        `json:"university"`
		Payload    map[string]any `json:"payload"`
	}
	if err := httpx.Decode(r, &body, 16<<10); err != nil || body.Payload == nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_submission", MsgSubmit)
		return
	}
	payload, err := CleanPayload(body.Kind, body.Payload, AlmatyToday(Now()).Format("2006-01-02"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_"+err.Error(), MsgSubmit)
		return
	}
	univ := ""
	if body.University != nil {
		univ = strings.TrimSpace(*body.University)
	}
	var term *Term
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		term, err = ActiveTerm(ctx, tx, u.ID, Now())
		return err
	}); err != nil {
		fail(w, r, "submit", err)
		return
	}
	authorRole := "student"
	switch {
	case term != nil:
		if univ != "" && univ != term.University {
			httpx.Error(w, r, http.StatusForbidden, "foreign_university", MsgForeign)
			return
		}
		univ, authorRole = term.University, "ambassador"
	case univ == "" && u.University != nil:
		univ = *u.University
	}
	if utf8.RuneCountInString(univ) > 120 {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_submission", MsgSubmit)
		return
	}
	payload["author_role"] = authorRole
	var id int64
	// as app_user: the submissions_own_insert policy (own row, pending)
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		id, err = moderation.Submit(ctx, tx, body.Kind, payload, &u.ID, univ)
		return err
	})
	if err != nil {
		fail(w, r, "submit", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "status": "pending"})
}

type mySubmission struct {
	ID         int64           `json:"id"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"` // pending | approved | rejected
	Reason     *string         `json:"reason"`
	University *string         `json:"university"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
	ReviewedAt *time.Time      `json:"reviewed_at"`
}

// mySubmissions: "Менің ұсыныстарым" — every kind, with the return reason.
func mySubmissions(w http.ResponseWriter, r *http.Request, u *auth.User) {
	out := []mySubmission{}
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, kind, status::text, reason, university, payload, created_at, reviewed_at
			from submissions where submitted_by = app_user_id() order by created_at desc limit 200`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (mySubmission, error) {
			var s mySubmission
			return s, row.Scan(&s.ID, &s.Kind, &s.Status, &s.Reason, &s.University, &s.Payload, &s.CreatedAt, &s.ReviewedAt)
		})
		return err
	})
	if err != nil {
		fail(w, r, "my submissions", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"submissions": out})
}

// publish copies an approved opportunity / place into community_content (kept when the
// author's term ends or the account is deleted).
func publish(kind string) moderation.Hook {
	return func(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
		var p map[string]any
		if err := json.Unmarshal(s.Payload, &p); err != nil {
			return err
		}
		source := "ambassador"
		if role, _ := p["author_role"].(string); role == "student" {
			source = "student"
		}
		var deadline *string
		if d, _ := p["deadline"].(string); d != "" {
			deadline = &d
		}
		_, err := tx.Exec(ctx, `insert into community_content (kind, payload, university, source, author_id, submission_id, deadline)
			values ($1, $2, $3, $4, $5, $6, $7::date) on conflict (submission_id) do nothing`,
			kind, s.Payload, s.University, source, s.SubmittedBy, s.ID, deadline)
		return err
	}
}

// ---------- /api/content extension ----------

func str(p map[string]any, k string) string { s, _ := p[k].(string); return s }

func bi(p map[string]any, name string) content.Bi {
	return content.Bi{KK: str(p, name+"_kk"), RU: str(p, name+"_ru")}
}

// record shapes a community_content row like the Sheets records, plus source / university.
func record(id int64, kind string, payload json.RawMessage, univ *string, published time.Time) (content.Record, error) {
	var p map[string]any
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	rid := ContentIDPrefix + strconv.FormatInt(id, 10)
	posted := published.UTC().Add(5 * time.Hour).Format("2006-01-02")
	var v any
	if kind == KindOpportunity {
		var deadline *string
		if d := str(p, "deadline"); d != "" {
			deadline = &d
		}
		funded, _ := p["funded"].(bool)
		free, _ := p["free"].(bool)
		v = struct {
			content.Opportunity
			Source     string  `json:"source"`
			University *string `json:"university"`
			Free       bool    `json:"free"`
		}{content.Opportunity{
			ID: rid, Type: str(p, "type"), Funded: funded, Title: bi(p, "title"), Org: bi(p, "org"),
			Spec: []string{}, Desc: bi(p, "desc"), Docs: []content.Bi{}, Steps: []content.Bi{},
			SourceURL: str(p, "source_url"), PostedAt: &posted, Deadline: deadline, City: str(p, "city"),
		}, "ambassador", univ, free}
	} else {
		v = map[string]any{
			"id": rid, "cat": str(p, "cat"), "name": bi(p, "name"), "desc": bi(p, "desc"),
			"address": str(p, "address"), "building_id": str(p, "building_id"), "hours": str(p, "hours"),
			"source_url": str(p, "source_url"), "posted_at": posted, "university": univ, "source": "ambassador",
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return content.RawRecord(raw, str(p, "deadline")), nil
}

// Published returns the visible community records per /api/content list.
func Published(ctx context.Context) (map[string][]content.Record, error) {
	out := map[string][]content.Record{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id, kind, payload, university, published_at from community_content
			where not hidden order by published_at desc limit 500`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var kind string
			var payload json.RawMessage
			var univ *string
			var at time.Time
			if err := rows.Scan(&id, &kind, &payload, &univ, &at); err != nil {
				return err
			}
			rec, err := record(id, kind, payload, univ, at)
			if err != nil {
				continue
			}
			list := "opportunities"
			if kind == KindPlace {
				list = "places"
			}
			out[list] = append(out[list], rec)
		}
		return rows.Err()
	})
	return out, err
}

func registerContent() {
	content.RegisterExtra("ambassador", func(ctx context.Context) (map[string][]content.Record, error) {
		if !features.Enabled("ambassador") {
			return nil, nil
		}
		if _, err := db.Pool(ctx); errors.Is(err, db.ErrNoDB) {
			return nil, nil
		}
		return Published(ctx)
	})
}

// reportTarget: reports on "amb-<id>" records hide / show the community_content row.
func reportTarget(kind string) moderation.ReportTarget {
	parse := func(id string) (int64, bool) {
		n, err := strconv.ParseInt(strings.TrimPrefix(id, ContentIDPrefix), 10, 64)
		return n, err == nil && strings.HasPrefix(id, ContentIDPrefix) && n > 0
	}
	return moderation.ReportTarget{
		Exists: func(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
			n, ok := parse(id)
			if !ok {
				return false, nil
			}
			var c int
			err := tx.QueryRow(ctx, `select count(*) from community_content where id = $1 and kind = $2`, n, kind).Scan(&c)
			return c == 1, err
		},
		SetHidden: func(ctx context.Context, tx pgx.Tx, id string, hidden bool) error {
			n, ok := parse(id)
			if !ok {
				return pgx.ErrNoRows
			}
			_, err := tx.Exec(ctx, `update community_content set hidden = $3 where id = $1 and kind = $2`, n, kind, hidden)
			return err
		},
	}
}

func registerExports() {
	me.RegisterExport("ambassadorApplications", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select id, university, motivation, hours_per_week, status, reason, created_at, reviewed_at
			from ambassador_applications where user_id = app_user_id() order by id`)
	})
	me.RegisterExport("ambassadorTerms", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select id, university, role_in_team, term_start, term_end, hours_per_week, ref_code, status,
			paused_until, renew_answer, cert_code, cert_issued_at from ambassador_terms where user_id = app_user_id() order by id`)
	})
}
