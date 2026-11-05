// Package printshop is the campus print / copy shop («Полиграфия», docs/print.md), FEATURE_PRINT: a student uploads
// files, configures each one (copies, colour, duplex, page range, paper size, binding, lamination), adds goods, sees
// the price the server computed, joins the shop's queue, pays with Kaspi straight to the shop's own Kaspi Pay
// account or at the counter, and is told when the job is ready. The operator works the queue on /print/staff.
//
// Security model (as the café, internal/cafe): every write is the server (connection owner) with explicit checks
// here; reads for a student or a staff member go through db.WithUser, so Row Level Security alone limits a student
// to their own jobs and files and staff to their own shop. Prices are recomputed by the server (pricing.go). Files
// are private: magic-byte validation, per-student daily quotas, random names, short-lived signed download links,
// automatic deletion after the job is closed (files.go). The shared café layer is internal/shopkit.
package printshop

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

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

const Feature = "print"

// Now is replaced by tests.
var Now = time.Now

// Provider is the Kaspi payment provider (the café's sidecar), nil while Kaspi is not configured. Tests replace it.
var Provider = func() payments.Provider {
	if k := payments.KaspiFromEnv(); k != nil {
		return k
	}
	return nil
}

// Cashier is the sidecar for the admin login flow, nil while not configured. Tests replace it.
var Cashier = func() shopkit.CashierLogin {
	if k := payments.KaspiFromEnv(); k != nil {
		return k
	}
	return nil
}

var sessions = shopkit.Sessions{Table: "print_kaspi_sessions", Key: "shop_id", Name: "print"}

// Limits (env, docs/print.md).
func maxActive() int       { return config.Int("PRINT_MAX_ACTIVE_JOBS", 3) }
func dailyJobs() int       { return config.Int("PRINT_DAILY_JOBS", 10) }
func jobsPerHour() int     { return config.Int("PRINT_JOBS_PER_HOUR", 20) }
func maxTotal() int64      { return int64(config.Int("PRINT_MAX_ORDER_TOTAL", 10000000)) } // tiyn: 100 000 ₸
func payTimeout() int      { return config.Int("PRINT_PAY_TIMEOUT_MIN", 10) }
func unpaidCancelMin() int { return config.Int("PRINT_UNPAID_CANCEL_MIN", 60) }
func noShowGrace() int     { return config.Int("PRINT_NO_SHOW_GRACE_MIN", 120) }

