package cafe

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

const webhookSecret = "test-webhook-secret-0123456789abcdef"

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("c", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("CAFE_ORDERS_PER_HOUR", "100000")
	os.Setenv("CAFE_DAILY_ORDERS", "1000")
	os.Setenv("FEATURE_CAFE", "true")
	os.Setenv("ALLOWED_HOSTS", "localhost")
	os.Setenv("CAFE_KASPI_SESSION_KEY", strings.Repeat("ab", 32))
	os.Setenv("CAFE_KASPI_WEBHOOK_SECRET", webhookSecret)
	os.Setenv("SITE_URL", "https://studenthub.test")
	// noon of today in Almaty: inside the test café's hours, and the same Almaty day as the database's now()
	n := time.Now().In(almaty)
	noon := time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, almaty)
	Now = func() time.Time { return noon }
	// installed once: notification goroutines of one test may still run while the next test starts
	current.Store(&fakeKaspi{})
	Provider = func() payments.Provider { return current.Load() }
	Send = func(_ context.Context, uid int64, text string, opts notify.Opts) (notify.Result, error) {
		sentMu.Lock()
		defer sentMu.Unlock()
		messages = append(messages, sent{uid, text, opts.Key})
		return notify.Sent, nil
	}
	os.Exit(testdb.Main(m, "cafe"))
}

// ---------- fakes ----------

type fakeKaspi struct {
	mu        sync.Mutex
	invoices  int
	statuses  int
	state     payments.State
	refundErr error
	refunds   []string
	delay     time.Duration
}

func (f *fakeKaspi) Name() string { return "fake" }
func (f *fakeKaspi) CreateInvoice(_ context.Context, m payments.Merchant, _ int64, amount int64) (*payments.Invoice, error) {
	if m.Session == nil {
		return nil, payments.ErrNoSession
	}
	f.mu.Lock()
	f.invoices++
	n, delay := f.invoices, f.delay
	f.mu.Unlock()
	time.Sleep(delay)
	op := fmt.Sprintf("77%06d%03d", time.Now().UnixNano()%1000000, n)
	exp := Now().Add(3 * time.Minute)
	return &payments.Invoice{OperationID: op, PayURL: "https://pay.kaspi.kz/pay/" + op, QR: "https://qr.kaspi.kz/" + op, Amount: amount, ExpiresAt: &exp}, nil
}
func (f *fakeKaspi) Status(_ context.Context, _ payments.Merchant, op string) (*payments.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses++
	st := f.state
	if st == "" {
		st = payments.StatePending
	}
	return &payments.Status{OperationID: op, State: st, Raw: string(st)}, nil
}
func (f *fakeKaspi) Refund(_ context.Context, _ payments.Merchant, op string, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds = append(f.refunds, op)
	return f.refundErr
}
func (f *fakeKaspi) count() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.invoices, len(f.refunds)
}

type sent struct {
	user int64
	text string
	key  string
}

var (
	sentMu   sync.Mutex
	messages []sent
)

var current atomic.Pointer[fakeKaspi]

// useFakes gives the test a fresh fake Kaspi (Send and Provider themselves are installed once in TestMain).
func useFakes(t *testing.T) *fakeKaspi {
	t.Helper()
	f := &fakeKaspi{}
	current.Store(f)
	return f
}

