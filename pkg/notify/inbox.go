package notify

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
)

// The in-site inbox (04-foundation: «хабарлама алдымен notifications кестесіне жазылады»).
// Send, Direct/DirectAs and Digest/DigestAs write one row before deciding anything about
// Telegram, so a student without a bot chat (email-only, never linked, blocked the bot,
// category off, quiet hours, cap) still finds every message on the site.

const (
	inboxMaxText   = 4096
	inboxDedupe    = 14 * 24 * time.Hour // a digest retried by a cron is stored once in this window
	InboxRetention = 90 * 24 * time.Hour // read rows older than this are deleted (pkg/cronrun)
	inboxPageMax   = 50
	inboxPageDef   = 20
)

type linkKey struct{}

// WithLink attaches a site path ("/event/12") to the inbox rows written with ctx, so the
// inbox item can open the right page. Without it the first site URL in the text is used.
func WithLink(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, linkKey{}, path)
}

var (
	reTag  = regexp.MustCompile(`<[^>]*>`)
	reLink = regexp.MustCompile(`^/[^/\\][^\s<>"']*$`)
)

// PlainText turns a bot message (Telegram HTML) into the inbox text: tags dropped, entities
// decoded. The site must still escape it when rendering.
func PlainText(s string) string {
	s = html.UnescapeString(reTag.ReplaceAllString(s, ""))
	s = strings.TrimSpace(s)
	for utf8.RuneCountInString(s) > inboxMaxText {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

// CleanLink accepts a site path ("/x", not "//host") and returns "" otherwise.
func CleanLink(p string) string {
	if len(p) > 300 || !reLink.MatchString(p) {
		return ""
	}
	return p
}

// linkFor: the explicit WithLink path, else the path of the first SITE_URL link in text.
func linkFor(ctx context.Context, text string) string {
	if p, ok := ctx.Value(linkKey{}).(string); ok {
		return CleanLink(p)
	}
	site := strings.TrimRight(config.SiteURL(), "/")
	i := strings.Index(text, site+"/")
	if site == "" || i < 0 {
		return ""
	}
	rest := text[i+len(site):]
	if j := strings.IndexAny(rest, " \t\n<>\"'"); j >= 0 {
		rest = rest[:j]
	}
	rest = strings.TrimRight(rest, ".,;:!?)»")
	return CleanLink(rest)
}

// record writes the inbox row. dedupe (digests, which crons retry until the bot message goes
// out) skips it when the same text in the same category is already in the inbox from the
// last two weeks. Deleted or blocked accounts get nothing.
func record(ctx context.Context, q Querier, userID int64, c Category, raw string, dedupe bool) error {
	text := PlainText(raw)
	if text == "" {
		return nil
	}
	var link *string
	if l := linkFor(ctx, raw); l != "" {
		link = &l
	}
	_, err := q.Exec(ctx, `insert into notifications (user_id, category, text, link)
		select $1, $2, $3, $4 where exists (select 1 from users where id = $1 and deleted_at is null)
			and not ($5 and exists (select 1 from notifications where user_id = $1 and category = $2 and text = $3
				and created_at > now() - make_interval(secs => $6::float8)))`,
		userID, string(c), text, link, dedupe, inboxDedupe.Seconds())
	return err
}

// recordLogged writes the inbox row; a failure is logged and never blocks the bot message.
func recordLogged(ctx context.Context, q Querier, userID int64, c Category, text string, dedupe bool) {
	if err := record(ctx, q, userID, c, text, dedupe); err != nil {
		log.Printf("notify: inbox %d (%s): %v", userID, c, err)
	}
}

// Unread is the number of unread inbox rows (GET /api/me "inboxUnread").
func Unread(ctx context.Context, q Querier, userID int64) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and read_at is null`, userID).Scan(&n)
	return n, err
}

// PruneInbox deletes read rows older than InboxRetention; the daily cron runs it.
func PruneInbox(ctx context.Context, q Querier, now time.Time) (int64, error) {
	tag, err := q.Exec(ctx, `delete from notifications where read_at is not null and created_at < $1`, now.Add(-InboxRetention))
	return tag.RowsAffected(), err
}

func init() {
	me.RegisterExport("notifications", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select id, category, text, link, created_at, read_at from notifications where user_id = app_user_id() order by id")
	})
	me.InboxUnread = func(ctx context.Context, tx pgx.Tx, userID int64) (int, error) { return Unread(ctx, tx, userID) }
}

// InboxItem is one row of GET /api/me/inbox.
type InboxItem struct {
	ID        int64      `json:"id"`
	Category  string     `json:"category"`
	Text      string     `json:"text"`
	Link      *string    `json:"link"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt"`
	Read      bool       `json:"read"`
}

// getInbox: GET /api/me/inbox?before=<id>&limit=<1..50>&unread=1 — newest first.
func getInbox(w http.ResponseWriter, r *http.Request, u *auth.User) {
	qs := r.URL.Query()
	before, err := strconv.ParseInt(qs.Get("before"), 10, 64)
	if qs.Get("before") != "" && (err != nil || before <= 0) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	limit := inboxPageDef
	if s := qs.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > inboxPageMax {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		limit = n
	}
	onlyUnread := qs.Get("unread") == "1" || qs.Get("unread") == "true"
	items := []InboxItem{}
	var unread int
	err = db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, category, text, link, created_at, read_at from notifications
			where user_id = app_user_id() and ($1 = 0 or id < $1) and (not $2 or read_at is null)
			order by id desc limit $3`, before, onlyUnread, limit)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboxItem, error) {
			var it InboxItem
			err := row.Scan(&it.ID, &it.Category, &it.Text, &it.Link, &it.CreatedAt, &it.ReadAt)
			it.Read = it.ReadAt != nil
			return it, err
		})
		if err != nil {
			return err
		}
		unread, err = Unread(r.Context(), tx, u.ID)
		return err
	})
	if err != nil {
		fail(w, r, "inbox", err)
		return
	}
	resp := map[string]any{"items": items, "unread": unread, "nextBefore": nil}
	if len(items) == limit {
		resp["nextBefore"] = items[len(items)-1].ID
	}
	httpx.JSON(w, http.StatusOK, resp)
}

var errInboxBody = errors.New("inbox: bad body")

// markRead: POST /api/me/inbox/read {"ids": [..]} (at most 100) or {"all": true}.
func markRead(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	err := httpx.Decode(r, &body, 4<<10)
	if err == nil && (body.All == (len(body.IDs) > 0) || len(body.IDs) > 100) {
		err = errInboxBody
	}
	for _, id := range body.IDs {
		if id <= 0 {
			err = errInboxBody
		}
	}
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var marked int64
	var unread int
	err = db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update notifications set read_at = now()
			where user_id = app_user_id() and read_at is null and ($1 or id = any($2))`, body.All, body.IDs)
		if err != nil {
			return err
		}
		marked = tag.RowsAffected()
		unread, err = Unread(r.Context(), tx, u.ID)
		return err
	})
	if err != nil {
		fail(w, r, "inbox read", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "marked": marked, "unread": unread})
}
