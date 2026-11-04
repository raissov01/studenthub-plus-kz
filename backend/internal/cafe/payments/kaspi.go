package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
)

// Kaspi talks to the kaspi-pos sidecar (services/kaspi-pos) over the internal compose network. Every call
// carries X-Internal-Api-Key; payment calls carry the café's cashier session headers. Never log headers or
// sidecar bodies: both carry tokens.
type Kaspi struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
}

// sessionEvicted is Kaspi's StatusCode when the cashier logged in on another device.
const sessionEvicted = -101001

// KaspiFromEnv is nil unless CAFE_KASPI_API_KEY (the sidecar's INTERNAL_API_KEY, 32+ chars) and a valid
// CAFE_KASPI_SESSION_KEY are set.
func KaspiFromEnv() *Kaspi {
	key := config.Str("CAFE_KASPI_API_KEY", "")
	if len(key) < 32 || SessionKey() == nil {
		return nil
	}
	return &Kaspi{
		BaseURL: strings.TrimRight(config.Str("CAFE_KASPI_URL", "http://kaspi-pos:3000"), "/"),
		APIKey:  key,
		Timeout: time.Duration(config.Int("CAFE_KASPI_TIMEOUT_MS", 10000)) * time.Millisecond,
	}
}

func (k *Kaspi) Name() string { return "kaspi" }

type jsonObj = map[string]any

func (k *Kaspi) request(ctx context.Context, method, path string, query url.Values, body any, s *Session) (jsonObj, int, error) {
	u := k.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(raw)
	}
	timeout := k.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Internal-Api-Key", k.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s != nil {
		req.Header.Set("X-Token-SN", s.TokenSN)
		req.Header.Set("X-Vtoken-Secret", s.VtokenSecret)
		req.Header.Set("X-Profile-Id", s.ProfileID)
	}
	c := k.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ErrTimeout
		}
		return nil, 0, fmt.Errorf("%w: kaspi-pos unreachable", ErrUpstream)
	}
	defer res.Body.Close()
	out := jsonObj{}
	dec := json.NewDecoder(io.LimitReader(res.Body, 1<<20))
	dec.UseNumber() // Kaspi operation ids are long integers
	_ = dec.Decode(&out)
	sidecarErr, _ := out["error"].(string)
	switch {
	case res.StatusCode == http.StatusUnauthorized && sidecarErr == "unauthorized":
		// the sidecar's own key check: our misconfiguration, not the café's session
		return nil, res.StatusCode, fmt.Errorf("%w: kaspi-pos rejected X-Internal-Api-Key", ErrNotConfigured)
	case res.StatusCode == http.StatusUnauthorized:
		return nil, res.StatusCode, ErrSessionExpired
	case res.StatusCode == http.StatusBadRequest:
		return nil, res.StatusCode, fmt.Errorf("%w: %s", ErrInvalid, clip(sidecarErr))
	case res.StatusCode >= 300:
		return nil, res.StatusCode, fmt.Errorf("%w: kaspi-pos HTTP %d %s", ErrUpstream, res.StatusCode, clip(sidecarErr))
	}
	return out, res.StatusCode, nil
}

// data checks a raw Kaspi answer ({StatusCode, Data, Message}) and returns Data.
func data(body jsonObj) (jsonObj, error) {
	code := num(body["StatusCode"])
	if code == sessionEvicted {
		return nil, ErrSessionExpired
	}
	d, ok := body["Data"].(map[string]any)
	if code != 0 || !ok {
		msg, _ := body["Message"].(string)
		if msg == "" {
			msg = fmt.Sprintf("Kaspi StatusCode %d", code)
		}
		return nil, fmt.Errorf("%w: %s", ErrUpstream, clip(msg))
	}
	return d, nil
}

func (k *Kaspi) session(m Merchant) (*Session, error) {
	if m.Session == nil || m.Session.TokenSN == "" || m.Session.VtokenSecret == "" {
		return nil, ErrNoSession
	}
	return m.Session, nil
}

var (
	// ReOperation is a Kaspi operation id as stored and accepted from the sidecar.
	ReOperation = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
	reDigits    = regexp.MustCompile(`^[0-9]{1,30}$`)
)

