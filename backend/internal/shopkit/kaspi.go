package shopkit

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
)

// ---------- the shop's own Kaspi cashier session ----------

// Sessions is one table of encrypted cashier sessions (cafe_kaspi_sessions, print_kaspi_sessions), keyed by the
// shop id column. Every shop has its own row: money always goes to that shop's own Kaspi Pay account.
type Sessions struct {
	Table string // e.g. cafe_kaspi_sessions; a constant, never input
	Key   string // e.g. cafe_id
	Name  string // for log lines: "cafe", "print"
}

// ErrNoMerchant: no session, a broken one or one that cannot be decrypted. The shop logs in again.
var ErrNoMerchant = errors.New("shopkit: no Kaspi session")

// Merchant loads and decrypts a shop's cashier session.
func (s Sessions) Merchant(ctx context.Context, shopID int64) (payments.Merchant, error) {
	m := payments.Merchant{CafeID: shopID}
	key := payments.SessionKey()
	if key == nil {
		return m, ErrNoMerchant
	}
	var ct string
	var broken bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select ciphertext, broken from `+s.Table+` where `+s.Key+` = $1`, shopID).Scan(&ct, &broken)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && broken) {
		return m, ErrNoMerchant
	}
	if err != nil {
		return m, err
	}
	sess, err := payments.Decrypt(ct, key)
	if err != nil {
		return m, ErrNoMerchant
	}
	m.Session = sess
	return m, nil
}

// MarkBroken flags the session after Kaspi rejected it; reports whether it was working until now (so the
// caller tells the managers once per breakage).
func (s Sessions) MarkBroken(ctx context.Context, shopID int64, cause error) bool {
	var changed bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+s.Table+` set broken = true, last_error = left($2, 300), updated_at = now() where `+s.Key+` = $1 and not broken`, shopID, cause.Error())
		changed = err == nil && tag.RowsAffected() > 0
		return err
	})
	if err != nil {
		log.Printf("%s: mark session broken %d: %v", s.Name, shopID, err)
	}
	if changed {
		log.Printf("%s: Kaspi session of shop %d stopped working: %v", s.Name, shopID, cause)
	}
	return changed
}

// KaspiState is what the admin sees of a session (never the session itself).
type KaspiState struct {
	Configured  bool       `json:"configured"`
	LoggedIn    bool       `json:"loggedIn"`
	Broken      bool       `json:"broken"`
	PhoneMasked string     `json:"phoneMasked"`
	OrgName     string     `json:"orgName"`
	LastError   *string    `json:"lastError"`
	UpdatedAt   *time.Time `json:"updatedAt"`
}

// CashierLogin is the part of the Kaspi client the admin login screens use.
type CashierLogin interface {
	LoginStart(ctx context.Context, phone string) (string, error)
	LoginVerify(ctx context.Context, processID, otp string) (*payments.Login, error)
	OrgContext(ctx context.Context, s payments.Session, organizationID int64) (string, string, error)
	Check(ctx context.Context, s payments.Session) error
}

// ---------- admin: the cashier login (the shop added our cashier number in Kaspi Pay) ----------

var (
	MsgKaspiOff   = httpx.Msg{KK: "Kaspi байланысы бапталмаған (CAFE_KASPI_*).", RU: "Связь с Kaspi не настроена (CAFE_KASPI_*)."}
	MsgKaspiLogin = httpx.Msg{KK: "Kaspi кіру сәтсіз: нөмірді не SMS кодын тексеріңіз.", RU: "Вход в Kaspi не удался: проверьте номер или SMS-код."}
)

// KaspiAdmin serves the four admin endpoints of one shop kind:
// POST …/{id}/kaspi/login/start, POST …/{id}/kaspi/login/verify, POST …/{id}/kaspi/check, DELETE …/{id}/kaspi.
type KaspiAdmin struct {
	Sessions Sessions
	// OrgID returns the shop's optional Kaspi OrganizationId; pgx.ErrNoRows when the shop does not exist.
	OrgID func(ctx context.Context, shopID int64) (*int64, error)
	// Cashier is the sidecar client (nil while not configured).
	Cashier func() CashierLogin
	// Broken is called after a failed check (the caller marks the session broken and tells the managers).
	Broken func(ctx context.Context, shopID int64, cause error)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (k KaspiAdmin) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("%s: %s: %v", k.Sessions.Name, what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func (k KaspiAdmin) cashier(w http.ResponseWriter, r *http.Request) CashierLogin {
	c := k.Cashier()
	if c == nil || payments.SessionKey() == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "kaspi_not_configured", MsgKaspiOff)
		return nil
	}
	return c
}

func (k KaspiAdmin) LoginStart(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r)
	var in struct {
		Phone string `json:"phone"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || payments.NormalizePhone(in.Phone) == "" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_phone", MsgKaspiLogin)
		return
	}
	c := k.cashier(w, r)
	if c == nil {
		return
	}
	if _, err := k.OrgID(r.Context(), id); err != nil {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	// one budget for every shop kind: the cashier number receives the SMS
	if ok, retry := ratelimit.Allow(r.Context(), "cafe:kaspi-login:"+strconv.FormatInt(u.ID, 10), 6, time.Hour); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	pid, err := c.LoginStart(r.Context(), in.Phone)
	if err != nil {
		log.Printf("%s: Kaspi login start for shop %d: %v", k.Sessions.Name, id, err)
		httpx.JSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"code": "kaspi_login",
			"message": Pick(httpx.Lang(r), MsgKaspiLogin.KK, MsgKaspiLogin.RU), "detail": Trim(err.Error(), 160)}})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"processId": pid})
}

