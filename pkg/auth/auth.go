// Package auth signs students in with Telegram (Mini App initData or Login Widget), keeps
// them signed in with an HMAC-signed cookie and guards state-changing requests (origin
// check + rate limits). Role and deleted_at are re-read from the database on every
// request, so a revoked role or a deleted account takes effect at once.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

// User is an account as the API returns it. GPA and phone are never stored server-side.
type User struct {
	ID             int64      `json:"id"`
	TelegramID     int64      `json:"telegramId"`
	Username       *string    `json:"username"`
	Name           *string    `json:"name"`
	Lang           string     `json:"lang"`
	University     *string    `json:"university"`
	Faculty        *string    `json:"faculty"`
	Course         *int16     `json:"course"`
	City           *string    `json:"city"`
	Role           string     `json:"role"`
	ConsentAt      *time.Time `json:"consentAt"`
	IsAdult        *bool      `json:"isAdult"`
	CreatedAt      time.Time  `json:"createdAt"`
	SessionVersion int        `json:"-"`
	DeletedAt      *time.Time `json:"-"`
}

// NeedsConsent is true until the student accepts the consent screen.
func (u *User) NeedsConsent() bool { return u.ConsentAt == nil }

// UserColumns matches ScanUser.
const UserColumns = `id, telegram_id, tg_username, name, lang, university, faculty, course, city,
	role::text, consent_at, is_adult, created_at, session_version, deleted_at`

// ScanUser reads one row selected with UserColumns.
func ScanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Name, &u.Lang, &u.University, &u.Faculty,
		&u.Course, &u.City, &u.Role, &u.ConsentAt, &u.IsAdult, &u.CreatedAt, &u.SessionVersion, &u.DeletedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

var (
	MsgOrigin  = httpx.Msg{KK: "Бұл сұраныс тек Student Hub сайтынан қабылданады.", RU: "Этот запрос принимается только с сайта Student Hub."}
	MsgTGBad   = httpx.Msg{KK: "Telegram деректері расталмады. Қайта кіріп көріңіз.", RU: "Не удалось проверить данные Telegram. Попробуйте войти снова."}
	MsgTGOld   = httpx.Msg{KK: "Кіру сілтемесінің мерзімі өтті. Қайта кіріңіз.", RU: "Срок входа истёк. Войдите снова."}
	MsgOff     = httpx.Msg{KK: "Кіру уақытша қолжетімсіз.", RU: "Вход временно недоступен."}
	MsgDeleted = httpx.Msg{KK: "Бұл аккаунт бұғатталған.", RU: "Этот аккаунт заблокирован."}
)

type ctxKey struct{}

type cached struct{ u *User }

// Current returns the signed-in user or nil. The token's session_version must match the
// database and the account must not be deleted. The lookup runs through db.WithUser.
func Current(r *http.Request) *User {
	if c, ok := r.Context().Value(ctxKey{}).(*cached); ok {
		return c.u
	}
	return lookup(r)
}

func lookup(r *http.Request) *User {
	tok := tokenFrom(r)
	if tok == "" {
		return nil
	}
	c, err := Parse(tok, time.Now())
	if err != nil {
		return nil
	}
	var u *User
	err = db.WithUser(r.Context(), c.UserID, func(tx pgx.Tx) error {
		var err error
		u, err = ScanUser(tx.QueryRow(r.Context(), "select "+UserColumns+" from users where id = app_user_id()"))
		return err
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, db.ErrNoDB) {
			log.Printf("auth: load user %d: %v", c.UserID, err)
		}
		return nil
	}
	if u.DeletedAt != nil || u.SessionVersion != c.Version {
		return nil
	}
	return u
}

// attach resolves the user once and caches it in the request context.
func attach(r *http.Request) *http.Request {
	if _, ok := r.Context().Value(ctxKey{}).(*cached); ok {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, &cached{lookup(r)}))
}

// Handler receives the signed-in user.
type Handler func(w http.ResponseWriter, r *http.Request, u *User)

