package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
)

type authorOut struct {
	Name        string  `json:"name"`
	Username    *string `json:"username"`
	TelegramURL *string `json:"telegramUrl"` // https://t.me/<username> — the "Жазу 💬" button
}

type itemOut struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Desc         string     `json:"desc"`
	Category     string     `json:"category"`
	Condition    string     `json:"condition"`
	University   string     `json:"university"`
	Photos       []string   `json:"photos"` // URLs under /api/market/photos/
	Status       string     `json:"status"`
	HiddenReason *string    `json:"hiddenReason,omitempty"` // only in the owner's / admin's views
	CreatedAt    time.Time  `json:"createdAt"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	GivenAt      *time.Time `json:"givenAt,omitempty"`
	DaysAgo      int        `json:"daysAgo"`
	Mine         bool       `json:"mine"`
	// Signed-in viewers only; anonymous visitors get null and pickupHint "university"
	// ("университет аумағында").
	Pickup     *string    `json:"pickup"`
	PickupHint string     `json:"pickupHint"`
	Author     *authorOut `json:"author"`
}

const itemCols = `i.id, i.title, i.descr, i.category, i.condition, i.university, i.pickup_text, i.photos, i.status,
	i.hidden_reason, i.created_at, i.expires_at, i.given_at, i.user_id, coalesce(u.name, ''), u.tg_username`

// scanItem reads itemCols. viewer is nil for anonymous visitors: the publisher's name,
// username and the exact pickup place are only for signed-in students.
func scanItem(row pgx.Row, viewer *auth.User, private bool) (itemOut, error) {
	var it itemOut
	var pickup, name string
	var photos []string
	var owner int64
	var username *string
	err := row.Scan(&it.ID, &it.Title, &it.Desc, &it.Category, &it.Condition, &it.University, &pickup, &photos, &it.Status,
		&it.HiddenReason, &it.CreatedAt, &it.ExpiresAt, &it.GivenAt, &owner, &name, &username)
	if err != nil {
		return it, err
	}
	it.Photos = make([]string, 0, len(photos))
	for _, p := range photos {
		it.Photos = append(it.Photos, photoURL(p))
	}
	it.DaysAgo = int(time.Since(it.CreatedAt).Hours() / 24)
	it.PickupHint = "university"
	if viewer != nil {
		it.Mine = viewer.ID == owner
		it.Pickup = &pickup
		it.Author = &authorOut{Name: name, Username: username}
		if username != nil && *username != "" {
			u := "https://t.me/" + *username
			it.Author.TelegramURL = &u
		}
	}
	if !private && !it.Mine {
		it.HiddenReason = nil
	}
	return it, nil
}

func cleanText(s string, min, max int) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < min || n > max {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' {
			return "", false
		}
	}
	return s, true
}

// browse: GET /api/market/items?university=&category=&condition=&q=&before=&limit=
// university defaults to the viewer's own ("all" = every university). Newest first; page with
// before=<last id>.
func browse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := auth.Current(r)
	qs := r.URL.Query()
	uni := strings.TrimSpace(qs.Get("university"))
	if uni == "" && v != nil {
		uni = deref(v.University)
	}
	if uni == "all" {
		uni = ""
	}
	cat, cond := qs.Get("category"), qs.Get("condition")
	q := strings.TrimSpace(qs.Get("q"))
	before, _ := strconv.ParseInt(qs.Get("before"), 10, 64)
	limit, err := strconv.Atoi(qs.Get("limit"))
	if err != nil || limit < 1 || limit > 48 {
		limit = 24
	}
	if (cat != "" && !contains(Categories, cat)) || (cond != "" && !contains(Conditions, cond)) ||
		utf8.RuneCountInString(uni) > 120 || utf8.RuneCountInString(q) > 64 {
		bad(w, r)
		return
	}
	like := ""
	if q != "" {
		like = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	}
	out := []itemOut{}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+itemCols+`
			from market_items i join users u on u.id = i.user_id
			where i.status in ('active', 'reserved') and i.expires_at > now() and u.deleted_at is null
				and ($1 = '' or i.university = $1) and ($2 = '' or i.category = $2) and ($3 = '' or i.condition = $3)
				and ($4 = '' or i.title ilike $4 or i.descr ilike $4) and ($5 = 0 or i.id < $5)
			order by i.id desc limit $6`, uni, cat, cond, like, before, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			it, err := scanItem(rows, v, false)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "browse", err)
		return
	}
	resp := map[string]any{"items": out, "university": uni, "season": features.MarketSeason(time.Now())}
	if len(out) == limit {
		resp["nextBefore"] = out[len(out)-1].ID
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// getItem: GET /api/market/items/{id} — active/reserved items for everyone; other states
// only for the owner and admins.
func getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	v := auth.Current(r)
	var viewerID int64
	admin := false
	if v != nil {
		viewerID, admin = v.ID, v.Role == "admin"
	}
	var it itemOut
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRow(ctx, `select `+itemCols+` from market_items i join users u on u.id = i.user_id
			where i.id = $1 and ((i.status in ('active', 'reserved') and i.expires_at > now()) or i.user_id = $2 or $3)`,
			id, viewerID, admin), v, admin)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		internal(w, r, "item", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
}

type itemIn struct {
	Title     *string  `json:"title"`
	Desc      *string  `json:"desc"`
	Category  *string  `json:"category"`
	Condition *string  `json:"condition"`
	Pickup    *string  `json:"pickup"`
	Photos    []string `json:"photos"`
	Status    *string  `json:"status"`
}

// create: POST /api/market/items {title, desc, category, condition, pickup, photos:[names]}.
// There is no price field; unknown fields (e.g. "price") are rejected.
func create(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in itemIn
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.Title == nil || in.Category == nil || in.Condition == nil || in.Status != nil {
		bad(w, r)
		return
	}
	title, ok1 := cleanText(*in.Title, 1, 60)
	desc, ok2 := cleanText(deref(in.Desc), 0, 300)
	pickup, ok3 := cleanText(deref(in.Pickup), 0, 120)
	if !ok1 || !ok2 || !ok3 || !contains(Categories, *in.Category) || !contains(Conditions, *in.Condition) || len(in.Photos) > 3 {
		bad(w, r)
		return
	}
	seen := map[string]bool{}
	for _, p := range in.Photos {
		if !nameRe.MatchString(p) || seen[p] {
			fail(w, r, "bad_photo", http.StatusBadRequest)
			return
		}
		seen[p] = true
	}
	if in.Photos == nil {
		in.Photos = []string{}
	}
	ctx := r.Context()
	var it itemOut
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var id int64
		var status string
		err := tx.QueryRow(ctx, `select id, status from market_item_create($1, $2, $3, $4, $5, $6, $7, $8)`,
			u.ID, title, desc, *in.Category, *in.Condition, pickup, in.Photos, dailyLimit()).Scan(&id, &status)
		if err != nil {
			return err
		}
		if status == "pending" {
			sub, err := moderation.Submit(ctx, tx, KindItem, map[string]any{"itemId": id, "title": title, "desc": desc,
				"category": *in.Category, "photos": in.Photos, "reason": "new_account"}, &u.ID, deref(u.University))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `update market_items set submission_id = $2 where id = $1`, id, sub); err != nil {
				return err
			}
		}
		it, err = scanItem(tx.QueryRow(ctx, `select `+itemCols+` from market_items i join users u on u.id = i.user_id where i.id = $1`, id), u, true)
		return err
	})
	if code := raised(err); code != "" {
		status := http.StatusForbidden
		switch code {
		case "daily_limit":
			status = http.StatusTooManyRequests
		case "bad_photo":
			status = http.StatusBadRequest
		}
		fail(w, r, code, status)
		return
	}
	if err != nil {
		internal(w, r, "create", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"item": it, "pending": it.Status == "pending"})
}

// patchItem: PATCH /api/market/items/{id} {title?, desc?, category?, condition?, pickup?,
// status?: "active"|"reserved"} — owner only, through RLS: someone else's item, or the
// owner's hidden/pending/expired/given item, simply matches no row (404).
func patchItem(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in itemIn
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || !ok || in.Photos != nil {
		bad(w, r)
		return
	}
	sets := []string{}
	args := []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if in.Title != nil {
		s, ok := cleanText(*in.Title, 1, 60)
		if !ok {
			bad(w, r)
			return
		}
		add("title", s)
	}
	if in.Desc != nil {
		s, ok := cleanText(*in.Desc, 0, 300)
		if !ok {
			bad(w, r)
			return
		}
		add("descr", s)
	}
	if in.Pickup != nil {
		s, ok := cleanText(*in.Pickup, 0, 120)
		if !ok {
			bad(w, r)
			return
		}
		add("pickup_text", s)
	}
	if in.Category != nil {
		if !contains(Categories, *in.Category) {
			bad(w, r)
			return
		}
		add("category", *in.Category)
	}
	if in.Condition != nil {
		if !contains(Conditions, *in.Condition) {
			bad(w, r)
			return
		}
		add("condition", *in.Condition)
	}
	if in.Status != nil {
		if *in.Status != "active" && *in.Status != "reserved" {
			bad(w, r)
			return
		}
		add("status", *in.Status)
	}
	if len(sets) == 0 {
		bad(w, r)
		return
	}
	n, err := ownerExec(r.Context(), u.ID, "update market_items set "+strings.Join(sets, ", ")+" where id = $1", args...)
	if err != nil {
		internal(w, r, "patch", err)
		return
	}
	if n == 0 {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	writeOwnItem(w, r, u, id)
}

// ownerExec runs one statement as app_user (RLS: own rows only) and returns rows affected.
func ownerExec(ctx context.Context, userID int64, sql string, args ...any) (int64, error) {
	var n int64
	err := db.WithUser(ctx, userID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		n = tag.RowsAffected()
		return err
	})
	return n, err
}

func writeOwnItem(w http.ResponseWriter, r *http.Request, u *auth.User, id int64) {
	ctx := r.Context()
	var it itemOut
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRow(ctx, `select `+itemCols+` from market_items i join users u on u.id = i.user_id
			where i.id = $1 and i.user_id = $2`, id, u.ID), u, true)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		internal(w, r, "item", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"item": it})
}

// deleteItem: DELETE /api/market/items/{id} — the owner removes the item and its photos.
func deleteItem(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var photos []string
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `delete from market_items where id = $1 returning photos`, id).Scan(&photos)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	if err != nil {
		internal(w, r, "delete", err)
		return
	}
	var names []string
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `delete from market_photos where user_id = $1 and name = any($2) returning name`, u.ID, photos)
		if err != nil {
			return err
		}
		names, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		log.Printf("market: delete photo rows: %v", err)
	}
	removeFiles(append(names, photos...))
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// markGiven: POST /api/market/items/{id}/given — "Берілді ✅". Photos are deleted; the third
// given item earns the self-declared "generous" (Жомарт) badge.
func markGiven(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	n, err := ownerExec(ctx, u.ID, `update market_items set status = 'given' where id = $1`, id)
	if err != nil {
		internal(w, r, "given", err)
		return
	}
	if n == 0 {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	var names []string
	badge := false
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if names, err = dropPhotos(ctx, tx, id); err != nil {
			return err
		}
		var given int
		if err := tx.QueryRow(ctx, `select count(*) from market_items where user_id = $1 and status = 'given'`, u.ID).Scan(&given); err != nil {
			return err
		}
		if given >= generousAfter() {
			tag, err := tx.Exec(ctx, `insert into user_badges (user_id, badge, verified) values ($1, 'generous', false)
				on conflict (user_id, badge) do nothing`, u.ID)
			badge = tag.RowsAffected() == 1
			return err
		}
		return nil
	})
	if err != nil {
		internal(w, r, "given cleanup", err)
		return
	}
	removeFiles(names)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "badgeAwarded": badge})
}

// extend: POST /api/market/items/{id}/extend — another 30 days from now (owner, active or
// reserved items).
func extend(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	n, err := ownerExec(r.Context(), u.ID, `update market_items set expires_at = now() + interval '30 days', reminded_at = null where id = $1`, id)
	if err != nil {
		internal(w, r, "extend", err)
		return
	}
	if n == 0 {
		fail(w, r, "item_not_found", http.StatusNotFound)
		return
	}
	writeOwnItem(w, r, u, id)
}

// mine: GET /api/market/mine — every own item in every state (RLS-scoped).
func mine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []itemOut{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+itemCols+` from market_items i join users u on u.id = i.user_id
			where i.user_id = app_user_id() order by i.id desc limit 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			it, err := scanItem(rows, u, true)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w, r, "mine", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// status: GET /api/market/me — can I publish, and if not, why (in check order).
