package portfolio

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/ambassador"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

func siteURL() string { return config.SiteURL() }

func itoa(n int) string { return strconv.Itoa(n) }

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func eventURL(id int64) string { return siteURL() + "/event/" + strconv.FormatInt(id, 10) }

// Bi is a two-language text.
type Bi struct {
	KK string `json:"kk"`
	RU string `json:"ru"`
}

// Page is the JSON of the public page (and the owner's preview). It never carries a user id,
// Telegram id, internal row id, GPA, phone or email. Every entry says whether the platform
// verified it; only system tables produce verified = true.
type Page struct {
	NoIndex      bool     `json:"noindex"` // always true: <meta name="robots" content="noindex">
	Profile      Profile  `json:"profile"`
	Verified     Verified `json:"verified"`
	SelfReported Self     `json:"selfReported"`
	Contact      *Contact `json:"contact"` // null unless showContact is on and the student has a Telegram username
	OG           OG       `json:"og"`
	Site         string   `json:"site"`
}

type Profile struct {
	Name       string  `json:"name"`
	Headline   *string `json:"headline"`
	Bio        *string `json:"bio"`
	University *string `json:"university"` // null when hidden
	Course     *int16  `json:"course"`     // null when hidden
}

// Verified is «Расталған жетістіктер ✓».
type Verified struct {
	Events       []VerifiedEvent `json:"events"`
	Certificates []VerifiedCert  `json:"certificates"`
	Badges       []Badge         `json:"badges"`
}

type VerifiedEvent struct {
	Title     Bi        `json:"title"`
	Date      time.Time `json:"date"`
	Organizer string    `json:"organizer"`
	VerifyURL string    `json:"verifyUrl"`
	Verified  bool      `json:"verified"`
}

type VerifiedCert struct {
	Kind      string    `json:"kind"` // event | ambassador
	Title     Bi        `json:"title"`
	Issuer    string    `json:"issuer"`
	IssuedAt  time.Time `json:"issuedAt"`
	VerifyURL string    `json:"verifyUrl"`
	Verified  bool      `json:"verified"`
}

type Badge struct {
	Badge     string    `json:"badge"` // mentor (verified) | generous, ... (self / community)
	AwardedAt time.Time `json:"awardedAt"`
	Verified  bool      `json:"verified"`
}

// Self is «Өзі көрсеткен».
type Self struct {
	Certificates []SelfItem `json:"certificates"`
	Experience   []SelfItem `json:"experience"`
	Skills       []SelfItem `json:"skills"`
	Badges       []Badge    `json:"badges"`
}

type SelfItem struct {
	Title    string `json:"title"`
	Org      string `json:"org"`
	Period   string `json:"period"`
	Desc     string `json:"desc"`
	URL      string `json:"url"`
	Verified bool   `json:"verified"` // always false
}

type Contact struct {
	TelegramURL string `json:"telegramUrl"`
}

type OG struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Image       string  `json:"image"`
	URL         string  `json:"url"`
}

// errHidden: no page (disabled, unknown slug, deleted account) — always the same neutral 404.
var errHidden = errors.New("portfolio: no page")

