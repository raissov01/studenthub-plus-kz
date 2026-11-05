package printshop

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

func shopExists(ctx context.Context, id int64) (orgID *int64, err error) {
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select kaspi_org_id from print_shops where id = $1`, id).Scan(&orgID)
	})
	return orgID, err
}

// the café's cashier login screens, against the print shops' own session table
var kaspiAdmin = shopkit.KaspiAdmin{
	Sessions: sessions,
	OrgID:    shopExists,
	Cashier:  func() shopkit.CashierLogin { return Cashier() },
	Broken:   markBroken,
}

func adminRoutes(h func(string, http.HandlerFunc)) {
	admin := func(fn auth.Handler) http.HandlerFunc { return auth.NeedRole(fn, "admin") }
	h("GET /api/admin/print/shops", admin(adminShops))
	h("POST /api/admin/print/shops", auth.Write(admin(saveShop)))
	h("PUT /api/admin/print/shops/{id}", auth.Write(admin(saveShop)))
	h("POST /api/admin/print/shops/{id}/staff", auth.Write(admin(addStaff)))
	h("DELETE /api/admin/print/shops/{id}/staff/{userId}", auth.Write(admin(removeStaff)))
	h("GET /api/admin/print/jobs", admin(adminJobs))
	h("GET /api/admin/print/jobs/{id}", admin(adminJob))
	h("POST /api/admin/print/shops/{id}/kaspi/login/start", auth.Write(admin(kaspiAdmin.LoginStart)))
	h("POST /api/admin/print/shops/{id}/kaspi/login/verify", auth.Write(admin(kaspiAdmin.LoginVerify)))
	h("POST /api/admin/print/shops/{id}/kaspi/check", auth.Write(admin(kaspiAdmin.Check)))
	h("DELETE /api/admin/print/shops/{id}/kaspi", auth.Write(admin(kaspiAdmin.Logout)))
}

func adminShops(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	now := Now()
	out := []*Shop{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+shopCols+`, k.shop_id is not null, coalesce(k.broken, false), coalesce(k.phone_masked, ''), coalesce(k.org_name, ''), k.last_error, k.updated_at
			from print_shops s left join print_kaspi_sessions k on k.shop_id = s.id order by s.active desc, s.id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			ks := &shopkit.KaspiState{Configured: Provider() != nil}
			s, err := scanShop(rows, &ks.LoggedIn, &ks.Broken, &ks.PhoneMasked, &ks.OrgName, &ks.LastError, &ks.UpdatedAt)
			if err != nil {
				rows.Close()
				return err
			}
			ref, org := s.KaspiMerchantRef, s.KaspiOrgID
			s.public(now)
			s.KaspiMerchantRef, s.KaspiOrgID, s.Kaspi, s.Staff = ref, org, ks, []Staff{}
			out = append(out, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select s.shop_id, s.user_id, u.name, u.tg_username, s.role from print_staff s join users u on u.id = s.user_id order by s.shop_id, s.role, u.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var shopID int64
			var st Staff
			if err := rows.Scan(&shopID, &st.UserID, &st.Name, &st.TG, &st.Role); err != nil {
				return err
			}
			for _, s := range out {
				if s.ID == shopID {
					s.Staff = append(s.Staff, st)
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "admin shops", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"shops": out, "kaspiConfigured": Provider() != nil})
}

var reClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

var formats = []string{"pdf", "docx", "xlsx", "pptx", "jpg", "png"}

type shopInput struct {
	NameKK           string   `json:"nameKk"`
	NameRU           string   `json:"nameRu"`
	Building         string   `json:"building"`
	PlaceKK          string   `json:"placeKk"`
	PlaceRU          string   `json:"placeRu"`
	OpenTime         string   `json:"openTime"`
	CloseTime        string   `json:"closeTime"`
	OpenDays         []int    `json:"openDays"`
	SlotMinutes      int      `json:"slotMinutes"`
	LeadMinutes      int      `json:"leadMinutes"`
	MinOrder         int64    `json:"minOrder"`
	Accepts          []string `json:"accepts"`
	RetentionDays    int      `json:"retentionDays"`
	Active           bool     `json:"active"`
	PaymentMode      string   `json:"paymentMode"`
	NoteKK           string   `json:"noteKk"`
	NoteRU           string   `json:"noteRu"`
	KaspiMerchantRef string   `json:"kaspiMerchantRef"`
	KaspiOrgID       *int64   `json:"kaspiOrgId"`
}

// ValidShop checks the admin form.
func ValidShop(in shopInput) bool {
	o, ok1 := shopkit.Clock(in.OpenTime)
	c, ok2 := shopkit.Clock(in.CloseTime)
	days := map[int]bool{}
	for _, d := range in.OpenDays {
		if d < 1 || d > 7 || days[d] {
			return false
		}
		days[d] = true
	}
	seen := map[string]bool{}
	for _, f := range in.Accepts {
		if !contains(formats, f) || seen[f] {
			return false
		}
		seen[f] = true
	}
	return shopkit.TextOK(in.NameKK, 2, 80) && shopkit.TextOK(in.NameRU, 2, 80) && utf8.RuneCountInString(in.Building) <= 120 &&
		utf8.RuneCountInString(in.PlaceKK) <= 200 && utf8.RuneCountInString(in.PlaceRU) <= 200 && reClock.MatchString(in.OpenTime) &&
		reClock.MatchString(in.CloseTime) && ok1 && ok2 && c > o && len(in.OpenDays) >= 1 && in.SlotMinutes >= 5 && in.SlotMinutes <= 60 &&
		in.LeadMinutes >= 0 && in.LeadMinutes <= 240 && in.MinOrder >= 0 && in.MinOrder <= 1000000 && in.MinOrder%100 == 0 &&
		len(in.Accepts) >= 1 && in.RetentionDays >= 1 && in.RetentionDays <= 30 &&
		(in.PaymentMode == "pickup" || in.PaymentMode == "kaspi" || in.PaymentMode == "both") &&
		utf8.RuneCountInString(in.NoteKK) <= 300 && utf8.RuneCountInString(in.NoteRU) <= 300 &&
		utf8.RuneCountInString(in.KaspiMerchantRef) <= 120 && (in.KaspiOrgID == nil || *in.KaspiOrgID >= 0)
}

func saveShop(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in shopInput
	if err := httpx.Decode(r, &in, 8<<10); err != nil || !ValidShop(in) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_shop", MsgShopData)
		return
	}
	days := make([]int16, len(in.OpenDays))
	for i, d := range in.OpenDays {
		days[i] = int16(d)
	}
	ctx := r.Context()
	var id int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if pid, ok := pathID(r, "id"); ok {
			return tx.QueryRow(ctx, `update print_shops set name_kk = $2, name_ru = $3, building = $4, place_kk = $5, place_ru = $6, open_time = $7::time,
				close_time = $8::time, open_days = $9, slot_minutes = $10, lead_minutes = $11, min_order = $12, accepts = $13, retention_days = $14,
				active = $15, payment_mode = $16, note_kk = $17, note_ru = $18, kaspi_merchant_ref = $19, kaspi_org_id = $20, updated_at = now()
				where id = $1 returning id`,
				pid, in.NameKK, in.NameRU, in.Building, in.PlaceKK, in.PlaceRU, in.OpenTime, in.CloseTime, days, in.SlotMinutes, in.LeadMinutes,
				in.MinOrder, in.Accepts, in.RetentionDays, in.Active, in.PaymentMode, in.NoteKK, in.NoteRU, in.KaspiMerchantRef, in.KaspiOrgID).Scan(&id)
		}
		return tx.QueryRow(ctx, `insert into print_shops (name_kk, name_ru, building, place_kk, place_ru, open_time, close_time, open_days, slot_minutes,
				lead_minutes, min_order, accepts, retention_days, active, payment_mode, note_kk, note_ru, kaspi_merchant_ref, kaspi_org_id, created_by)
			values ($1, $2, $3, $4, $5, $6::time, $7::time, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20) returning id`,
			in.NameKK, in.NameRU, in.Building, in.PlaceKK, in.PlaceRU, in.OpenTime, in.CloseTime, days, in.SlotMinutes, in.LeadMinutes,
			in.MinOrder, in.Accepts, in.RetentionDays, in.Active, in.PaymentMode, in.NoteKK, in.NoteRU, in.KaspiMerchantRef, in.KaspiOrgID, u.ID).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save shop", err)
		return
	}
	log.Printf("print: admin %d saved shop %d (active=%v, payment=%s)", u.ID, id, in.Active, in.PaymentMode)
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
}

