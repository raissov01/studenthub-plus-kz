package auth

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	sendmail "github.com/raissov01/studenthub-plus-kz/pkg/mail"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
)

// Email + password sign-in (docs/api/email-auth.md). Rules that hold for every endpoint:
//   - answers to anonymous requests never tell whether an address has an account: sign-up
//     with a taken address, a reset for an unknown one and a resend all answer 200 {ok},
//     and the address owner gets a notice instead of a code;
//   - a wrong, expired, used-up or missing code is one error (400 invalid_code);
//   - a wrong email and a wrong password are one error (401 invalid_credentials), and an
//     unknown email still costs one argon2id verification;
//   - on top of auth.Write, login and code endpoints are throttled per address and per IP.

var (
	MsgEmailOff = httpx.Msg{KK: "Email арқылы кіру әзірге қосылмаған.", RU: "Вход по email пока не подключён."}
	MsgEmailBad = httpx.Msg{KK: "Email адресі дұрыс емес.", RU: "Некорректный email."}
	MsgCreds    = httpx.Msg{KK: "Email немесе құпиясөз қате.", RU: "Неверный email или пароль."}
	MsgCode     = httpx.Msg{KK: "Код қате немесе мерзімі өткен. Қайта тексеріңіз немесе жаңа код сұраңыз.", RU: "Код неверный или устарел. Проверьте его или запросите новый."}
	MsgVerify   = httpx.Msg{KK: "Email әлі расталмаған. Поштаңызға жіберілген кодты енгізіңіз.", RU: "Email ещё не подтверждён. Введите код из письма."}
	MsgPwWrong  = httpx.Msg{KK: "Қазіргі құпиясөз қате.", RU: "Текущий пароль неверный."}
	MsgNoPw     = httpx.Msg{KK: "Бұл аккаунтта құпиясөз жоқ. Алдымен Баптауларда email қосыңыз.", RU: "У аккаунта нет пароля. Сначала добавьте email в настройках."}
	MsgSameMail = httpx.Msg{KK: "Бұл email аккаунтыңызда тұр.", RU: "Этот email уже указан в аккаунте."}
	MsgTaken    = httpx.Msg{KK: "Бұл email басқа аккаунтта қолданылады.", RU: "Этот email используется другим аккаунтом."}

	msgPw = map[error]httpx.Msg{
		ErrPwShort:  {KK: "Құпиясөз кемінде 8 таңба болсын.", RU: "Пароль должен быть не короче 8 символов."},
		ErrPwLong:   {KK: "Құпиясөз 128 таңбадан аспасын.", RU: "Пароль должен быть не длиннее 128 символов."},
		ErrPwCommon: {KK: "Бұл құпиясөз тым оңай. Басқасын ойлап табыңыз.", RU: "Этот пароль слишком простой. Придумайте другой."},
		ErrPwEmail:  {KK: "Құпиясөз email-мен бірдей болмасын.", RU: "Пароль не должен совпадать с email."},
	}
)

// Throttles (fixed windows in rate_limits, keyed by IP, by an HMAC of the address or by user).
type limit struct {
	n      int
	window time.Duration
}

var (
	loginPerEmail = limit{10, 15 * time.Minute}
	loginPerIP    = limit{30, 15 * time.Minute}
	sendPerEmail  = limit{5, time.Hour}
	sendPerIP     = limit{20, time.Hour}
	checkPerEmail = limit{10, 15 * time.Minute}
	checkPerIP    = limit{30, 15 * time.Minute}
)

func emailRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/email/register", Write(emailRegister))
	mux.HandleFunc("POST /api/auth/email/verify", Write(emailVerify))
	mux.HandleFunc("POST /api/auth/email/login", Write(emailLogin))
	mux.HandleFunc("POST /api/auth/email/resend", Write(emailResend))
	mux.HandleFunc("POST /api/auth/email/reset/request", Write(resetRequest))
	mux.HandleFunc("POST /api/auth/email/reset/confirm", Write(resetConfirm))
	mux.HandleFunc("POST /api/me/password", Write(Need(changePassword)))
	mux.HandleFunc("POST /api/me/email", Write(Need(linkEmail)))
	mux.HandleFunc("POST /api/me/email/confirm", Write(Need(confirmEmail)))
}

