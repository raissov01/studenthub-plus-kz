// Package clubs serves the university clubs section (FEATURE_CLUBS): the public catalogue and
// club pages, join requests (or a direct Telegram link, per club), per-club admins
// («клуб жетекшісі», many-to-many: club_admins), moderated club news, the leader application
// that goes to the shared moderation queue, and site-admin CRUD. Club events are ordinary
// events with events.club_id (internal/events, wired in internal/server/glue_clubs.go).
//
// API reference and privacy rules: docs/clubs.md. Migration: 20261105000041_clubs.sql.
package clubs

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
)

// Directions are the catalogue filters; the frontend keeps the same list (lib/clubs.ts).
var Directions = []string{"it", "science", "debate", "sport", "art", "volunteer", "media", "business", "languages", "kvn", "culture", "other"}

const (
	KindLeader = "club_leader" // «Клуб жетекшісімін» application
	KindPost   = "club_post"   // club news waiting for moderation
)

// Notify sends to one student (community category: switchable, quiet hours, daily cap); tests replace it.
var Notify = notify.For(notify.CatCommunity)

// University is what a new club belongs to unless the admin says otherwise (CLUBS_UNIVERSITY).
func University() string {
	return config.Str("CLUBS_UNIVERSITY", "Сейфуллин университеті")
}

var (
	MsgNoClub   = httpx.Msg{KK: "Клуб табылмады.", RU: "Клуб не найден."}
	MsgInvalid  = httpx.Msg{KK: "Клуб деректері толық емес не қате.", RU: "Данные клуба неполные или неверные."}
	MsgNotAdmin = httpx.Msg{KK: "Мұны тек осы клубтың жетекшісі жасай алады.", RU: "Это может сделать только руководитель этого клуба."}
	MsgConsent  = httpx.Msg{KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."}
	MsgViaLink  = httpx.Msg{KK: "Бұл клубқа Telegram тобы арқылы қосыласыз.", RU: "В этот клуб вступают через Telegram-группу."}
	MsgNoReq    = httpx.Msg{KK: "Өтінім табылмады.", RU: "Заявка не найдена."}
	MsgPending  = httpx.Msg{KK: "Өтініміңіз әлдеқашан қаралып жатыр.", RU: "Ваша заявка уже на рассмотрении."}
	MsgAlready  = httpx.Msg{KK: "Сіз бұл клубтың жетекшісісіз.", RU: "Вы уже руководитель этого клуба."}
	MsgProof    = httpx.Msg{KK: "Дәлел жазыңыз (20–1000 таңба) және https:// сілтеме қосыңыз.", RU: "Опишите подтверждение (20–1000 символов) и добавьте ссылку https://."}
	MsgPost     = httpx.Msg{KK: "Жаңалық мәтіні 3–2000 таңба болуы керек.", RU: "Текст новости — 3–2000 символов."}
)

func init() {
	moderation.OnApprove(KindPost, approvePost)
	moderation.OnReject(KindPost, rejectPost)
	moderation.OnApprove(KindLeader, approveLeader)
	moderation.Notice(KindLeader, leaderNotice)
	moderation.Notice(KindPost, postNotice)

	me.RegisterExport("clubMemberships", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select club_id, status, message, created_at, decided_at from club_members where user_id = app_user_id() order by club_id")
	})
	me.RegisterExport("clubAdmin", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select club_id, granted_at from club_admins where user_id = app_user_id() order by club_id")
	})
	me.RegisterExport("clubPosts", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select id, club_id, body_kk, body_ru, link_url, status, created_at, published_at from club_posts where author_id = app_user_id() order by id")
	})
}

// Routes wires /api/clubs/* and /api/admin/clubs/*, all behind the "clubs" switch.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.Require("clubs", fn)) }
	h("GET /api/clubs", list)
	h("GET /api/clubs/my", auth.Need(mine))
	h("GET /api/clubs/{id}", detail)
	h("POST /api/clubs/{id}/join", auth.Write(auth.Need(join)))
	h("DELETE /api/clubs/{id}/join", auth.Write(auth.Need(leave)))
	h("POST /api/clubs/leader-applications", auth.Write(auth.Need(applyLeader)))
	// club admins (RLS: club_admins)
	h("PATCH /api/clubs/{id}", auth.Write(auth.Need(editClub)))
	h("GET /api/clubs/{id}/members", auth.Need(members))
	h("POST /api/clubs/{id}/requests/{userId}", auth.Write(auth.Need(decideRequest)))
	h("DELETE /api/clubs/{id}/members/{userId}", auth.Write(auth.Need(removeMember)))
	h("POST /api/clubs/{id}/posts", auth.Write(auth.Need(createPost)))
	// site admin
	h("GET /api/admin/clubs", auth.NeedRole(adminList, "admin"))
	h("POST /api/admin/clubs", auth.Write(auth.NeedRole(adminCreate, "admin")))
	h("PATCH /api/admin/clubs/{id}", auth.Write(auth.NeedRole(adminEdit, "admin")))
	h("POST /api/admin/clubs/{id}/admins", auth.Write(auth.NeedRole(adminGrant, "admin")))
	h("DELETE /api/admin/clubs/{id}/admins/{userId}", auth.Write(auth.NeedRole(adminRevoke, "admin")))
}