func (f *fakeKaspi) set(fn func(f *fakeKaspi)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func sentTo(uid int64, keyPrefix string) int {
	sentMu.Lock()
	defer sentMu.Unlock()
	n := 0
	for _, m := range messages {
		if m.user == uid && strings.HasPrefix(m.key, keyPrefix) {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---------- fixtures ----------

var seq atomic.Int64

func bg() context.Context { return context.Background() }

func newUser(t *testing.T, role string) int64 {
	t.Helper()
	testdb.Need(t)
	n := seq.Add(1)
	var id int64
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `insert into users (telegram_id, name, role, consent_at, bot_inactive) values ($1, $2, $3, now(), true) returning id`,
			770000+n+time.Now().UnixNano()%100000*10, fmt.Sprintf("Студент%d Тестов", n), role).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := db.Tx(bg(), func(tx pgx.Tx) error { _, err := tx.Exec(bg(), sql, args...); return err }); err != nil {
		t.Fatal(err)
	}
}

func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := db.Tx(bg(), func(tx pgx.Tx) error { return tx.QueryRow(bg(), sql, args...).Scan(&v) }); err != nil {
		t.Fatal(err)
	}
	return v
}

type fixture struct {
	cafe, latte, cookie, staff int64
}

// newCafe: open every day 08:00–20:00, 5-minute slots, latte with a required size and optional syrups.
func newCafe(t *testing.T, mode string) fixture {
	t.Helper()
	var f fixture
	f.cafe = scalar[int64](t, `insert into cafes (name_kk, name_ru, open_time, close_time, open_days, slot_minutes, slot_capacity, lead_minutes, active, payment_mode)
		values ('Тест кофе', 'Тест кофе', '08:00', '20:00', '{1,2,3,4,5,6,7}', 5, 100, 0, true, $1) returning id`, mode)
	opts := `[{"id":"size","nameKk":"Көлемі","nameRu":"Объём","required":true,"multi":false,"choices":[{"id":"s","nameKk":"S","nameRu":"S","delta":0},{"id":"l","nameKk":"L","nameRu":"L","delta":20000}]},
		{"id":"syrup","nameKk":"Сироп","nameRu":"Сироп","required":false,"multi":true,"choices":[{"id":"van","nameKk":"Ваниль","nameRu":"Ваниль","delta":10000},{"id":"car","nameKk":"Карамель","nameRu":"Карамель","delta":10000}]}]`
	f.latte = scalar[int64](t, `insert into cafe_menu_items (cafe_id, name_kk, name_ru, price, options) values ($1, 'Латте', 'Латте', 90000, $2) returning id`, f.cafe, opts)
	f.cookie = scalar[int64](t, `insert into cafe_menu_items (cafe_id, name_kk, name_ru, price) values ($1, 'Печенье', 'Печенье', 40000) returning id`, f.cafe)
	f.staff = newUser(t, "student")
	exec(t, `insert into cafe_staff (cafe_id, user_id, role) values ($1, $2, 'manager')`, f.cafe, f.staff)
	if mode != "pickup" {
		ct, err := payments.Encrypt(payments.Session{TokenSN: "T", VtokenSecret: "V", ProfileID: "1"}, payments.SessionKey())
		if err != nil {
			t.Fatal(err)
		}
		exec(t, `insert into cafe_kaspi_sessions (cafe_id, ciphertext, phone_hash) values ($1, $2, repeat('b', 64))`, f.cafe, ct)
	}
	return f
}

func handler() http.Handler {
	m := http.NewServeMux()
	Routes(m)
	return m
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Origin", "http://localhost")
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", w.Body, err)
	}
	return v
}

type orderReply struct {
	Order     Order `json:"order"`
	Duplicate bool  `json:"duplicate"`
}

var keySeq atomic.Int64

func key() string { return fmt.Sprintf("testkey%d", keySeq.Add(1)) }

func slot() time.Time { return Now().Add(10 * time.Minute).UTC() }

func orderBody(f fixture, payment string) map[string]any {
	return map[string]any{
		"cafeId": f.cafe, "pickupAt": slot(), "payment": payment, "key": key(),
		"items": []map[string]any{{"itemId": f.latte, "qty": 2, "options": map[string][]string{"size": {"l"}, "syrup": {"van"}}}, {"itemId": f.cookie, "qty": 1}},
	}
}

func place(t *testing.T, f fixture, uid int64, payment string) Order {
	t.Helper()
	w := do(t, "POST", "/api/cafe/orders", uid, orderBody(f, payment))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	return decode[orderReply](t, w).Order
}

func sign(raw []byte) string {
	m := hmac.New(sha256.New, []byte(webhookSecret))
	m.Write(raw)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func webhookCall(t *testing.T, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(payload)
	r := httptest.NewRequest("POST", "/api/cafe/kaspi/webhook", bytes.NewReader(raw))
	r.Header.Set("X-Webhook-Signature", sign(raw))
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
	return w
}

// ---------- pure logic ----------

func TestPriceComesOnlyFromTheMenu(t *testing.T) {
	size := Option{ID: "size", NameKK: "К", NameRU: "О", Required: true, Choices: []Choice{{ID: "s", NameKK: "S", NameRU: "S"}, {ID: "l", NameKK: "L", NameRU: "L", Delta: 20000}}}
	milk := Option{ID: "milk", NameKK: "С", NameRU: "М", Multi: true, Choices: []Choice{{ID: "oat", NameKK: "О", NameRU: "О", Delta: 15000}, {ID: "soy", NameKK: "С", NameRU: "С", Delta: 15000}}}
	menu := map[int64]Item{
		1: {ID: 1, NameKK: "Латте", NameRU: "Латте", Price: 90000, Available: true, Options: []Option{size, milk}},
		2: {ID: 2, NameKK: "Шай", NameRU: "Чай", Price: 30000, Available: false},
		3: {ID: 3, NameKK: "Ескі", NameRU: "Старое", Price: 30000, Available: true, Archived: true},
	}
	lines, total, err := Price(menu, []LineInput{{ItemID: 1, Qty: 2, Options: map[string][]string{"size": {"l"}, "milk": {"oat", "soy"}}}})
	if err != nil || total != 2*(90000+20000+30000) || lines[0].UnitPrice != 140000 || len(lines[0].Options) != 3 {
		t.Fatalf("price: %v %d %+v", err, total, lines)
	}
	for name, in := range map[string][]LineInput{
		"unavailable":      {{ItemID: 2, Qty: 1}},
		"archived":         {{ItemID: 3, Qty: 1}},
		"unknown item":     {{ItemID: 99, Qty: 1}},
		"missing size":     {{ItemID: 1, Qty: 1}},
		"two sizes":        {{ItemID: 1, Qty: 1, Options: map[string][]string{"size": {"s", "l"}}}},
		"unknown choice":   {{ItemID: 1, Qty: 1, Options: map[string][]string{"size": {"xl"}}}},
		"unknown group":    {{ItemID: 1, Qty: 1, Options: map[string][]string{"size": {"s"}, "extra": {"x"}}}},
		"duplicate milk":   {{ItemID: 1, Qty: 1, Options: map[string][]string{"size": {"s"}, "milk": {"oat", "oat"}}}},
		"zero quantity":    {{ItemID: 1, Qty: 0, Options: map[string][]string{"size": {"s"}}}},
		"eleven of a kind": {{ItemID: 1, Qty: 11, Options: map[string][]string{"size": {"s"}}}},
		"empty cart":       {},
	} {
		if _, _, err := Price(menu, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSlotsFollowOpeningHoursAndLeadTime(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 3, 0, 0, almaty) // Thursday
	c := &Cafe{Active: true, OpenTime: "08:00", CloseTime: "11:00", OpenDays: []int{4}, SlotMinutes: 15, LeadMinutes: 10}
	s := Slots(c, now)
	if len(s) != 3 || s[0].In(almaty).Format("15:04") != "10:15" || s[2].In(almaty).Format("15:04") != "10:45" {
		t.Fatalf("slots: %v", s)
	}
	if !ValidSlot(c, now, s[1]) || ValidSlot(c, now, s[1].Add(time.Minute)) || ValidSlot(c, now, now) {
		t.Fatal("ValidSlot")
	}
	c.OpenDays = []int{1, 2, 3, 5}
	if len(Slots(c, now)) != 0 {
		t.Fatal("closed weekday has slots")
	}
	c.OpenDays, c.Active = []int{4}, false
	if len(Slots(c, now)) != 0 {
		t.Fatal("inactive café has slots")
	}
}

func TestValidation(t *testing.T) {
	good := itemInput{CafeID: 1, NameKK: "Латте", NameRU: "Латте", Price: 90000}
	if !ValidItem(good) {
		t.Fatal("good item refused")
	}
	for name, mut := range map[string]func(*itemInput){
		"tiyn price": func(i *itemInput) { i.Price = 90050 },
		"zero price": func(i *itemInput) { i.Price = 0 },
		"no name":    func(i *itemInput) { i.NameRU = "" },
		"bad option": func(i *itemInput) {
			i.Options = []Option{{ID: "Bad Id", NameKK: "x", NameRU: "x", Choices: []Choice{{ID: "a", NameKK: "a", NameRU: "a"}}}}
		},
		"neg delta": func(i *itemInput) {
			i.Options = []Option{{ID: "s", NameKK: "x", NameRU: "x", Choices: []Choice{{ID: "a", NameKK: "a", NameRU: "a", Delta: -100}}}}
		},
	} {
		in := good
		mut(&in)
		if ValidItem(in) {
			t.Errorf("%s accepted", name)
		}
	}
	c := cafeInput{NameKK: "Кофе", NameRU: "Кофе", OpenTime: "08:00", CloseTime: "18:00", OpenDays: []int{1, 2}, SlotMinutes: 10, SlotCapacity: 5, LeadMinutes: 10, PaymentMode: "both"}
	if !ValidCafe(c) {
		t.Fatal("good café refused")
	}
	c.CloseTime = "07:00"
	if ValidCafe(c) {
		t.Fatal("closing before opening accepted")
	}
	if !VerifySignature([]byte("x"), sign([]byte("x")), webhookSecret) || VerifySignature([]byte("y"), sign([]byte("x")), webhookSecret) || VerifySignature([]byte("x"), sign([]byte("x")), "") {
		t.Fatal("signature check")
	}
}

// ---------- HTTP + database ----------

func TestOrderTotalIgnoresTheClient(t *testing.T) {
	useFakes(t)
	f := newCafe(t, "pickup")
	student := newUser(t, "student")
	body := orderBody(f, "pickup")
	body["total"] = 100 // 1 ₸: ignored
	body["items"].([]map[string]any)[0]["price"] = 100
	w := do(t, "POST", "/api/cafe/orders", student, body)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	o := decode[orderReply](t, w).Order
	// 2 × (900 + 200 L + 100 vanilla) + 400 = 2 800 ₸
	if o.Total != 280000 || o.Items[0].UnitPrice != 120000 || o.Status != "new" || o.PaymentStatus != "unpaid" || len(o.PickupCode) != 3 {
		t.Fatalf("order: %+v", o)
	}
	if got := scalar[int64](t, `select total from cafe_orders where id = $1`, o.ID); got != 280000 {
		t.Fatal("stored total", got)
	}
	// the counter heard about a pay-at-pickup order at once
	eventually(t, "staff message", func() bool { return sentTo(f.staff, fmt.Sprintf("cafe:new:%d", o.ID)) == 1 })
	// an unavailable item, a wrong slot and a closed café are refused
	exec(t, `update cafe_menu_items set available = false where id = $1`, f.cookie)
	if w := do(t, "POST", "/api/cafe/orders", student, orderBody(f, "pickup")); w.Code != 409 || !strings.Contains(w.Body.String(), "item_unavailable") {
		t.Fatalf("unavailable: %d %s", w.Code, w.Body)
	}
	exec(t, `update cafe_menu_items set available = true where id = $1`, f.cookie)
	b := orderBody(f, "pickup")
	b["pickupAt"] = slot().Add(2 * time.Minute)
	if w := do(t, "POST", "/api/cafe/orders", student, b); w.Code != 409 || !strings.Contains(w.Body.String(), "bad_slot") {
		t.Fatalf("slot: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/cafe/orders", student, orderBody(f, "kaspi")); w.Code != 409 {
		t.Fatalf("kaspi at a pickup-only café: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", "/api/cafe/orders", 0, orderBody(f, "pickup")); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
}

func TestIdempotentCreateAndMaxActiveOrders(t *testing.T) {
	useFakes(t)
	f := newCafe(t, "pickup")
	student := newUser(t, "student")
	body := orderBody(f, "pickup")
	first := do(t, "POST", "/api/cafe/orders", student, body)
	again := do(t, "POST", "/api/cafe/orders", student, body)
	if first.Code != 201 || again.Code != 200 || decode[orderReply](t, again).Order.ID != decode[orderReply](t, first).Order.ID || !decode[orderReply](t, again).Duplicate {
		t.Fatalf("retry: %d %d %s", first.Code, again.Code, again.Body)
	}
	second := place(t, f, student, "pickup")
	w := do(t, "POST", "/api/cafe/orders", student, orderBody(f, "pickup"))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "too_many_active") {
		t.Fatalf("third active order: %d %s", w.Code, w.Body)
	}
	// parallel attempts cannot slip past the limit either
	other := newUser(t, "student")
	var wg sync.WaitGroup
	var created atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := do(t, "POST", "/api/cafe/orders", other, orderBody(f, "pickup")); w.Code == 201 {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 2 {
		t.Fatalf("parallel: %d orders created, want 2", created.Load())
	}
	// cancelling frees a place
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/cancel", second.ID), student, nil); w.Code != 200 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	place(t, f, student, "pickup")
}

func TestCancelRules(t *testing.T) {
	fk := useFakes(t)
	f := newCafe(t, "both")
	student := newUser(t, "student")
	// accepted: too late
	o := place(t, f, student, "pickup")
	if _, err := Act(bg(), &auth.User{ID: f.staff, Role: "student"}, o.ID, "accept", ""); err != nil {
		t.Fatal(err)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/cancel", o.ID), student, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "too_late") {
		t.Fatalf("accepted: %d %s", w.Code, w.Body)
	}
	eventually(t, "accepted message", func() bool { return sentTo(student, fmt.Sprintf("cafe:accepted:%d", o.ID)) == 1 })
	// someone else's order looks missing
	other := newUser(t, "student")
	o2 := place(t, f, other, "pickup")
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/cancel", o2.ID), student, nil); w.Code != 404 {
		t.Fatalf("foreign cancel: %d", w.Code)
	}
	// a paid Kaspi order: cancelled and refunded through the café's session
	paid := place(t, f, other, "kaspi")
	w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", paid.ID), other, nil)
	op := *decode[orderReply](t, w).Order.operationIDFromPay(t)
	if w := webhookCall(t, map[string]any{"event": "payment.success", "paymentId": op, "status": "Processed", "amount": float64(paid.Total) / 100}); w.Code != 200 {
		t.Fatalf("webhook: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/cancel", paid.ID), other, nil); w.Code != 200 {
		t.Fatalf("cancel paid: %d %s", w.Code, w.Body)
	}
	if st := scalar[string](t, `select payment_status from cafe_orders where id = $1`, paid.ID); st != "refunded" {
		t.Fatal("not refunded:", st)
	}
	// the cashier may not refund: the café returns the money by hand
	fk.set(func(f *fakeKaspi) { f.refundErr = payments.ErrRefundNotAllowed })
	manual := place(t, f, student, "kaspi")
	w = do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", manual.ID), student, nil)
	op = *decode[orderReply](t, w).Order.operationIDFromPay(t)
	webhookCall(t, map[string]any{"event": "payment.success", "paymentId": op, "status": "Processed", "amount": float64(manual.Total) / 100})
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/cancel", manual.ID), student, nil); w.Code != 200 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	if !scalar[bool](t, `select refund_manual and payment_status = 'paid' from cafe_orders where id = $1`, manual.ID) {
		t.Fatal("refund_manual not set")
	}
	eventually(t, "manager alert", func() bool { return sentTo(f.staff, "cafe:refund_manual:") >= 1 })
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/staff/orders/%d/refunded", manual.ID), f.staff, nil); w.Code != 200 {
		t.Fatalf("mark refunded: %d %s", w.Code, w.Body)
	}
	if st := scalar[string](t, `select payment_status from cafe_orders where id = $1`, manual.ID); st != "refunded" {
		t.Fatal(st)
	}
}

// operationIDFromPay reads the operation id the pay endpoint stored (the reply only carries the links).
func (o Order) operationIDFromPay(t *testing.T) *string {
	t.Helper()
	if o.Pay == nil {
		t.Fatalf("no payment link: %+v", o)
	}
	op := scalar[string](t, `select kaspi_operation_id from cafe_orders where id = $1`, o.ID)
	return &op
}

func TestPayIsIdempotent(t *testing.T) {
	fk := useFakes(t)
	fk.set(func(f *fakeKaspi) { f.delay = 50 * time.Millisecond })
	f := newCafe(t, "kaspi")
	student := newUser(t, "student")
	o := place(t, f, student, "kaspi")
	if sentTo(f.staff, fmt.Sprintf("cafe:new:%d", o.ID)) != 0 {
		t.Fatal("unpaid Kaspi order announced to the counter")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o.ID), student, nil).Code
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 && c != 409 {
			t.Fatalf("pay: %d", c)
		}
	}
	w1 := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o.ID), student, nil)
	w2 := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o.ID), student, nil)
	p1, p2 := decode[orderReply](t, w1).Order.Pay, decode[orderReply](t, w2).Order.Pay
	if n, _ := fk.count(); n != 1 || p1 == nil || p2 == nil || p1.URL != p2.URL || !strings.HasPrefix(p1.URL, "https://pay.kaspi.kz/") {
		t.Fatalf("invoices created: %d, %+v %+v", n, p1, p2)
	}
	// someone else cannot pay (or see) the order
	if w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o.ID), newUser(t, "student"), nil); w.Code != 404 {
		t.Fatalf("foreign pay: %d", w.Code)
	}
	// the counter cannot start an unpaid Kaspi order
	if _, err := Act(bg(), &auth.User{ID: f.staff}, o.ID, "accept", ""); err == nil || !strings.Contains(err.Error(), "not_paid") {
		t.Fatalf("accept unpaid: %v", err)
	}
}