// Need answers 401 unless someone is signed in.
func Need(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = attach(r)
		u := Current(r)
		if u == nil {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", httpx.MsgAuth)
			return
		}
		h(w, r, u)
	}
}

// NeedRole answers 401 when signed out and 403 unless the user's current role is one of roles.
func NeedRole(h Handler, roles ...string) http.HandlerFunc {
	return Need(func(w http.ResponseWriter, r *http.Request, u *User) {
		if !slices.Contains(roles, u.Role) {
			httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
			return
		}
		h(w, r, u)
	})
}

// Write guards a state-changing endpoint: the Origin (or Referer) host must be in
// ALLOWED_HOSTS (CSRF defence on top of SameSite=Lax), then RATE_WRITE_PER_MIN (default 30)
// applies per IP and, when signed in, per user.
func Write(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !OriginAllowed(r) {
			httpx.Error(w, r, http.StatusForbidden, "bad_origin", MsgOrigin)
			return
		}
		limit := config.Int("RATE_WRITE_PER_MIN", 30)
		if !allow(w, r, "w:ip:"+httpx.ClientIP(r), limit) {
			return
		}
		r = attach(r)
		if u := Current(r); u != nil && !allow(w, r, "w:u:"+strconv.FormatInt(u.ID, 10), limit) {
			return
		}
		h(w, r)
	}
}

func allow(w http.ResponseWriter, r *http.Request, key string, limit int) bool {
	ok, retry := ratelimit.Allow(r.Context(), key, limit, time.Minute)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
	}
	return ok
}

