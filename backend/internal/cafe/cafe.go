// Package cafe is campus café pre-ordering («Кофе / Кофейня», docs/cafe.md), FEATURE_CAFE: a student orders
// from the menu for a pickup slot and pays either at the counter or with Kaspi straight to the café's own
// Kaspi Pay business account (payments/, services/kaspi-pos). Staff run the order board at the counter.
//
// Security model: every write is the server (connection owner) with explicit checks in this package; reads
// for a student or a staff member go through db.WithUser, so Row Level Security alone already limits a
// student to their own orders and staff to their own café. The total is always recomputed from the menu.
package cafe

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/me"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/ratelimit"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
)

const Feature = "cafe"

// Now is replaced by tests.
var Now = time.Now

var almaty = shopkit.Almaty

// Provider is the Kaspi payment provider, nil while Kaspi is not configured. Tests replace it.
var Provider = func() payments.Provider {
	if k := payments.KaspiFromEnv(); k != nil {
		return k
	}
	return nil
}

// Cashier is the sidecar for the admin login flow, nil while not configured. Tests replace it.
var Cashier = func() CashierLogin {
	if k := payments.KaspiFromEnv(); k != nil {
		return k
	}
	return nil
}

// CashierLogin is the part of the Kaspi client the admin login screens use.
type CashierLogin = shopkit.CashierLogin

// Limits (env, docs/cafe.md).
func maxActive() int       { return config.Int("CAFE_MAX_ACTIVE_ORDERS", 2) }
func dailyCap() int        { return config.Int("CAFE_DAILY_ORDERS", 6) }
func ordersPerHour() int   { return config.Int("CAFE_ORDERS_PER_HOUR", 10) }
func maxTotal() int64      { return int64(config.Int("CAFE_MAX_ORDER_TOTAL", 3000000)) } // tiyn: 30 000 ₸
func noShowLimit() int     { return config.Int("CAFE_NO_SHOW_LIMIT", 2) }
func noShowWindow() int    { return config.Int("CAFE_NO_SHOW_DAYS", 60) }
func noShowGrace() int     { return config.Int("CAFE_NO_SHOW_GRACE_MIN", 10) }
func payTimeout() int      { return config.Int("CAFE_PAY_TIMEOUT_MIN", 10) }
func unpaidCancelMin() int { return config.Int("CAFE_UNPAID_CANCEL_MIN", 30) }