// ---------- model ----------

// Club is the API shape. LeaderContact is null unless the leader opted in (or the reader manages the club).
type Club struct {
	ID                  int64   `json:"id"`
	NameKK              string  `json:"name_kk"`
	NameRU              *string `json:"name_ru"`
	Direction           string  `json:"direction"`
	SummaryKK           *string `json:"summary_kk"`
	SummaryRU           *string `json:"summary_ru"`
	AboutKK             *string `json:"about_kk"`
	AboutRU             *string `json:"about_ru"`
	University          string  `json:"university"`
	BuildingID          *string `json:"building_id"`
	Room                *string `json:"room"`
	ScheduleKK          *string `json:"schedule_kk"`
	ScheduleRU          *string `json:"schedule_ru"`
	LeaderTitleKK       *string `json:"leader_title_kk"`
	LeaderTitleRU       *string `json:"leader_title_ru"`
	LeaderContact       *string `json:"leader_contact"`
	LeaderContactPublic bool    `json:"leader_contact_public"`
	TelegramURL         *string `json:"telegram_url"`
	InstagramURL        *string `json:"instagram_url"`
	JoinMode            string  `json:"join_mode"`
	JoinLink            *string `json:"join_link"`
	Recruiting          bool    `json:"recruiting"`
	Status              string  `json:"status"`
	SourceURL           *string `json:"source_url"`
	VerifiedAt          *string `json:"verified_at"`
	Members             int     `json:"members"`
	My                  *My     `json:"my,omitempty"`
}

// My is the signed-in student's relation to a club.
type My struct {
	Status string `json:"status"` // "" | pending | member
	Admin  bool   `json:"admin"`
}

const clubCols = `c.id, c.name_kk, c.name_ru, c.direction, c.summary_kk, c.summary_ru, c.about_kk, c.about_ru, c.university,
	c.building_id, c.room, c.schedule_kk, c.schedule_ru, c.leader_title_kk, c.leader_title_ru, c.leader_contact,
	c.leader_contact_public, c.telegram_url, c.instagram_url, c.join_mode, c.join_link, c.recruiting, c.status,
	c.source_url, to_char(c.verified_at, 'YYYY-MM-DD'),
	(select count(*) from club_members m where m.club_id = c.id and m.status = 'member')`

func scanClub(row pgx.Row) (*Club, error) {
	var c Club
	err := row.Scan(&c.ID, &c.NameKK, &c.NameRU, &c.Direction, &c.SummaryKK, &c.SummaryRU, &c.AboutKK, &c.AboutRU, &c.University,
		&c.BuildingID, &c.Room, &c.ScheduleKK, &c.ScheduleRU, &c.LeaderTitleKK, &c.LeaderTitleRU, &c.LeaderContact,
		&c.LeaderContactPublic, &c.TelegramURL, &c.InstagramURL, &c.JoinMode, &c.JoinLink, &c.Recruiting, &c.Status,
		&c.SourceURL, &c.VerifiedAt, &c.Members)
	return &c, err
}

// public hides what only the club's admins see.
func (c *Club) public() {
	if !c.LeaderContactPublic {
		c.LeaderContact = nil
	}
}

func (c *Club) name(lang string) string {
	if lang == "ru" && c.NameRU != nil && *c.NameRU != "" {
		return *c.NameRU
	}
	return c.NameKK
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("clubs: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func pathInt(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func load(ctx context.Context, tx pgx.Tx, id int64) (*Club, error) {
	return scanClub(tx.QueryRow(ctx, "select "+clubCols+" from clubs c where c.id = $1", id))
}

// IsAdmin reports whether userID administers the club (any status). Used by the events glue.
func IsAdmin(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, clubID, userID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `select exists (select 1 from club_admins a join users u on u.id = a.user_id
		where a.club_id = $1 and a.user_id = $2 and u.deleted_at is null)`, clubID, userID).Scan(&ok)
	return ok, err
}

