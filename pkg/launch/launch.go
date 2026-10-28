// Package launch provides server-only launch settings and privacy-preserving monitoring.
package launch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/cronrun"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/ratelimit"
	"github.com/raissov01/studenthub-plus-kz/pkg/settings"
)

func Routes(mux httpx.Router) {
	mux.HandleFunc("POST /api/client-errors", auth.Write(ingest))
	mux.HandleFunc("GET /api/admin/settings", auth.NeedRole(getSettings, "admin"))
	mux.HandleFunc("PUT /api/admin/settings", auth.Write(auth.NeedRole(putSettings, "admin")))
	mux.HandleFunc("GET /api/admin/health", auth.NeedRole(health, "admin"))
}

func internal(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusServiceUnavailable, "unavailable", httpx.MsgInternal)
}
func getSettings(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	show, err := settings.ShowDemo(r.Context())
	if err != nil {
		internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"showDemo": show})
}
func putSettings(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	var b struct {
		ShowDemo *bool `json:"showDemo"`
	}
	if err := strictDecode(r, &b); err != nil || b.ShowDemo == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	if err := settings.SetShowDemo(r.Context(), *b.ShowDemo); err != nil {
		internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"showDemo": *b.ShowDemo})
}
func strictDecode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return io.ErrUnexpectedEOF
	}
	return nil
}
func ingest(w http.ResponseWriter, r *http.Request) {
	// The limiter key is a daily HMAC, never the raw IP; client_errors contains neither.
	key := errorRateKey(httpx.ClientIP(r), time.Now())
	if ok, retry := ratelimit.Allow(r.Context(), key, 5, time.Minute); !ok {
		w.Header().Set("Retry-After", durationSeconds(retry))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	var b struct {
		Message string `json:"message"`
		Screen  string `json:"screen"`
		Browser string `json:"browser"`
	}
	if err := strictDecode(r, &b); err != nil || strings.TrimSpace(b.Message) == "" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `insert into client_errors (message,screen,browser) values ($1,$2,$3)`, cleanMessage(b.Message), cleanScreen(b.Screen), cleanBrowser(b.Browser))
		return err
	})
	if err != nil {
		internal(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type errorGroup struct {
	Message  string    `json:"message"`
	Count    int       `json:"count"`
	LastSeen time.Time `json:"lastSeen"`
}
type cronStatus struct {
	Endpoint    string     `json:"endpoint"`
	LastSuccess *time.Time `json:"lastSuccessAt"`
}
type healthReport struct {
	DBReachable     bool          `json:"dbReachable"`
	Errors          []errorGroup  `json:"errors"`
	CronRuns        []cronStatus  `json:"cronRuns"`
	ModerationQueue *int          `json:"moderationQueue"`
	Telegram        webhookStatus `json:"telegram"`
}

func health(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	out := healthReport{Errors: []errorGroup{}, CronRuns: []cronStatus{}}
	for _, path := range cronrun.Endpoints {
		out.CronRuns = append(out.CronRuns, cronStatus{Endpoint: path})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "select 1"); err != nil {
			return err
		}
		out.DBReachable = true
		rows, err := tx.Query(ctx, `select message, count(*), max(created_at) from client_errors where created_at >= now() - interval '24 hours' group by message order by count(*) desc, message limit 100`)
		if err != nil {
			return err
		}
		groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (errorGroup, error) {
			var g errorGroup
			err := row.Scan(&g.Message, &g.Count, &g.LastSeen)
			return g, err
		})
		if err != nil {
			return err
		}
		out.Errors = groups
		for i := range out.CronRuns {
			if err := tx.QueryRow(ctx, `select max(last_success_at) from cron_runs where endpoint=$1`, out.CronRuns[i].Endpoint).Scan(&out.CronRuns[i].LastSuccess); err != nil {
				return err
			}
		}
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from submissions where status='pending'`).Scan(&n); err != nil {
			return err
		}
		out.ModerationQueue = &n
		return nil
	})
	cancel()
	out.Telegram = webhook(r.Context())
	status := http.StatusOK
	if err != nil {
		status = http.StatusServiceUnavailable
	}
	httpx.JSON(w, status, out)
}

type webhookStatus struct {
	Configured     bool       `json:"configured"`
	Reachable      bool       `json:"reachable"`
	Installed      bool       `json:"installed"`
	PendingUpdates int        `json:"pendingUpdates"`
	LastErrorAt    *time.Time `json:"lastErrorAt"`
}

func webhook(ctx context.Context) webhookStatus {
	token := config.Str("BOT_TOKEN", "")
	out := webhookStatus{Configured: token != ""}
	if token == "" {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(config.Str("TELEGRAM_API_BASE", "https://api.telegram.org"), "/") + "/bot" + token + "/getWebhookInfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return out
	}
	// Do not follow redirects with a bot token in the path.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return out
	}
	defer res.Body.Close()
	var b struct {
		OK     bool `json:"ok"`
		Result struct {
			URL       string `json:"url"`
			Pending   int    `json:"pending_update_count"`
			LastError int64  `json:"last_error_date"`
		} `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&b) != nil || !b.OK {
		return out
	}
	out.Reachable = true
	out.Installed = b.Result.URL != ""
	out.PendingUpdates = b.Result.Pending
	if b.Result.LastError > 0 {
		t := time.Unix(b.Result.LastError, 0).UTC()
		out.LastErrorAt = &t
	}
	return out
}
