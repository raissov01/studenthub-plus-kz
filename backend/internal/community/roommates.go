package community

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// Roommates («Пәтерлес», FEATURE_ROOMMATES), the Bagdar /paterles rules:
//   - a listing never carries its author's id, name, username or any contact;
//   - free text (district, about) may not contain phones, @handles or links;
//   - «only my gender» is strict: such a listing is invisible to anyone of another (or an unset)
//     gender, and asking for it by id answers 404; gender lives in roommate_genders only;
//   - one active listing per person, closed automatically after 30 days;
//   - a «request» shows the owner the requester's course, university and listing summary;
//     only the owner's «accept» reveals both contacts; at most 10 requests a day;
//   - complaints go through the shared report rule; there is no AI moderation of listings.
//
// Reading needs an account: the gender rule depends on who is looking.

var (
	RMKinds   = []string{"have_room", "looking"}
	RMHousing = []string{"dorm", "flat"}
	RMHabits  = []string{"no_smoking", "smoker", "early_bird", "night_owl", "quiet", "social", "tidy", "pets_ok", "no_pets", "cooks", "sport", "studious"}
)

func roommateRoutes(mux httpx.Router) {
	h := func(p string, fn http.HandlerFunc) { handle(mux, FeatureRoommates, p, fn) }
	h("GET /api/roommates/me", auth.Need(rmMe))
	h("POST /api/roommates/gender", write(rmGender))
	h("GET /api/roommates/listings", auth.Need(rmList))
	h("GET /api/roommates/listings/{id}", auth.Need(rmGet))
	h("POST /api/roommates/listings", write(rmCreate))
	h("POST /api/roommates/listings/{id}/close", write(rmClose))
	h("POST /api/roommates/listings/{id}/requests", write(rmRequest))
	h("POST /api/roommates/requests/{id}/decide", write(rmDecide))
	h("GET /api/roommates/requests", auth.Need(rmRequests))
	h("POST /api/admin/roommates/moderate", admin(adminModerate(TargetRoommate)))
}