// NormalizeEmail trims and lower-cases addr and checks it is a bare address
// (local@domain.tld, no display name, at most 254 bytes).
func NormalizeEmail(addr string) (string, bool) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if len(addr) < 6 || len(addr) > 254 || strings.ContainsFunc(addr, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", false
	}
	p, err := mail.ParseAddress(addr)
	if err != nil || p.Address != addr || p.Name != "" {
		return "", false
	}
	local, domain, _ := strings.Cut(addr, "@")
	if local == "" || len(local) > 64 || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") ||
		strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", false
	}
	return addr, true
}

func throttle(w http.ResponseWriter, r *http.Request, key string, l limit) bool {
	ok, retry := ratelimit.Allow(r.Context(), key, l.n, l.window)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
	}
	return ok
}

// throttleBoth applies a per-IP and a per-address limit under the given name.
func throttleBoth(w http.ResponseWriter, r *http.Request, name, email string, ip, addr limit) bool {
	return throttle(w, r, "e:"+name+":ip:"+httpx.ClientIP(r), ip) &&
		throttle(w, r, "e:"+name+":em:"+emailKey(email), addr)
}

func mailOn(w http.ResponseWriter, r *http.Request) bool {
	if !sendmail.Enabled() {
		httpx.Error(w, r, http.StatusServiceUnavailable, "email_auth_off", MsgEmailOff)
		return false
	}
	if _, err := secret(); err != nil {
		log.Printf("auth: %v", err)
		httpx.Error(w, r, http.StatusServiceUnavailable, "auth_off", MsgOff)
		return false
	}
	return true
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("auth: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func badPassword(w http.ResponseWriter, r *http.Request, err error) {
	httpx.Error(w, r, http.StatusBadRequest, err.Error(), msgPw[err])
}

// sent is the generic answer of every endpoint that may have sent an email.
func sent(w http.ResponseWriter) {
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "resendAfter": int(CodeResendAfter.Seconds())})
}

func pickLang(lang string, r *http.Request) string {
	if lang == "kk" || lang == "ru" {
		return lang
	}
	return httpx.Lang(r)
}

// account is the sign-in state of the users row that holds an address.
type account struct {
	ID       int64
	Lang     string
	Verified bool
	HasTG    bool
	Consent  bool
	Deleted  bool
	PwHash   *string
}

// pending is a sign-up still waiting for its code: never had a session, holds no data.
func (a *account) pending() bool { return !a.Verified && !a.HasTG && !a.Consent }