var (
	MsgClosed     = httpx.Msg{KK: "Кофейня қазір тапсырыс қабылдамайды.", RU: "Кофейня сейчас не принимает заказы."}
	MsgSlot       = httpx.Msg{KK: "Бұл уақыт қолжетімсіз. Басқа уақытты таңдаңыз.", RU: "Это время недоступно. Выберите другое."}
	MsgSlotFull   = httpx.Msg{KK: "Бұл уақытқа тапсырыс толып қалды. Басқа уақытты таңдаңыз.", RU: "На это время заказов слишком много. Выберите другое."}
	MsgItem       = httpx.Msg{KK: "Мәзірдегі бір тағам қазір жоқ. Себетті жаңартыңыз.", RU: "Одной позиции из корзины сейчас нет. Обновите корзину."}
	MsgOptions    = httpx.Msg{KK: "Опцияларды қайта таңдаңыз.", RU: "Выберите опции заново."}
	MsgTooMuch    = httpx.Msg{KK: "Тапсырыс тым үлкен.", RU: "Слишком большой заказ."}
	MsgCodes      = httpx.Msg{KK: "Кофейняда ашық тапсырыс тым көп. Бірнеше минуттан соң қайталаңыз.", RU: "В кофейне слишком много открытых заказов. Повторите через несколько минут."}
	MsgActive     = httpx.Msg{KK: "Сізде алынбаған 2 тапсырыс бар. Алдымен оларды алыңыз не болдырмаңыз.", RU: "У вас уже 2 незабранных заказа. Сначала заберите или отмените их."}
	MsgDaily      = httpx.Msg{KK: "Бүгінгі тапсырыс шегіне жеттіңіз.", RU: "Вы достигли лимита заказов на сегодня."}
	MsgPayAhead   = httpx.Msg{KK: "Бірнеше тапсырыс алынбай қалды, сондықтан енді алдын ала Kaspi арқылы төлеу керек.", RU: "Несколько заказов не были забраны, поэтому теперь нужна предоплата через Kaspi."}
	MsgPayMethod  = httpx.Msg{KK: "Бұл кофейня бұл төлем тәсілін қабылдамайды.", RU: "Эта кофейня не принимает такой способ оплаты."}
	MsgKaspiDown  = httpx.Msg{KK: "Kaspi төлемі уақытша қолжетімсіз. Кассада төлеуді таңдаңыз.", RU: "Оплата Kaspi временно недоступна. Выберите оплату на кассе."}
	MsgTooLate    = httpx.Msg{KK: "Тапсырыс қабылданып қойды — оны енді болдырмау мүмкін емес. Кофейняға жазыңыз.", RU: "Заказ уже принят, отменить его нельзя. Обратитесь в кофейню."}
	MsgState      = httpx.Msg{KK: "Тапсырыс күйі өзгерген. Бетті жаңартыңыз.", RU: "Статус заказа изменился. Обновите страницу."}
	MsgNotPaid    = httpx.Msg{KK: "Тапсырыс әлі төленбеген.", RU: "Заказ ещё не оплачен."}
	MsgNoShowSoon = httpx.Msg{KK: "Алу уақыты әлі өтпеді.", RU: "Время выдачи ещё не прошло."}
	MsgPayBusy    = httpx.Msg{KK: "Төлем жасалып жатыр, бір сәттен соң қайталаңыз.", RU: "Платёж создаётся, повторите через секунду."}
	MsgNotStaff   = httpx.Msg{KK: "Сіз бұл кофейняның қызметкері емессіз.", RU: "Вы не сотрудник этой кофейни."}
	MsgManager    = httpx.Msg{KK: "Мәзірді тек менеджер өзгертеді.", RU: "Меню меняет только менеджер."}
	MsgCafeData   = httpx.Msg{KK: "Кофейня деректерін тексеріңіз.", RU: "Проверьте данные кофейни."}
	MsgMenuData   = httpx.Msg{KK: "Мәзір позициясын тексеріңіз: баға бүтін теңгемен, атаулары толық.", RU: "Проверьте позицию меню: цена в целых тенге, названия заполнены."}
	MsgNoUser     = httpx.Msg{KK: "Пайдаланушы табылмады.", RU: "Пользователь не найден."}
	MsgKaspiOff   = shopkit.MsgKaspiOff
	MsgKaspiLogin = shopkit.MsgKaspiLogin
)

