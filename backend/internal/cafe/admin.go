package cafe

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

func adminRoutes(h func(string, http.HandlerFunc)) {
	admin := func(fn auth.Handler) http.HandlerFunc { return auth.NeedRole(fn, "admin") }
	h("GET /api/admin/cafes", admin(adminCafes))
	h("POST /api/admin/cafes", auth.Write(admin(saveCafe)))
	h("PUT /api/admin/cafes/{id}", auth.Write(admin(saveCafe)))
	h("POST /api/admin/cafes/{id}/staff", auth.Write(admin(addStaff)))
	h("DELETE /api/admin/cafes/{id}/staff/{userId}", auth.Write(admin(removeStaff)))
	h("GET /api/admin/cafe/orders", admin(adminOrders))
	h("GET /api/admin/cafe/orders/{id}", admin(adminOrder))
	h("POST /api/admin/cafes/{id}/kaspi/login/start", auth.Write(admin(kaspiLoginStart)))
	h("POST /api/admin/cafes/{id}/kaspi/login/verify", auth.Write(admin(kaspiLoginVerify)))
	h("POST /api/admin/cafes/{id}/kaspi/check", auth.Write(admin(kaspiCheck)))
	h("DELETE /api/admin/cafes/{id}/kaspi", auth.Write(admin(kaspiLogout)))
}

// ---------- cafés ----------