func findAccount(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, email string, lock bool) (*account, error) {
	sql := `select u.id, u.lang, u.email_verified_at is not null, u.telegram_id is not null,
			u.consent_at is not null, u.deleted_at is not null, p.hash
		from users u left join user_passwords p on p.user_id = u.id where lower(u.email) = $1`
	if lock {
		sql += " for update of u"
	}
	var a account
	err := q.QueryRow(ctx, sql, email).Scan(&a.ID, &a.Lang, &a.Verified, &a.HasTG, &a.Consent, &a.Deleted, &a.PwHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func setPassword(ctx context.Context, tx pgx.Tx, userID int64, hash string) error {
	_, err := tx.Exec(ctx, `insert into user_passwords (user_id, hash) values ($1, $2)
		on conflict (user_id) do update set hash = excluded.hash, updated_at = now()`, userID, hash)
	return err
}

// POST /api/auth/email/register {email, password, lang, ref?}
func emailRegister(w http.ResponseWriter, r *http.Request) {
	if !mailOn(w, r) {
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Lang     string `json:"lang"`
		Ref      string `json:"ref"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email, ok := NormalizeEmail(body.Email)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_email", MsgEmailBad)
		return
	}
	if err := CheckPassword(body.Password, email); err != nil {
		badPassword(w, r, err)
		return
	}
	if !throttleBoth(w, r, "send", email, sendPerIP, sendPerEmail) {
		return
	}
	// Hash first, whatever happens next: a taken address must cost the same time.
	hash, err := HashPassword(body.Password)
	if err != nil {
		fail(w, r, "hash", err)
		return
	}
	lang := pickLang(body.Lang, r)
	ctx := r.Context()
	var msg *sendmail.Message
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		a, err := findAccount(ctx, tx, email, true)
		if err != nil {
			return err
		}
		var id int64
		switch {
		case a == nil:
			err := tx.QueryRow(ctx, `insert into users (email, lang, referred_by) values ($1, $2, $3) returning id`,
				email, lang, nullable(CleanRef(body.Ref))).Scan(&id)
			if err != nil {
				return err
			}
		case a.Deleted:
			return nil
		case a.pending():
			// An unfinished sign-up: the latest password wins. Whoever verifies must also
			// know it (verify checks the password), so nobody can pre-set a password for an
			// address they do not own.
			id = a.ID
			if _, err := tx.Exec(ctx, `update users set lang = $2 where id = $1`, id, lang); err != nil {
				return err
			}
		default:
			m := sendmail.Notice(a.Lang, sendmail.AccountExists, email)
			msg = &m
			return nil
		}
		if err := setPassword(ctx, tx, id, hash); err != nil {
			return err
		}
		code, err := issueCode(ctx, tx, id, PurposeVerify, email, nil)
		if err != nil || code == "" {
			return err
		}
		m := sendmail.Code(lang, sendmail.VerifyEmail, email, code)
		msg = &m
		return nil
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		err = nil // a parallel sign-up for the same address won; answer the same way
	}
	if err != nil {
		fail(w, r, "register", err)
		return
	}
	if msg != nil {
		sendmail.Go(*msg)
	}
	sent(w)
}

// POST /api/auth/email/verify {email, code, password} -> session
func emailVerify(w http.ResponseWriter, r *http.Request) {
	if !mailOn(w, r) {
		return
	}
	var body struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email, ok := NormalizeEmail(body.Email)
	code := strings.TrimSpace(body.Code)
	if !ok || !validCode(code) || len(body.Password) > 4*PasswordMax {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	if !throttleBoth(w, r, "check", email, checkPerIP, checkPerEmail) {
		return
	}
	ctx := r.Context()
	a, err := findAccount(ctx, mustPool(ctx), email, false)
	if err != nil {
		fail(w, r, "verify lookup", err)
		return
	}
	hash := dummyHash()
	if a != nil && a.PwHash != nil {
		hash = *a.PwHash
	}
	pwOK, _ := VerifyPassword(body.Password, hash)
	if a == nil || a.Deleted {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	if _, err := checkCode(ctx, a.ID, PurposeVerify, code, pwOK); err != nil {
		if !errors.Is(err, errBadCode) {
			fail(w, r, "verify code", err)
			return
		}
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	var u *User
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		u, err = ScanUser(tx.QueryRow(ctx, `update users set email_verified_at = coalesce(email_verified_at, now()),
			last_seen_at = now() where id = $1 returning `+UserColumns, a.ID))
		if err != nil {
			return err
		}
		return bootstrapAdmin(ctx, tx, u)
	})
	if err != nil {
		fail(w, r, "verify", err)
		return
	}
	startSession(w, r, u, false)
}

// POST /api/auth/email/login {email, password} -> session, or 403 email_not_verified
// (a fresh code is sent) when the password is right but the address is not confirmed yet.
func emailLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email, ok := NormalizeEmail(body.Email)
	if !ok || len(body.Password) > 4*PasswordMax {
		httpx.Error(w, r, http.StatusUnauthorized, "invalid_credentials", MsgCreds)
		return
	}
	if _, err := secret(); err != nil {
		log.Printf("auth: %v", err)
		httpx.Error(w, r, http.StatusServiceUnavailable, "auth_off", MsgOff)
		return
	}
	if !throttleBoth(w, r, "login", email, loginPerIP, loginPerEmail) {
		return
	}
	ctx := r.Context()
	a, err := findAccount(ctx, mustPool(ctx), email, false)
	if err != nil {
		fail(w, r, "login lookup", err)
		return
	}
	hash := dummyHash()
	if a != nil && a.PwHash != nil {
		hash = *a.PwHash
	}
	pwOK, rehash := VerifyPassword(body.Password, hash)
	if a == nil || a.PwHash == nil || !pwOK {
		httpx.Error(w, r, http.StatusUnauthorized, "invalid_credentials", MsgCreds)
		return
	}
	if a.Deleted {
		httpx.Error(w, r, http.StatusForbidden, "account_blocked", MsgDeleted)
		return
	}
	if !a.Verified {
		// The caller knows the password, so saying "not verified" reveals nothing new.
		if !sendmail.Enabled() {
			httpx.Error(w, r, http.StatusServiceUnavailable, "email_auth_off", MsgEmailOff)
			return
		}
		var code string
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			var err error
			code, err = issueCode(ctx, tx, a.ID, PurposeVerify, email, nil)
			return err
		})
		if err != nil {
			fail(w, r, "login code", err)
			return
		}
		if code != "" { // none while the previous code is younger than CodeResendAfter
			sendmail.Go(sendmail.Code(a.Lang, sendmail.VerifyEmail, email, code))
		}
		httpx.Error(w, r, http.StatusForbidden, "email_not_verified", MsgVerify)
		return
	}
	var u *User
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		if rehash {
			if h, err := HashPassword(body.Password); err == nil {
				if err := setPassword(ctx, tx, a.ID, h); err != nil {
					return err
				}
			}
		}
		var err error
		u, err = ScanUser(tx.QueryRow(ctx, `update users set last_seen_at = now() where id = $1 returning `+UserColumns, a.ID))
		if err != nil {
			return err
		}
		return bootstrapAdmin(ctx, tx, u)
	})
	if err != nil {
		fail(w, r, "login", err)
		return
	}
	startSession(w, r, u, false)
}

// POST /api/auth/email/resend {email, purpose?}: purpose verify_email (default) or reset_password.
func emailResend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := httpx.Decode(r, &body, 2<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	switch body.Purpose {
	case "", PurposeVerify:
		sendCode(w, r, body.Email, PurposeVerify)
	case PurposeReset:
		sendCode(w, r, body.Email, PurposeReset)
	default:
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
	}
}

// POST /api/auth/email/reset/request {email}
func resetRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(r, &body, 2<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	sendCode(w, r, body.Email, PurposeReset)
}

// sendCode issues a verify_email code (unfinished sign-ups only) or a reset_password code
// (any live account with this address) and always answers the same.
func sendCode(w http.ResponseWriter, r *http.Request, rawEmail, purpose string) {
	if !mailOn(w, r) {
		return
	}
	email, ok := NormalizeEmail(rawEmail)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_email", MsgEmailBad)
		return
	}
	if !throttleBoth(w, r, "send", email, sendPerIP, sendPerEmail) {
		return
	}
	ctx := r.Context()
	var msg *sendmail.Message
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		a, err := findAccount(ctx, tx, email, true)
		if err != nil || a == nil || a.Deleted {
			return err
		}
		if purpose == PurposeVerify && a.Verified {
			return nil
		}
		code, err := issueCode(ctx, tx, a.ID, purpose, email, nil)
		if err != nil || code == "" {
			return err
		}
		kind := sendmail.VerifyEmail
		if purpose == PurposeReset {
			kind = sendmail.ResetPassword
		}
		m := sendmail.Code(a.Lang, kind, email, code)
		msg = &m
		return nil
	})
	if err != nil {
		fail(w, r, "send code", err)
		return
	}
	if msg != nil {
		sendmail.Go(*msg)
	}
	sent(w)
}

// POST /api/auth/email/reset/confirm {email, code, newPassword} -> session; every other
// session of the account ends (session_version + 1).
func resetConfirm(w http.ResponseWriter, r *http.Request) {
	if !mailOn(w, r) {
		return
	}
	var body struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		NewPassword string `json:"newPassword"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email, ok := NormalizeEmail(body.Email)
	code := strings.TrimSpace(body.Code)
	if !ok || !validCode(code) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	if err := CheckPassword(body.NewPassword, email); err != nil {
		badPassword(w, r, err)
		return
	}
	if !throttleBoth(w, r, "check", email, checkPerIP, checkPerEmail) {
		return
	}
	ctx := r.Context()
	hash, err := HashPassword(body.NewPassword)
	if err != nil {
		fail(w, r, "hash", err)
		return
	}
	a, err := findAccount(ctx, mustPool(ctx), email, false)
	if err != nil {
		fail(w, r, "reset lookup", err)
		return
	}
	if a == nil || a.Deleted {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	if _, err := checkCode(ctx, a.ID, PurposeReset, code, true); err != nil {
		if !errors.Is(err, errBadCode) {
			fail(w, r, "reset code", err)
			return
		}
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	var u *User
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		if err := setPassword(ctx, tx, a.ID, hash); err != nil {
			return err
		}
		// The code proves the mailbox, so an unfinished sign-up is confirmed too.
		var err error
		u, err = ScanUser(tx.QueryRow(ctx, `update users set session_version = session_version + 1,
			email_verified_at = coalesce(email_verified_at, now()), last_seen_at = now()
			where id = $1 returning `+UserColumns, a.ID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from auth_codes where user_id = $1 and purpose <> 'change_email'`, a.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'password_reset', $1, null, '{}')`, a.ID); err != nil {
			return err
		}
		return bootstrapAdmin(ctx, tx, u)
	})
	if err != nil {
		fail(w, r, "reset", err)
		return
	}
	sendmail.Go(sendmail.Notice(u.Lang, sendmail.PasswordChanged, email))
	startSession(w, r, u, false)
}

// POST /api/me/password {currentPassword, newPassword}: other devices are signed out, this
// one gets a fresh cookie.
func changePassword(w http.ResponseWriter, r *http.Request, u *User) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || len(body.CurrentPassword) > 4*PasswordMax {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email := ""
	if u.Email != nil {
		email = *u.Email
	}
	if err := CheckPassword(body.NewPassword, email); err != nil {
		badPassword(w, r, err)
		return
	}
	if !throttle(w, r, "e:login:u:"+strconv.FormatInt(u.ID, 10), loginPerEmail) {
		return
	}
	ctx := r.Context()
	var cur *string
	err := mustPool(ctx).QueryRow(ctx, `select hash from user_passwords where user_id = $1`, u.ID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusBadRequest, "no_password", MsgNoPw)
		return
	}
	if err != nil {
		fail(w, r, "password lookup", err)
		return
	}
	if ok, _ := VerifyPassword(body.CurrentPassword, *cur); !ok {
		httpx.Error(w, r, http.StatusBadRequest, "wrong_password", MsgPwWrong)
		return
	}
	hash, err := HashPassword(body.NewPassword)
	if err != nil {
		fail(w, r, "hash", err)
		return
	}
	var out *User
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		if err := setPassword(ctx, tx, u.ID, hash); err != nil {
			return err
		}
		var err error
		out, err = ScanUser(tx.QueryRow(ctx, `update users set session_version = session_version + 1
			where id = $1 returning `+UserColumns, u.ID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from auth_codes where user_id = $1 and purpose = 'reset_password'`, u.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'password_change', $1, null, '{}')`, u.ID)
		return err
	})
	if err != nil {
		fail(w, r, "change password", err)
		return
	}
	if email != "" && sendmail.Enabled() {
		sendmail.Go(sendmail.Notice(out.Lang, sendmail.PasswordChanged, email))
	}
	startSession(w, r, out, false)
}

// POST /api/me/email {email, password | currentPassword}: sends a change_email code to the
// address. An account without a password sets one here ("password"); an account with one
// confirms it ("currentPassword"). The answer is the same whether the address is free.
func linkEmail(w http.ResponseWriter, r *http.Request, u *User) {
	if !mailOn(w, r) {
		return
	}
	var body struct {
		Email           string `json:"email"`
		Password        string `json:"password"`
		CurrentPassword string `json:"currentPassword"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || len(body.CurrentPassword) > 4*PasswordMax {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	email, ok := NormalizeEmail(body.Email)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_email", MsgEmailBad)
		return
	}
	if u.Email != nil && *u.Email == email {
		httpx.Error(w, r, http.StatusBadRequest, "same_email", MsgSameMail)
		return
	}
	if !throttle(w, r, "e:send:u:"+strconv.FormatInt(u.ID, 10), sendPerEmail) ||
		!throttleBoth(w, r, "send", email, sendPerIP, sendPerEmail) {
		return
	}
	ctx := r.Context()
	var cur *string
	err := mustPool(ctx).QueryRow(ctx, `select hash from user_passwords where user_id = $1`, u.ID).Scan(&cur)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "password lookup", err)
		return
	}
	var newHash *string
	if cur != nil {
		if !throttle(w, r, "e:login:u:"+strconv.FormatInt(u.ID, 10), loginPerEmail) {
			return
		}
		if ok, _ := VerifyPassword(body.CurrentPassword, *cur); !ok {
			httpx.Error(w, r, http.StatusBadRequest, "wrong_password", MsgPwWrong)
			return
		}
	} else {
		if err := CheckPassword(body.Password, email); err != nil {
			badPassword(w, r, err)
			return
		}
		h, err := HashPassword(body.Password)
		if err != nil {
			fail(w, r, "hash", err)
			return
		}
		newHash = &h
	}
	var msg *sendmail.Message
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		a, err := findAccount(ctx, tx, email, true)
		if err != nil {
			return err
		}
		if a != nil && a.ID != u.ID && !a.pending() {
			if !a.Deleted {
				m := sendmail.Notice(a.Lang, sendmail.EmailInUse, email)
				msg = &m
			}
			return nil
		}
		code, err := issueCode(ctx, tx, u.ID, PurposeChange, email, newHash)
		if err != nil || code == "" {
			return err
		}
		m := sendmail.Code(u.Lang, sendmail.ChangeEmail, email, code)
		msg = &m
		return nil
	})
	if err != nil {
		fail(w, r, "link email", err)
		return
	}
	if msg != nil {
		sendmail.Go(*msg)
	}
	sent(w)
}

// POST /api/me/email/confirm {code}: the address from POST /api/me/email becomes the
// account's verified email (and its password, if one was chosen). An unfinished sign-up
// holding the same address is removed; a real account holding it means 409 email_taken.
// Replacing an earlier address signs out other devices.
func confirmEmail(w http.ResponseWriter, r *http.Request, u *User) {
	var body struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || !validCode(strings.TrimSpace(body.Code)) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	if !throttle(w, r, "e:check:u:"+strconv.FormatInt(u.ID, 10), checkPerEmail) {
		return
	}
	ctx := r.Context()
	c, err := checkCode(ctx, u.ID, PurposeChange, strings.TrimSpace(body.Code), true)
	if err != nil {
		if !errors.Is(err, errBadCode) {
			fail(w, r, "confirm code", err)
			return
		}
		httpx.Error(w, r, http.StatusBadRequest, "invalid_code", MsgCode)
		return
	}
	var out *User
	errTaken := errors.New("taken")
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		a, err := findAccount(ctx, tx, c.Email, true)
		if err != nil {
			return err
		}
		if a != nil && a.ID != u.ID {
			if !a.pending() {
				return errTaken
			}
			if _, err := tx.Exec(ctx, `delete from users where id = $1`, a.ID); err != nil {
				return err
			}
		}
		bump := 0
		if u.Email != nil {
			bump = 1
		}
		out, err = ScanUser(tx.QueryRow(ctx, `update users set email = $2, email_verified_at = now(),
			session_version = session_version + $3 where id = $1 returning `+UserColumns, u.ID, c.Email, bump))
		if err != nil {
			return err
		}
		if c.PasswordHash != nil {
			if err := setPassword(ctx, tx, u.ID, *c.PasswordHash); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `delete from auth_codes where user_id = $1 and purpose <> 'change_email'`, u.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
			values ($1, 'email_link', $1, null, '{}')`, u.ID)
		return err
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, errTaken), errors.As(err, &pgErr) && pgErr.Code == "23505":
		httpx.Error(w, r, http.StatusConflict, "email_taken", MsgTaken)
		return
	case err != nil:
		fail(w, r, "confirm email", err)
		return
	}
	startSession(w, r, out, false)
}

// mustPool returns the pool; db.Pool's error (no DATABASE_URL) surfaces on first use.
func mustPool(ctx context.Context) poolQuerier {
	p, err := db.Pool(ctx)
	if err != nil {
		return errQuerier{err}
	}
	return p
}

type poolQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type errQuerier struct{ err error }

func (e errQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return errRow(e) }

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }
