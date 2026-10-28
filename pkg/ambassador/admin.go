package ambassador

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// RenewAskDays: the "continue next semester?" question appears this many days before term_end.
const RenewAskDays = 14

type adminTerm struct {
	Term
	Name     *string `json:"name"`
	Username *string `json:"username"`
	Joined   int     `json:"joined"` // sign-ups via the ref link
}

func adminList(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	univ := r.URL.Query().Get("university")
	out := []adminTerm{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select t.id, t.user_id, t.university, t.role_in_team, t.term_start, t.term_end, t.hours_per_week,
				t.ref_code, t.status, t.paused_until, t.renew_answer, t.cert_code, t.cert_issued_at, u.name, u.tg_username,
				(select count(*) from users x where x.referred_by = 'amb_' || t.ref_code and x.deleted_at is null and x.id <> t.user_id)
			from ambassador_terms t join users u on u.id = t.user_id
			where ($1 = 'all' or t.status = $1) and ($2 = '' or t.university = $2)
			order by t.university, t.role_in_team = 'lead' desc, t.term_start limit 500`, status, univ)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (adminTerm, error) {
			var a adminTerm
			t := &a.Term
			return a, row.Scan(&t.ID, &t.UserID, &t.University, &t.RoleInTeam, &t.TermStart, &t.TermEnd, &t.HoursPerWeek,
				&t.RefCode, &t.Status, &t.PausedUntil, &t.RenewAnswer, &t.CertCode, &t.CertIssuedAt, &a.Name, &a.Username, &a.Joined)
		})
		return err
	})
	if err != nil {
		fail(w, r, "admin list", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"terms": out})
}

func termID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, r, http.StatusNotFound, "term_not_found", MsgNoTerm)
		return 0, false
	}
	return id, true
}

// adminPatch: term_end (YYYY-MM-DD), role_in_team, hours_per_week.
func adminPatch(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	id, ok := termID(w, r)
	if !ok {
		return
	}
	var body struct {
		TermEnd      *string `json:"term_end"`
		RoleInTeam   *string `json:"role_in_team"`
		HoursPerWeek *int    `json:"hours_per_week"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var end *time.Time
	if body.TermEnd != nil {
		t, err := time.Parse("2006-01-02", *body.TermEnd)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		end = &t
	}
	if (body.RoleInTeam != nil && *body.RoleInTeam != "lead" && *body.RoleInTeam != "member") ||
		(body.HoursPerWeek != nil && (*body.HoursPerWeek < 1 || *body.HoursPerWeek > 10)) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var t *Term
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		t, err = scanTerm(tx.QueryRow(r.Context(), `update ambassador_terms set
				term_end = coalesce($2, term_end), role_in_team = coalesce($3, role_in_team), hours_per_week = coalesce($4, hours_per_week)
			where id = $1 and status = 'active' returning `+termCols, id, end, body.RoleInTeam, body.HoursPerWeek))
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, target_id, meta)
			values ($1, 'ambassador_term_update', $2, jsonb_build_object('term', $3::bigint))`, admin.ID, t.UserID, id)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "term_not_found", MsgNoTerm)
	case isUnique(err):
		httpx.Error(w, r, http.StatusConflict, "lead_taken", MsgTeamFull)
	case err != nil:
		fail(w, r, "admin patch", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"term": t})
	}
}

// endTerm closes one term; the role goes back to student unless another active term remains.
// The ambassador's content stays where it is.
func endTerm(ctx context.Context, tx pgx.Tx, termID int64, actor *int64, reason string) (bool, error) {
	var userID int64
	err := tx.QueryRow(ctx, `update ambassador_terms set status = 'ended', ended_at = now()
		where id = $1 and status = 'active' returning user_id`, termID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `update users set role = 'student' where id = $1 and role = 'ambassador'
		and not exists (select 1 from ambassador_terms where user_id = $1 and status = 'active')`, userID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'role_revoke', $2, $3, jsonb_build_object('from', 'ambassador', 'to', 'student', 'term', $4::bigint))`,
			actor, userID, reason, termID)
	}
	return true, err
}

func adminEnd(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	id, ok := termID(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = httpx.Decode(r, &body, 2<<10)
	reason := strings.TrimSpace(body.Reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		httpx.Error(w, r, http.StatusBadRequest, "reason_required", MsgReason)
		return
	}
	var ended bool
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		ended, err = endTerm(r.Context(), tx, id, &admin.ID, reason)
		return err
	})
	if err != nil {
		fail(w, r, "admin end", err)
		return
	}
	if !ended {
		httpx.Error(w, r, http.StatusNotFound, "term_not_found", MsgNoTerm)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ended": true})
}