// ManagedUniversity is events.ClubManager: ok when userID administers the active club, and the
// club's university. For any active club the university is returned, so the site admin path can use it.
func ManagedUniversity(ctx context.Context, userID, clubID int64) (string, bool) {
	var univ string
	var ok bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select c.university, exists (select 1 from club_admins a join users u on u.id = a.user_id
			where a.club_id = c.id and a.user_id = $2 and u.deleted_at is null)
			from clubs c where c.id = $1 and c.status = 'active'`, clubID, userID).Scan(&univ, &ok)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		log.Printf("clubs: managed %d/%d: %v", clubID, userID, err)
		return "", false
	}
	return univ, ok
}

// ---------- reading ----------

func myRelations(ctx context.Context, uid int64) (map[int64]*My, error) {
	out := map[int64]*My{}
	// RLS: own membership rows and own admin rows only
	err := db.WithUser(ctx, uid, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select club_id, status, false from club_members where user_id = app_user_id()
			union all select club_id, '', true from club_admins where user_id = app_user_id()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var st string
			var adm bool
			if err := rows.Scan(&id, &st, &adm); err != nil {
				return err
			}
			m := out[id]
			if m == nil {
				m = &My{}
				out[id] = m
			}
			if adm {
				m.Admin = true
			} else {
				m.Status = st
			}
		}
		return rows.Err()
	})
	return out, err
}

func list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.Current(r)
	out := []*Club{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "select "+clubCols+" from clubs c where c.status = 'active' order by c.recruiting desc, c.name_kk limit 500")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanClub(rows)
			if err != nil {
				return err
			}
			c.public()
			c.AboutKK, c.AboutRU = nil, nil // the card needs the summary only
			out = append(out, c)
		}
		return rows.Err()
	})
	if err == nil && u != nil {
		var mine map[int64]*My
		if mine, err = myRelations(ctx, u.ID); err == nil {
			for _, c := range out {
				c.My = mine[c.ID]
			}
		}
	}
	if err != nil {
		fail(w, r, "list", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"clubs": out, "directions": Directions})
}

// Post is a club news item.
type Post struct {
	ID          int64      `json:"id"`
	BodyKK      string     `json:"body_kk"`
	BodyRU      *string    `json:"body_ru"`
	LinkURL     *string    `json:"link_url"`
	Status      string     `json:"status"`
	Source      string     `json:"source"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at"`
}

func detail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	u := auth.Current(r)
	var c *Club
	var posts []Post
	pending := 0
	manage := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if c, err = load(ctx, tx, id); err != nil {
			return err
		}
		if u != nil {
			if manage, err = IsAdmin(ctx, tx, id, u.ID); err != nil {
				return err
			}
			manage = manage || u.Role == "admin"
		}
		if c.Status != "active" && !manage {
			return pgx.ErrNoRows
		}
		statuses := "('published')"
		if manage {
			statuses = "('published', 'pending')"
			if err := tx.QueryRow(ctx, `select count(*) from club_members where club_id = $1 and status = 'pending'`, id).Scan(&pending); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `select id, body_kk, body_ru, link_url, status, source, created_at, published_at from club_posts
			where club_id = $1 and status in `+statuses+` order by coalesce(published_at, created_at) desc limit 30`, id)
		if err != nil {
			return err
		}
		posts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Post, error) {
			var p Post
			return p, row.Scan(&p.ID, &p.BodyKK, &p.BodyRU, &p.LinkURL, &p.Status, &p.Source, &p.CreatedAt, &p.PublishedAt)
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	if err == nil && u != nil {
		var mine map[int64]*My
		if mine, err = myRelations(ctx, u.ID); err == nil {
			c.My = mine[id]
		}
	}
	if err != nil {
		fail(w, r, "detail", err)
		return
	}
	if !manage {
		c.public()
	}
	if posts == nil {
		posts = []Post{}
	}
	resp := map[string]any{"club": c, "posts": posts, "can_manage": manage}
	if manage {
		resp["pending_requests"] = pending
	}
	httpx.JSON(w, http.StatusOK, resp)
}

type myClub struct {
	ID        int64   `json:"id"`
	NameKK    string  `json:"name_kk"`
	NameRU    *string `json:"name_ru"`
	Direction string  `json:"direction"`
	Status    string  `json:"status"` // pending | member | admin
}