var (
	MsgClosed     = httpx.Msg{KK: "Полиграфия қазір тапсырыс қабылдамайды.", RU: "Полиграфия сейчас не принимает заказы."}
	MsgSlot       = httpx.Msg{KK: "Бұл уақыт қолжетімсіз. Басқа уақытты не «Мүмкіндігінше тез» таңдаңыз.", RU: "Это время недоступно. Выберите другое или «Как можно скорее»."}
	MsgActive     = httpx.Msg{KK: "Сізде алынбаған тапсырыстар көп. Алдымен оларды алыңыз не болдырмаңыз.", RU: "У вас много незабранных заказов. Сначала заберите или отмените их."}
	MsgDaily      = httpx.Msg{KK: "Бүгінгі тапсырыс шегіне жеттіңіз.", RU: "Вы достигли лимита заказов на сегодня."}
	MsgPayMethod  = httpx.Msg{KK: "Бұл полиграфия бұл төлем тәсілін қабылдамайды.", RU: "Эта полиграфия не принимает такой способ оплаты."}
	MsgKaspiDown  = httpx.Msg{KK: "Kaspi төлемі уақытша қолжетімсіз. Кассада төлеуді таңдаңыз.", RU: "Оплата Kaspi временно недоступна. Выберите оплату на кассе."}
	MsgTooLate    = httpx.Msg{KK: "Тапсырыс басылып жатыр — оны енді болдырмау мүмкін емес. Полиграфияға жазыңыз.", RU: "Заказ уже печатается, отменить нельзя. Обратитесь в полиграфию."}
	MsgState      = httpx.Msg{KK: "Тапсырыс күйі өзгерген. Бетті жаңартыңыз.", RU: "Статус заказа изменился. Обновите страницу."}
	MsgNotPaid    = httpx.Msg{KK: "Тапсырыс әлі төленбеген.", RU: "Заказ ещё не оплачен."}
	MsgPriceOpen  = httpx.Msg{KK: "Алдымен бағаны растаңыз: оператор бетті тексеріп жатыр.", RU: "Сначала подтвердите цену: оператор проверяет страницы."}
	MsgPaidAlrdy  = httpx.Msg{KK: "Тапсырыс төленіп қойған: бағаны кассада реттеңіз.", RU: "Заказ уже оплачен: разницу решите на кассе."}
	MsgReason     = httpx.Msg{KK: "Бағаны неге өзгерткеніңізді жазыңыз (студент көреді).", RU: "Напишите, почему меняете цену (студент увидит)."}
	MsgNoShowSoon = httpx.Msg{KK: "Тапсырыс дайын болғанына әлі көп болған жоқ.", RU: "Заказ готов совсем недавно."}
	MsgPayBusy    = httpx.Msg{KK: "Төлем жасалып жатыр, бір сәттен соң қайталаңыз.", RU: "Платёж создаётся, повторите через секунду."}
	MsgNotStaff   = httpx.Msg{KK: "Сіз бұл полиграфияның қызметкері емессіз.", RU: "Вы не сотрудник этой полиграфии."}
	MsgManager    = httpx.Msg{KK: "Прайсты тек менеджер өзгертеді.", RU: "Прайс меняет только менеджер."}
	MsgShopData   = httpx.Msg{KK: "Полиграфия деректерін тексеріңіз.", RU: "Проверьте данные полиграфии."}
	MsgCatalog    = httpx.Msg{KK: "Қызметті тексеріңіз: баға бүтін теңгемен, атаулары толық.", RU: "Проверьте услугу: цена в целых тенге, названия заполнены."}
	MsgNoUser     = httpx.Msg{KK: "Пайдаланушы табылмады.", RU: "Пользователь не найден."}
)

// quoteMsgs: the price engine's refusals.
var quoteMsgs = map[string]httpx.Msg{
	"empty_job":           {KK: "Файл не тауар қосыңыз.", RU: "Добавьте файл или товар."},
	"bad_file":            {KK: "Файл табылмады не басқа тапсырыста тұр. Қайта жүктеңіз.", RU: "Файл не найден или уже в другом заказе. Загрузите заново."},
	"format_not_accepted": {KK: "Бұл полиграфия мұндай файлды қабылдамайды.", RU: "Эта полиграфия не принимает такой формат."},
	"service_unavailable": {KK: "Таңдалған қызмет қазір жоқ. Бетті жаңартыңыз.", RU: "Выбранной услуги сейчас нет. Обновите страницу."},
	"bad_size":            {KK: "Бұл қағаз өлшемі бұл қызметте жоқ.", RU: "Этого формата бумаги нет в услуге."},
	"no_color":            {KK: "Бұл өлшемде түрлі түсті басу жоқ.", RU: "Цветной печати этого формата нет."},
	"no_duplex":           {KK: "Бұл өлшемде екі жақты басу жоқ.", RU: "Двусторонней печати этого формата нет."},
	"bad_range":           {KK: "Беттер аралығын тексеріңіз: мысалы 1-5, 8.", RU: "Проверьте диапазон страниц: например 1-5, 8."},
	"pages_required":      {KK: "Файлдағы бет санын жазыңыз (оператор тексереді).", RU: "Укажите число страниц в файле (оператор проверит)."},
	"bad_copies":          {KK: "Дана саны 1-ден 100-ге дейін.", RU: "Количество копий — от 1 до 100."},
	"binding_too_thick":   {KK: "Бұл файл түптеуге тым қалың.", RU: "Файл слишком толстый для переплёта."},
	"goods_unavailable":   {KK: "Бір тауар қазір жоқ. Тізімді жаңартыңыз.", RU: "Одного товара сейчас нет. Обновите список."},
	"too_large":           {KK: "Тапсырыс тым үлкен.", RU: "Слишком большой заказ."},
	"duplicate_file":      {KK: "Бір файл екі рет қосылған.", RU: "Один файл добавлен дважды."},
}