func TestWebhookMarksPaidExactlyOnce(t *testing.T) {
	fk := useFakes(t)
	f := newCafe(t, "kaspi")
	student := newUser(t, "student")
	o := place(t, f, student, "kaspi")
	w := do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o.ID), student, nil)
	op := *decode[orderReply](t, w).Order.operationIDFromPay(t)
	// a forged delivery is refused and changes nothing
	raw, _ := json.Marshal(map[string]any{"event": "payment.success", "paymentId": op, "status": "Processed"})
	r := httptest.NewRequest("POST", "/api/cafe/kaspi/webhook", bytes.NewReader(raw))
	r.Header.Set("X-Webhook-Signature", "sha256="+strings.Repeat("0", 64))
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, r)
	if rec.Code != 401 {
		t.Fatalf("forged: %d", rec.Code)
	}
	// webhooks, the poller and status reads race for the same payment
	fk.set(func(f *fakeKaspi) { f.state = payments.StatePaid })
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch i % 3 {
			case 0:
				webhookCall(t, map[string]any{"event": "payment.success", "paymentId": op, "status": "Processed", "amount": float64(o.Total) / 100})
			case 1:
				checkPayment(bg(), o.ID, f.cafe, op, "test")
			default:
				do(t, "GET", fmt.Sprintf("/api/cafe/orders/%d", o.ID), student, nil)
			}
		}()
	}
	wg.Wait()
	if n := scalar[int](t, `select count(*) from cafe_order_events where order_id = $1 and kind = 'paid'`, o.ID); n != 1 {
		t.Fatalf("paid events: %d", n)
	}
	if n := scalar[int](t, `select count(*) from cafe_kaspi_webhooks where operation_id = $1`, op); n != 1 {
		t.Fatalf("webhook rows: %d", n)
	}
	if st := scalar[string](t, `select payment_status from cafe_orders where id = $1`, o.ID); st != "paid" {
		t.Fatal(st)
	}
	eventually(t, "staff message", func() bool { return sentTo(f.staff, fmt.Sprintf("cafe:new:%d", o.ID)) == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := sentTo(f.staff, fmt.Sprintf("cafe:new:%d", o.ID)); n != 1 {
		t.Fatalf("staff told %d times", n)
	}
	// a later failure report does not undo the payment
	webhookCall(t, map[string]any{"event": "payment.failed", "paymentId": op, "status": "Rejected"})
	if st := scalar[string](t, `select payment_status from cafe_orders where id = $1`, o.ID); st != "paid" {
		t.Fatal("failure after payment:", st)
	}
	// a different amount is never marked paid
	o2 := place(t, f, student, "kaspi")
	w = do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o2.ID), student, nil)
	op2 := *decode[orderReply](t, w).Order.operationIDFromPay(t)
	if w := webhookCall(t, map[string]any{"event": "payment.success", "paymentId": op2, "status": "Processed", "amount": 1}); !strings.Contains(w.Body.String(), "amount_mismatch") {
		t.Fatal(w.Body.String())
	}
	if st := scalar[string](t, `select payment_status from cafe_orders where id = $1`, o2.ID); st != "pending" {
		t.Fatal(st)
	}
}