func mine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	rel, err := myRelations(ctx, u.ID)
	out := []myClub{}
	if err == nil && len(rel) > 0 {
		err = db.Tx(ctx, func(tx pgx.Tx) error {
			ids := make([]int64, 0, len(rel))
			for id := range rel {
				ids = append(ids, id)
			}
			rows, err := tx.Query(ctx, `select id, name_kk, name_ru, direction from clubs where id = any($1) order by name_kk`, ids)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var m myClub
				if err := rows.Scan(&m.ID, &m.NameKK, &m.NameRU, &m.Direction); err != nil {
					return err
				}
				m.Status = rel[m.ID].Status
				if rel[m.ID].Admin {
					m.Status = "admin"
				}
				out = append(out, m)
			}
			return rows.Err()
		})
	}
	if err != nil {
		fail(w, r, "mine", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"clubs": out})
}

// ---------- validation ----------

var (
	reBuilding = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	reTelegram = regexp.MustCompile(`^https://t\.me/[A-Za-z0-9_+/-]{2,100}$`)
	reInsta    = regexp.MustCompile(`^https://(www\.)?instagram\.com/[A-Za-z0-9_.]{1,60}/?$`)
	reHTTPS    = regexp.MustCompile(`^https://[^\s<>"']{3,500}$`)
	reDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// cleanText trims s and checks length; "" means null. Control characters other than \n and \t are refused.
func cleanText(s string, min, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return "", false
	}
	n := utf8.RuneCountInString(s)
	return s, n <= max && (n == 0 || n >= min)
}

type field struct {
	kind     string // text | bool | enum | re
	min, max int
	re       *regexp.Regexp
	enum     []string
	required bool // may not be cleared
	admin    bool // site admin only
}

var fields = map[string]field{
	"name_kk":               {kind: "text", min: 2, max: 120, required: true, admin: true},
	"name_ru":               {kind: "text", min: 2, max: 120, admin: true},
	"direction":             {kind: "enum", enum: Directions, required: true, admin: true},
	"university":            {kind: "text", min: 2, max: 120, required: true, admin: true},
	"status":                {kind: "enum", enum: []string{"active", "archived"}, required: true, admin: true},
	"source_url":            {kind: "re", re: reHTTPS, admin: true},
	"verified_at":           {kind: "re", re: reDate, admin: true},
	"summary_kk":            {kind: "text", min: 3, max: 300},
	"summary_ru":            {kind: "text", min: 3, max: 300},
	"about_kk":              {kind: "text", min: 3, max: 4000},
	"about_ru":              {kind: "text", min: 3, max: 4000},
	"building_id":           {kind: "re", re: reBuilding},
	"room":                  {kind: "text", min: 1, max: 60},
	"schedule_kk":           {kind: "text", min: 2, max: 200},
	"schedule_ru":           {kind: "text", min: 2, max: 200},
	"leader_title_kk":       {kind: "text", min: 2, max: 80},
	"leader_title_ru":       {kind: "text", min: 2, max: 80},
	"leader_contact":        {kind: "text", min: 3, max: 120},
	"leader_contact_public": {kind: "bool", required: true},
	"telegram_url":          {kind: "re", re: reTelegram},
	"instagram_url":         {kind: "re", re: reInsta},
	"join_mode":             {kind: "enum", enum: []string{"request", "link"}, required: true},
	"join_link":             {kind: "re", re: reTelegram},
	"recruiting":            {kind: "bool", required: true},
}

// sets turns a JSON object into "col = $n" pairs (args start after `offset`). Unknown keys, and
// admin-only keys when !admin, are refused, so a club admin cannot rename or archive a club.
func sets(in map[string]any, admin bool, offset int) ([]string, []any, string) {
	var cols []string
	var args []any
	for k, v := range in {
		f, ok := fields[k]
		if !ok || (f.admin && !admin) {
			return nil, nil, k
		}
		var val any
		switch {
		case v == nil:
			if f.required {
				return nil, nil, k
			}
		case f.kind == "bool":
			b, ok := v.(bool)
			if !ok {
				return nil, nil, k
			}
			val = b
		default:
			s, ok := v.(string)
			if !ok {
				return nil, nil, k
			}
			switch f.kind {
			case "text":
				if s, ok = cleanText(s, f.min, f.max); !ok {
					return nil, nil, k
				}
			case "enum":
				if !contains(f.enum, s) {
					return nil, nil, k
				}
			case "re":
				s = strings.TrimSpace(s)
				if s != "" && !f.re.MatchString(s) {
					return nil, nil, k
				}
			}
			if s == "" {
				if f.required {
					return nil, nil, k
				}
				break
			}
			val = s
		}
		args = append(args, val)
		cols = append(cols, fmt.Sprintf("%s = $%d", k, offset+len(args)))
	}
	return cols, args, ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func invalid(w http.ResponseWriter, r *http.Request, fieldName string) {
	httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{
		"code": "invalid_club", "field": fieldName, "message": pick(httpx.Lang(r), MsgInvalid),
	}})
}