func init() {
	me.RegisterExport("printJobs", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select id, shop_id, lines, total, price_state, pickup_at, status, payment_method, payment_status, note, created_at
			from print_jobs where user_id = app_user_id() order by id`)
	})
	me.RegisterExport("printFiles", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select id, job_id, display_name, kind, bytes, pages, created_at, deleted_at
			from print_files where user_id = app_user_id() order by id`)
	})
	me.RegisterDeleteHook(deleteUserFiles)
	shopkit.RegisterOperations(reconcileWebhook)
}

// Routes wires every print endpoint, all behind FEATURE_PRINT.
func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.Require(Feature, fn)) }
	// public catalogue
	h("GET /api/print/shops", listShops)
	h("GET /api/print/shops/{id}", getShop)
	// student
	h("POST /api/print/uploads", auth.Write(auth.Need(uploadFile)))
	h("DELETE /api/print/uploads/{id}", auth.Write(auth.Need(deleteUpload)))
	h("POST /api/print/quote", auth.Write(auth.Need(quoteJob)))
	h("POST /api/print/jobs", auth.Write(auth.Need(createJob)))
	h("GET /api/print/jobs/mine", auth.Need(myJobs))
	h("GET /api/print/jobs/{id}", auth.Need(getJob))
	h("POST /api/print/jobs/{id}/cancel", auth.Write(auth.Need(cancelJob)))
	h("POST /api/print/jobs/{id}/price", auth.Write(auth.Need(answerPrice)))
	h("POST /api/print/jobs/{id}/pay", auth.Write(auth.Need(payJob)))
	// a file, by a short-lived signed link (no session: the link itself is the permission)
	h("GET /api/print/files/{id}", serveFile)
	staffRoutes(h)
	adminRoutes(h)
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("print: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func limited(w http.ResponseWriter, r *http.Request, key string, limit int, window time.Duration) bool {
	ok, retry := ratelimit.Allow(r.Context(), "print:"+key, limit, window)
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

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func uid(u *auth.User) *int64 { id := u.ID; return &id }

// apiErr is a refusal with its HTTP status, code and message.
type apiErr struct {
	status int
	code   string
	msg    httpx.Msg
}

func (e *apiErr) Error() string { return e.code }

func refuse(status int, code string, msg httpx.Msg) error { return &apiErr{status, code, msg} }

// reply writes err as an API error (a refusal, a quote error) or a 500.
func reply(w http.ResponseWriter, r *http.Request, what string, err error) {
	var ae *apiErr
	if errors.As(err, &ae) {
		httpx.Error(w, r, ae.status, ae.code, ae.msg)
		return
	}
	var qe quoteErr
	if errors.As(err, &qe) {
		status := http.StatusBadRequest
		if qe == "service_unavailable" || qe == "goods_unavailable" || qe == "bad_file" {
			status = http.StatusConflict
		}
		httpx.Error(w, r, status, string(qe), quoteMsgs[string(qe)])
		return
	}
	fail(w, r, what, err)
}

// ---------- model ----------

type Shop struct {
	ID            int64    `json:"id"`
	NameKK        string   `json:"nameKk"`
	NameRU        string   `json:"nameRu"`
	Building      string   `json:"building"`
	PlaceKK       string   `json:"placeKk"`
	PlaceRU       string   `json:"placeRu"`
	OpenTime      string   `json:"openTime"`
	CloseTime     string   `json:"closeTime"`
	OpenDays      []int    `json:"openDays"`
	SlotMinutes   int      `json:"slotMinutes"`
	LeadMinutes   int      `json:"leadMinutes"`
	MinOrder      int64    `json:"minOrder"`
	Accepts       []string `json:"accepts"`
	RetentionDays int      `json:"retentionDays"`
	Active        bool     `json:"active"`
	PaymentMode   string   `json:"paymentMode"`
	NoteKK        string   `json:"noteKk"`
	NoteRU        string   `json:"noteRu"`
	// computed
	OpenNow  bool     `json:"openNow"`
	Payments []string `json:"payments"`
	// admin only
	KaspiMerchantRef *string             `json:"kaspiMerchantRef,omitempty"`
	KaspiOrgID       *int64              `json:"kaspiOrgId,omitempty"`
	Kaspi            *shopkit.KaspiState `json:"kaspi,omitempty"`
	Staff            []Staff             `json:"staff,omitempty"`
	// staff only
	MyRole string `json:"myRole,omitempty"`

	kaspiOK bool
}

type Staff struct {
	UserID int64   `json:"userId"`
	Name   *string `json:"name"`
	TG     *string `json:"username"`
	Role   string  `json:"role"`
}

type Service struct {
	ID      int64   `json:"id"`
	ShopID  int64   `json:"shopId"`
	Kind    string  `json:"kind"`
	NameKK  string  `json:"nameKk"`
	NameRU  string  `json:"nameRu"`
	DescKK  string  `json:"descKk"`
	DescRU  string  `json:"descRu"`
	Pricing Pricing `json:"pricing"`
	Online  bool    `json:"online"`
	Active  bool    `json:"active"`
	Sort    int     `json:"sort"`
}

type Goods struct {
	ID       int64  `json:"id"`
	ShopID   int64  `json:"shopId"`
	NameKK   string `json:"nameKk"`
	NameRU   string `json:"nameRu"`
	Price    int64  `json:"price"`
	InStock  bool   `json:"inStock"`
	Archived bool   `json:"archived"`
	Sort     int    `json:"sort"`
}

const shopCols = `s.id, s.name_kk, s.name_ru, s.building, s.place_kk, s.place_ru, to_char(s.open_time, 'HH24:MI'), to_char(s.close_time, 'HH24:MI'),
	s.open_days, s.slot_minutes, s.lead_minutes, s.min_order, s.accepts, s.retention_days, s.active, s.payment_mode, s.note_kk, s.note_ru,
	s.kaspi_merchant_ref, s.kaspi_org_id, print_kaspi_ready(s.id)`

func scanShop(row pgx.Row, extra ...any) (*Shop, error) {
	var s Shop
	var days []int16
	var ref string
	var org *int64
	err := row.Scan(append([]any{&s.ID, &s.NameKK, &s.NameRU, &s.Building, &s.PlaceKK, &s.PlaceRU, &s.OpenTime, &s.CloseTime,
		&days, &s.SlotMinutes, &s.LeadMinutes, &s.MinOrder, &s.Accepts, &s.RetentionDays, &s.Active, &s.PaymentMode, &s.NoteKK, &s.NoteRU,
		&ref, &org, &s.kaspiOK}, extra...)...)
	if err != nil {
		return nil, err
	}
	for _, d := range days {
		s.OpenDays = append(s.OpenDays, int(d))
	}
	s.KaspiMerchantRef, s.KaspiOrgID = &ref, org
	return &s, nil
}

func (s *Shop) hours() shopkit.Hours {
	return shopkit.Hours{Active: s.Active, OpenDays: s.OpenDays, OpenTime: s.OpenTime, CloseTime: s.CloseTime, SlotMinutes: s.SlotMinutes, LeadMinutes: s.LeadMinutes}
}

// public hides the admin-only fields and fills the computed ones.
func (s *Shop) public(now time.Time) *Shop {
	s.KaspiMerchantRef, s.KaspiOrgID = nil, nil
	s.OpenNow = s.hours().OpenAt(now)
	s.Payments = s.allowed()
	return s
}

// allowed: pay at the counter when the shop takes it; Kaspi only while configured and the shop's session works.
func (s *Shop) allowed() []string {
	out := []string{}
	if s.PaymentMode == "pickup" || s.PaymentMode == "both" {
		out = append(out, "pickup")
	}
	if (s.PaymentMode == "kaspi" || s.PaymentMode == "both") && s.kaspiOK && Provider() != nil {
		out = append(out, "kaspi")
	}
	return out
}

func (s *Shop) accepts(method string) bool { return contains(s.allowed(), method) }

const serviceCols = `v.id, v.shop_id, v.kind, v.name_kk, v.name_ru, v.desc_kk, v.desc_ru, v.pricing, v.online, v.active, v.sort`

func scanService(row pgx.Row) (Service, error) {
	var v Service
	var raw []byte
	err := row.Scan(&v.ID, &v.ShopID, &v.Kind, &v.NameKK, &v.NameRU, &v.DescKK, &v.DescRU, &raw, &v.Online, &v.Active, &v.Sort)
	if err != nil {
		return v, err
	}
	_ = json.Unmarshal(raw, &v.Pricing)
	return v, nil
}

const goodsCols = `g.id, g.shop_id, g.name_kk, g.name_ru, g.price, g.in_stock, g.archived, g.sort`

func scanGoods(row pgx.Row) (Goods, error) {
	var g Goods
	return g, row.Scan(&g.ID, &g.ShopID, &g.NameKK, &g.NameRU, &g.Price, &g.InStock, &g.Archived, &g.Sort)
}

// catalog loads a shop's services and goods (all of them; Quote refuses the inactive ones).
func catalog(ctx context.Context, tx pgx.Tx, shopID int64) ([]Service, []Goods, error) {
	rows, err := tx.Query(ctx, `select `+serviceCols+` from print_services v where v.shop_id = $1 order by v.sort, v.id`, shopID)
	if err != nil {
		return nil, nil, err
	}
	svcs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Service, error) { return scanService(row) })
	if err != nil {
		return nil, nil, err
	}
	rows, err = tx.Query(ctx, `select `+goodsCols+` from print_goods g where g.shop_id = $1 order by g.archived, g.sort, g.id`, shopID)
	if err != nil {
		return nil, nil, err
	}
	goods, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Goods, error) { return scanGoods(row) })
	return svcs, goods, err
}

