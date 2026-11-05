package feeds

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Routes wires the admin side (/api/admin/feeds*), behind FEATURE_FEEDS and role admin. Pending
// items are approved / rejected / edited through the shared queue (/api/admin/queue/{submission}).
func Routes(mux httpx.Router) {
	// moderators read the feed and unpublish items; switching sources on and off stays with admins
	mod := func(h auth.Handler) http.Handler {
		return features.Require(Feature, auth.NeedScope(h, auth.ScopeModerator))
	}
	modWrite := func(h auth.Handler) http.Handler {
		return features.Require(Feature, auth.Write(auth.NeedScope(h, auth.ScopeModerator)))
	}
	write := func(h auth.Handler) http.Handler {
		return features.Require(Feature, auth.Write(auth.NeedRole(h, "admin")))
	}
	mux.Handle("GET /api/admin/feeds", mod(overview))
	mux.Handle("GET /api/admin/feeds/items", mod(listItems))
	mux.Handle("POST /api/admin/feeds/sources/{id}", write(setSource))
	mux.Handle("POST /api/admin/feeds/items/{id}/unpublish", modWrite(unpublish))
	mux.Handle("POST /api/admin/feeds/items/bulk", modWrite(bulk))
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("feeds: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

type sourceRow struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Handle    string     `json:"handle"`
	URL       string     `json:"url"`
	Name      string     `json:"name"`
	Category  string     `json:"category"`
	Scope     string     `json:"scope"`
	Trust     string     `json:"trust"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	LastOKAt  *time.Time `json:"last_ok_at"`
	LastError *string    `json:"last_error"`
	LastNew   int        `json:"last_new"`
	Posts     int        `json:"posts"`
	Published int        `json:"published"`
}

func overview(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	sources := []sourceRow{}
	counts := map[string]int{"pending": 0, "published": 0, "rejected": 0, "expired": 0}
	var usage struct {
		Calls    int     `json:"calls"`
		Tokens   int     `json:"tokens"`
		Failures int     `json:"failures"`
		Limit    int     `json:"limit"`
		AutoMin  float64 `json:"autopublish_min_confidence"`
		Channel  bool    `json:"channel"`
		AIOn     bool    `json:"ai"`
	}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select s.id, s.kind, s.handle, s.url, s.name, s.category, s.scope, s.trust, s.enabled,
				s.last_run_at, s.last_ok_at, s.last_error, s.last_new,
				(select count(*) from feed_posts p where p.source_id = s.id),
				(select count(*) from feed_items i where i.source_id = s.id and i.status = 'published')
			from feed_sources s order by s.enabled desc, s.name`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s sourceRow
			if err := rows.Scan(&s.ID, &s.Kind, &s.Handle, &s.URL, &s.Name, &s.Category, &s.Scope, &s.Trust, &s.Enabled,
				&s.LastRunAt, &s.LastOKAt, &s.LastError, &s.LastNew, &s.Posts, &s.Published); err != nil {
				rows.Close()
				return err
			}
			sources = append(sources, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		crows, err := tx.Query(ctx, `select status, count(*) from feed_items group by status`)
		if err != nil {
			return err
		}
		for crows.Next() {
			var st string
			var n int
			if err := crows.Scan(&st, &n); err != nil {
				crows.Close()
				return err
			}
			counts[st] = n
		}
		crows.Close()
		usage.Calls, usage.Tokens, usage.Failures, err = UsageToday(ctx, tx, time.Now())
		return err
	})
	if err != nil {
		fail(w, r, "overview", err)
		return
	}
	var c Collector
	c.Settings()
	usage.Limit, usage.AutoMin, usage.Channel = c.DailyTokens, c.AutoMin, c.Channel != ""
	usage.AIOn = config.Str("OPENAI_API_KEY", "") != ""
	httpx.JSON(w, http.StatusOK, map[string]any{"sources": sources, "counts": counts, "usage": usage})
}

// ItemRow is one row of GET /api/admin/feeds/items.
type ItemRow struct {
	ID           int64           `json:"id"`
	Status       string          `json:"status"`
	Category     string          `json:"category"`
	Source       string          `json:"source"`
	SourceID     string          `json:"source_id"`
	Data         json.RawMessage `json:"data"`
	Confidence   float64         `json:"confidence"`
	Auto         bool            `json:"auto"`
	SubmissionID *int64          `json:"submission_id"`
	Deadline     *string         `json:"deadline"`
	CreatedAt    time.Time       `json:"created_at"`
	PublishedAt  *time.Time      `json:"published_at"`
	Extra        json.RawMessage `json:"extra_sources"`
}

func listItems(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	if status == "" {
		status = StatusPublished
	}
	if status != StatusPending && status != StatusPublished && status != StatusRejected && status != StatusExpired {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	items := []ItemRow{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select i.id, i.status, i.category, s.name, s.id, i.data, i.confidence, i.auto, i.submission_id,
				i.deadline::text, i.created_at, i.published_at, i.extra_sources
			from feed_items i join feed_sources s on s.id = i.source_id
			where i.status = $1 order by coalesce(i.published_at, i.created_at) desc, i.id desc limit 100`, status)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it ItemRow
			if err := rows.Scan(&it.ID, &it.Status, &it.Category, &it.Source, &it.SourceID, &it.Data, &it.Confidence, &it.Auto, &it.SubmissionID,
				&it.Deadline, &it.CreatedAt, &it.PublishedAt, &it.Extra); err != nil {
				return err
			}
			items = append(items, it)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "items", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

var msgNoSource = httpx.Msg{KK: "Мұндай дереккөз табылмады.", RU: "Источник не найден."}

func setSource(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || body.Enabled == nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	id := r.PathValue("id")
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update feed_sources set enabled = $2, updated_at = now() where id = $1`, id, *body.Enabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		_, err = tx.Exec(r.Context(), `insert into audit_log (actor_id, action, meta) values ($1, 'feed_source', jsonb_build_object('source', $2::text, 'enabled', $3::boolean))`,
			admin.ID, id, *body.Enabled)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", msgNoSource)
	case err != nil:
		fail(w, r, "source", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "enabled": *body.Enabled})
	}
}

func unpublish(w http.ResponseWriter, r *http.Request, admin *auth.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	err = db.Tx(r.Context(), func(tx pgx.Tx) error { return Unpublish(r.Context(), tx, id, admin.ID) })
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
	case err != nil:
		fail(w, r, "unpublish", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "status": StatusRejected})
	}
}