func status(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	var rules bool
	var banUntil *time.Time
	var today int
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select
			exists (select 1 from market_rules where user_id = app_user_id()),
			(select until from market_bans where user_id = app_user_id() and until > now()),
			coalesce((select n from market_daily where user_id = app_user_id() and day = $1::date), 0)`,
			notify.Day(time.Now())).Scan(&rules, &banUntil, &today)
	})
	if err != nil {
		internal(w, r, "status", err)
		return
	}
	blockers := []string{}
	switch {
	case u.NeedsConsent():
		blockers = append(blockers, "consent_required")
	case deref(u.University) == "":
		blockers = append(blockers, "no_account")
	}
	if deref(u.Username) == "" {
		blockers = append(blockers, "no_username")
	}
	if u.IsAdult == nil || !*u.IsAdult {
		blockers = append(blockers, "minor")
	}
	if !rules {
		blockers = append(blockers, "no_rules")
	}
	if banUntil != nil {
		blockers = append(blockers, "banned")
	}
	if today >= dailyLimit() {
		blockers = append(blockers, "daily_limit")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"canPublish": len(blockers) == 0, "blockers": blockers, "rulesAccepted": rules,
		"hasUsername": deref(u.Username) != "", "banUntil": banUntil, "today": today, "dailyLimit": dailyLimit(),
	})
}

// acceptRules: POST /api/market/rules/accept — the one-time rules screen (free only,
// prohibited items, meet on campus or in a crowded place).
func acceptRules(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	if _, err := ownerExec(r.Context(), u.ID, `insert into market_rules (user_id) values (app_user_id()) on conflict do nothing`); err != nil {
		internal(w, r, "rules", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- reports ----

// report: POST /api/market/items/{id}/report {reason: selling|prohibited|scam|other, text?}.
// One report per student per item. The first report opens a market_report submission and
// pings admins; at MARKET_REPORT_HIDE (default 3) counted reporters the item is hidden until
// an admin decides.
func report(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Reason string `json:"reason"`
		Text   string `json:"text"`
	}
	if err := httpx.Decode(r, &in, 2<<10); err != nil || !ok || !contains(ReportReasons, in.Reason) {
		bad(w, r)
		return
	}
	text, ok := cleanText(in.Text, 0, 300)
	if !ok {
		bad(w, r)
		return
	}
	if u.NeedsConsent() {
		fail(w, r, "consent_required", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	code := ""
	var res struct {
		Already bool `json:"alreadyReported"`
		Hidden  bool `json:"hidden"`
	}
	first := false
	var title string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var owner int64
		var st string
		err := tx.QueryRow(ctx, `select user_id, status, title from market_items where id = $1 and status in ('active', 'reserved', 'hidden')
			for update`, id).Scan(&owner, &st, &title)
		if errors.Is(err, pgx.ErrNoRows) {
			code = "item_not_found"
			return nil
		}
		if err != nil {
			return err
		}
		if owner == u.ID {
			code = "own_item"
			return nil
		}
		reason := in.Reason
		if text != "" {
			reason += ": " + text
		}
		tag, err := tx.Exec(ctx, `insert into reports (target_kind, target_id, reporter_id, reason) values ($1, $2, $3, $4)
			on conflict do nothing`, TargetKind, strconv.FormatInt(id, 10), u.ID, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			res.Already = true
			return nil
		}
		var pending bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from submissions where kind = $1 and status = 'pending'
			and (payload->>'itemId')::bigint = $2)`, KindReport, id).Scan(&pending); err != nil {
			return err
		}
		if !pending {
			first = true
			var uni string
			_ = tx.QueryRow(ctx, `select university from market_items where id = $1`, id).Scan(&uni)
			if _, err := moderation.Submit(ctx, tx, KindReport, map[string]any{"itemId": id, "title": title, "ownerId": owner,
				"reason": in.Reason, "text": text}, &u.ID, uni); err != nil {
				return err
			}
		}
		n, err := moderation.CountedReports(ctx, tx, TargetKind, strconv.FormatInt(id, 10))
		if err != nil {
			return err
		}
		if n >= hideThreshold() && st != "hidden" {
			res.Hidden = true
			_, err = tx.Exec(ctx, `update market_items set status = 'hidden', hidden_reason = 'reports' where id = $1`, id)
		}
		res.Hidden = res.Hidden || st == "hidden"
		return err
	})
	if err != nil {
		internal(w, r, "report", err)
		return
	}
	if code != "" {
		status := http.StatusNotFound
		if code == "own_item" {
			status = http.StatusBadRequest
		}
		fail(w, r, code, status)
		return
	}
	if first {
		notifyAdmins(ctx, fmt.Sprintf("⚠️ Маркет: «%s» (#%d) затына шағым (%s). Кезек: %s",
			html.EscapeString(title), id, in.Reason, config.SiteURL()+"/#/admin/queue"))
	}
	httpx.JSON(w, http.StatusOK, res)
}

type itemPayload struct {
	ItemID int64 `json:"itemId"`
}

func payloadItem(s moderation.Submission) (int64, error) {
	var p itemPayload
	if err := json.Unmarshal(s.Payload, &p); err != nil {
		return 0, err
	}
	return p.ItemID, nil
}

// hideByAdmin hides an item for good: status hidden ('admin'), photos deleted.
func hideByAdmin(ctx context.Context, tx pgx.Tx, id int64) (owner int64, title string, err error) {
	err = tx.QueryRow(ctx, `update market_items set status = 'hidden', hidden_reason = 'admin' where id = $1
		returning user_id, title`, id).Scan(&owner, &title)
	if err != nil {
		return 0, "", err
	}
	names, err := dropPhotos(ctx, tx, id)
	if err != nil {
		return 0, "", err
	}
	removeFiles(names)
	return owner, title, nil
}

// dismissReports marks the item's reports groundless; an item hidden by reports (or by an
// admin) comes back unless it already expired.
func dismissReports(ctx context.Context, tx pgx.Tx, id int64) error {
	if _, err := tx.Exec(ctx, `update reports set dismissed = true where target_kind = $1 and target_id = $2`,
		TargetKind, strconv.FormatInt(id, 10)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update market_items set hidden_reason = null,
		status = case when expires_at > now() then 'active' else 'expired' end
		where id = $1 and status = 'hidden'`, id)
	return err
}

// upholdReport (admin approves the report): the item is hidden for good, the owner is told.
func upholdReport(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id, err := payloadItem(s)
	if err != nil {
		return err
	}
	owner, title, err := hideByAdmin(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t := html.EscapeString(title)
	tellOwner(ctx, owner, "Маркеттегі «"+t+"» затыңыз ережеге сай емес деп жасырылды.", "Ваша вещь «"+t+"» скрыта за нарушение правил маркета.")
	return nil
}

// dismissReport (admin rejects the report as groundless): reports dismissed, item visible again.
func dismissReport(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id, err := payloadItem(s)
	if err != nil {
		return err
	}
	return dismissReports(ctx, tx, id)
}

func approveItem(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id, err := payloadItem(s)
	if err != nil {
		return err
	}
	var owner int64
	var title string
	err = tx.QueryRow(ctx, `update market_items set status = 'active', expires_at = now() + interval '30 days'
		where id = $1 and status = 'pending' returning user_id, title`, id).Scan(&owner, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t := html.EscapeString(title)
	tellOwner(ctx, owner, "«"+t+"» затыңыз жарияланды. Рақмет!", "Ваша вещь «"+t+"» опубликована. Спасибо!")
	return nil
}

func rejectItem(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	id, err := payloadItem(s)
	if err != nil {
		return err
	}
	names, err := dropPhotos(ctx, tx, id)
	if err != nil {
		return err
	}
	var owner int64
	var title string
	err = tx.QueryRow(ctx, `delete from market_items where id = $1 and status = 'pending' returning user_id, title`, id).Scan(&owner, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	removeFiles(names)
	t, why := html.EscapeString(title), html.EscapeString(deref(s.Reason))
	tellOwner(ctx, owner, "«"+t+"» заты жарияланбады. Себебі: "+why, "Вещь «"+t+"» не опубликована. Причина: "+why)
	return nil
}

// tellOwner sends a direct message in the owner's language; failures are only logged.
func tellOwner(ctx context.Context, owner int64, kk, ru string) {
	p, err := db.Pool(ctx)
	if err != nil {
		return
	}
	var lang string
	if err := p.QueryRow(ctx, `select lang from users where id = $1`, owner).Scan(&lang); err != nil {
		return
	}
	text := kk
	if lang == "ru" {
		text = ru
	}
	if _, err := notify.Send(ctx, owner, notify.CatModeration, text, notify.Opts{}); err != nil {
		log.Printf("market: notify %d: %v", owner, err)
	}
}

func notifyAdmins(ctx context.Context, text string) {
	p, err := db.Pool(ctx)
	if err != nil {
		return
	}
	rows, err := p.Query(ctx, `select id from users where role = 'admin' and deleted_at is null`)
	if err != nil {
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return
	}
	for _, id := range ids {
		if _, err := notify.Send(ctx, id, notify.CatModeration, text, notify.Opts{}); err != nil {
			log.Printf("market: notify admin %d: %v", id, err)
		}
	}
}