func addStaff(w http.ResponseWriter, r *http.Request, u *auth.User) {
	shopID, ok := pathID(r, "id")
	var in struct {
		UserID int64  `json:"userId"`
		Role   string `json:"role"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || in.UserID <= 0 || (in.Role != "operator" && in.Role != "manager") {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from users where id = $1 and deleted_at is null)`, in.UserID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return refuse(http.StatusNotFound, "no_user", MsgNoUser)
		}
		tag, err := tx.Exec(ctx, `insert into print_staff (shop_id, user_id, role, added_by) select $1, $2, $3, $4 where exists (select 1 from print_shops where id = $1)
			on conflict (shop_id, user_id) do update set role = excluded.role`, shopID, in.UserID, in.Role, u.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		return err
	})
	if err != nil {
		reply(w, r, "add staff", err)
		return
	}
	log.Printf("print: admin %d made user %d %s of shop %d", u.ID, in.UserID, in.Role, shopID)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func removeStaff(w http.ResponseWriter, r *http.Request, u *auth.User) {
	shopID, ok1 := pathID(r, "id")
	userID, ok2 := pathID(r, "userId")
	if !ok1 || !ok2 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `delete from print_staff where shop_id = $1 and user_id = $2`, shopID, userID)
		return err
	})
	if err != nil {
		fail(w, r, "remove staff", err)
		return
	}
	log.Printf("print: admin %d removed user %d from shop %d", u.ID, userID, shopID)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/admin/print/jobs?shop=&status=