func pick(lang string, m httpx.Msg) string {
	if lang == "ru" {
		return m.RU
	}
	return m.KK
}

// ---------- club admin: edit ----------

func editClub(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	var in map[string]any
	if err := httpx.Decode(r, &in, 32<<10); err != nil || len(in) == 0 {
		invalid(w, r, "")
		return
	}
	cols, args, bad := sets(in, false, 1)
	if bad != "" {
		invalid(w, r, bad)
		return
	}
	cols = append(cols, "updated_at = now()")
	q := "update clubs set " + strings.Join(cols, ", ") + " where id = $1"
	run := func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, q, append([]any{id}, args...)...)
		if err == nil && tag.RowsAffected() != 1 {
			return errNotAdmin
		}
		return err
	}
	// the site admin edits any club as the owner; everyone else runs under RLS (clubs_admin_update)
	var err error
	if u.Role == "admin" {
		err = db.Tx(ctx, run)
	} else {
		err = db.WithUser(ctx, u.ID, run)
	}
	writeEdit(w, r, id, err)
}

var errNotAdmin = errors.New("clubs: not a club admin")

func writeEdit(w http.ResponseWriter, r *http.Request, id int64, err error) {
	switch {
	case errors.Is(err, errNotAdmin) || pgCode(err) == "42501":
		httpx.Error(w, r, http.StatusForbidden, "not_club_admin", MsgNotAdmin)
		return
	case pgCode(err) == "23514" || pgCode(err) == "22001":
		invalid(w, r, "")
		return
	case err != nil:
		fail(w, r, "edit", err)
		return
	}
	var c *Club
	err = db.Tx(r.Context(), func(tx pgx.Tx) error { var e error; c, e = load(r.Context(), tx, id); return e })
	if err != nil {
		fail(w, r, "edit load", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"club": c})
}

// ---------- joining ----------

func join(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	_ = httpx.Decode(r, &body, 4<<10)
	msg, ok := cleanText(body.Message, 1, 300)
	if !ok {
		invalid(w, r, "message")
		return
	}
	var c *Club
	err := db.Tx(ctx, func(tx pgx.Tx) error { var e error; c, e = load(ctx, tx, id); return e })
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.Status != "active") {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	if err != nil {
		fail(w, r, "join load", err)
		return
	}
	if c.JoinMode != "request" {
		httpx.JSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"code": "join_via_link",
			"message": pick(httpx.Lang(r), MsgViaLink)}, "join_link": c.JoinLink})
		return
	}
	status, created := "pending", false
	err = db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `insert into club_members (club_id, user_id, message) values ($1, app_user_id(), nullif($2, ''))
			on conflict (club_id, user_id) do nothing`, id, msg)
		if err != nil {
			return err
		}
		created = tag.RowsAffected() == 1
		return tx.QueryRow(ctx, `select status from club_members where club_id = $1 and user_id = app_user_id()`, id).Scan(&status)
	})
	if pgCode(err) == "42501" {
		httpx.Error(w, r, http.StatusConflict, "join_via_link", MsgViaLink)
		return
	}
	if err != nil {
		fail(w, r, "join", err)
		return
	}
	if created {
		go tellAdmins(context.WithoutCancel(ctx), c)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"status": status, "created": created})
}

func clubLink(id int64) string { return "/club?id=" + strconv.FormatInt(id, 10) }

func tellAdmins(ctx context.Context, c *Club) {
	var ids []int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select user_id from club_admins where club_id = $1`, c.ID)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	if err != nil {
		log.Printf("clubs: admins of %d: %v", c.ID, err)
		return
	}
	link := config.SiteURL() + clubLink(c.ID)
	for _, id := range ids {
		text := fmt.Sprintf("🙋 «%s» клубына жаңа өтінім түсті.\n%s", html.EscapeString(c.NameKK), link)
		if err := Notify(notify.WithLink(ctx, clubLink(c.ID)), id, text); err != nil {
			log.Printf("clubs: notify admin %d: %v", id, err)
		}
	}
}

func leave(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	var n int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from club_members where club_id = $1 and user_id = app_user_id()`, id)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		fail(w, r, "leave", err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, http.StatusNotFound, "not_member", MsgNoReq)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- club admin: members ----------

// Member is one row of the members list (club admins only).
type Member struct {
	UserID    int64      `json:"user_id"`
	Name      *string    `json:"name"`
	Username  *string    `json:"username"`
	Faculty   *string    `json:"faculty"`
	Course    *int16     `json:"course"`
	Status    string     `json:"status"`
	Message   *string    `json:"message"`
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at"`
}