// CreateInvoice makes a Kaspi QR for the amount in the café's name. Kaspi QR has no comment field, so the
// order is matched by the returned operation id only.
func (k *Kaspi) CreateInvoice(ctx context.Context, m Merchant, _ int64, amount int64) (*Invoice, error) {
	s, err := k.session(m)
	if err != nil {
		return nil, err
	}
	tenge, err := Tenge(amount)
	if err != nil {
		return nil, err
	}
	body, _, err := k.request(ctx, http.MethodPost, "/api/qr/create", nil, jsonObj{"amount": tenge}, s)
	if err != nil {
		return nil, err
	}
	d, err := data(body)
	if err != nil {
		return nil, err
	}
	inv := &Invoice{OperationID: idString(d["QrOperationId"]), Amount: amount}
	inv.PayURL, _ = d["QrToken"].(string)
	inv.QR, _ = d["QrOriginalToken"].(string)
	if inv.QR == "" {
		inv.QR = inv.PayURL
	}
	if !ReOperation.MatchString(inv.OperationID) || !strings.HasPrefix(inv.PayURL, "https://") {
		return nil, fmt.Errorf("%w: Kaspi QR without operation id/link", ErrUpstream)
	}
	if exp, ok := d["ExpireDate"].(string); ok {
		if t, err := time.Parse(time.RFC3339, exp); err == nil {
			inv.ExpiresAt = &t
		}
	}
	return inv, nil
}

// qrStates maps Kaspi QR statuses (services/kaspi-pos src/polling.js); anything unknown and final is a failure.
var qrStates = map[string]State{
	"QrTokenCreated": StatePending, "Wait": StatePending,
	"Processed":        StatePaid,
	"QrTokenDiscarded": StateExpired, "Expired": StateExpired,
}

// StateOf reduces a raw Kaspi QR status.
func StateOf(raw string) State {
	if s, ok := qrStates[raw]; ok {
		return s
	}
	if raw == "" {
		return StatePending
	}
	return StateFailed
}

func (k *Kaspi) Status(ctx context.Context, m Merchant, operationID string) (*Status, error) {
	s, err := k.session(m)
	if err != nil {
		return nil, err
	}
	if !ReOperation.MatchString(operationID) {
		return nil, ErrInvalid
	}
	body, _, err := k.request(ctx, http.MethodGet, "/api/qr/status", url.Values{"qrOperationId": {operationID}}, nil, s)
	if err != nil {
		return nil, err
	}
	d, err := data(body)
	if err != nil {
		return nil, err
	}
	raw, _ := d["Status"].(string)
	st := &Status{OperationID: operationID, Raw: raw, State: StateOf(raw)}
	if a := num(d["Amount"]); a > 0 {
		st.Amount = a * 100
	}
	return st, nil
}

// Refund asks Kaspi to return the payment. Kaspi does not say whether a refusal is about the cashier's
// rights, so every refusal is ErrRefundNotAllowed: the café returns the money by hand (docs/cafe.md).
func (k *Kaspi) Refund(ctx context.Context, m Merchant, operationID string, amount int64) error {
	s, err := k.session(m)
	if err != nil {
		return err
	}
	tenge, err := Tenge(amount)
	if err != nil {
		return err
	}
	if !reDigits.MatchString(operationID) {
		return ErrInvalid
	}
	body, _, err := k.request(ctx, http.MethodPost, "/api/refund/create", nil, jsonObj{"qrOperationId": operationID, "returnAmount": tenge}, s)
	if err != nil {
		if errors.Is(err, ErrUpstream) || errors.Is(err, ErrInvalid) {
			return fmt.Errorf("%w: %v", ErrRefundNotAllowed, err)
		}
		return err
	}
	code := num(body["StatusCode"])
	if code == sessionEvicted {
		return ErrSessionExpired
	}
	if code != 0 {
		msg, _ := body["Message"].(string)
		return fmt.Errorf("%w: %s", ErrRefundNotAllowed, clip(msg))
	}
	return nil
}

// ---------- cashier login (admin panel) ----------