// Build assembles the page for userID from its settings. preview = the owner's own view, shown
// even while the page is disabled.
func Build(ctx context.Context, userID int64, preview bool) (*Page, error) {
	p := &Page{NoIndex: true, Site: siteURL()}
	p.Verified = Verified{Events: []VerifiedEvent{}, Certificates: []VerifiedCert{}, Badges: []Badge{}}
	p.SelfReported = Self{Certificates: []SelfItem{}, Experience: []SelfItem{}, Skills: []SelfItem{}, Badges: []Badge{}}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var s Settings
		var name, univ, username *string
		var course *int16
		err := tx.QueryRow(ctx, `select s.enabled, s.slug, s.display_name, s.headline, s.bio, s.show_university, s.show_course,
				s.show_events, s.show_certs, s.show_experience, s.show_skills, s.show_badges, s.show_contact, s.sync_consent_at,
				u.name, u.university, u.course, u.tg_username
			from portfolio_settings s join users u on u.id = s.user_id and u.deleted_at is null and u.consent_at is not null
			where s.user_id = $1`, userID).
			Scan(&s.Enabled, &s.Slug, &s.DisplayName, &s.Headline, &s.Bio, &s.ShowUniversity, &s.ShowCourse, &s.ShowEvents,
				&s.ShowCerts, &s.ShowExperience, &s.ShowSkills, &s.ShowBadges, &s.ShowContact, &s.SyncConsentAt,
				&name, &univ, &course, &username)
		if errors.Is(err, pgx.ErrNoRows) {
			return errHidden
		}
		if err != nil {
			return err
		}
		if !s.Enabled && !preview {
			return errHidden
		}
		p.Profile.Name = "Student Hub"
		if s.DisplayName != nil {
			p.Profile.Name = *s.DisplayName
		} else if name != nil && *name != "" {
			p.Profile.Name = *name
		}
		p.Profile.Headline, p.Profile.Bio = s.Headline, s.Bio
		if s.ShowUniversity {
			p.Profile.University = univ
		}
		if s.ShowCourse {
			p.Profile.Course = course
		}
		if s.ShowContact && username != nil && *username != "" {
			p.Contact = &Contact{TelegramURL: "https://t.me/" + *username}
		}
		p.OG = OG{Title: p.Profile.Name + " — Student Hub портфолиосы", Description: s.Headline,
			Image: siteURL() + "/og.png", URL: URL(siteURL(), s.Slug)}

		if s.ShowEvents {
			rows, err := tx.Query(ctx, `select v.event_id, v.title_kk, coalesce(v.title_ru, ''), v.university, v.starts_at
				from verified_attendance v where v.user_id = $1 order by v.starts_at desc limit 100`, userID)
			if err != nil {
				return err
			}
			p.Verified.Events, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (VerifiedEvent, error) {
				var e VerifiedEvent
				var id int64
				err := row.Scan(&id, &e.Title.KK, &e.Title.RU, &e.Organizer, &e.Date)
				e.VerifyURL, e.Verified = eventURL(id), true
				return e, err
			})
			if err != nil {
				return err
			}
		}
		if s.ShowCerts {
			rows, err := tx.Query(ctx, `select e.id, e.title_kk, coalesce(e.title_ru, ''), e.university, c.issued_at
				from event_certificates c join events e on e.id = c.event_id
				where c.user_id = $1 order by c.issued_at desc limit 100`, userID)
			if err != nil {
				return err
			}
			certs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (VerifiedCert, error) {
				var c VerifiedCert
				var id int64
				err := row.Scan(&id, &c.Title.KK, &c.Title.RU, &c.Issuer, &c.IssuedAt)
				c.Kind, c.VerifyURL, c.Verified = "event", eventURL(id), true
				return c, err
			})
			if err != nil {
				return err
			}
			rows, err = tx.Query(ctx, `select cert_code, university, role_in_team, cert_issued_at from ambassador_terms
				where user_id = $1 and cert_code is not null and cert_issued_at is not null order by cert_issued_at desc`, userID)
			if err != nil {
				return err
			}
			amb, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (VerifiedCert, error) {
				var c VerifiedCert
				var code, role string
				err := row.Scan(&code, &c.Issuer, &role, &c.IssuedAt)
				c.Kind, c.VerifyURL, c.Verified = "ambassador", ambassador.CertificateURL(code), true
				c.Title = Bi{KK: "Student Hub амбассадоры", RU: "Амбассадор Student Hub"}
				if role == "lead" {
					c.Title = Bi{KK: "Student Hub амбассадорлар жетекшісі", RU: "Лидер амбассадоров Student Hub"}
				}
				return c, err
			})
			if err != nil {
				return err
			}
			p.Verified.Certificates = append(certs, amb...)
		}
		if s.ShowBadges {
			rows, err := tx.Query(ctx, `select badge, awarded_at, verified from user_badges where user_id = $1 order by awarded_at`, userID)
			if err != nil {
				return err
			}
			badges, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Badge, error) {
				var b Badge
				return b, row.Scan(&b.Badge, &b.AwardedAt, &b.Verified)
			})
			if err != nil {
				return err
			}
			for _, b := range badges {
				if b.Verified {
					p.Verified.Badges = append(p.Verified.Badges, b)
				} else {
					p.SelfReported.Badges = append(p.SelfReported.Badges, b)
				}
			}
		}
		if s.SyncConsentAt != nil && (s.ShowCerts || s.ShowExperience || s.ShowSkills) {
			items, err := ownItems(ctx, tx, `select kind, title, org, period, descr, url from portfolio_items
				where user_id = $1 order by kind, position, id`, userID)
			if err != nil {
				return err
			}
			for _, it := range items {
				si := SelfItem{Title: it.Title, Org: it.Org, Period: it.Period, Desc: it.Desc, URL: it.URL}
				switch {
				case it.Kind == "cert" && s.ShowCerts:
					p.SelfReported.Certificates = append(p.SelfReported.Certificates, si)
				case it.Kind == "experience" && s.ShowExperience:
					p.SelfReported.Experience = append(p.SelfReported.Experience, si)
				case it.Kind == "skill" && s.ShowSkills:
					p.SelfReported.Skills = append(p.SelfReported.Skills, si)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// notFound is the one neutral answer for a disabled page, an unknown or old slug, a deleted
// account and the switch being off — byte for byte the same as an unknown /api route.
func notFound(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
}

// public: GET /api/p/{slug} — the JSON the Next.js page /p/<slug> renders. No auth, no cache,
// noindex. Same 404 for "disabled" and "does not exist".
func public(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	ctx := r.Context()
	if ok, retry := ratelimit.Allow(ctx, "p:ip:"+httpx.ClientIP(r), publicLimit, time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	slug := r.PathValue("slug")
	if !ValidSlug(slug) {
		notFound(w, r)
		return
	}
	var uid int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select s.user_id from portfolio_settings s
			join users u on u.id = s.user_id and u.deleted_at is null and u.consent_at is not null
			where s.slug = $1 and s.enabled`, slug).Scan(&uid)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w, r)
		return
	}
	if err != nil {
		fail(w, r, "public lookup", err)
		return
	}
	page, err := Build(ctx, uid, false)
	if errors.Is(err, errHidden) {
		notFound(w, r)
		return
	}
	if err != nil {
		fail(w, r, "public", err)
		return
	}
	httpx.JSON(w, http.StatusOK, page)
}