func TestStaffSeeOnlyTheirCafe(t *testing.T) {
	useFakes(t)
	a, b := newCafe(t, "pickup"), newCafe(t, "pickup")
	student := newUser(t, "student")
	oa := place(t, a, student, "pickup")
	ob := place(t, b, newUser(t, "student"), "pickup")
	w := do(t, "GET", "/api/cafe/staff/orders", a.staff, nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	board := decode[struct {
		Orders []Order `json:"orders"`
		Cafes  []Cafe  `json:"cafes"`
	}](t, w)
	if len(board.Orders) != 1 || board.Orders[0].ID != oa.ID || len(board.Cafes) != 1 || board.Cafes[0].ID != a.cafe || board.Orders[0].Customer == nil {
		t.Fatalf("board: %+v", board)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/cafe/staff/orders?cafe=%d", b.cafe), a.staff, nil); w.Code != 403 {
		t.Fatalf("other café board: %d", w.Code)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/cafe/orders/%d", ob.ID), a.staff, nil); w.Code != 404 {
		t.Fatalf("other café order: %d", w.Code)
	}
	if w := do(t, "GET", fmt.Sprintf("/api/cafe/orders/%d", oa.ID), a.staff, nil); w.Code != 200 {
		t.Fatalf("own café order: %d", w.Code)
	}
	for _, action := range []string{"accept", "ready", "decline"} {
		if w := do(t, "POST", fmt.Sprintf("/api/cafe/staff/orders/%d/%s", ob.ID, action), a.staff, nil); w.Code != 404 {
			t.Fatalf("%s other café: %d", action, w.Code)
		}
	}
	// RLS itself, not just the handler: app_user sees no row of the other café
	var n int
	if err := db.WithUser(bg(), a.staff, func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `select count(*) from cafe_orders where cafe_id = $1`, b.cafe).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("RLS leak: %d %v", n, err)
	}
	// a student is not staff
	if w := do(t, "GET", "/api/cafe/staff/orders", student, nil); w.Code != 403 {
		t.Fatalf("student board: %d", w.Code)
	}
	if w := do(t, "POST", "/api/cafe/staff/menu/items", a.staff, map[string]any{"cafeId": b.cafe, "nameKk": "X", "nameRu": "X", "price": 10000}); w.Code != 403 {
		t.Fatalf("menu of other café: %d", w.Code)
	}
	// the full flow at the own counter, with the student told twice
	for _, step := range []string{"accept", "ready", "picked-up"} {
		if w := do(t, "POST", fmt.Sprintf("/api/cafe/staff/orders/%d/%s", oa.ID, step), a.staff, nil); w.Code != 200 {
			t.Fatalf("%s: %d %s", step, w.Code, w.Body)
		}
	}
	eventually(t, "ready message", func() bool { return sentTo(student, fmt.Sprintf("cafe:ready:%d", oa.ID)) == 1 })
	if st := scalar[string](t, `select status || '/' || payment_status from cafe_orders where id = $1`, oa.ID); st != "picked_up/paid" {
		t.Fatal(st)
	}
	if n := scalar[int](t, `select count(*) from cafe_order_events where order_id = $1 and actor_kind = 'staff'`, oa.ID); n != 3 {
		t.Fatalf("audit rows: %d", n)
	}
}