func adminCafes(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	now := Now()
	out := []*Cafe{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+cafeCols+`, k.cafe_id is not null, coalesce(k.broken, false), coalesce(k.phone_masked, ''), coalesce(k.org_name, ''), k.last_error, k.updated_at
			from cafes c left join cafe_kaspi_sessions k on k.cafe_id = c.id order by c.active desc, c.id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			ks := &KaspiState{Configured: Provider() != nil}
			c, err := scanCafe(rows, &ks.LoggedIn, &ks.Broken, &ks.PhoneMasked, &ks.OrgName, &ks.LastError, &ks.UpdatedAt)
			if err != nil {
				rows.Close()
				return err
			}
			ref, org := c.KaspiMerchantRef, c.KaspiOrgID
			c.public(now)
			c.KaspiMerchantRef, c.KaspiOrgID, c.Kaspi, c.Staff = ref, org, ks, []Staff{}
			out = append(out, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select s.cafe_id, s.user_id, u.name, u.tg_username, s.role from cafe_staff s join users u on u.id = s.user_id order by s.cafe_id, s.role, u.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cafeID int64
			var s Staff
			if err := rows.Scan(&cafeID, &s.UserID, &s.Name, &s.TG, &s.Role); err != nil {
				return err
			}
			for _, c := range out {
				if c.ID == cafeID {
					c.Staff = append(c.Staff, s)
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "admin cafes", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cafes": out, "kaspiConfigured": Provider() != nil})
}

var reClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

type cafeInput struct {
	NameKK           string `json:"nameKk"`
	NameRU           string `json:"nameRu"`
	Building         string `json:"building"`
	PlaceKK          string `json:"placeKk"`
	PlaceRU          string `json:"placeRu"`
	OpenTime         string `json:"openTime"`
	CloseTime        string `json:"closeTime"`
	OpenDays         []int  `json:"openDays"`
	SlotMinutes      int    `json:"slotMinutes"`
	SlotCapacity     int    `json:"slotCapacity"`
	LeadMinutes      int    `json:"leadMinutes"`
	Active           bool   `json:"active"`
	PaymentMode      string `json:"paymentMode"`
	KaspiMerchantRef string `json:"kaspiMerchantRef"`
	KaspiOrgID       *int64 `json:"kaspiOrgId"`
}

// ValidCafe checks the admin form.
func ValidCafe(in cafeInput) bool {
	o, ok1 := clock(in.OpenTime)
	c, ok2 := clock(in.CloseTime)
	days := map[int]bool{}
	for _, d := range in.OpenDays {
		if d < 1 || d > 7 || days[d] {
			return false
		}
		days[d] = true
	}
	return textOK(in.NameKK, 2, 80) && textOK(in.NameRU, 2, 80) && len([]rune(in.Building)) <= 120 && len([]rune(in.PlaceKK)) <= 200 &&
		len([]rune(in.PlaceRU)) <= 200 && reClock.MatchString(in.OpenTime) && reClock.MatchString(in.CloseTime) && ok1 && ok2 && c > o &&
		len(in.OpenDays) >= 1 && in.SlotMinutes >= 5 && in.SlotMinutes <= 60 && in.SlotCapacity >= 1 && in.SlotCapacity <= 100 &&
		in.LeadMinutes >= 0 && in.LeadMinutes <= 120 && (in.PaymentMode == "pickup" || in.PaymentMode == "kaspi" || in.PaymentMode == "both") &&
		len([]rune(in.KaspiMerchantRef)) <= 120 && (in.KaspiOrgID == nil || *in.KaspiOrgID >= 0)
}

func saveCafe(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var in cafeInput
	if err := httpx.Decode(r, &in, 8<<10); err != nil || !ValidCafe(in) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_cafe", MsgCafeData)
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
			return tx.QueryRow(ctx, `update cafes set name_kk = $2, name_ru = $3, building = $4, place_kk = $5, place_ru = $6, open_time = $7::time, close_time = $8::time,
				open_days = $9, slot_minutes = $10, slot_capacity = $11, lead_minutes = $12, active = $13, payment_mode = $14, kaspi_merchant_ref = $15,
				kaspi_org_id = $16, updated_at = now() where id = $1 returning id`,
				pid, in.NameKK, in.NameRU, in.Building, in.PlaceKK, in.PlaceRU, in.OpenTime, in.CloseTime, days, in.SlotMinutes, in.SlotCapacity,
				in.LeadMinutes, in.Active, in.PaymentMode, in.KaspiMerchantRef, in.KaspiOrgID).Scan(&id)
		}
		return tx.QueryRow(ctx, `insert into cafes (name_kk, name_ru, building, place_kk, place_ru, open_time, close_time, open_days, slot_minutes, slot_capacity,
				lead_minutes, active, payment_mode, kaspi_merchant_ref, kaspi_org_id, created_by)
			values ($1, $2, $3, $4, $5, $6::time, $7::time, $8, $9, $10, $11, $12, $13, $14, $15, $16) returning id`,
			in.NameKK, in.NameRU, in.Building, in.PlaceKK, in.PlaceRU, in.OpenTime, in.CloseTime, days, in.SlotMinutes, in.SlotCapacity,
			in.LeadMinutes, in.Active, in.PaymentMode, in.KaspiMerchantRef, in.KaspiOrgID, u.ID).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "save cafe", err)
		return
	}
	log.Printf("cafe: admin %d saved café %d (active=%v, payment=%s)", u.ID, id, in.Active, in.PaymentMode)
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
}

// ---------- staff ----------

func addStaff(w http.ResponseWriter, r *http.Request, u *auth.User) {
	cafeID, ok := pathID(r, "id")
	var in struct {
		UserID int64  `json:"userId"`
		Role   string `json:"role"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || in.UserID <= 0 || (in.Role != "barista" && in.Role != "manager") {
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
		tag, err := tx.Exec(ctx, `insert into cafe_staff (cafe_id, user_id, role, added_by) select $1, $2, $3, $4 where exists (select 1 from cafes where id = $1)
			on conflict (cafe_id, user_id) do update set role = excluded.role`, cafeID, in.UserID, in.Role, u.ID)
		if err == nil && tag.RowsAffected() == 0 {
			return refuse(http.StatusNotFound, "not_found", httpx.MsgNotFound)
		}
		return err
	})
	var oe *orderErr
	if errors.As(err, &oe) {
		httpx.Error(w, r, oe.status, oe.code, oe.msg)
		return
	}
	if err != nil {
		fail(w, r, "add staff", err)
		return
	}
	log.Printf("cafe: admin %d made user %d %s of café %d", u.ID, in.UserID, in.Role, cafeID)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func removeStaff(w http.ResponseWriter, r *http.Request, u *auth.User) {
	cafeID, ok1 := pathID(r, "id")
	userID, ok2 := pathID(r, "userId")
	if !ok1 || !ok2 {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `delete from cafe_staff where cafe_id = $1 and user_id = $2`, cafeID, userID)
		return err
	})
	if err != nil {
		fail(w, r, "remove staff", err)
		return
	}
	log.Printf("cafe: admin %d removed user %d from café %d", u.ID, userID, cafeID)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- order audit ----------

