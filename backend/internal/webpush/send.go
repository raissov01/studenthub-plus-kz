package webpush

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
)

// Enabled reports whether push is switched on (FEATURE_WEBPUSH) and fully configured.
func Enabled() bool { return Default() != nil }

var (
	cacheMu  sync.Mutex
	cacheKey string
	cached   *Sender
)

// Default is the Sender built from the environment, or nil when push is off or misconfigured
// (a bad key is logged once per distinct configuration by the caller of Enabled, never silently sent).
func Default() *Sender {
	if !config.Bool("FEATURE_WEBPUSH", false) {
		return nil
	}
	pub, priv, sub := config.Str("VAPID_PUBLIC_KEY", ""), config.Str("VAPID_PRIVATE_KEY", ""), config.Str("VAPID_SUBJECT", "")
	if pub == "" || priv == "" || sub == "" {
		return nil
	}
	key := pub + "\x00" + priv + "\x00" + sub
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if key != cacheKey {
		cacheKey, cached = key, nil
		if v, err := ParseVAPID(pub, priv, sub); err == nil {
			cached = &Sender{VAPID: v, Client: pushClient()}
		}
	}
	return cached
}

// pushClient posts only to public addresses and never follows a redirect: the endpoint is chosen by the
// browser, so without this a subscription could make the API call kaspi-pos, itself, other services on the
// host or cloud metadata (directly or through a 30x from an attacker's https server).
func pushClient() *http.Client {
	d := &net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil || !publicIP(net.ParseIP(host)) {
			return errors.New("webpush: endpoint is not a public address")
		}
		return nil
	}}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = d.DialContext
	return &http.Client{Timeout: 10 * time.Second, Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// cgnat is 100.64.0.0/10 (shared address space), not covered by net.IP.IsPrivate.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() &&
		!cgnat.Contains(ip)
}

// PublicConfig is the "webPush" object of GET /api/config.
func PublicConfig() map[string]any {
	if s := Default(); s != nil {
		return map[string]any{"enabled": true, "publicKey": s.VAPID.Public}
	}
	return map[string]any{"enabled": false}
}

// Message is the JSON the service worker receives (frontend/public/sw.js, "push" event).
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Icon  string `json:"icon,omitempty"`
	// Tag collapses repeats on the device: a retried delivery replaces, never duplicates.
	Tag string `json:"tag,omitempty"`
}

// Payload marshals m, shortening the body until it fits MaxPayload.
func (m Message) Payload() ([]byte, error) {
	if m.Icon == "" {
		m.Icon = "/icons/icon-192.png"
	}
	const maxBody = 1000
	if utf8.RuneCountInString(m.Body) > maxBody {
		m.Body = string([]rune(m.Body)[:maxBody-1]) + "…"
	}
	for {
		b, err := json.Marshal(m)
		if err != nil || len(b) <= MaxPayload {
			return b, err
		}
		r := []rune(m.Body)
		if len(r) < 10 {
			return nil, errors.New("webpush: message too large")
		}
		m.Body = string(r[:len(r)*3/4]) + "…"
	}
}

// Outcome classifies a push service response.
type Outcome int

const (
	Delivered Outcome = iota // 201/202: accepted by the push service
	Gone                     // 404/410: subscription expired or unsubscribed; delete it
	Rejected                 // other 4xx: this message will never be accepted
	Retry                    // 429, 5xx, network: try again later
)

// Sender posts encrypted messages to push services.
type Sender struct {
	VAPID  *VAPID
	Client *http.Client
	TTL    time.Duration // how long the push service keeps an undelivered message (default 24h)
}

// Subscription is one browser's push endpoint.
type Subscription struct {
	ID       int64
	Endpoint string
	Keys     Keys
}

// CheckEndpoint accepts only an absolute https URL without credentials (the push service
// chosen by the browser). It is the only URL the server ever POSTs a push to.
func CheckEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 1024 || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("webpush: endpoint must be an https URL")
	}
	return nil
}

func audience(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", errors.New("webpush: bad endpoint")
	}
	return u.Scheme + "://" + u.Host, nil
}

// Send encrypts payload for sub and posts it. The error carries the status for logs; it
// never contains the endpoint (a capability URL) or keys.
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, urgency string) (Outcome, error) {
	body, err := Encrypt(payload, sub.Keys)
	if err != nil {
		return Rejected, err
	}
	aud, err := audience(sub.Endpoint)
	if err != nil {
		return Gone, err
	}
	auth, err := s.VAPID.Authorization(aud, time.Now())
	if err != nil {
		return Retry, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Gone, errors.New("webpush: bad endpoint")
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if urgency == "" {
		urgency = "normal"
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Authorization", auth)
	c := s.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL is the subscription's capability; keep it out of logs
		}
		return Retry, fmt.Errorf("webpush transport: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	return classify(res.StatusCode), statusErr(res.StatusCode)
}

func classify(code int) Outcome {
	switch {
	case code >= 200 && code < 300:
		return Delivered
	case code == http.StatusNotFound || code == http.StatusGone:
		return Gone
	case code == http.StatusTooManyRequests || code >= 500:
		return Retry
	default:
		return Rejected
	}
}

func statusErr(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	return fmt.Errorf("webpush: push service answered %d %s", code, strings.ToLower(http.StatusText(code)))
}