// members: the list exists only for the club's admins. Rows are read under RLS
// (club_members_admin_read): a non-admin gets nothing even if the Go check were skipped.
func members(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	var isAdmin bool
	if err := db.Tx(ctx, func(tx pgx.Tx) error { var e error; isAdmin, e = IsAdmin(ctx, tx, id, u.ID); return e }); err != nil {
		fail(w, r, "members check", err)
		return
	}
	if !isAdmin && u.Role != "admin" {
		httpx.Error(w, r, http.StatusForbidden, "not_club_admin", MsgNotAdmin)
		return
	}
	type row struct {
		uid              int64
		status           string
		msg              *string
		created, decided *time.Time
	}
	var rs []row
	read := func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select user_id, status, message, created_at, decided_at from club_members where club_id = $1
			order by status = 'member', created_at`, id)
		if err != nil {
			return err
		}
		rs, err = pgx.CollectRows(rows, func(x pgx.CollectableRow) (row, error) {
			var v row
			return v, x.Scan(&v.uid, &v.status, &v.msg, &v.created, &v.decided)
		})
		return err
	}
	var err error
	if isAdmin {
		err = db.WithUser(ctx, u.ID, read)
	} else {
		err = db.Tx(ctx, read)
	}
	out := []Member{}
	if err == nil && len(rs) > 0 {
		err = db.Tx(ctx, func(tx pgx.Tx) error {
			for _, v := range rs {
				m := Member{UserID: v.uid, Status: v.status, Message: v.msg, CreatedAt: *v.created, DecidedAt: v.decided}
				err := tx.QueryRow(ctx, `select name, tg_username, faculty, course from users where id = $1 and deleted_at is null`, v.uid).
					Scan(&m.Name, &m.Username, &m.Faculty, &m.Course)
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				out = append(out, m)
			}
			return nil
		})
	}
	if err != nil {
		fail(w, r, "members", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
}

func decideRequest(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok1 := pathInt(r, "id")
	uid, ok2 := pathInt(r, "userId")
	var body struct {
		Decision string `json:"decision"`
	}
	if err := httpx.Decode(r, &body, 1<<10); err != nil || !ok1 || !ok2 || (body.Decision != "approve" && body.Decision != "decline") {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var n int64
	run := func(tx pgx.Tx) error {
		var tag pgconn.CommandTag
		var err error
		if body.Decision == "approve" {
			tag, err = tx.Exec(ctx, `update club_members set status = 'member', decided_at = now(), decided_by = $3
				where club_id = $1 and user_id = $2 and status = 'pending'`, id, uid, u.ID)
		} else {
			tag, err = tx.Exec(ctx, `delete from club_members where club_id = $1 and user_id = $2 and status = 'pending'`, id, uid)
		}
		n = tag.RowsAffected()
		return err
	}
	isAdmin := false
	err := db.Tx(ctx, func(tx pgx.Tx) error { var e error; isAdmin, e = IsAdmin(ctx, tx, id, u.ID); return e })
	if err == nil {
		switch {
		case isAdmin:
			err = db.WithUser(ctx, u.ID, run) // RLS: club_members_admin_decide / _admin_delete
		case u.Role == "admin":
			err = db.Tx(ctx, run)
		default:
			httpx.Error(w, r, http.StatusForbidden, "not_club_admin", MsgNotAdmin)
			return
		}
	}
	if err != nil {
		fail(w, r, "decide", err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, http.StatusNotFound, "no_request", MsgNoReq)
		return
	}
	go tellStudent(context.WithoutCancel(ctx), id, uid, body.Decision == "approve")
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func tellStudent(ctx context.Context, clubID, uid int64, approved bool) {
	var c *Club
	lang := "kk"
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_ = tx.QueryRow(ctx, `select lang from users where id = $1`, uid).Scan(&lang)
		var e error
		c, e = load(ctx, tx, clubID)
		return e
	})
	if err != nil {
		log.Printf("clubs: tell %d: %v", uid, err)
		return
	}
	name := html.EscapeString(c.name(lang))
	var text string
	switch {
	case approved && lang == "ru":
		text = fmt.Sprintf("✅ Вас приняли в клуб «%s».", name)
	case approved:
		text = fmt.Sprintf("✅ Сіз «%s» клубына қабылдандыңыз.", name)
	case lang == "ru":
		text = fmt.Sprintf("Заявка в клуб «%s» не принята.", name)
	default:
		text = fmt.Sprintf("«%s» клубына өтініміңіз қабылданбады.", name)
	}
	if c.TelegramURL != nil && approved {
		text += "\n" + *c.TelegramURL
	}
	if err := Notify(notify.WithLink(ctx, clubLink(clubID)), uid, text); err != nil {
		log.Printf("clubs: notify student %d: %v", uid, err)
	}
}

func removeMember(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok1 := pathInt(r, "id")
	uid, ok2 := pathInt(r, "userId")
	if !ok1 || !ok2 {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	var n int64
	run := func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from club_members where club_id = $1 and user_id = $2`, id, uid)
		n = tag.RowsAffected()
		return err
	}
	var isAdmin bool
	err := db.Tx(ctx, func(tx pgx.Tx) error { var e error; isAdmin, e = IsAdmin(ctx, tx, id, u.ID); return e })
	if err == nil {
		switch {
		case isAdmin:
			err = db.WithUser(ctx, u.ID, run)
		case u.Role == "admin":
			err = db.Tx(ctx, run)
		default:
			httpx.Error(w, r, http.StatusForbidden, "not_club_admin", MsgNotAdmin)
			return
		}
	}
	if err != nil {
		fail(w, r, "remove", err)
		return
	}
	if n == 0 {
		httpx.Error(w, r, http.StatusNotFound, "no_request", MsgNoReq)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- club news (moderated) ----------

func createPost(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathInt(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
		return
	}
	var in struct {
		BodyKK  string `json:"body_kk"`
		BodyRU  string `json:"body_ru"`
		LinkURL string `json:"link_url"`
	}
	if err := httpx.Decode(r, &in, 16<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_post", MsgPost)
		return
	}
	kk, ok1 := cleanText(in.BodyKK, 3, 2000)
	ru, ok2 := cleanText(in.BodyRU, 3, 2000)
	link := strings.TrimSpace(in.LinkURL)
	if !ok1 || !ok2 || kk == "" || (link != "" && !reHTTPS.MatchString(link)) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_post", MsgPost)
		return
	}
	var c *Club
	if err := db.Tx(ctx, func(tx pgx.Tx) error { var e error; c, e = load(ctx, tx, id); return e }); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
			return
		}
		fail(w, r, "post load", err)
		return
	}
	var postID, subID int64
	// as app_user: club_posts_insert lets only this club's admins add a pending post, and
	// submissions_own_insert files the moderation item under the author
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `insert into club_posts (club_id, author_id, body_kk, body_ru, link_url)
			values ($1, app_user_id(), $2, nullif($3, ''), nullif($4, '')) returning id`, id, kk, ru, link).Scan(&postID); err != nil {
			return err
		}
		var err error
		subID, err = moderation.Submit(ctx, tx, KindPost, map[string]any{
			"post_id": postID, "club_id": id, "club": c.NameKK, "body_kk": kk, "body_ru": ru, "link_url": link,
		}, &u.ID, c.University)
		return err
	})
	if pgCode(err) == "42501" {
		httpx.Error(w, r, http.StatusForbidden, "not_club_admin", MsgNotAdmin)
		return
	}
	if err != nil {
		fail(w, r, "post", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"post_id": postID, "submission_id": subID, "status": "pending"})
}