// GET /api/admin/cafe/orders?cafe=&status=&limit=
func adminOrders(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	q := r.URL.Query()
	var cafeID int64
	if v := q.Get("cafe"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
			return
		}
		cafeID = n
	}
	status := q.Get("status")
	switch status {
	case "", "new", "accepted", "ready", "picked_up", "cancelled", "no_show", "refund_manual":
	default:
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	out := []*Order{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+orderCols+`, coalesce(u.name, '') `+orderFrom+` left join users u on u.id = o.user_id
			where ($1 = 0 or o.cafe_id = $1) and ($2 = '' or o.status = $2 or ($2 = 'refund_manual' and o.refund_manual))
			order by o.id desc limit 200`, cafeID, status)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			o, err := scanOrder(rows, &name)
			if err != nil {
				return err
			}
			o.Customer, o.UserID = &name, o.userID
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "admin orders", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"orders": out})
}

// GET /api/admin/cafe/orders/{id}: one order with its full audit trail.
func adminOrder(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	var o *Order
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		o, err = loadOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		o.UserID = o.userID
		rows, err := tx.Query(ctx, `select id, actor_id, actor_kind, kind, detail, created_at from cafe_order_events where order_id = $1 order by id`, id)
		if err != nil {
			return err
		}
		o.Events, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
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
		fail(w, r, "admin order", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"order": o})
}

// ---------- Kaspi cashier login (the café added our cashier number in Kaspi Pay; docs/cafe.md) ----------

func cashier(w http.ResponseWriter, r *http.Request) CashierLogin {
	c := Cashier()
	if c == nil || payments.SessionKey() == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "kaspi_not_configured", MsgKaspiOff)
		return nil
	}
	return c
}

