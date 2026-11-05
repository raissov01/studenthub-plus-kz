package community

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/market"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/points"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
)

// Lost & found («Табылған заттар», FEATURE_LOSTFOUND).
//
// A post never names its author. To get an item back, a student sends a claim: a private
// message only the poster reads (on a «found» post, a detail only the owner knows). The poster
// confirms → both see each other's contact. Both then mark «returned»; the finder earns a small,
// capped points award. Documents and cards: never a photo, never their number, and a found one
// is handed to the dean's office or security.

var (
	LFKinds      = []string{"lost", "found"}
	LFCategories = []string{"documents", "cards", "keys", "phone", "electronics", "bag", "clothes", "books", "other"}
	LFHandedTo   = []string{"", "dean", "security"}
)

// Sensitive categories: identity documents and cards (student, bank, transport).
func Sensitive(category string) bool { return category == "documents" || category == "cards" }

func in(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

var (
	buildingRe = regexp.MustCompile(`^[a-z0-9_-]{0,20}$`)
	roomRe     = regexp.MustCompile(`^[\p{L}\p{N} .,/-]{0,20}$`)
	photoRe    = regexp.MustCompile(`^[0-9a-f]{32}\.jpg$`)
)

func lostFoundRoutes(mux httpx.Router) {
	h := func(p string, fn http.HandlerFunc) { handle(mux, FeatureLostFound, p, fn) }
	h("GET /api/lostfound/posts", lfBrowse)
	h("GET /api/lostfound/posts/{id}", lfGet)
	h("POST /api/lostfound/posts", write(lfCreate))
	h("POST /api/lostfound/posts/{id}/close", write(lfClose))
	h("POST /api/lostfound/posts/{id}/claims", write(lfClaim))
	h("POST /api/lostfound/claims/{id}/decide", write(lfDecide))
	h("POST /api/lostfound/claims/{id}/returned", write(lfReturned))
	h("GET /api/lostfound/mine", auth.Need(lfMine))
	h("POST /api/lostfound/photos", write(lfUpload))
	h("GET /api/lostfound/photos/{name}", lfPhoto)
	h("POST /api/admin/lostfound/moderate", admin(adminModerate(TargetLostFound)))
}

type lfPost struct {
	ID           int64     `json:"id"`
	Kind         string    `json:"kind"`
	Category     string    `json:"category"`
	Title        string    `json:"title"`
	Desc         string    `json:"desc"`
	Building     string    `json:"building"`
	Room         string    `json:"room"`
	HappenedOn   string    `json:"happenedOn"`
	Photo        *string   `json:"photo"`
	HandedTo     string    `json:"handedTo"`
	University   string    `json:"university"`
	Status       string    `json:"status"`
	HiddenReason *string   `json:"hiddenReason,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Mine         bool      `json:"mine"`
	// owner's view only: claims waiting for a decision
	PendingClaims *int `json:"pendingClaims,omitempty"`
}

const lfCols = `p.id, p.kind, p.category, p.title, p.descr, p.building, p.room, to_char(p.happened_on, 'YYYY-MM-DD'), p.photo,
	p.handed_to, p.university, p.status, p.hidden_reason, p.created_at, p.expires_at, p.user_id`

func scanPost(row pgx.Row, viewer int64, admin bool) (lfPost, int64, error) {
	var p lfPost
	var owner int64
	err := row.Scan(&p.ID, &p.Kind, &p.Category, &p.Title, &p.Desc, &p.Building, &p.Room, &p.HappenedOn, &p.Photo,
		&p.HandedTo, &p.University, &p.Status, &p.HiddenReason, &p.CreatedAt, &p.ExpiresAt, &owner)
	if err != nil {
		return p, 0, err
	}
	if p.Photo != nil {
		u := "/api/lostfound/photos/" + *p.Photo
		p.Photo = &u
	}
	p.Mine = viewer != 0 && viewer == owner
	if !p.Mine && !admin {
		p.HiddenReason = nil
	}
	return p, owner, nil
}

// lfBrowse: GET /api/lostfound/posts?kind=&category=&university=&q=&before=&limit= — active posts,
// newest first; university defaults to the viewer's own («all» = every university).
func lfBrowse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := auth.Current(r)
	qs := r.URL.Query()
	kind, cat := qs.Get("kind"), qs.Get("category")
	uni := strings.TrimSpace(qs.Get("university"))
	if uni == "" && v != nil {
		uni = deref(v.University)
	}
	if uni == "all" {
		uni = ""
	}
	q := strings.TrimSpace(qs.Get("q"))
	before, _ := strconv.ParseInt(qs.Get("before"), 10, 64)
	lim, err := strconv.Atoi(qs.Get("limit"))
	if err != nil || lim < 1 || lim > 50 {
		lim = 30
	}
	if (kind != "" && !in(LFKinds, kind)) || (cat != "" && !in(LFCategories, cat)) || len([]rune(uni)) > 120 || len([]rune(q)) > 64 {
		bad(w, r)
		return
	}
	like := ""
	if q != "" {
		like = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	}
	var viewer int64
	if v != nil {
		viewer = v.ID
	}
	out := []lfPost{}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+lfCols+` from lostfound_posts p
			where p.status = 'active' and p.expires_at > now()
				and ($1 = '' or p.kind = $1) and ($2 = '' or p.category = $2) and ($3 = '' or p.university = $3)
				and ($4 = '' or p.title ilike $4 or p.descr ilike $4) and ($5 = 0 or p.id < $5)
			order by p.id desc limit $6`, kind, cat, uni, like, before, lim)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, _, err := scanPost(rows, viewer, false)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if respond(w, r, "lf browse", err) {
		return
	}
	resp := map[string]any{"posts": out, "university": uni}
	if len(out) == lim {
		resp["nextBefore"] = out[len(out)-1].ID
	}
	httpx.JSON(w, http.StatusOK, resp)
}

type lfClaimOut struct {
	ID           int64     `json:"id"`
	PostID       int64     `json:"postId"`
	Message      string    `json:"message"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	IReturned    bool      `json:"iReturned"`
	TheyReturned bool      `json:"theyReturned"`
	// only once the poster confirmed: the other side's contact
	Contact *Contact `json:"contact"`
	// in «my claims»: the post's title and kind, while it is visible to me
	PostTitle *string `json:"postTitle,omitempty"`
	PostKind  *string `json:"postKind,omitempty"`
	other     int64
}

func revealed(status string) bool { return status == "confirmed" || status == "returned" }

// fillContacts loads the other side's contact for confirmed / returned claims only.
func fillContacts(ctx context.Context, claims []lfClaimOut) error {
	for i := range claims {
		if !revealed(claims[i].Status) {
			continue
		}
		c, err := loadContact(ctx, claims[i].other)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		claims[i].Contact = c
	}
	return nil
}

// lfGet: GET /api/lostfound/posts/{id}. Active posts for everyone; others for the author and
// admins. The author also gets the claims; a claimant gets their own claim.
func lfGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	v := auth.Current(r)
	var viewer int64
	isAdmin := false
	if v != nil {
		viewer, isAdmin = v.ID, v.Can(auth.ScopeModerator)
	}
	var post lfPost
	var owner int64
	claims := []lfClaimOut{}
	var mine *lfClaimOut
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		post, owner, err = scanPost(tx.QueryRow(ctx, `select `+lfCols+` from lostfound_posts p where p.id = $1`, id), viewer, isAdmin)
		if err != nil {
			return err
		}
		visible := post.Status == "active" && post.ExpiresAt.After(time.Now())
		if !visible && !post.Mine && !isAdmin {
			return pgx.ErrNoRows
		}
		if viewer == 0 {
			return nil
		}
		rows, err := tx.Query(ctx, `select id, post_id, message, status, created_at, poster_returned_at is not null,
				claimant_returned_at is not null, claimant_id
			from lostfound_claims where post_id = $1 and (claimant_id = $2 or $3) order by id`, id, viewer, post.Mine)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c lfClaimOut
			var poster, claimant bool
			if err := rows.Scan(&c.ID, &c.PostID, &c.Message, &c.Status, &c.CreatedAt, &poster, &claimant, &c.other); err != nil {
				return err
			}
			if post.Mine {
				c.IReturned, c.TheyReturned = poster, claimant
				claims = append(claims, c)
			} else {
				c.IReturned, c.TheyReturned = claimant, poster
				c.other = owner
				cc := c
				mine = &cc
			}
		}
		return rows.Err()
	})
	if respond(w, r, "lf get", err) {
		return
	}
	if err := fillContacts(ctx, claims); err != nil {
		internalErr(w, r, "lf contacts", err)
		return
	}
	if mine != nil {
		one := []lfClaimOut{*mine}
		if err := fillContacts(ctx, one); err != nil {
			internalErr(w, r, "lf contacts", err)
			return
		}
		mine = &one[0]
	}
	resp := map[string]any{"post": post, "myClaim": mine}
	if post.Mine {
		resp["claims"] = claims
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func parseDay(s string) (time.Time, bool) {
	d, err := time.Parse("2006-01-02", s)
	return d, err == nil
}

// lfCreate: POST /api/lostfound/posts {kind, category, title, desc, building, room, happenedOn,
// photo, handedTo}.
func lfCreate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, true) {
		return
	}
	var b struct {
		Kind       string `json:"kind"`
		Category   string `json:"category"`
		Title      string `json:"title"`
		Desc       string `json:"desc"`
		Building   string `json:"building"`
		Room       string `json:"room"`
		HappenedOn string `json:"happenedOn"`
		Photo      string `json:"photo"`
		HandedTo   string `json:"handedTo"`
	}
	if err := httpx.Decode(r, &b, 8<<10); err != nil || !in(LFKinds, b.Kind) || !in(LFCategories, b.Category) || !in(LFHandedTo, b.HandedTo) {
		bad(w, r)
		return
	}
	title, ok1 := oneLine(b.Title, 3, 80)
	desc, ok2 := cleanText(b.Desc, 0, 500)
	room, ok3 := oneLine(b.Room, 0, 20)
	if !ok1 || !ok2 || !ok3 {
		fail(w, r, "bad_text", http.StatusBadRequest)
		return
	}
	if !buildingRe.MatchString(b.Building) || !roomRe.MatchString(room) || (b.Photo != "" && !photoRe.MatchString(b.Photo)) {
		bad(w, r)
		return
	}
	// a document's number reads like a phone: name the more useful rule first
	if Sensitive(b.Category) && HasLongNumber(title+"\n"+desc) {
		fail(w, r, "has_number", http.StatusBadRequest)
		return
	}
	if HasContact(title + "\n" + desc + "\n" + room) {
		fail(w, r, "has_contact", http.StatusBadRequest)
		return
	}
	if HasLongNumber(title + "\n" + desc) {
		fail(w, r, "has_number", http.StatusBadRequest)
		return
	}
	if Sensitive(b.Category) && b.Photo != "" {
		fail(w, r, "sensitive_photo", http.StatusBadRequest)
		return
	}
	if Sensitive(b.Category) && b.Kind == "found" && b.HandedTo == "" {
		fail(w, r, "handed_required", http.StatusBadRequest)
		return
	}
	day, ok := parseDay(b.HappenedOn)
	if t := today(); !ok || day.After(t) || day.Before(t.AddDate(0, 0, -90)) {
		fail(w, r, "bad_date", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var out lfPost
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from users where id = $1 for update`, u.ID); err != nil {
			return err
		}
		if ok, err := hasChannel(ctx, tx, u.ID); err != nil || !ok {
			return firstErr(err, refuse("no_contact", http.StatusForbidden))
		}
		n, err := countToday(ctx, tx, `select count(*) from lostfound_posts where user_id = $1 and created_at >= $2`, u.ID)
		if err != nil {
			return err
		}
		if n >= limit("LOSTFOUND_DAILY_POSTS", 5) {
			return refuse("daily_limit", http.StatusTooManyRequests)
		}
		var photo *string
		if b.Photo != "" {
			tag, err := tx.Exec(ctx, `select 1 from lostfound_photos where name = $1 and user_id = $2 and post_id is null for update`, b.Photo, u.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return refuse("bad_photo", http.StatusBadRequest)
			}
			photo = &b.Photo
		}
		out, _, err = scanPost(tx.QueryRow(ctx, `insert into lostfound_posts as p (user_id, kind, category, title, descr, building, room,
				happened_on, photo, handed_to, university)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning `+lfCols,
			u.ID, b.Kind, b.Category, title, desc, b.Building, room, day, photo, b.HandedTo, strings.TrimSpace(deref(u.University))), u.ID, false)
		if err != nil {
			return err
		}
		if photo != nil {
			_, err = tx.Exec(ctx, `update lostfound_photos set post_id = $1 where name = $2`, out.ID, *photo)
		}
		return err
	})
	if respond(w, r, "lf create", err) {
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"post": out})
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// lfClose: POST /api/lostfound/posts/{id}/close — the author takes an active post down.
func lfClose(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update lostfound_posts set status = 'closed', updated_at = now()
			where id = $1 and user_id = $2 and status = 'active'`, id, u.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return err
	})
	if respond(w, r, "lf close", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// lfClaim: POST /api/lostfound/posts/{id}/claims {message} — a private message to the poster.
// No contact data in it: contacts open only after the poster confirms.
func lfClaim(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, false) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var b struct {
		Message string `json:"message"`
	}
	if err := httpx.Decode(r, &b, 4<<10); err != nil {
		bad(w, r)
		return
	}
	msg, ok := cleanText(b.Message, 10, 300)
	if !ok {
		fail(w, r, "bad_text", http.StatusBadRequest)
		return
	}
	if HasContact(msg) {
		fail(w, r, "has_contact", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var claimID, poster int64
	var kind string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from users where id = $1 for update`, u.ID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `select user_id, kind from lostfound_posts where id = $1 and status = 'active' and expires_at > now()`, id).Scan(&poster, &kind)
		if err != nil {
			return err
		}
		if poster == u.ID {
			return refuse("own_item", http.StatusBadRequest)
		}
		if ok, err := hasChannel(ctx, tx, u.ID); err != nil || !ok {
			return firstErr(err, refuse("no_contact", http.StatusForbidden))
		}
		n, err := countToday(ctx, tx, `select count(*) from lostfound_claims where claimant_id = $1 and created_at >= $2`, u.ID)
		if err != nil {
			return err
		}
		if n >= limit("LOSTFOUND_DAILY_CLAIMS", 10) {
			return refuse("daily_limit", http.StatusTooManyRequests)
		}
		err = tx.QueryRow(ctx, `insert into lostfound_claims (post_id, claimant_id, message) values ($1, $2, $3)
			on conflict (post_id, claimant_id) do nothing returning id`, id, u.ID, msg).Scan(&claimID)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse("already", http.StatusConflict)
		}
		return err
	})
	if respond(w, r, "lf claim", err) {
		return
	}
	link := fmt.Sprintf("/lostfound?id=%d", id)
	tell(ctx, poster, fmt.Sprintf("lostfound:claim:%d", claimID), link, func(lang string) string {
		if kind == "found" {
			return tr(lang, "🔎 Сіз тапқан затқа иесі хабарласты. Белгісін тексеріп, растаңыз не бас тартыңыз.",
				"🔎 По найденной вами вещи откликнулся владелец. Проверьте примету и подтвердите или откажите.")
		}
		return tr(lang, "🔎 Жоғалған затыңыз туралы біреу жазды. Хабарламаны оқып, растаңыз не бас тартыңыз.",
			"🔎 Кто-то написал о вашей потерянной вещи. Прочитайте сообщение и подтвердите или откажите.")
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{"claim": map[string]any{"id": claimID, "status": "pending"}})
}

// lfDecide: POST /api/lostfound/claims/{id}/decide {accept} — the poster only. Accepting opens
// both contacts; one confirmed claim per post at a time.
func lfDecide(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	var b struct {
		Accept *bool `json:"accept"`
	}
	if err := httpx.Decode(r, &b, 1<<10); err != nil || b.Accept == nil {
		bad(w, r)
		return
	}
	ctx := r.Context()
	var claimant, postID int64
	status := "declined"
	if *b.Accept {
		status = "confirmed"
	}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var cur string
		err := tx.QueryRow(ctx, `select c.claimant_id, c.post_id, c.status from lostfound_claims c
			join lostfound_posts p on p.id = c.post_id
			where c.id = $1 and p.user_id = $2 and p.status = 'active' for update of c, p`, id, u.ID).Scan(&claimant, &postID, &cur)
		if err != nil {
			return err
		}
		if cur != "pending" {
			return refuse("bad_state", http.StatusConflict)
		}
		if *b.Accept {
			var other bool
			if err := tx.QueryRow(ctx, `select exists (select 1 from lostfound_claims where post_id = $1 and status = 'confirmed')`, postID).Scan(&other); err != nil {
				return err
			}
			if other {
				return refuse("bad_state", http.StatusConflict)
			}
		}
		_, err = tx.Exec(ctx, `update lostfound_claims set status = $2, decided_at = now() where id = $1`, id, status)
		return err
	})
	if respond(w, r, "lf decide", err) {
		return
	}
	link := fmt.Sprintf("/lostfound?id=%d", postID)
	tell(ctx, claimant, fmt.Sprintf("lostfound:decision:%d", id), link, func(lang string) string {
		if *b.Accept {
			return tr(lang, "✅ Хабарламаңыз расталды: енді бір-біріңіздің байланысыңызды көресіздер. Кездесуді адам көп жерде, университетте өткізіңіз.",
				"✅ Ваше сообщение подтверждено: теперь вы видите контакты друг друга. Встречайтесь в людном месте, в университете.")
		}
		return tr(lang, "Хабарламаңыз расталмады. Белгілер сәйкес келмеген болуы мүмкін.",
			"Ваше сообщение не подтверждено. Возможно, приметы не совпали.")
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

// lfReturned: POST /api/lostfound/claims/{id}/returned — either party marks the item returned.
// With both marks the claim is «returned», the post «resolved» and the finder earns points.
func lfReturned(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var done bool
	var award *points.Award
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var poster, claimant, postID int64
		var kind, status string
		err := tx.QueryRow(ctx, `select p.user_id, c.claimant_id, p.id, p.kind, c.status from lostfound_claims c
			join lostfound_posts p on p.id = c.post_id where c.id = $1 and (p.user_id = $2 or c.claimant_id = $2)
			for update of c, p`, id, u.ID).Scan(&poster, &claimant, &postID, &kind, &status)
		if err != nil {
			return err
		}
		if status != "confirmed" {
			return refuse("bad_state", http.StatusConflict)
		}
		col := "claimant_returned_at"
		if u.ID == poster {
			col = "poster_returned_at"
		}
		if err := tx.QueryRow(ctx, `update lostfound_claims set `+col+` = coalesce(`+col+`, now())
			where id = $1 returning poster_returned_at is not null and claimant_returned_at is not null`, id).Scan(&done); err != nil {
			return err
		}
		if !done {
			return nil
		}
		if _, err := tx.Exec(ctx, `update lostfound_claims set status = 'returned' where id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update lostfound_posts set status = 'resolved', updated_at = now() where id = $1`, postID); err != nil {
			return err
		}
		finder := poster
		if kind == "lost" {
			finder = claimant
		}
		award = grant(ctx, tx, points.Earn{UserID: finder, Reason: ReasonLostFoundReturn, RefType: "lostfound_claim", RefID: strconv.FormatInt(id, 10)})
		return nil
	})
	if respond(w, r, "lf returned", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "returned": done, "award": award})
}

// grant never fails the action: a points error is logged and the award skipped.
func grant(ctx context.Context, tx pgx.Tx, e points.Earn) *points.Award {
	a, err := points.Grant(ctx, tx, e)
	if err != nil {
		internalLog("points "+e.Reason, err)
		return nil
	}
	return a
}

// lfMine: GET /api/lostfound/mine — my posts (any state) and my claims, read under RLS.
func lfMine(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	posts := []lfPost{}
	claims := []lfClaimOut{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+lfCols+`, (select count(*) from lostfound_claims c where c.post_id = p.id and c.status = 'pending')
			from lostfound_posts p where p.user_id = app_user_id() order by p.id desc limit 100`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p lfPost
			var owner int64
			var pending int
			if err := rows.Scan(&p.ID, &p.Kind, &p.Category, &p.Title, &p.Desc, &p.Building, &p.Room, &p.HappenedOn, &p.Photo,
				&p.HandedTo, &p.University, &p.Status, &p.HiddenReason, &p.CreatedAt, &p.ExpiresAt, &owner, &pending); err != nil {
				rows.Close()
				return err
			}
			if p.Photo != nil {
				s := "/api/lostfound/photos/" + *p.Photo
				p.Photo = &s
			}
			p.Mine, p.PendingClaims = true, &pending
			posts = append(posts, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// RLS: the post row is visible only while active (or mine), so the title may be null
		rows, err = tx.Query(ctx, `select c.id, c.post_id, c.message, c.status, c.created_at, c.claimant_returned_at is not null,
				c.poster_returned_at is not null, p.title, p.kind
			from lostfound_claims c left join lostfound_posts p on p.id = c.post_id
			where c.claimant_id = app_user_id() order by c.id desc limit 100`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c lfClaimOut
			if err := rows.Scan(&c.ID, &c.PostID, &c.Message, &c.Status, &c.CreatedAt, &c.IReturned, &c.TheyReturned, &c.PostTitle, &c.PostKind); err != nil {
				return err
			}
			claims = append(claims, c)
		}
		return rows.Err()
	})
	if respond(w, r, "lf mine", err) {
		return
	}
	// the poster's id is not readable under RLS: look it up as the server, only for revealed claims
	for i := range claims {
		if !revealed(claims[i].Status) {
			continue
		}
		err := db.Tx(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select p.user_id from lostfound_posts p join lostfound_claims c on c.post_id = p.id
				where c.id = $1 and c.claimant_id = $2`, claims[i].ID, u.ID).Scan(&claims[i].other)
		})
		if err != nil {
			internalErr(w, r, "lf mine poster", err)
			return
		}
	}
	if err := fillContacts(ctx, claims); err != nil {
		internalErr(w, r, "lf contacts", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"posts": posts, "claims": claims})
}