func payloadInt(p map[string]any, k string) int64 {
	switch v := p[k].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

func payload(s moderation.Submission) map[string]any {
	var p map[string]any
	_ = jsonUnmarshal(s.Payload, &p)
	if p == nil {
		p = map[string]any{}
	}
	return p
}

func approvePost(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	_, err := tx.Exec(ctx, `update club_posts set status = 'published', published_at = now(), submission_id = $2
		where id = $1 and status = 'pending'`, payloadInt(payload(s), "post_id"), s.ID)
	return err
}

func rejectPost(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	_, err := tx.Exec(ctx, `update club_posts set status = 'rejected', submission_id = $2 where id = $1 and status = 'pending'`,
		payloadInt(payload(s), "post_id"), s.ID)
	return err
}

func postNotice(s moderation.Submission, approved bool, lang string) string {
	link := config.SiteURL() + clubLink(payloadInt(payload(s), "club_id"))
	if approved {
		if lang == "ru" {
			return "✅ Новость клуба опубликована.\n" + link
		}
		return "✅ Клуб жаңалығы жарияланды.\n" + link
	}
	return moderation.GenericNotice(s, false, lang)
}

// ---------- leader application ----------

func applyLeader(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	var in struct {
		ClubID    int64  `json:"club_id"`
		NameKK    string `json:"name_kk"`
		Direction string `json:"direction"`
		ProofText string `json:"proof_text"`
		ProofURL  string `json:"proof_url"`
	}
	if err := httpx.Decode(r, &in, 8<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	proof, ok := cleanText(in.ProofText, 20, 1000)
	url := strings.TrimSpace(in.ProofURL)
	if !ok || proof == "" || !reHTTPS.MatchString(url) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_proof", MsgProof)
		return
	}
	p := map[string]any{"proof_text": proof, "proof_url": url}
	univ := University()
	if in.ClubID > 0 {
		var c *Club
		var adm bool
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			var e error
			if c, e = load(ctx, tx, in.ClubID); e != nil {
				return e
			}
			adm, e = IsAdmin(ctx, tx, in.ClubID, u.ID)
			return e
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.Status != "active") {
			httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoClub)
			return
		}
		if err != nil {
			fail(w, r, "apply load", err)
			return
		}
		if adm {
			httpx.Error(w, r, http.StatusConflict, "already_admin", MsgAlready)
			return
		}
		p["club_id"], p["club"], univ = c.ID, c.NameKK, c.University
	} else {
		name, ok := cleanText(in.NameKK, 2, 120)
		if !ok || name == "" || !contains(Directions, in.Direction) {
			invalid(w, r, "name_kk")
			return
		}
		p["name_kk"], p["direction"] = name, in.Direction
	}
	var id int64
	dup := false
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		// one pending application per club (or per new-club name) at a time
		if err := tx.QueryRow(ctx, `select exists (select 1 from submissions where submitted_by = app_user_id() and kind = $1 and status = 'pending'
			and (payload->>'club_id' = $2 or (payload->>'club_id' is null and lower(payload->>'name_kk') = lower($3))))`,
			KindLeader, strconv.FormatInt(in.ClubID, 10), in.NameKK).Scan(&dup); err != nil || dup {
			return err
		}
		var err error
		id, err = moderation.Submit(ctx, tx, KindLeader, p, &u.ID, univ)
		return err
	})
	if err != nil {
		fail(w, r, "apply", err)
		return
	}
	if dup {
		httpx.Error(w, r, http.StatusConflict, "already_pending", MsgPending)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "status": "pending"})
}