func cafeExists(ctx context.Context, id int64) (orgID *int64, err error) {
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select kaspi_org_id from cafes where id = $1`, id).Scan(&orgID)
	})
	return orgID, err
}

func kaspiLoginStart(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		Phone string `json:"phone"`
	}
	if !ok || httpx.Decode(r, &in, 1<<10) != nil || payments.NormalizePhone(in.Phone) == "" {
		httpx.Error(w, r, http.StatusBadRequest, "bad_phone", MsgKaspiLogin)
		return
	}
	c := cashier(w, r)
	if c == nil {
		return
	}
	if _, err := cafeExists(r.Context(), id); err != nil {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if limited(w, r, "kaspi-login:"+itoa(u.ID), 6, time.Hour) {
		return
	}
	pid, err := c.LoginStart(r.Context(), in.Phone)
	if err != nil {
		log.Printf("cafe: Kaspi login start for café %d: %v", id, err)
		httpx.JSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"code": "kaspi_login", "message": pick(httpx.Lang(r), MsgKaspiLogin.KK, MsgKaspiLogin.RU), "detail": trim(err.Error(), 160)}})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"processId": pid})
}

var reOTP = regexp.MustCompile(`^[0-9]{4,8}$`)

func kaspiLoginVerify(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	var in struct {
		ProcessID string `json:"processId"`
		OTP       string `json:"otp"`
	}
	if !ok || httpx.Decode(r, &in, 2<<10) != nil || in.ProcessID == "" || len(in.ProcessID) > 200 || !reOTP.MatchString(in.OTP) {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", MsgKaspiLogin)
		return
	}
	c := cashier(w, r)
	if c == nil {
		return
	}
	ctx := r.Context()
	orgID, err := cafeExists(ctx, id)
	if err != nil {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	l, err := c.LoginVerify(ctx, in.ProcessID, in.OTP)
	if err != nil {
		log.Printf("cafe: Kaspi login verify for café %d: %v", id, err)
		httpx.Error(w, r, http.StatusBadGateway, "kaspi_login", MsgKaspiLogin)
		return
	}
	orgName := l.OrgName
	// one number in several businesses: this café's business, by its OrganizationId
	if orgID != nil && *orgID != 0 && *orgID != l.OrganizationID {
		pid, name, err := c.OrgContext(ctx, l.Session, *orgID)
		if err != nil {
			log.Printf("cafe: Kaspi org-context %d for café %d: %v", *orgID, id, err)
			httpx.Error(w, r, http.StatusBadGateway, "kaspi_org", MsgKaspiLogin)
			return
		}
		l.Session.ProfileID, orgName = pid, name
	}
	key := payments.SessionKey()
	ct, err := payments.Encrypt(l.Session, key)
	if err != nil {
		fail(w, r, "encrypt session", err)
		return
	}
	hash := payments.PhoneHash(l.Phone, key)
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into cafe_kaspi_sessions (cafe_id, ciphertext, phone_hash, phone_masked, org_name, created_by)
			values ($1, $2, $3, $4, $5, $6) on conflict (cafe_id) do update set ciphertext = excluded.ciphertext, phone_hash = excluded.phone_hash,
			phone_masked = excluded.phone_masked, org_name = excluded.org_name, broken = false, last_error = null, created_by = excluded.created_by, updated_at = now()`,
			id, ct, hash, payments.MaskPhone(l.Phone), trim(orgName, 200), u.ID); err != nil {
			return err
		}
		// a new login of the same number replaces the old token on Kaspi's side: other cafés of the number get the
		// fresh token with their own profile
		rows, err := tx.Query(ctx, `select cafe_id, ciphertext from cafe_kaspi_sessions where phone_hash = $1 and cafe_id <> $2 for update`, hash, id)
		if err != nil {
			return err
		}
		type other struct {
			id int64
			ct string
		}
		others, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (other, error) {
			var o other
			return o, row.Scan(&o.id, &o.ct)
		})
		if err != nil {
			return err
		}
		for _, o := range others {
			old, err := payments.Decrypt(o.ct, key)
			if err != nil {
				continue
			}
			fresh := l.Session
			fresh.ProfileID = old.ProfileID
			nct, err := payments.Encrypt(fresh, key)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `update cafe_kaspi_sessions set ciphertext = $2, broken = false, last_error = null, updated_at = now() where cafe_id = $1`, o.id, nct); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		fail(w, r, "save session", err)
		return
	}
	log.Printf("cafe: admin %d logged in the Kaspi cashier of café %d (%s)", u.ID, id, payments.MaskPhone(l.Phone))
	httpx.JSON(w, http.StatusOK, map[string]any{"phoneMasked": payments.MaskPhone(l.Phone), "orgName": orgName})
}

func kaspiCheck(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	c := cashier(w, r)
	if c == nil {
		return
	}
	ctx := r.Context()
	var ct string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select ciphertext from cafe_kaspi_sessions where cafe_id = $1`, id).Scan(&ct)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.JSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	if err != nil {
		fail(w, r, "kaspi check", err)
		return
	}
	s, err := payments.Decrypt(ct, payments.SessionKey())
	if err == nil {
		err = c.Check(ctx, *s)
	}
	if err != nil {
		markBroken(ctx, id, err)
		httpx.JSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	_ = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update cafe_kaspi_sessions set broken = false, last_error = null, updated_at = now() where cafe_id = $1`, id)
		return err
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"active": true})
}

func kaspiLogout(w http.ResponseWriter, r *http.Request, u *auth.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `delete from cafe_kaspi_sessions where cafe_id = $1`, id)
		return err
	})
	if err != nil {
		fail(w, r, "kaspi logout", err)
		return
	}
	log.Printf("cafe: admin %d removed the Kaspi session of café %d", u.ID, id)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