func TestStudentsSeeOnlyTheirOrders(t *testing.T) {
	useFakes(t)
	f := newCafe(t, "pickup")
	alice, bob := newUser(t, "student"), newUser(t, "student")
	oa := place(t, f, alice, "pickup")
	place(t, f, bob, "pickup")
	if w := do(t, "GET", fmt.Sprintf("/api/cafe/orders/%d", oa.ID), bob, nil); w.Code != 404 {
		t.Fatalf("bob reads alice's order: %d", w.Code)
	}
	mine := decode[struct {
		Orders []Order `json:"orders"`
	}](t, do(t, "GET", "/api/cafe/orders/mine", bob, nil))
	for _, o := range mine.Orders {
		if o.ID == oa.ID {
			t.Fatal("alice's order in bob's list")
		}
	}
	if len(mine.Orders) != 1 {
		t.Fatalf("bob's orders: %d", len(mine.Orders))
	}
	var n int
	if err := db.WithUser(bg(), bob, func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `select count(*) from cafe_orders where user_id = $1`, alice).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("RLS leak: %d %v", n, err)
	}
	if err := db.WithUser(bg(), bob, func(tx pgx.Tx) error {
		_, err := tx.Exec(bg(), `update cafe_orders set total = 100 where user_id = $1`, bob)
		return err
	}); err == nil {
		t.Fatal("app_user may write orders")
	}
	if err := db.WithUser(bg(), bob, func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `select count(*) from cafe_kaspi_sessions`).Scan(&n)
	}); err == nil {
		t.Fatal("app_user reads cashier sessions")
	}
}

