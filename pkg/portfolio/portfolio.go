// Package portfolio is the public portfolio (prompt 09): the student's settings for a public
// page at /p/<slug>, the self-declared items they chose to sync, and the public JSON the
// Next.js page /p/<slug> is server-rendered from.
//
// Honesty: "verified" items come only from system tables (verified_attendance,
// event_certificates, ambassador certificates, verified user_badges). Self-declared items live
// in portfolio_items, which has no verified flag at all.
//
// Everything is behind the "portfolio" feature switch. API reference: docs/api/portfolio.md.
package portfolio

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
)

// Feature is the switch name (FEATURE_PORTFOLIO).
const Feature = "portfolio"

// Limits.
const (
	MaxItems    = 60
	slugLen     = 10
	slugChars   = "abcdefghijkmnpqrstuvwxyz23456789"
	publicLimit = 60 // GET /api/p/{slug} per IP per minute
)

var (
	MsgConsent = httpx.Msg{KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."}
	MsgInvalid = httpx.Msg{KK: "Деректер толық емес не қате.", RU: "Данные неполные или неверные."}
	MsgSync    = httpx.Msg{KK: "Алдымен деректерді аккаунтқа сақтауға келісім беріңіз.", RU: "Сначала разрешите сохранить данные в аккаунте."}
	MsgNoStore = httpx.Msg{KK: "GPA, телефон және email жария портфолиода болмайды.", RU: "GPA, телефон и email не публикуются в портфолио."}
)

func init() {
	me.RegisterExport("portfolioSettings", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select enabled, slug, display_name, headline, bio, show_university, show_course, show_events,
			show_certs, show_experience, show_skills, show_badges, show_contact, sync_consent_at, slug_rotated_at, created_at, updated_at
			from portfolio_settings where user_id = app_user_id()`)
	})
	me.RegisterExport("portfolioItems", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select kind, title, org, period, descr, url, position, created_at
			from portfolio_items where user_id = app_user_id() order by kind, position, id`)
	})
	// portfolio_settings / portfolio_items cascade with the account: the page is gone at once.
}

// Routes wires /api/portfolio/* and the public /api/p/{slug}, all behind the switch.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	h("GET /api/portfolio/me", auth.Need(mine))
	h("PATCH /api/portfolio/settings", auth.Write(auth.Need(patchSettings)))
	h("POST /api/portfolio/slug", auth.Write(auth.Need(rotateSlug)))
	h("PUT /api/portfolio/items", auth.Write(auth.Need(putItems)))
	h("DELETE /api/portfolio/items", auth.Write(auth.Need(deleteItems)))
	h("GET /api/p/{slug}", public)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("portfolio: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

// NewSlug draws a random 10-character slug (unrelated to the username, not enumerable).
func NewSlug() string {
	b := make([]byte, slugLen)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(slugChars))))
		b[i] = slugChars[k.Int64()]
	}
	return string(b)
}