func catalogOf(s *Shop, svcs []Service, goods []Goods) Catalog {
	c := Catalog{MinOrder: s.MinOrder, Accepts: s.Accepts, MaxTotal: maxTotal(), Services: map[int64]Service{}, Goods: map[int64]Goods{}}
	for _, v := range svcs {
		c.Services[v.ID] = v
	}
	for _, g := range goods {
		c.Goods[g.ID] = g
	}
	return c
}

// ---------- public catalogue ----------

// GET /api/print/shops: active shops.
func listShops(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := Now()
	out := []*Shop{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select `+shopCols+` from print_shops s where s.active order by s.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scanShop(rows)
			if err != nil {
				return err
			}
			out = append(out, s.public(now))
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, r, "shops", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"shops": out})
}

type Slot struct {
	At time.Time `json:"at"`
}

// GET /api/print/shops/{id}: one active shop, its price list (every active service, online or at the counter),
// goods and today's pickup times.
func getShop(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	ctx := r.Context()
	now := Now()
	var s *Shop
	svcs := []Service{}
	goods := []Goods{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		s, err = scanShop(tx.QueryRow(ctx, `select `+shopCols+` from print_shops s where s.id = $1 and s.active`, id))
		if err != nil {
			return err
		}
		all, allGoods, err := catalog(ctx, tx, id)
		for _, v := range all {
			if v.Active {
				svcs = append(svcs, v)
			}
		}
		for _, g := range allGoods {
			if !g.Archived {
				goods = append(goods, g)
			}
		}
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
		return
	}
	if err != nil {
		fail(w, r, "shop", err)
		return
	}
	slots := []Slot{}
	for _, t := range s.hours().Slots(now) {
		slots = append(slots, Slot{At: t.UTC()})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"shop": s.public(now), "services": svcs, "goods": goods, "slots": slots, "limits": limitsView()})
}

// limitsView: what the upload screen tells the student up front.
func limitsView() map[string]any {
	return map[string]any{"maxFileBytes": maxFileBytes, "maxFiles": maxJobFiles, "dailyFiles": dailyFiles(), "dailyBytes": dailyBytes()}
}
