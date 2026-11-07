package webpush

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
)

func init() {
	me.RegisterExport("pushSubscriptions", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select id, endpoint, ua, created_at, last_ok, failures from push_subscriptions where user_id = app_user_id() order by id")
	})
}

var (
	MsgEndpoint = httpx.Msg{KK: "Браузер жазылымы дұрыс емес. Бетті жаңартып, қайталаңыз.", RU: "Некорректная подписка браузера. Обновите страницу и повторите."}
	MsgNoSubs   = httpx.Msg{KK: "Бұл аккаунтта браузер хабарламалары қосылмаған.", RU: "Браузерные уведомления для этого аккаунта не включены."}
)

// Routes wires POST/DELETE /api/me/push (this browser's subscription) and POST
// /api/me/push/test. Off (404, like an unknown route) unless Enabled.
func Routes(mux httpx.Router) {
	mux.Handle("POST /api/me/push", require(auth.Write(auth.Need(subscribe))))
	mux.Handle("DELETE /api/me/push", require(auth.Write(auth.Need(unsubscribe))))
	mux.Handle("POST /api/me/push/test", require(auth.Write(auth.Need(sendTest))))
}

func require(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Enabled() {
			httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
			return
		}
		h(w, r)
	})
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("webpush: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

type subscriptionJSON struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// browser shortens the User-Agent to something a student recognizes in the data export.
func browser(ua string) string {
	ua = strings.TrimSpace(ua)
	for _, b := range []string{"Edg/", "OPR/", "YaBrowser/", "Firefox/", "Chrome/", "Version/"} {
		if i := strings.Index(ua, b); i >= 0 {
			name := strings.TrimSuffix(b, "/")
			if name == "Version" {
				name = "Safari"
			}
			v := ua[i+len(b):]
			if j := strings.IndexAny(v, ". "); j >= 0 {
				v = v[:j]
			}
			ua = name + " " + v
			break
		}
	}
	for utf8.RuneCountInString(ua) > 200 {
		ua = string([]rune(ua)[:200])
	}
	return ua
}

func subscribe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body subscriptionJSON
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if CheckEndpoint(body.Endpoint) != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_subscription", MsgEndpoint)
		return
	}
	k, err := ParseKeys(body.Keys.P256dh, body.Keys.Auth)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_subscription", MsgEndpoint)
		return
	}
	ctx := r.Context()
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		return Save(ctx, tx, u.ID, body.Endpoint, k, browser(r.UserAgent()))
	}); err != nil {
		fail(w, r, "subscribe", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func unsubscribe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := httpx.Decode(r, &body, 4<<10); err != nil || body.Endpoint == "" || len(body.Endpoint) > 1024 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var removed bool
	if err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		removed, err = Remove(ctx, tx, u.ID, body.Endpoint)
		return err
	}); err != nil {
		fail(w, r, "unsubscribe", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true, "removed": removed})
}

// testText is the test notification in the UI language (en: ?lang=en, which the English UI adds).
func testText(r *http.Request) (title, body string) {
	switch {
	case r.URL.Query().Get("lang") == "en" || strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "en"):
		return "SIVA", "Browser notifications are on. This is a test."
	case httpx.Lang(r) == "ru":
		return "SIVA", "Уведомления в браузере включены. Это проверка."
	default:
		return "SIVA", "Браузер хабарламалары қосулы. Бұл — тексеру."
	}
}

// sendTest pushes one message to every subscription of the account, outside the notify
// budget (it is the student's own action), at most 5 times in 10 minutes.
func sendTest(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if ok, retry := ratelimit.Allow(ctx, "push:test:"+strconv.FormatInt(u.ID, 10), 5, 10*time.Minute); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	pool, err := db.Pool(ctx)
	if err != nil {
		fail(w, r, "test pool", err)
		return
	}
	subs, err := List(ctx, pool, u.ID)
	if err != nil {
		fail(w, r, "test list", err)
		return
	}
	if len(subs) == 0 {
		httpx.Error(w, r, http.StatusConflict, "no_subscription", MsgNoSubs)
		return
	}
	title, text := testText(r)
	payload, err := Message{Title: title, Body: text, URL: "/settings", Tag: "studenthub-test"}.Payload()
	if err != nil {
		fail(w, r, "test payload", err)
		return
	}
	res := Default().SendAll(context.WithoutCancel(ctx), pool, subs, payload, "normal")
	httpx.JSON(w, http.StatusOK, map[string]int{"delivered": res.Delivered, "gone": res.Gone, "failed": res.Failed})
}