type rmListing struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	Housing    string    `json:"housing"`
	District   string    `json:"district"`
	BudgetMin  int       `json:"budgetMin"`
	BudgetMax  int       `json:"budgetMax"`
	MoveIn     string    `json:"moveIn"`
	Habits     []string  `json:"habits"`
	About      string    `json:"about"`
	SameGender bool      `json:"sameGender"`
	Gender     *string   `json:"gender"` // only for «only my gender» listings: who may apply
	University string    `json:"university"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Mine       bool      `json:"mine"`
	// the viewer's own request on it: pending / accepted / declined
	MyRequest *string `json:"myRequest"`
	// matching: shared habits
	Common *int `json:"common,omitempty"`
}

const rmCols = `l.id, l.kind, l.housing, l.district, l.budget_min, l.budget_max, to_char(l.move_in, 'YYYY-MM-DD'), l.habits, l.about,
	l.same_gender, case when l.same_gender then g.gender end, l.university, l.status, l.created_at, l.expires_at, l.user_id = $1,
	(select r.status from roommate_requests r where r.listing_id = l.id and r.from_user = $1)`

// visibleTo is the strict gender rule ($1 = viewer, $2 = viewer's gender or ”).
const rmVisible = `(l.user_id = $1 or not l.same_gender or g.gender = $2)`

func scanListing(row pgx.Row, extra ...any) (rmListing, error) {
	var l rmListing
	dest := []any{&l.ID, &l.Kind, &l.Housing, &l.District, &l.BudgetMin, &l.BudgetMax, &l.MoveIn, &l.Habits, &l.About,
		&l.SameGender, &l.Gender, &l.University, &l.Status, &l.CreatedAt, &l.ExpiresAt, &l.Mine, &l.MyRequest}
	err := row.Scan(append(dest, extra...)...)
	if l.Habits == nil {
		l.Habits = []string{}
	}
	return l, err
}

func viewerGender(ctx context.Context, tx pgx.Tx, uid int64) (string, error) {
	var g string
	err := tx.QueryRow(ctx, `select gender from roommate_genders where user_id = $1`, uid).Scan(&g)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return g, err
}

// rmMe: GET /api/roommates/me — my gender (for the strict rule), my active listing, today's
// request count and what blocks publishing.
func rmMe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	var gender string
	var mine *rmListing
	var today int
	var channel bool
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if gender, err = viewerGender(ctx, tx, u.ID); err != nil {
			return err
		}
		l, err := scanListing(tx.QueryRow(ctx, `select `+rmCols+` from roommate_listings l left join roommate_genders g on g.user_id = l.user_id
			where l.user_id = $1 and l.status = 'active' and $2::text is not null`, u.ID, gender))
		if err == nil {
			mine = &l
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if today, err = countToday(ctx, tx, `select count(*) from roommate_requests where from_user = $1 and created_at >= $2`, u.ID); err != nil {
			return err
		}
		channel, err = hasChannel(ctx, tx, u.ID)
		return err
	})
	if respond(w, r, "rm me", err) {
		return
	}
	blockers := []string{}
	if u.NeedsConsent() {
		blockers = append(blockers, "consent_required")
	}
	if strings.TrimSpace(deref(u.University)) == "" {
		blockers = append(blockers, "no_university")
	}
	if !channel {
		blockers = append(blockers, "no_contact")
	}
	var g *string
	if gender != "" {
		g = &gender
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"gender": g, "listing": mine, "requestsToday": today,
		"dailyLimit": limit("ROOMMATES_DAILY_REQUESTS", 10), "blockers": blockers})
}

// rmGender: POST /api/roommates/gender {gender: f|m|null} — kept apart from the profile, used
// only by the strict rule. Clearing it is refused while my active listing is strict.
func rmGender(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, false) {
		return
	}
	var b struct {
		Gender *string `json:"gender"`
	}
	if err := httpx.Decode(r, &b, 1<<10); err != nil || (b.Gender != nil && *b.Gender != "f" && *b.Gender != "m") {
		bad(w, r)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var strict bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from roommate_listings where user_id = $1 and status = 'active' and same_gender)`, u.ID).Scan(&strict); err != nil {
			return err
		}
		if strict {
			// changing the gender behind a strict listing would show it to the other side
			return refuse("bad_state", http.StatusConflict)
		}
		if b.Gender == nil {
			_, err := tx.Exec(ctx, `delete from roommate_genders where user_id = $1`, u.ID)
			return err
		}
		_, err := tx.Exec(ctx, `insert into roommate_genders (user_id, gender) values ($1, $2)
			on conflict (user_id) do update set gender = excluded.gender, updated_at = now()`, u.ID, *b.Gender)
		return err
	})
	if respond(w, r, "rm gender", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"gender": b.Gender})
}