// ValidSlug reports whether s has the slug shape (checked before touching the database).
func ValidSlug(s string) bool {
	if len(s) != slugLen {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// URL is the public page address.
func URL(site, slug string) string { return site + "/p/" + slug }

// Settings is the owner's view of portfolio_settings.
type Settings struct {
	Enabled        bool       `json:"enabled"`
	Slug           string     `json:"slug"`
	URL            string     `json:"url"`
	DisplayName    *string    `json:"displayName"`
	Headline       *string    `json:"headline"`
	Bio            *string    `json:"bio"`
	ShowUniversity bool       `json:"showUniversity"`
	ShowCourse     bool       `json:"showCourse"`
	ShowEvents     bool       `json:"showEvents"`
	ShowCerts      bool       `json:"showCerts"`
	ShowExperience bool       `json:"showExperience"`
	ShowSkills     bool       `json:"showSkills"`
	ShowBadges     bool       `json:"showBadges"`
	ShowContact    bool       `json:"showContact"`
	SyncConsentAt  *time.Time `json:"syncConsentAt"`
	SlugRotatedAt  *time.Time `json:"slugRotatedAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

const settingsCols = `enabled, slug, display_name, headline, bio, show_university, show_course, show_events, show_certs,
	show_experience, show_skills, show_badges, show_contact, sync_consent_at, slug_rotated_at, updated_at`

func scanSettings(row pgx.Row) (*Settings, error) {
	var s Settings
	err := row.Scan(&s.Enabled, &s.Slug, &s.DisplayName, &s.Headline, &s.Bio, &s.ShowUniversity, &s.ShowCourse,
		&s.ShowEvents, &s.ShowCerts, &s.ShowExperience, &s.ShowSkills, &s.ShowBadges, &s.ShowContact,
		&s.SyncConsentAt, &s.SlugRotatedAt, &s.UpdatedAt)
	return &s, err
}

// ensure returns the caller's settings row, creating it (disabled, random slug) on first use.
// Runs as app_user.
func ensure(ctx context.Context, tx pgx.Tx) (*Settings, error) {
	s, err := scanSettings(tx.QueryRow(ctx, `select `+settingsCols+` from portfolio_settings where user_id = app_user_id()`))
	if !errors.Is(err, pgx.ErrNoRows) {
		return s, err
	}
	for range 5 {
		s, err = scanSettings(tx.QueryRow(ctx, `insert into portfolio_settings (user_id, slug) values (app_user_id(), $1)
			on conflict do nothing returning `+settingsCols, NewSlug()))
		if !errors.Is(err, pgx.ErrNoRows) {
			return s, err
		}
		// slug collision (or a concurrent insert of the same row): read again / retry
		s, err = scanSettings(tx.QueryRow(ctx, `select `+settingsCols+` from portfolio_settings where user_id = app_user_id()`))
		if !errors.Is(err, pgx.ErrNoRows) {
			return s, err
		}
	}
	return nil, errors.New("portfolio: could not draw a free slug")
}

// Item is a self-declared portfolio entry.
type Item struct {
	Kind   string `json:"kind"` // cert | experience | skill
	Title  string `json:"title"`
	Org    string `json:"org"`
	Period string `json:"period"`
	Desc   string `json:"desc"`
	URL    string `json:"url"`
}

func ownItems(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]Item, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) {
		var it Item
		return it, row.Scan(&it.Kind, &it.Title, &it.Org, &it.Period, &it.Desc, &it.URL)
	})
}

// mine: GET /api/portfolio/me — settings (created on first call), synced items and a preview of
// what the public page would show right now.
func mine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var s *Settings
	var items []Item
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		if s, err = ensure(ctx, tx); err != nil {
			return err
		}
		items, err = ownItems(ctx, tx, `select kind, title, org, period, descr, url from portfolio_items
			where user_id = app_user_id() order by kind, position, id`)
		return err
	})
	if err != nil {
		fail(w, r, "me", err)
		return
	}
	page, err := Build(ctx, u.ID, true)
	if err != nil {
		fail(w, r, "preview", err)
		return
	}
	s.URL = URL(siteURL(), s.Slug)
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": s, "items": items, "preview": page})
}

var showCols = map[string]string{
	"showUniversity": "show_university", "showCourse": "show_course", "showEvents": "show_events",
	"showCerts": "show_certs", "showExperience": "show_experience", "showSkills": "show_skills",
	"showBadges": "show_badges", "showContact": "show_contact",
}

var textCols = map[string]struct {
	col string
	max int
}{
	"displayName": {"display_name", 80}, "headline": {"headline", 80}, "bio": {"bio", 300},
}

// never on the public page, not even behind a toggle
var refused = map[string]bool{"gpa": true, "phone": true, "email": true, "showgpa": true, "showphone": true, "showemail": true}

func cleanText(raw any, max int, multiline bool) (*string, bool) {
	if raw == nil {
		return nil, true
	}
	s, ok := raw.(string)
	if !ok {
		return nil, false
	}
	s = strings.TrimSpace(s)
	if strings.IndexFunc(s, func(c rune) bool { return unicode.IsControl(c) && !(multiline && c == '\n') }) >= 0 {
		return nil, false
	}
	if utf8.RuneCountInString(s) > max {
		return nil, false
	}
	if s == "" {
		return nil, true
	}
	return &s, true
}

// patchSettings: PATCH /api/portfolio/settings — any subset of enabled, displayName, headline,
// bio, show* and syncConsent. Turning the sync consent off deletes the synced items.
func patchSettings(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body map[string]any
	if err := httpx.Decode(r, &body, 8<<10); err != nil || len(body) == 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var sets []string
	var args []any
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	dropItems := false
	for k, v := range body {
		if refused[strings.ToLower(k)] {
			httpx.Error(w, r, http.StatusBadRequest, "not_public", MsgNoStore)
			return
		}
		switch {
		case k == "enabled":
			b, ok := v.(bool)
			if !ok {
				httpx.Error(w, r, http.StatusBadRequest, "invalid_enabled", MsgInvalid)
				return
			}
			add("enabled", b)
		case k == "syncConsent":
			b, ok := v.(bool)
			if !ok {
				httpx.Error(w, r, http.StatusBadRequest, "invalid_syncConsent", MsgInvalid)
				return
			}
			if b {
				sets = append(sets, "sync_consent_at = coalesce(sync_consent_at, now())")
			} else {
				sets = append(sets, "sync_consent_at = null")
				dropItems = true
			}
		case showCols[k] != "":
			b, ok := v.(bool)
			if !ok {
				httpx.Error(w, r, http.StatusBadRequest, "invalid_"+k, MsgInvalid)
				return
			}
			add(showCols[k], b)
		case textCols[k].col != "":
			s, ok := cleanText(v, textCols[k].max, k == "bio")
			if !ok {
				httpx.Error(w, r, http.StatusBadRequest, "invalid_"+k, MsgInvalid)
				return
			}
			add(textCols[k].col, s)
		default:
			httpx.Error(w, r, http.StatusBadRequest, "unknown_field", httpx.MsgBad)
			return
		}
	}
	sets = append(sets, "updated_at = now()")
	var s *Settings
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if _, err := ensure(ctx, tx); err != nil {
			return err
		}
		if dropItems {
			if _, err := tx.Exec(ctx, `delete from portfolio_items where user_id = app_user_id()`); err != nil {
				return err
			}
		}
		var err error
		s, err = scanSettings(tx.QueryRow(ctx, "update portfolio_settings set "+strings.Join(sets, ", ")+
			" where user_id = app_user_id() returning "+settingsCols, args...))
		return err
	})
	if err != nil {
		fail(w, r, "settings", err)
		return
	}
	s.URL = URL(siteURL(), s.Slug)
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": s})
}

// rotateSlug: POST /api/portfolio/slug — "Жаңа сілтеме жасау". The old link stops working at once.
func rotateSlug(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var s *Settings
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if _, err := ensure(ctx, tx); err != nil {
			return err
		}
		for range 5 {
			var err error
			// a savepoint so a (very unlikely) slug collision can be retried in the same transaction
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			s, err = scanSettings(sp.QueryRow(ctx, `update portfolio_settings set slug = $1, slug_rotated_at = now(), updated_at = now()
				where user_id = app_user_id() returning `+settingsCols, NewSlug()))
			if err == nil {
				return sp.Commit(ctx)
			}
			_ = sp.Rollback(ctx)
			if !isUnique(err) {
				return err
			}
		}
		return errors.New("portfolio: could not draw a free slug")
	})
	if err != nil {
		fail(w, r, "slug", err)
		return
	}
	s.URL = URL(siteURL(), s.Slug)
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": s})
}

var itemKinds = map[string]bool{"cert": true, "experience": true, "skill": true}

// putItems: PUT /api/portfolio/items — replaces the synced self-declared items with the list the
// device holds. Needs the sync consent: {"consent": true} in the same body grants it.
func putItems(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var body struct {
		Consent bool             `json:"consent"`
		Items   []map[string]any `json:"items"`
	}
	if err := httpx.Decode(r, &body, 64<<10); err != nil || body.Items == nil || len(body.Items) > MaxItems {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	items := make([]Item, 0, len(body.Items))
	for _, in := range body.Items {
		var it Item
		for k := range in {
			switch k {
			case "kind", "title", "org", "period", "desc", "url":
			default:
				// "verified", "gpa", "phone", ... are not accepted
				httpx.Error(w, r, http.StatusBadRequest, "unknown_field", httpx.MsgBad)
				return
			}
		}
		kind, _ := in["kind"].(string)
		if !itemKinds[kind] {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_kind", MsgInvalid)
			return
		}
		it.Kind = kind
		for _, f := range []struct {
			key      string
			dst      *string
			min, max int
		}{{"title", &it.Title, 1, 120}, {"org", &it.Org, 0, 120}, {"period", &it.Period, 0, 40}, {"desc", &it.Desc, 0, 300}, {"url", &it.URL, 0, 500}} {
			v, ok := cleanText(in[f.key], f.max, f.key == "desc")
			if !ok || (v == nil && f.min > 0) {
				httpx.Error(w, r, http.StatusBadRequest, "invalid_"+f.key, MsgInvalid)
				return
			}
			if v != nil {
				*f.dst = *v
			}
		}
		if it.URL != "" && (!strings.HasPrefix(strings.ToLower(it.URL), "https://") || strings.ContainsAny(it.URL, " <>\"'")) {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_url", MsgInvalid)
			return
		}
		items = append(items, it)
	}
	noConsent := false
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		s, err := ensure(ctx, tx)
		if err != nil {
			return err
		}
		if s.SyncConsentAt == nil {
			if !body.Consent {
				noConsent = true
				return nil
			}
			if _, err := tx.Exec(ctx, `update portfolio_settings set sync_consent_at = now(), updated_at = now() where user_id = app_user_id()`); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `delete from portfolio_items where user_id = app_user_id()`); err != nil {
			return err
		}
		for i, it := range items {
			if _, err := tx.Exec(ctx, `insert into portfolio_items (user_id, kind, title, org, period, descr, url, position)
				values (app_user_id(), $1, $2, $3, $4, $5, $6, $7)`, it.Kind, it.Title, it.Org, it.Period, it.Desc, it.URL, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, r, "items", err)
		return
	}
	if noConsent {
		httpx.Error(w, r, http.StatusConflict, "sync_consent_required", MsgSync)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// deleteItems: DELETE /api/portfolio/items — withdraw the sync consent, delete the items.
func deleteItems(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from portfolio_items where user_id = app_user_id()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update portfolio_settings set sync_consent_at = null, updated_at = now() where user_id = app_user_id()`)
		return err
	})
	if err != nil {
		fail(w, r, "delete items", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": []Item{}})
}