// ExpireTerms ends every active term whose term_end is before today (Almaty) and moves the
// people back to the student role. The daily cron calls it (GET /api/cron/ambassador).
func ExpireTerms(ctx context.Context, now time.Time) (int, error) {
	n := 0
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select id from ambassador_terms where status = 'active' and term_end < $1::date for update`, AlmatyToday(now))
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, id := range ids {
			ok, err := endTerm(ctx, tx, id, nil, "ambassador term ended")
			if err != nil {
				return err
			}
			if ok {
				n++
			}
		}
		return nil
	})
	return n, err
}

func adminExpire(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	n, err := ExpireTerms(r.Context(), Now())
	if err != nil {
		fail(w, r, "expire", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ended": n})
}

// DailyStats is what the ambassador daily job did.
type DailyStats struct {
	RenewAsks int `json:"renew_asks"`
	Ended     int `json:"ended"`
}

// Daily asks "continue next semester?" RenewAskDays before term_end (once, direct message) and
// expires finished terms.
func Daily(ctx context.Context, now time.Time) (DailyStats, error) {
	var st DailyStats
	type ask struct {
		id, user int64
		lang     string
		end      time.Time
	}
	var asks []ask
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `update ambassador_terms t set renew_asked_at = now() from users u
			where u.id = t.user_id and t.status = 'active' and t.renew_asked_at is null and t.renew_answer is null
				and t.term_end >= $1::date and t.term_end <= $1::date + $2::int
			returning t.id, t.user_id, u.lang, t.term_end`, AlmatyToday(now), RenewAskDays)
		if err != nil {
			return err
		}
		asks, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ask, error) {
			var a ask
			return a, row.Scan(&a.id, &a.user, &a.lang, &a.end)
		})
		return err
	})
	if err != nil {
		return st, err
	}
	link := config.SiteURL() + "/ambassador"
	for _, a := range asks {
		text := "Амбассадорлық мерзіміңіз " + a.end.Format("02.01.2006") + " аяқталады. Келесі семестрде жалғастырасыз ба? " + link
		if a.lang == "ru" {
			text = "Ваш срок амбассадора заканчивается " + a.end.Format("02.01.2006") + ". Продолжите в следующем семестре? " + link
		}
		if err := Notify(ctx, a.user, text); err != nil {
			log.Printf("ambassador: renew ask %d: %v", a.user, err)
			continue
		}
		st.RenewAsks++
	}
	st.Ended, err = ExpireTerms(ctx, now)
	return st, err
}

func cronHandler(w http.ResponseWriter, r *http.Request) {
	secret := config.Str("CRON_SECRET", "")
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if secret == "" || !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) != 1 {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
		return
	}
	st, err := Daily(r.Context(), Now())
	if err != nil {
		fail(w, r, "cron", err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

// ---------- certificates ----------

func adminCertificate(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	id, ok := termID(w, r)
	if !ok {
		return
	}
	var code string
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `update ambassador_terms set cert_code = coalesce(cert_code, $2),
				cert_issued_at = coalesce(cert_issued_at, now()), cert_issued_by = coalesce(cert_issued_by, $3)
			where id = $1 returning cert_code`, id, randomCode(16), admin.ID).Scan(&code)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "term_not_found", MsgNoTerm)
		return
	}
	if err != nil {
		fail(w, r, "certificate", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cert_code": code, "url": CertificateURL(code)})
}

// CertificateURL is the printable page and the verification link.
func CertificateURL(code string) string { return config.SiteURL() + "/certificate/" + code }

// certificate: public data for #/certificate/<code> (exists only once an admin issued it).
func certificate(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if len(code) < 8 || len(code) > 40 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoCert)
		return
	}
	var out struct {
		Name                 *string   `json:"name"`
		University           string    `json:"university"`
		RoleInTeam           string    `json:"role_in_team"`
		TermStart            time.Time `json:"term_start"`
		TermEnd              time.Time `json:"term_end"`
		IssuedAt             time.Time `json:"issued_at"`
		EventsOrganized      int       `json:"events_organized"`
		PublishedSuggestions int       `json:"published_suggestions"`
		VerifyURL            string    `json:"verify_url"`
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `select u.name, t.university, t.role_in_team, t.term_start, t.term_end, t.cert_issued_at,
				(select count(*) from events e where e.created_by = t.user_id and e.university = t.university
					and e.status in ('published', 'finished') and e.created_at::date between t.term_start and t.term_end),
				(select count(*) from community_content c where c.author_id = t.user_id
					and c.published_at::date between t.term_start and t.term_end + 30)
			from ambassador_terms t join users u on u.id = t.user_id and u.deleted_at is null
			where t.cert_code = $1 and t.cert_issued_at is not null`, code).
			Scan(&out.Name, &out.University, &out.RoleInTeam, &out.TermStart, &out.TermEnd, &out.IssuedAt,
				&out.EventsOrganized, &out.PublishedSuggestions)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoCert)
		return
	}
	if err != nil {
		fail(w, r, "certificate", err)
		return
	}
	out.VerifyURL = CertificateURL(code)
	httpx.JSON(w, http.StatusOK, out)
}
