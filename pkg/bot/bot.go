// Package bot is the Telegram deadline reminder bot: POST /api/telegram (webhook) and
// GET /api/cron (daily job). Students are rows in users keyed by telegram_id.
package bot

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Config is read from the environment on every request (cheap, and tests can vary it).
type Config struct {
	Token         string
	WebhookSecret string
	CronSecret    string
	SiteURL       string
	ContentURL    string // where /api/content is read from; the API's own address on the server
	APIBase       string
	Admins        map[string]bool
	Digest        bool
}

func ConfigFromEnv() Config {
	admins := map[string]bool{}
	for _, id := range config.List("ADMIN_IDS", "") {
		admins[id] = true
	}
	return Config{
		Token:         config.Str("BOT_TOKEN", ""),
		WebhookSecret: config.Str("WEBHOOK_SECRET", ""),
		CronSecret:    config.Str("CRON_SECRET", ""),
		SiteURL:       config.SiteURL(),
		ContentURL:    config.Str("CONTENT_URL", config.SiteURL()),
		APIBase:       strings.TrimRight(config.Str("TELEGRAM_API_BASE", "https://api.telegram.org"), "/"),
		Admins:        admins,
		Digest:        config.Bool("DIGEST_ENABLED", false),
	}
}

// Bot holds what one invocation needs. Pool may be nil until a request needs the database.
type Bot struct {
	Cfg  Config
	Pool *pgxpool.Pool
	HTTP *http.Client
	Now  func() time.Time
}

func (b *Bot) client() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return http.DefaultClient
}

func (b *Bot) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Routes wires the bot into the API mux using env config and the shared pool.
func Routes(mux *http.ServeMux) {
	get := func(r *http.Request) (*Bot, error) {
		p, err := db.Pool(r.Context())
		return &Bot{Cfg: ConfigFromEnv(), Pool: p}, err
	}
	mux.HandleFunc("POST /api/telegram", func(w http.ResponseWriter, r *http.Request) {
		b := &Bot{Cfg: ConfigFromEnv()}
		if !b.webhookAuthorized(r) {
			httpx.JSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
			return
		}
		b, err := get(r)
		if err != nil {
			log.Printf("telegram: database: %v", err)
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]bool{"ok": false})
			return
		}
		b.ServeWebhook(w, r)
	})
	mux.HandleFunc("GET /api/cron", func(w http.ResponseWriter, r *http.Request) {
		b := &Bot{Cfg: ConfigFromEnv()}
		if !b.cronAuthorized(r) {
			httpx.JSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
			return
		}
		b, err := get(r)
		if err != nil {
			log.Printf("cron: database: %v", err)
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]bool{"ok": false})
			return
		}
		b.ServeCron(w, r)
	})
}

// Register wires this Bot (fixed config and pool) into mux; used by tests.
func (b *Bot) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/telegram", b.ServeWebhook)
	mux.HandleFunc("GET /api/cron", b.ServeCron)
}

func (b *Bot) webhookAuthorized(r *http.Request) bool {
	return safeEqual(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"), b.Cfg.WebhookSecret)
}

func (b *Bot) cronAuthorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && safeEqual(token, b.Cfg.CronSecret)
}

// ServeWebhook handles one Telegram update (secret checked again here, in constant time).
func (b *Bot) ServeWebhook(w http.ResponseWriter, r *http.Request) {
	if !b.webhookAuthorized(r) {
		httpx.JSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
		return
	}
	var up Update
	if err := httpx.Decode(r, &up, 1<<20); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]bool{"ok": false})
		return
	}
	if err := b.HandleUpdate(r.Context(), &up); err != nil {
		// Answer 200 anyway: a non-2xx makes Telegram redeliver the same update again and again.
		log.Printf("telegram update %d failed: %v", up.UpdateID, err)
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeCron runs the daily job (Vercel Cron sends Authorization: Bearer <CRON_SECRET>).
func (b *Bot) ServeCron(w http.ResponseWriter, r *http.Request) {
	if !b.cronAuthorized(r) {
		httpx.JSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
		return
	}
	res, err := b.RunCron(r.Context(), b.now())
	if err != nil {
		log.Printf("cron failed: %v", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]bool{"ok": false})
		return
	}
	line, _ := json.Marshal(res)
	log.Printf("cron %s", line)
	httpx.JSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
		*CronResult
	}{true, res})
}

var errNoDB = errors.New("bot: no database pool")

func (b *Bot) ready() error {
	if b.Pool == nil {
		return errNoDB
	}
	return nil
}