var reOTP = regexp.MustCompile(`^[0-9]{4,8}$`)

func (k KaspiAdmin) LoginVerify(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r)
	var in struct {
		ProcessID string `json:"processId"`
		OTP       string `json:"otp"`
	}
	if !ok || httpx.Decode(r, &in, 2<<10) != nil || in.ProcessID == "" || len(in.ProcessID) > 200 || !reOTP.MatchString(in.OTP) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", MsgKaspiLogin)
		return
	}
	c := k.cashier(w, r)
	if c == nil {
		return
	}
	ctx := r.Context()
	orgID, err := k.OrgID(ctx, id)
	if err != nil {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	l, err := c.LoginVerify(ctx, in.ProcessID, in.OTP)
	if err != nil {
		log.Printf("%s: Kaspi login verify for shop %d: %v", k.Sessions.Name, id, err)
		httpx.Error(w, r, http.StatusBadGateway, "kaspi_login", MsgKaspiLogin)
		return
	}
	orgName := l.OrgName
	// one number in several businesses: this shop's business, by its OrganizationId
	if orgID != nil && *orgID != 0 && *orgID != l.OrganizationID {
		pid, name, err := c.OrgContext(ctx, l.Session, *orgID)
		if err != nil {
			log.Printf("%s: Kaspi org-context %d for shop %d: %v", k.Sessions.Name, *orgID, id, err)
			httpx.Error(w, r, http.StatusBadGateway, "kaspi_org", MsgKaspiLogin)
			return
		}
		l.Session.ProfileID, orgName = pid, name
	}
	key := payments.SessionKey()
	ct, err := payments.Encrypt(l.Session, key)
	if err != nil {
		k.fail(w, r, "encrypt session", err)
		return
	}
	hash := payments.PhoneHash(l.Phone, key)
	t, col := k.Sessions.Table, k.Sessions.Key
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into `+t+` (`+col+`, ciphertext, phone_hash, phone_masked, org_name, created_by)
			values ($1, $2, $3, $4, $5, $6) on conflict (`+col+`) do update set ciphertext = excluded.ciphertext, phone_hash = excluded.phone_hash,
			phone_masked = excluded.phone_masked, org_name = excluded.org_name, broken = false, last_error = null, created_by = excluded.created_by, updated_at = now()`,
			id, ct, hash, payments.MaskPhone(l.Phone), Trim(orgName, 200), u.ID); err != nil {
			return err
		}
		// a new login of the same number replaces the old token on Kaspi's side: other shops of the number get the
		// fresh token with their own profile
		rows, err := tx.Query(ctx, `select `+col+`, ciphertext from `+t+` where phone_hash = $1 and `+col+` <> $2 for update`, hash, id)
		if err != nil {
			return err
		}
		type other struct {
			id int64
			ct string
		}
		others, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (other, error) {
			var o other
			return o, row.Scan(&o.id, &o.ct)
		})
		if err != nil {
			return err
		}
		for _, o := range others {
			old, err := payments.Decrypt(o.ct, key)
			if err != nil {
				continue
			}
			fresh := l.Session
			fresh.ProfileID = old.ProfileID
			nct, err := payments.Encrypt(fresh, key)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `update `+t+` set ciphertext = $2, broken = false, last_error = null, updated_at = now() where `+col+` = $1`, o.id, nct); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		k.fail(w, r, "save session", err)
		return
	}
	log.Printf("%s: admin %d logged in the Kaspi cashier of shop %d (%s)", k.Sessions.Name, u.ID, id, payments.MaskPhone(l.Phone))
	httpx.JSON(w, http.StatusOK, map[string]any{"phoneMasked": payments.MaskPhone(l.Phone), "orgName": orgName})
}

func (k KaspiAdmin) Check(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	c := k.cashier(w, r)
	if c == nil {
		return
	}
	ctx := r.Context()
	var ct string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select ciphertext from `+k.Sessions.Table+` where `+k.Sessions.Key+` = $1`, id).Scan(&ct)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.JSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	if err != nil {
		k.fail(w, r, "kaspi check", err)
		return
	}
	s, err := payments.Decrypt(ct, payments.SessionKey())
	if err == nil {
		err = c.Check(ctx, *s)
	}
	if err != nil {
		k.Broken(ctx, id, err)
		httpx.JSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	_ = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update `+k.Sessions.Table+` set broken = false, last_error = null, updated_at = now() where `+k.Sessions.Key+` = $1`, id)
		return err
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"active": true})
}

func (k KaspiAdmin) Logout(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `delete from `+k.Sessions.Table+` where `+k.Sessions.Key+` = $1`, id)
		return err
	})
	if err != nil {
		k.fail(w, r, "kaspi logout", err)
		return
	}
	log.Printf("%s: admin %d removed the Kaspi session of shop %d", k.Sessions.Name, u.ID, id)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- one sidecar, one webhook, several shop kinds ----------

// OperationHandler reconciles a webhook for a Kaspi operation that is not a café order, inside the webhook's
// transaction (the delivery row is already stored once). handled=false: not this shop kind's operation.
// after runs once the transaction committed (notifications, refunds).
type OperationHandler func(ctx context.Context, tx pgx.Tx, op string, st payments.State, raw string, amount int64) (outcome string, after func(context.Context), handled bool, err error)

var (
	opMu       sync.RWMutex
	opHandlers []OperationHandler
)

// RegisterOperations adds a shop kind to the kaspi-pos webhook (POST /api/cafe/kaspi/webhook): the sidecar has a
// single WEBHOOK_URL, so the café's endpoint asks every registered kind about an operation it does not know.
func RegisterOperations(h OperationHandler) {
	opMu.Lock()
	defer opMu.Unlock()
	opHandlers = append(opHandlers, h)
}

// ReconcileOther runs the registered handlers for an operation the café does not know.
func ReconcileOther(ctx context.Context, tx pgx.Tx, op string, st payments.State, raw string, amount int64) (string, func(context.Context), bool, error) {
	opMu.RLock()
	hs := append([]OperationHandler(nil), opHandlers...)
	opMu.RUnlock()
	for _, h := range hs {
		outcome, after, handled, err := h(ctx, tx, op, st, raw, amount)
		if err != nil || handled {
			return outcome, after, handled, err
		}
	}
	return "", nil, false, nil
}