// approveLeader: an existing club gets the applicant as a club admin; a club not in the
// catalogue yet is created (name + direction only; the new leader fills in the page).
func approveLeader(ctx context.Context, tx pgx.Tx, s moderation.Submission) error {
	if s.SubmittedBy == nil {
		return &moderation.Refusal{Code: "no_author", Msg: httpx.Msg{KK: "Өтінім иесі аккаунтын өшірген.", RU: "Автор заявки удалил аккаунт."}}
	}
	p := payload(s)
	clubID := payloadInt(p, "club_id")
	if clubID == 0 {
		name, _ := p["name_kk"].(string)
		dir, _ := p["direction"].(string)
		univ := University()
		if s.University != nil && *s.University != "" {
			univ = *s.University
		}
		if !contains(Directions, dir) {
			dir = "other"
		}
		if err := tx.QueryRow(ctx, `insert into clubs (name_kk, direction, university, created_by) values ($1, $2, $3, $4) returning id`,
			name, dir, univ, *s.SubmittedBy).Scan(&clubID); err != nil {
			return err
		}
	} else {
		var st string
		err := tx.QueryRow(ctx, `select status from clubs where id = $1`, clubID).Scan(&st)
		if errors.Is(err, pgx.ErrNoRows) || st != "active" {
			return &moderation.Refusal{Code: "club_gone", Msg: MsgNoClub}
		}
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `insert into club_admins (club_id, user_id, granted_by)
		values ($1, $2, (select reviewed_by from submissions where id = $3)) on conflict do nothing`, clubID, *s.SubmittedBy, s.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `update submissions set payload = payload || jsonb_build_object('club_id', $2::bigint) where id = $1`, s.ID, clubID)
	return err
}

func leaderNotice(s moderation.Submission, approved bool, lang string) string {
	if !approved {
		return moderation.GenericNotice(s, false, lang)
	}
	// the approve hook stored club_id in the row; s.Payload is the pre-hook copy for new clubs
	var id int64
	if err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `select coalesce((payload->>'club_id')::bigint, 0) from submissions where id = $1`, s.ID).Scan(&id)
	}); err != nil {
		id = payloadInt(payload(s), "club_id")
	}
	link := config.SiteURL() + clubLink(id)
	if lang == "ru" {
		return "✅ Вы — руководитель клуба на Student Hub. Заполните страницу клуба:\n" + link
	}
	return "✅ Сіз Student Hub-та клуб жетекшісісіз. Клуб бетін толтырыңыз:\n" + link
}