func init() {
	me.RegisterExport("cafeOrders", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select id, cafe_id, items, total, pickup_at, status, payment_method, payment_status, note, created_at
			from cafe_orders where user_id = app_user_id() order by id`)
	})
}

// Routes wires every café endpoint, all behind FEATURE_CAFE.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.Require(Feature, fn)) }
	// public menu
	h("GET /api/cafes", listCafes)
	h("GET /api/cafes/{id}/menu", getMenu)
	// student
	h("POST /api/cafe/orders", auth.Write(auth.Need(createOrder)))
	h("GET /api/cafe/orders/mine", auth.Need(myOrders))
	h("GET /api/cafe/orders/{id}", auth.Need(getOrder))
	h("POST /api/cafe/orders/{id}/cancel", auth.Write(auth.Need(cancelOrder)))
	h("POST /api/cafe/orders/{id}/pay", auth.Write(auth.Need(payOrder)))
	// the sidecar (HMAC signature instead of a session)
	h("POST /api/cafe/kaspi/webhook", webhook)
	staffRoutes(h)
	adminRoutes(h)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("cafe: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func limited(w http.ResponseWriter, r *http.Request, key string, limit int, window time.Duration) bool {
	ok, retry := ratelimit.Allow(r.Context(), "cafe:"+key, limit, window)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited", httpx.MsgSlow)
	}
	return !ok
}

func consented(w http.ResponseWriter, r *http.Request, u *auth.User) bool {
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", me.MsgConsent)
		return false
	}
	return true
}

// ---------- model ----------

type Choice struct {
	ID     string `json:"id"`
	NameKK string `json:"nameKk"`
	NameRU string `json:"nameRu"`
	Delta  int64  `json:"delta"` // tiyn
}

type Option struct {
	ID       string   `json:"id"`
	NameKK   string   `json:"nameKk"`
	NameRU   string   `json:"nameRu"`
	Required bool     `json:"required"`
	Multi    bool     `json:"multi"`
	Choices  []Choice `json:"choices"`
}

type Cafe struct {
	ID           int64  `json:"id"`
	NameKK       string `json:"nameKk"`
	NameRU       string `json:"nameRu"`
	Building     string `json:"building"`
	PlaceKK      string `json:"placeKk"`
	PlaceRU      string `json:"placeRu"`
	OpenTime     string `json:"openTime"`
	CloseTime    string `json:"closeTime"`
	OpenDays     []int  `json:"openDays"`
	SlotMinutes  int    `json:"slotMinutes"`
	SlotCapacity int    `json:"slotCapacity"`
	LeadMinutes  int    `json:"leadMinutes"`
	Active       bool   `json:"active"`
	PaymentMode  string `json:"paymentMode"`
	IsDemo       bool   `json:"isDemo"`
	// computed
	OpenNow  bool     `json:"openNow"`
	Payments []string `json:"payments"` // what a student can choose right now: pickup, kaspi
	// admin only
	KaspiMerchantRef *string     `json:"kaspiMerchantRef,omitempty"`
	KaspiOrgID       *int64      `json:"kaspiOrgId,omitempty"`
	Kaspi            *KaspiState `json:"kaspi,omitempty"`
	Staff            []Staff     `json:"staff,omitempty"`
	// staff only
	MyRole string `json:"myRole,omitempty"`

	kaspiOK bool
}

type KaspiState = shopkit.KaspiState

type Staff struct {
	UserID int64   `json:"userId"`
	Name   *string `json:"name"`
	TG     *string `json:"username"`
	Role   string  `json:"role"`
}

type Category struct {
	ID     int64  `json:"id"`
	NameKK string `json:"nameKk"`
	NameRU string `json:"nameRu"`
	Sort   int    `json:"sort"`
	Active bool   `json:"active"`
}

type Item struct {
	ID         int64    `json:"id"`
	CafeID     int64    `json:"cafeId"`
	CategoryID *int64   `json:"categoryId"`
	NameKK     string   `json:"nameKk"`
	NameRU     string   `json:"nameRu"`
	DescKK     string   `json:"descKk"`
	DescRU     string   `json:"descRu"`
	Price      int64    `json:"price"` // tiyn
	Available  bool     `json:"available"`
	Archived   bool     `json:"archived"`
	Options    []Option `json:"options"`
	Sort       int      `json:"sort"`
}

const cafeCols = `c.id, c.name_kk, c.name_ru, c.building, c.place_kk, c.place_ru, to_char(c.open_time, 'HH24:MI'), to_char(c.close_time, 'HH24:MI'),
	c.open_days, c.slot_minutes, c.slot_capacity, c.lead_minutes, c.active, c.payment_mode, c.is_demo, c.kaspi_merchant_ref, c.kaspi_org_id,
	cafe_kaspi_ready(c.id)`

func scanCafe(row pgx.Row, extra ...any) (*Cafe, error) {
	var c Cafe
	var days []int16
	var ref string
	var org *int64
	err := row.Scan(append([]any{&c.ID, &c.NameKK, &c.NameRU, &c.Building, &c.PlaceKK, &c.PlaceRU, &c.OpenTime, &c.CloseTime,
		&days, &c.SlotMinutes, &c.SlotCapacity, &c.LeadMinutes, &c.Active, &c.PaymentMode, &c.IsDemo, &ref, &org, &c.kaspiOK}, extra...)...)
	if err != nil {
		return nil, err
	}
	for _, d := range days {
		c.OpenDays = append(c.OpenDays, int(d))
	}
	c.KaspiMerchantRef, c.KaspiOrgID = &ref, org
	return &c, nil
}

// public hides the admin-only fields and fills the computed ones.
func (c *Cafe) public(now time.Time) *Cafe {
	c.KaspiMerchantRef, c.KaspiOrgID = nil, nil
	c.OpenNow = len(Slots(c, now)) > 0
	c.Payments = c.allowed()
	return c
}

// allowed: pay at pickup when the café takes it; Kaspi only while configured and the café's session works.
func (c *Cafe) allowed() []string {
	out := []string{}
	if c.PaymentMode == "pickup" || c.PaymentMode == "both" {
		out = append(out, "pickup")
	}
	if (c.PaymentMode == "kaspi" || c.PaymentMode == "both") && c.kaspiOK && Provider() != nil {
		out = append(out, "kaspi")
	}
	return out
}

func (c *Cafe) accepts(method string) bool {
	for _, m := range c.allowed() {
		if m == method {
			return true
		}
	}
	return false
}

const itemCols = `i.id, i.cafe_id, i.category_id, i.name_kk, i.name_ru, i.desc_kk, i.desc_ru, i.price, i.available, i.archived, i.options, i.sort`

func scanItem(row pgx.Row) (Item, error) {
	var it Item
	var opts []byte
	err := row.Scan(&it.ID, &it.CafeID, &it.CategoryID, &it.NameKK, &it.NameRU, &it.DescKK, &it.DescRU, &it.Price, &it.Available, &it.Archived, &opts, &it.Sort)
	if err != nil {
		return it, err
	}
	if err := json.Unmarshal(opts, &it.Options); err != nil || it.Options == nil {
		it.Options = []Option{}
	}
	return it, nil
}

// ---------- opening hours and pickup slots ----------

func clock(s string) (int, bool) { return shopkit.Clock(s) }

func (c *Cafe) hours() shopkit.Hours {
	return shopkit.Hours{Active: c.Active, OpenDays: c.OpenDays, OpenTime: c.OpenTime, CloseTime: c.CloseTime, SlotMinutes: c.SlotMinutes, LeadMinutes: c.LeadMinutes}
}

// Slots are today's pickup times (Asia/Almaty) a student may still choose: every slot_minutes from opening
// to the last slot that starts before closing, no earlier than now + lead_minutes. Empty on a closed day.
func Slots(c *Cafe, now time.Time) []time.Time { return c.hours().Slots(now) }

// ValidSlot reports whether at is one of today's slots.
func ValidSlot(c *Cafe, now, at time.Time) bool { return c.hours().ValidSlot(now, at) }

// ---------- menu options and prices ----------

var reOptID = regexp.MustCompile(`^[a-z0-9_-]{1,24}$`)

// ValidateOptions checks an item's option groups as a manager entered them.
func ValidateOptions(opts []Option) error {
	if len(opts) > 8 {
		return errors.New("too many option groups")
	}
	seen := map[string]bool{}
	for _, o := range opts {
		if !reOptID.MatchString(o.ID) || seen[o.ID] || !textOK(o.NameKK, 1, 40) || !textOK(o.NameRU, 1, 40) {
			return errors.New("bad option group")
		}
		seen[o.ID] = true
		if len(o.Choices) == 0 || len(o.Choices) > 12 {
			return errors.New("bad choices")
		}
		cs := map[string]bool{}
		for _, ch := range o.Choices {
			if !reOptID.MatchString(ch.ID) || cs[ch.ID] || !textOK(ch.NameKK, 1, 40) || !textOK(ch.NameRU, 1, 40) ||
				ch.Delta < 0 || ch.Delta > 1000000 || ch.Delta%100 != 0 {
				return errors.New("bad choice")
			}
			cs[ch.ID] = true
		}
	}
	return nil
}

func textOK(s string, min, max int) bool { return shopkit.TextOK(s, min, max) }

// LineInput is one cart line as the client sends it: an item id, a quantity and the chosen option ids.
// The client never sends a price.
type LineInput struct {
	ItemID  int64               `json:"itemId"`
	Qty     int                 `json:"qty"`
	Options map[string][]string `json:"options"`
}

// LineOption and Line are the order's snapshot: names and prices as they were when the order was placed.
type LineOption struct {
	Group  string `json:"group"`
	Choice string `json:"choice"`
	NameKK string `json:"nameKk"`
	NameRU string `json:"nameRu"`
	Delta  int64  `json:"delta"`
}

type Line struct {
	ItemID    int64        `json:"itemId"`
	NameKK    string       `json:"nameKk"`
	NameRU    string       `json:"nameRu"`
	Qty       int          `json:"qty"`
	UnitPrice int64        `json:"unitPrice"`
	Options   []LineOption `json:"options"`
	Total     int64        `json:"total"`
}

var (
	errItem    = errors.New("cafe: item unavailable")
	errOptions = errors.New("cafe: bad options")
	errTooMuch = errors.New("cafe: order too large")
)

const (
	maxLines = 15
	maxQty   = 10
)

// Price recomputes an order from the café's own menu rows: unknown, archived or unavailable items, missing
// required options, unknown choices and over-limit quantities are refused. Prices come only from menu.
func Price(menu map[int64]Item, in []LineInput) ([]Line, int64, error) {
	if len(in) == 0 || len(in) > maxLines {
		return nil, 0, errTooMuch
	}
	var lines []Line
	var total int64
	for _, li := range in {
		it, ok := menu[li.ItemID]
		if !ok || it.Archived || !it.Available {
			return nil, 0, errItem
		}
		if li.Qty < 1 || li.Qty > maxQty {
			return nil, 0, errTooMuch
		}
		line := Line{ItemID: it.ID, NameKK: it.NameKK, NameRU: it.NameRU, Qty: li.Qty, UnitPrice: it.Price, Options: []LineOption{}}
		groups := map[string]Option{}
		for _, o := range it.Options {
			groups[o.ID] = o
		}
		for g := range li.Options {
			if _, ok := groups[g]; !ok {
				return nil, 0, errOptions
			}
		}
		for _, o := range it.Options {
			picked := li.Options[o.ID]
			if (o.Required && len(picked) == 0) || (!o.Multi && len(picked) > 1) || len(picked) > len(o.Choices) {
				return nil, 0, errOptions
			}
			dup := map[string]bool{}
			for _, cid := range picked {
				var found *Choice
				for i := range o.Choices {
					if o.Choices[i].ID == cid {
						found = &o.Choices[i]
					}
				}
				if found == nil || dup[cid] {
					return nil, 0, errOptions
				}
				dup[cid] = true
				line.UnitPrice += found.Delta
				line.Options = append(line.Options, LineOption{Group: o.ID, Choice: found.ID, NameKK: o.NameKK + ": " + found.NameKK, NameRU: o.NameRU + ": " + found.NameRU, Delta: found.Delta})
			}
		}
		line.Total = line.UnitPrice * int64(line.Qty)
		total += line.Total
		lines = append(lines, line)
	}
	if total <= 0 || total > maxTotal() {
		return nil, 0, errTooMuch
	}
	return lines, total, nil
}

// pickupCode is the three digits the student shows at the counter, unique among the café's open orders (the
// caller holds the café row lock; cafe_orders_open_code is the database's guard). Today's closed orders' codes
// are avoided while any other is free.
func pickupCode(ctx context.Context, tx pgx.Tx, cafeID int64, now time.Time) (string, error) {
	return shopkit.UniqueCode(ctx, tx, 3, "cafe_orders", "cafe_id", cafeID, []string{"new", "accepted", "ready"}, now)
}

// tenge formats tiyn as «1 250 ₸».
func tenge(tiyn int64) string { return shopkit.Tenge(tiyn) }

// ---------- public menu ----------

// GET /api/cafes: active cafés with what can be ordered now.
func listCafes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := Now()
	out := []*Cafe{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+cafeCols+` from cafes c where c.active and (not c.is_demo or $1) order by c.is_demo, c.id`, showDemo())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCafe(rows)
			if err != nil {
				return err
			}
			out = append(out, c.public(now))
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "cafes", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"cafes": out})
}

type Slot struct {
	At   time.Time `json:"at"`
	Full bool      `json:"full"`
}

// GET /api/cafes/{id}/menu: one active café, its categories, available items and today's pickup slots.
func getMenu(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	now := Now()
	var c *Cafe
	cats := []Category{}
	items := []Item{}
	slots := []Slot{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		c, err = scanCafe(tx.QueryRow(ctx, `select `+cafeCols+` from cafes c where c.id = $1 and c.active and (not c.is_demo or $2)`, id, showDemo()))
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select id, name_kk, name_ru, sort, active from cafe_menu_categories where cafe_id = $1 and active order by sort, id`, id)
		if err != nil {
			return err
		}
		cats, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Category, error) {
			var x Category
			return x, row.Scan(&x.ID, &x.NameKK, &x.NameRU, &x.Sort, &x.Active)
		})
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select `+itemCols+` from cafe_menu_items i where i.cafe_id = $1 and not i.archived order by i.sort, i.id`, id)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) { return scanItem(row) })
		if err != nil {
			return err
		}
		times := Slots(c, now)
		if len(times) == 0 {
			return nil
		}
		used := map[int64]int{}
		rows, err = tx.Query(ctx, `select pickup_at, count(*) from cafe_orders where cafe_id = $1 and pickup_at = any($2) and status <> 'cancelled' group by pickup_at`, id, times)
		if err != nil {
			return err
		}
		for rows.Next() {
			var at time.Time
			var n int
			if err := rows.Scan(&at, &n); err != nil {
				return err
			}
			used[at.Unix()] = n
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, t := range times {
			slots = append(slots, Slot{At: t.UTC(), Full: used[t.Unix()] >= c.SlotCapacity})
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "menu", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cafe": c.public(now), "categories": cats, "items": items, "slots": slots})
}