// NormalizePhone turns a KZ mobile number into the 10-digit 7XXXXXXXXX form Kaspi accepts
// ("+7 701 123 45 67", "87011234567", "77011234567", "7011234567"); "" when it is not one.
func NormalizePhone(in string) string {
	var d strings.Builder
	for _, r := range in {
		if r >= '0' && r <= '9' {
			d.WriteRune(r)
		}
	}
	s := d.String()
	if len(s) == 11 && (s[0] == '7' || s[0] == '8') {
		s = s[1:]
	}
	if len(s) == 10 && s[0] == '7' {
		return s
	}
	return ""
}

// LoginStart: init + phone; Kaspi sends an SMS to the cashier number. Returns the processId for LoginVerify.
func (k *Kaspi) LoginStart(ctx context.Context, phone string) (string, error) {
	p := NormalizePhone(phone)
	if p == "" {
		return "", fmt.Errorf("%w: phone", ErrInvalid)
	}
	init, _, err := k.request(ctx, http.MethodPost, "/api/auth/init", nil, jsonObj{}, nil)
	if err != nil {
		return "", err
	}
	pid, _ := init["processId"].(string)
	if pid == "" {
		return "", fmt.Errorf("%w: no processId", ErrUpstream)
	}
	sent, _, err := k.request(ctx, http.MethodPost, "/api/auth/send-phone", nil, jsonObj{"phoneNumber": p, "processId": pid}, nil)
	if err != nil {
		return "", err
	}
	if ok, _ := sent["success"].(bool); !ok {
		code, _ := sent["errorCode"].(string)
		if code == "OldVersionToUpdate" {
			return "", fmt.Errorf("%w: OldVersionToUpdate (raise APP_VERSION/APP_BUILD of kaspi-pos)", ErrUpstream)
		}
		return "", fmt.Errorf("%w: SMS not sent %s", ErrInvalid, clip(code))
	}
	return pid, nil
}

// Login is a finished cashier login.
type Login struct {
	Session        Session
	Phone          string
	OrgName        string
	OrganizationID int64
}

func (k *Kaspi) LoginVerify(ctx context.Context, processID, otp string) (*Login, error) {
	r, _, err := k.request(ctx, http.MethodPost, "/api/auth/verify-otp", nil, jsonObj{"otp": otp, "processId": processID}, nil)
	if err != nil {
		return nil, err
	}
	if ok, _ := r["success"].(bool); !ok {
		return nil, fmt.Errorf("%w: wrong or expired SMS code", ErrInvalid)
	}
	l := &Login{}
	l.Session.TokenSN, _ = r["tokenSN"].(string)
	l.Session.VtokenSecret, _ = r["vtokenSecret"].(string)
	l.Session.ProfileID = idString(r["profileId"])
	l.Phone, _ = r["phone"].(string)
	l.OrgName, _ = r["orgName"].(string)
	l.OrganizationID = num(r["organizationId"])
	if l.Session.TokenSN == "" || l.Session.VtokenSecret == "" || l.Session.ProfileID == "" {
		return nil, fmt.Errorf("%w: login finished without a full session", ErrUpstream)
	}
	return l, nil
}

// OrgContext picks one business of a cashier number that serves several (experimental, docs/cafe.md):
// the same session, that business's profileId.
func (k *Kaspi) OrgContext(ctx context.Context, s Session, organizationID int64) (profileID, orgName string, err error) {
	r, _, err := k.request(ctx, http.MethodPost, "/api/session/org-context", nil, jsonObj{"organizationId": organizationID}, &s)
	if err != nil {
		return "", "", err
	}
	profileID = idString(r["profileId"])
	orgName, _ = r["orgName"].(string)
	if profileID == "" {
		return "", "", fmt.Errorf("%w: no profile for organization %d", ErrUpstream, organizationID)
	}
	return profileID, orgName, nil
}

// Check asks Kaspi whether the session still works.
func (k *Kaspi) Check(ctx context.Context, s Session) error {
	r, _, err := k.request(ctx, http.MethodGet, "/api/session/check", nil, nil, &s)
	if err != nil {
		return err
	}
	if ok, _ := r["active"].(bool); !ok {
		return ErrSessionExpired
	}
	return nil
}

// ---------- helpers ----------

func num(v any) int64 {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return int64(f)
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return int64(n)
	}
	return 0
}

func idString(v any) string {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	}
	return ""
}

func clip(s string) string {
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