func TestNoShowsRequirePayAhead(t *testing.T) {
	useFakes(t)
	f := newCafe(t, "both")
	student := newUser(t, "student")
	prev := Now
	t.Cleanup(func() { Now = prev })
	for range 2 {
		o := place(t, f, student, "pickup")
		if _, err := Act(bg(), &auth.User{ID: f.staff}, o.ID, "ready", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := Act(bg(), &auth.User{ID: f.staff}, o.ID, "no-show", ""); err == nil || !strings.Contains(err.Error(), "too_early") {
			t.Fatalf("no-show before pickup time: %v", err)
		}
		at := prev().Add(30 * time.Minute)
		Now = func() time.Time { return at }
		if _, err := Act(bg(), &auth.User{ID: f.staff}, o.ID, "no-show", ""); err != nil {
			t.Fatal(err)
		}
		Now = prev
	}
	w := do(t, "POST", "/api/cafe/orders", student, orderBody(f, "pickup"))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "pay_ahead_required") {
		t.Fatalf("after no-shows: %d %s", w.Code, w.Body)
	}
	place(t, f, student, "kaspi")
}

func TestPollerTimesOutAndCancelsUnpaid(t *testing.T) {
	useFakes(t)
	f := newCafe(t, "kaspi")
	student := newUser(t, "student")
	o := place(t, f, student, "kaspi")
	do(t, "POST", fmt.Sprintf("/api/cafe/orders/%d/pay", o.ID), student, nil)
	exec(t, `update cafe_orders set pay_started_at = $2, created_at = $2 where id = $1`, o.ID, Now().Add(-time.Hour))
	res, err := Sweep(bg())
	if err != nil {
		t.Fatal(err)
	}
	if res.TimedOut < 1 || res.Cancelled < 1 {
		t.Fatalf("sweep: %+v", res)
	}
	if st := scalar[string](t, `select status || '/' || payment_status from cafe_orders where id = $1`, o.ID); st != "cancelled/failed" {
		t.Fatal(st)
	}
	// a session Kaspi rejects stops Kaspi from being offered
	markBroken(bg(), f.cafe, errors.New("evicted"))
	menu := decode[struct {
		Cafe Cafe `json:"cafe"`
	}](t, do(t, "GET", fmt.Sprintf("/api/cafes/%d/menu", f.cafe), 0, nil))
	if len(menu.Cafe.Payments) != 0 {
		t.Fatalf("payments offered with a broken session: %v", menu.Cafe.Payments)
	}
}