// rmList: GET /api/roommates/listings?kind=&housing=&budgetMax=&match=1&university=&before= —
// active listings the viewer may see. match=1 ranks the listings that fit my own listing
// (complementary kind, same housing, overlapping budget, my own gender rule) by shared habits.
func rmList(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	qs := r.URL.Query()
	kind, housing := qs.Get("kind"), qs.Get("housing")
	budget, _ := strconv.Atoi(qs.Get("budgetMax"))
	before, _ := strconv.ParseInt(qs.Get("before"), 10, 64)
	match := qs.Get("match") == "1"
	uni := strings.TrimSpace(qs.Get("university"))
	if uni == "" {
		uni = strings.TrimSpace(deref(u.University))
	}
	if uni == "all" {
		uni = ""
	}
	if (kind != "" && !in(RMKinds, kind)) || (housing != "" && !in(RMHousing, housing)) || budget < 0 || budget > 1_000_000 || len([]rune(uni)) > 120 {
		bad(w, r)
		return
	}
	out := []rmListing{}
	needListing := false
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		gender, err := viewerGender(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		where := `l.status = 'active' and l.expires_at > now() and u.deleted_at is null and ` + rmVisible + `
			and ($3 = '' or l.university = $3) and ($4 = '' or l.kind = $4) and ($5 = '' or l.housing = $5)
			and ($6 = 0 or l.budget_min <= $6) and ($7 = 0 or l.id < $7)`
		args := []any{u.ID, gender, uni, kind, housing, budget, before}
		order := `l.id desc`
		common := `0`
		if match {
			var my rmListing
			my, err = scanListing(tx.QueryRow(ctx, `select `+rmCols+` from roommate_listings l left join roommate_genders g on g.user_id = l.user_id
				where l.user_id = $1 and l.status = 'active' and $2::text is not null`, u.ID, gender))
			if errors.Is(err, pgx.ErrNoRows) {
				needListing = true
				return nil
			}
			if err != nil {
				return err
			}
			where += ` and l.user_id <> $1 and l.housing = $8 and l.budget_min <= $9 and l.budget_max >= $10
				and ($11 = 'looking' or l.kind = 'looking') and (not $12 or g.gender = $2)`
			args = append(args, my.Housing, my.BudgetMax, my.BudgetMin, my.Kind, my.SameGender, my.Habits)
			common = `cardinality(array(select unnest(l.habits) intersect select unnest($13::text[])))`
			order = `c desc, abs(l.move_in - '` + my.MoveIn + `'::date), l.id desc`
		}
		rows, err := tx.Query(ctx, `select `+rmCols+`, `+common+` as c
			from roommate_listings l join users u on u.id = l.user_id left join roommate_genders g on g.user_id = l.user_id
			where `+where+` order by `+order+` limit 30`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c int
			l, err := scanListing(rows, &c)
			if err != nil {
				return err
			}
			if match {
				l.Common = &c
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if respond(w, r, "rm list", err) {
		return
	}
	resp := map[string]any{"listings": out, "university": uni}
	if needListing {
		resp["needListing"] = true
	}
	if !match && len(out) == 30 {
		resp["nextBefore"] = out[len(out)-1].ID
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// rmGet: GET /api/roommates/listings/{id} — 404 for a strict listing of the other gender, just
// like a missing one.
func rmGet(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var l rmListing
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		gender, err := viewerGender(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		l, err = scanListing(tx.QueryRow(ctx, `select `+rmCols+` from roommate_listings l join users u on u.id = l.user_id
			left join roommate_genders g on g.user_id = l.user_id
			where l.id = $3 and `+rmVisible+` and (l.user_id = $1 or $4 or (l.status = 'active' and l.expires_at > now() and u.deleted_at is null))`,
			u.ID, gender, id, u.Role == "admin"))
		return err
	})
	if respond(w, r, "rm get", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"listing": l})
}

// rmCreate: POST /api/roommates/listings {kind, housing, district, budgetMin, budgetMax, moveIn,
// habits, about, sameGender}.
func rmCreate(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, true) {
		return
	}
	var b struct {
		Kind       string   `json:"kind"`
		Housing    string   `json:"housing"`
		District   string   `json:"district"`
		BudgetMin  int      `json:"budgetMin"`
		BudgetMax  int      `json:"budgetMax"`
		MoveIn     string   `json:"moveIn"`
		Habits     []string `json:"habits"`
		About      string   `json:"about"`
		SameGender bool     `json:"sameGender"`
	}
	if err := httpx.Decode(r, &b, 8<<10); err != nil || !in(RMKinds, b.Kind) || !in(RMHousing, b.Housing) || len(b.Habits) > 8 {
		bad(w, r)
		return
	}
	habits := []string{}
	seen := map[string]bool{}
	for _, h := range b.Habits {
		if !in(RMHabits, h) {
			bad(w, r)
			return
		}
		if !seen[h] {
			seen[h] = true
			habits = append(habits, h)
		}
	}
	district, ok1 := oneLine(b.District, 2, 60)
	about, ok2 := cleanText(b.About, 0, 400)
	if !ok1 || !ok2 {
		fail(w, r, "bad_text", http.StatusBadRequest)
		return
	}
	if HasContact(district + "\n" + about) {
		fail(w, r, "has_contact", http.StatusBadRequest)
		return
	}
	if b.BudgetMin < 0 || b.BudgetMax < 5000 || b.BudgetMax > 1_000_000 || b.BudgetMin > b.BudgetMax {
		fail(w, r, "bad_budget", http.StatusBadRequest)
		return
	}
	day, ok := parseDay(b.MoveIn)
	if t := today(); !ok || day.Before(t) || day.After(t.AddDate(0, 6, 0)) {
		fail(w, r, "bad_move_in", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var l rmListing
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from users where id = $1 for update`, u.ID); err != nil {
			return err
		}
		if ok, err := hasChannel(ctx, tx, u.ID); err != nil || !ok {
			return firstErr(err, refuse("no_contact", http.StatusForbidden))
		}
		gender, err := viewerGender(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if b.SameGender && gender == "" {
			return refuse("need_gender", http.StatusBadRequest)
		}
		n, err := countToday(ctx, tx, `select count(*) from roommate_listings where user_id = $1 and created_at >= $2`, u.ID)
		if err != nil {
			return err
		}
		if n >= limit("ROOMMATES_DAILY_LISTINGS", 3) {
			return refuse("daily_limit", http.StatusTooManyRequests)
		}
		var id int64
		err = tx.QueryRow(ctx, `insert into roommate_listings (user_id, kind, housing, district, budget_min, budget_max, move_in, habits, about, same_gender, university)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning id`,
			u.ID, b.Kind, b.Housing, district, b.BudgetMin, b.BudgetMax, day, habits, about, b.SameGender, strings.TrimSpace(deref(u.University))).Scan(&id)
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return refuse("one_active", http.StatusConflict)
		}
		if err != nil {
			return err
		}
		l, err = scanListing(tx.QueryRow(ctx, `select `+rmCols+` from roommate_listings l left join roommate_genders g on g.user_id = l.user_id
			where l.id = $3 and $2::text is not null`, u.ID, gender, id))
		return err
	})
	if respond(w, r, "rm create", err) {
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"listing": l})
}

func rmClose(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update roommate_listings set status = 'closed' where id = $1 and user_id = $2 and status = 'active'`, id, u.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return err
	})
	if respond(w, r, "rm close", err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// rmRequest: POST /api/roommates/listings/{id}/requests — «I'm interested». No free text.
func rmRequest(w http.ResponseWriter, r *http.Request, u *auth.User) {
	if !writer(w, r, u, false) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		notFound(w, r)
		return
	}
	ctx := r.Context()
	var owner, reqID int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from users where id = $1 for update`, u.ID); err != nil {
			return err
		}
		gender, err := viewerGender(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select l.user_id from roommate_listings l join users u on u.id = l.user_id
			left join roommate_genders g on g.user_id = l.user_id
			where l.id = $3 and l.status = 'active' and l.expires_at > now() and u.deleted_at is null and `+rmVisible,
			u.ID, gender, id).Scan(&owner); err != nil {
			return err
		}
		if owner == u.ID {
			return refuse("own_item", http.StatusBadRequest)
		}
		if ok, err := hasChannel(ctx, tx, u.ID); err != nil || !ok {
			return firstErr(err, refuse("no_contact", http.StatusForbidden))
		}
		n, err := countToday(ctx, tx, `select count(*) from roommate_requests where from_user = $1 and created_at >= $2`, u.ID)
		if err != nil {
			return err
		}
		if n >= limit("ROOMMATES_DAILY_REQUESTS", 10) {
			return refuse("daily_limit", http.StatusTooManyRequests)
		}
		err = tx.QueryRow(ctx, `insert into roommate_requests (listing_id, from_user) values ($1, $2)
			on conflict (listing_id, from_user) do nothing returning id`, id, u.ID).Scan(&reqID)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse("already", http.StatusConflict)
		}
		return err
	})
	if respond(w, r, "rm request", err) {
		return
	}
	tell(ctx, owner, fmt.Sprintf("roommates:request:%d", reqID), "/roommates?id=requests", func(lang string) string {
		return tr(lang, "🏠 «Пәтерлес»: хабарландыруыңызға біреу қызығушылық білдірді. Келіссеңіз ғана байланыстар ашылады.",
			"🏠 «Соседи»: вашим объявлением заинтересовались. Контакты откроются, только если вы согласитесь.")
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{"request": map[string]any{"id": reqID, "status": "pending"}})
}