// OriginAllowed is true for safe methods and for requests whose Origin (or, failing that,
// Referer) hostname is listed in ALLOWED_HOSTS. Requests with neither header are refused.
func OriginAllowed(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	src := r.Header.Get("Origin")
	if src == "" || src == "null" {
		src = r.Header.Get("Referer")
	}
	u, err := url.Parse(src)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	hosts := config.List("ALLOWED_HOSTS", "studenthub-plus-kz.vercel.app,studenthub-platform.vercel.app,localhost,127.0.0.1")
	for _, h := range hosts {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

// Routes wires the login and logout endpoints.
func Routes(mux httpx.Router) {
	mux.HandleFunc("POST /api/auth/telegram/webapp", Write(webappLogin))
	mux.HandleFunc("POST /api/auth/telegram/widget", Write(widgetLogin))
	mux.HandleFunc("POST /api/auth/logout", Write(logout))
}

func webappLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"initData"`
		Ref      string `json:"ref"` // optional stored ?ref= (amb_<code> / share), see CleanRef
	}
	if err := httpx.Decode(r, &body, 16<<10); err != nil || body.InitData == "" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	tg, err := VerifyWebApp(body.InitData, config.Str("BOT_TOKEN", ""), time.Now())
	if !verified(w, r, err) {
		return
	}
	login(w, r, tg, true, CleanRef(body.Ref))
}

func widgetLogin(w http.ResponseWriter, r *http.Request) {
	raw := map[string]any{}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	// "ref" is ours, not Telegram's: it must not take part in the hash check.
	ref, _ := raw["ref"].(string)
	delete(raw, "ref")
	tg, err := VerifyWidget(raw, config.Str("BOT_TOKEN", ""), time.Now())
	if !verified(w, r, err) {
		return
	}
	login(w, r, tg, false, CleanRef(ref))
}

var reAmbRef = regexp.MustCompile(`^amb_[a-z0-9_-]{1,30}$`)

// CleanRef accepts "share" or "amb_<code>" (case-insensitive) and returns it lower-cased; "" otherwise.
// Same rule as analytics.CleanRef.
func CleanRef(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "share" || reAmbRef.MatchString(ref) {
		return ref
	}
	return ""
}

func verified(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrNoToken):
		log.Print("auth: BOT_TOKEN is not set")
		httpx.Error(w, r, http.StatusServiceUnavailable, "auth_off", MsgOff)
	case errors.Is(err, ErrExpired):
		httpx.Error(w, r, http.StatusUnauthorized, "telegram_expired", MsgTGOld)
	default:
		httpx.Error(w, r, http.StatusUnauthorized, "telegram_invalid", MsgTGBad)
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	for utf8.RuneCountInString(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// login links the Telegram identity to its users row (the bot may have created it already),
// applies the ADMIN_TELEGRAM_IDS bootstrap and sets the session cookie. ref (already cleaned)
// is recorded as users.referred_by only at sign-up: while the account has no consent yet and
// no referral recorded.
func login(w http.ResponseWriter, r *http.Request, tg TGUser, webapp bool, ref string) {
	ctx := r.Context()
	name := clip(tg.FirstName+" "+tg.LastName, 80)
	lang := "kk"
	if strings.HasPrefix(strings.ToLower(tg.LanguageCode), "ru") {
		lang = "ru"
	}
	var u *User
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		u, err = ScanUser(tx.QueryRow(ctx, `insert into users (telegram_id, tg_username, name, lang, last_seen_at)
			values ($1, $2, $3, $4, now())
			on conflict (telegram_id) do update set
				tg_username = coalesce(excluded.tg_username, users.tg_username),
				name = coalesce(users.name, excluded.name),
				last_seen_at = now()
			returning `+UserColumns, tg.ID, nullable(clip(tg.Username, 64)), nullable(name), lang))
		if err != nil || u.DeletedAt != nil {
			return err
		}
		if ref != "" && u.ConsentAt == nil {
			if _, err := tx.Exec(ctx, `update users set referred_by = $2 where id = $1 and referred_by is null`, u.ID, ref); err != nil {
				return err
			}
		}
		return bootstrapAdmin(ctx, tx, u)
	})
	if err != nil {
		log.Printf("auth: login %d: %v", tg.ID, err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
		return
	}
	if u.DeletedAt != nil {
		httpx.Error(w, r, http.StatusForbidden, "account_blocked", MsgDeleted)
		return
	}
	tok, err := Sign(Claims{UserID: u.ID, Version: u.SessionVersion, Exp: time.Now().Add(SessionTTL).Unix()})
	if err != nil {
		log.Printf("auth: %v", err)
		httpx.Error(w, r, http.StatusServiceUnavailable, "auth_off", MsgOff)
		return
	}
	setCookie(w, r, tok, int(SessionTTL.Seconds()))
	resp := map[string]any{"user": u, "needsConsent": u.NeedsConsent()}
	if webapp {
		// Telegram Web opens Mini Apps in an iframe that may drop cookies; the Mini App keeps
		// this token in memory only and sends it as "Authorization: Bearer".
		resp["token"] = tok
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// bootstrapAdmin makes ADMIN_TELEGRAM_IDS admins on login. The grant is audited when the
// role actually changes, so it is logged once.
func bootstrapAdmin(ctx context.Context, tx pgx.Tx, u *User) error {
	if u.Role == "admin" || !slices.Contains(config.List("ADMIN_TELEGRAM_IDS", ""), strconv.FormatInt(u.TelegramID, 10)) {
		return nil
	}
	if _, err := tx.Exec(ctx, `update users set role = 'admin' where id = $1`, u.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values (null, 'role_grant', $1, 'ADMIN_TELEGRAM_IDS bootstrap', jsonb_build_object('from', $2::text, 'to', 'admin'))`, u.ID, u.Role)
	u.Role = "admin"
	return err
}

func logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		All bool `json:"all"`
	}
	_ = httpx.Decode(r, &body, 1<<10) // body is optional
	if u := Current(r); u != nil && body.All {
		// "Log out everywhere": every token carries the old session_version and stops working.
		err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `update users set session_version = session_version + 1 where id = app_user_id()`)
			return err
		})
		if err != nil {
			log.Printf("auth: logout all %d: %v", u.ID, err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
			return
		}
	}
	ClearCookie(w, r)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