// ---------- photos (re-encoded by market.Process: EXIF/GPS and every other metadata dropped) ----------

// PhotoDir is $UPLOAD_DIR/lostfound.
func PhotoDir() string { return filepath.Join(config.Str("UPLOAD_DIR", "./uploads"), "lostfound") }

var errPhotoTooLarge = errors.New("photo_too_large")

func readPhoto(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	max := int64(config.Int("MARKET_PHOTO_MAX_KB", 600)) << 10
	r.Body = http.MaxBytesReader(w, r.Body, max+64<<10)
	var src io.Reader = r.Body
	if mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		mr, err := r.MultipartReader()
		if err != nil || params["boundary"] == "" {
			return nil, err
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				return nil, err
			}
			if part.FormName() == "photo" {
				src = part
				break
			}
			part.Close()
		}
	}
	data, err := io.ReadAll(io.LimitReader(src, max+1))
	if err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return nil, errPhotoTooLarge
		}
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errPhotoTooLarge
	}
	return data, nil
}

// lfUpload: POST /api/lostfound/photos — returns {name, url}; pass the name as "photo".
func lfUpload(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, false) {
		return
	}
	if ok, _ := ratelimit.Allow(r.Context(), "lfp:u:"+strconv.FormatInt(u.ID, 10), config.Int("MARKET_PHOTOS_PER_HOUR", 30), time.Hour); !ok {
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
		return
	}
	data, err := readPhoto(w, r)
	if errors.Is(err, errPhotoTooLarge) {
		httpx.Error(w, r, http.StatusRequestEntityTooLarge, "photo_too_large", httpx.Msg{KK: "Фото тым үлкен (ең көбі 600 КБ).", RU: "Фото слишком большое (максимум 600 КБ)."})
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "photo_invalid", httpx.Msg{KK: "Фотоны оқу мүмкін болмады.", RU: "Не удалось прочитать фото."})
		return
	}
	out, _, _, err := market.Process(r.Context(), data)
	if err != nil {
		code := err.Error()
		if !strings.HasPrefix(code, "photo_") {
			code = "photo_invalid"
		}
		httpx.Error(w, r, http.StatusBadRequest, code, httpx.Msg{KK: "Фотоны қабылдау мүмкін болмады (JPEG, PNG не WebP, 600 КБ дейін).", RU: "Фото не принято (JPEG, PNG или WebP, до 600 КБ)."})
		return
	}
	var rb [16]byte
	if _, err := rand.Read(rb[:]); err != nil {
		internalErr(w, r, "photo name", err)
		return
	}
	name := hex.EncodeToString(rb[:]) + ".jpg"
	if err := saveFile(name, out); err != nil {
		internalErr(w, r, "photo save", err)
		return
	}
	err = db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `insert into lostfound_photos (name, user_id, bytes) values ($1, $2, $3)`, name, u.ID, len(out))
		return err
	})
	if err != nil {
		_ = os.Remove(filepath.Join(PhotoDir(), name))
		internalErr(w, r, "photo register", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"name": name, "url": "/api/lostfound/photos/" + name, "bytes": len(out)})
}