// rmDecide: POST /api/roommates/requests/{id}/decide {accept} — the listing owner only.
func rmDecide(w http.ResponseWriter, r *http.Request, u *auth.User) {
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
	status := "declined"
	if *b.Accept {
		status = "accepted"
	}
	ctx := r.Context()
	var from int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var cur string
		if err := tx.QueryRow(ctx, `select r.from_user, r.status from roommate_requests r join roommate_listings l on l.id = r.listing_id
			where r.id = $1 and l.user_id = $2 for update of r`, id, u.ID).Scan(&from, &cur); err != nil {
			return err
		}
		if cur != "pending" {
			return refuse("bad_state", http.StatusConflict)
		}
		_, err := tx.Exec(ctx, `update roommate_requests set status = $2, decided_at = now() where id = $1`, id, status)
		return err
	})
	if respond(w, r, "rm decide", err) {
		return
	}
	tell(ctx, from, fmt.Sprintf("roommates:decision:%d", id), "/roommates?id=requests", func(lang string) string {
		if *b.Accept {
			return tr(lang, "✅ «Пәтерлес»: сұрауыңыз қабылданды, байланыстар ашылды. Алғашқы кездесуді адам көп жерде өткізіңіз, алдын ала ақша аудармаңыз.",
				"✅ «Соседи»: ваш запрос принят, контакты открыты. Первую встречу проведите в людном месте, не переводите деньги заранее.")
		}
		return tr(lang, "«Пәтерлес»: бұл жолы келіспеді. Басқа хабарландыруларды қараңыз.", "«Соседи»: в этот раз не сложилось. Посмотрите другие объявления.")
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

type rmRequestOut struct {
	ID        int64      `json:"id"`
	ListingID int64      `json:"listingId"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	Incoming  bool       `json:"incoming"`
	Course    *int16     `json:"course,omitempty"`     // incoming: the requester's course
	FromUni   *string    `json:"university,omitempty"` // incoming: the requester's university
	Their     *rmListing `json:"listing"`              // incoming: the requester's active listing; outgoing: the listing asked for
	Contact   *Contact   `json:"contact"`              // accepted only
	other     int64
}

// rmRequests: GET /api/roommates/requests — incoming (on my listings) and outgoing requests.
// The request rows are read under RLS (parties only); contacts appear for accepted ones only.
func rmRequests(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	out := []rmRequestOut{}
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select r.id, r.listing_id, r.status, r.created_at, r.from_user <> app_user_id()
			from roommate_requests r order by r.id desc limit 100`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var q rmRequestOut
			if err := rows.Scan(&q.ID, &q.ListingID, &q.Status, &q.CreatedAt, &q.Incoming); err != nil {
				return err
			}
			out = append(out, q)
		}
		return rows.Err()
	})
	if respond(w, r, "rm requests", err) {
		return
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		gender, err := viewerGender(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		for i := range out {
			q := &out[i]
			if q.Incoming {
				if err := tx.QueryRow(ctx, `select r.from_user, u.course, u.university from roommate_requests r join users u on u.id = r.from_user
					where r.id = $1`, q.ID).Scan(&q.other, &q.Course, &q.FromUni); err != nil {
					return err
				}
				l, err := scanListing(tx.QueryRow(ctx, `select `+rmCols+` from roommate_listings l left join roommate_genders g on g.user_id = l.user_id
					where l.user_id = $3 and l.status = 'active' and $2::text is not null`, u.ID, gender, q.other))
				if err == nil {
					q.Their = &l
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				continue
			}
			l, err := scanListing(tx.QueryRow(ctx, `select `+rmCols+` from roommate_listings l left join roommate_genders g on g.user_id = l.user_id
				where l.id = $3 and $2::text is not null`, u.ID, gender, q.ListingID))
			if err != nil {
				return err
			}
			q.Their = &l
			if err := tx.QueryRow(ctx, `select user_id from roommate_listings where id = $1`, q.ListingID).Scan(&q.other); err != nil {
				return err
			}
		}
		return nil
	})
	if respond(w, r, "rm requests detail", err) {
		return
	}
	for i := range out {
		if out[i].Status != "accepted" {
			continue
		}
		c, err := loadContact(ctx, out[i].other)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			internalErr(w, r, "rm contact", err)
			return
		}
		out[i].Contact = c
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"requests": out})
}