func TestFeatureSwitchHidesEverything(t *testing.T) {
	t.Setenv("FEATURE_CAFE", "false")
	for _, p := range []string{"/api/cafes", "/api/cafe/orders/mine"} {
		if w := do(t, "GET", p, 0, nil); w.Code != 404 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
}

func TestDemoCafeOnlyWhereAllowed(t *testing.T) {
	testdb.Need(t)
	id, err := SeedDemo(bg())
	if err != nil {
		t.Fatal(err)
	}
	if again, err := SeedDemo(bg()); err != nil || again != id {
		t.Fatalf("seed twice: %d %d %v", id, again, err)
	}
	listed := func() bool {
		var out struct {
			Cafes []Cafe `json:"cafes"`
		}
		_ = json.Unmarshal(do(t, "GET", "/api/cafes", 0, nil).Body.Bytes(), &out)
		for _, c := range out.Cafes {
			if c.ID == id {
				return c.IsDemo
			}
		}
		return false
	}
	t.Setenv("CAFE_SHOW_DEMO", "false")
	if listed() || do(t, "GET", fmt.Sprintf("/api/cafes/%d/menu", id), 0, nil).Code != 404 {
		t.Fatal("demo café visible without CAFE_SHOW_DEMO")
	}
	t.Setenv("CAFE_SHOW_DEMO", "true")
	if !listed() {
		t.Fatal("demo café hidden")
	}
	menu := decode[struct {
		Items []Item `json:"items"`
	}](t, do(t, "GET", fmt.Sprintf("/api/cafes/%d/menu", id), 0, nil))
	if len(menu.Items) != 5 || len(menu.Items[0].Options) != 3 {
		t.Fatalf("demo menu: %+v", menu.Items)
	}
}