func saveFile(name string, data []byte) error {
	dir := PhotoDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".up-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr, os.Chmod(tmp.Name(), 0o640)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// lfPhoto: GET /api/lostfound/photos/{name} — public while the post is active; otherwise only
// the uploader and admins.
func lfPhoto(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !photoRe.MatchString(name) {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var owner int64
	var public bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select f.user_id, coalesce(p.status = 'active' and p.expires_at > now(), false)
			from lostfound_photos f left join lostfound_posts p on p.id = f.post_id where f.name = $1`, name).Scan(&owner, &public)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(w, r)
		return
	}
	if err != nil {
		internalErr(w, r, "photo", err)
		return
	}
	if !public {
		u := auth.Current(r)
		if u == nil || (u.ID != owner && !u.Can(auth.ScopeModerator)) {
			notFound(w, r)
			return
		}
	}
	f, err := os.Open(filepath.Join(PhotoDir(), name))
	if err != nil {
		notFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		notFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if public {
		h.Set("Cache-Control", "public, max-age=3600")
	} else {
		h.Set("Cache-Control", "private, no-store")
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// deletePhotos is the account-deletion hook: every lost & found photo file the user uploaded.
func deletePhotos(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `select name from lostfound_photos where user_id = $1`, userID)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		if !photoRe.MatchString(n) {
			return fmt.Errorf("invalid lost & found photo name")
		}
		if err := os.Remove(filepath.Join(PhotoDir(), n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete lost & found photo: %w", err)
		}
	}
	return nil
}

func registerExports() {
	me.RegisterDeleteHook(deletePhotos)
	for section, q := range map[string]string{
		"lostfoundPosts":   "select * from lostfound_posts where user_id = app_user_id() order by id",
		"lostfoundPhotos":  "select name, post_id, bytes, created_at from lostfound_photos where user_id = app_user_id() order by created_at",
		"lostfoundClaims":  "select * from lostfound_claims where claimant_id = app_user_id() order by id",
		"qaQuestions":      "select * from qa_questions where user_id = app_user_id() order by id",
		"qaAnswers":        "select * from qa_answers where user_id = app_user_id() order by id",
		"qaVotes":          "select * from qa_votes where user_id = app_user_id() order by created_at",
		"roommateGender":   "select gender, updated_at from roommate_genders where user_id = app_user_id()",
		"roommateListings": "select * from roommate_listings where user_id = app_user_id() order by id",
		"roommateRequests": "select * from roommate_requests where from_user = app_user_id() order by id",
	} {
		me.RegisterExport(section, func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) { return me.Rows(ctx, tx, q) })
	}
}