func adminJobs(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	q := r.URL.Query()
	var shopID int64
	if v := q.Get("shop"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		shopID = n
	}
	status := q.Get("status")
	switch status {
	case "", "new", "printing", "ready", "picked_up", "cancelled", "no_show", "refund_manual", "review":
	default:
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	out := []*Job{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+jobCols+`, coalesce(u.name, '') `+jobFrom+` left join users u on u.id = j.user_id
			where ($1 = 0 or j.shop_id = $1) and ($2 = '' or j.status = $2 or ($2 = 'refund_manual' and j.refund_manual) or ($2 = 'review' and j.price_state <> 'final'))
			order by j.id desc limit 200`, shopID, status)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Job, error) {
			var name string
			j, err := scanJob(row, &name)
			if err != nil {
				return nil, err
			}
			j.Customer, j.UserID = &name, j.userID
			return j, nil
		})
		return err
	})
	if err != nil {
		fail(w, r, "admin jobs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"jobs": out})
}

// GET /api/admin/print/jobs/{id}: one job with its full audit trail (file names and sizes, never the files).
func adminJob(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var j *Job
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		j, err = loadJob(ctx, tx, id)
		if err != nil {
			return err
		}
		j.UserID = j.userID
		rows, err := tx.Query(ctx, `select id, actor_id, actor_kind, kind, detail, created_at from print_job_events where job_id = $1 order by id`, id)
		if err != nil {
			return err
		}
		j.Events, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
			var e Event
			return e, row.Scan(&e.ID, &e.ActorID, &e.ActorKind, &e.Kind, &e.Detail, &e.CreatedAt)
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "admin job", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"job": j})
}
