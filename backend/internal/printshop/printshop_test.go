package printshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/cafe/payments"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/notify"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/shopkit"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

const webhookSecret = "test-webhook-secret-0123456789abcdef"

var uploadDir string

func TestMain(m *testing.M) {
	os.Setenv("SESSION_SECRET", strings.Repeat("p", 48))
	os.Setenv("RATE_WRITE_PER_MIN", "100000")
	os.Setenv("PRINT_JOBS_PER_HOUR", "100000")
	os.Setenv("PRINT_UPLOADS_PER_HOUR", "100000")
	os.Setenv("PRINT_DAILY_JOBS", "1000")
	os.Setenv("PRINT_MAX_ACTIVE_JOBS", "1000")
	os.Setenv("PRINT_DAILY_FILES", "1000")
	os.Setenv("FEATURE_PRINT", "true")
	os.Setenv("FEATURE_CAFE", "true")
	os.Setenv("ALLOWED_HOSTS", "localhost")
	os.Setenv("CAFE_KASPI_SESSION_KEY", strings.Repeat("ab", 32))
	os.Setenv("CAFE_KASPI_WEBHOOK_SECRET", webhookSecret)
	os.Setenv("SITE_URL", "https://studenthub.test")
	dir, err := os.MkdirTemp("", "print-test-*")
	if err != nil {
		panic(err)
	}
	uploadDir = dir
	os.Setenv("UPLOAD_DIR", dir)
	n := time.Now().In(shopkit.Almaty)
	noon := time.Date(n.Year(), n.Month(), n.Day(), 12, 0, 0, 0, shopkit.Almaty)
	Now = func() time.Time { return noon }
	current.Store(&fakeKaspi{})
	Provider = func() payments.Provider { return current.Load() }
	cafe.Provider = func() payments.Provider { return current.Load() }
	Send = func(_ context.Context, uid int64, text string, opts notify.Opts) (notify.Result, error) {
		sentMu.Lock()
		defer sentMu.Unlock()
		messages = append(messages, sent{uid, text, opts.Key})
		return notify.Sent, nil
	}
	code := testdb.Main(m, "print")
	os.RemoveAll(dir)
	os.Exit(code)
}

// ---------- fakes ----------

type fakeKaspi struct {
	mu        sync.Mutex
	invoices  int
	state     payments.State
	refundErr error
	refunds   []string
}

func (f *fakeKaspi) Name() string { return "fake" }
func (f *fakeKaspi) CreateInvoice(_ context.Context, m payments.Merchant, _ int64, amount int64) (*payments.Invoice, error) {
	if m.Session == nil {
		return nil, payments.ErrNoSession
	}
	f.mu.Lock()
	f.invoices++
	n := f.invoices
	f.mu.Unlock()
	op := fmt.Sprintf("66%06d%03d", time.Now().UnixNano()%1000000, n)
	exp := Now().Add(3 * time.Minute)
	return &payments.Invoice{OperationID: op, PayURL: "https://pay.kaspi.kz/pay/" + op, QR: "https://qr.kaspi.kz/" + op, Amount: amount, ExpiresAt: &exp}, nil
}
func (f *fakeKaspi) Status(_ context.Context, _ payments.Merchant, op string) (*payments.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

var current atomic.Pointer[fakeKaspi]

func useFakes() *fakeKaspi {
	f := &fakeKaspi{}
	current.Store(f)
	return f
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
	for i := 0; i < 150; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---------- files ----------

// makePDF builds a small valid PDF with n pages (classic xref table with exact offsets).
func makePDF(n int) []byte {
	var b bytes.Buffer
	offs := []int{}
	obj := func(body string) {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offs), body)
	}
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := []string{}
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for i := 0; i < n; i++ {
		obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>")
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, xref)
	return b.Bytes()
}

func makeOffice(t *testing.T, kind string, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`}
	switch kind {
	case "docx":
		files["word/document.xml"] = "<w:document/>"
		files["docProps/app.xml"] = `<Properties><Pages>12</Pages><Words>100</Words></Properties>`
	case "pptx":
		files["ppt/presentation.xml"] = "<p:presentation/>"
		files["docProps/app.xml"] = `<Properties><Slides>7</Slides></Properties>`
	case "xlsx":
		files["xl/workbook.xml"] = "<workbook/>"
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func makePNG() []byte {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 20, 30)))
	return buf.Bytes()
}

func TestInspectTrustsBytesNotNames(t *testing.T) {
	ctx := context.Background()
	kind, pages, _, err := Inspect(ctx, makePDF(7))
	if err != nil || kind != "pdf" || pages != 7 {
		t.Fatalf("pdf: %s %d %v", kind, pages, err)
	}
	// a broken PDF is still a PDF, but the server cannot count it: the student declares, the operator confirms
	kind, pages, _, err = Inspect(ctx, []byte("%PDF-1.7\nnot really a pdf at all\n"))
	if err != nil || kind != "pdf" || pages != 0 {
		t.Fatalf("broken pdf: %s %d %v", kind, pages, err)
	}
	if kind, pages, _, err := Inspect(ctx, makePNG()); err != nil || kind != "png" || pages != 1 {
		t.Fatalf("png: %s %d %v", kind, pages, err)
	}
	if kind, _, hint, err := Inspect(ctx, makeOffice(t, "docx", nil)); err != nil || kind != "docx" || hint != 12 {
		t.Fatalf("docx: %s %d %v", kind, hint, err)
	}
	if kind, _, hint, err := Inspect(ctx, makeOffice(t, "pptx", nil)); err != nil || kind != "pptx" || hint != 7 {
		t.Fatalf("pptx: %s %d %v", kind, hint, err)
	}
	if kind, _, hint, err := Inspect(ctx, makeOffice(t, "xlsx", nil)); err != nil || kind != "xlsx" || hint != 0 {
		t.Fatalf("xlsx: %s %d %v", kind, hint, err)
	}
	for name, data := range map[string][]byte{
		"exe renamed .pdf": append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{0}, 64)...),
		"html":             []byte("<html><script>alert(1)</script></html>"),
		"plain zip": func() []byte {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			w, _ := z.Create("a.txt")
			w.Write([]byte("x"))
			z.Close()
			return b.Bytes()
		}(),
		"old .doc (OLE)":     append([]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"), bytes.Repeat([]byte{0}, 64)...),
		"truncated jpeg":     {0xFF, 0xD8, 0xFF, 0xE0, 0, 0},
		"macro docm":         makeOffice(t, "docx", map[string]string{"word/vbaProject.bin": "VBA"}),
		"macro content-type": makeOffice(t, "docx", map[string]string{"[Content_Types].xml": `<Types><Override ContentType="application/vnd.ms-word.document.macroEnabled.main+xml"/></Types>`}),
	} {
		if _, _, _, err := Inspect(ctx, data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, _, _, err := Inspect(ctx, bytes.Repeat([]byte("a"), maxFileBytes+1)); err != errLarge {
		t.Errorf("too large: %v", err)
	}
	if got := DisplayName(`C:\Users\a\Курстық <жұмыс>.exe.pdf`, "pdf"); got != "Курстық жұмыс .exe.pdf" && got != "Курстық жұмыс.exe.pdf" {
		t.Errorf("display name: %q", got)
	}
	if got := DisplayName("../../etc/passwd", "pdf"); got != "passwd.pdf" {
		t.Errorf("display name path: %q", got)
	}
}

// ---------- pricing ----------

func TestPageRange(t *testing.T) {
	for in, want := range map[string]int{"": 10, "1-3": 3, "1-3,5": 4, "2, 4-6": 4, "1-10": 10, "3,3,3": 1, "1-4,3-6": 6} {
		if n, _, err := PageRange(in, 10); err != nil || n != want {
			t.Errorf("%q: %d %v", in, n, err)
		}
	}
	for _, bad := range []string{"0", "11", "5-3", "1-", "a", "1--2", "-3", "1,,2"} {
		if _, _, err := PageRange(bad, 10); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, norm, _ := PageRange("5,1-3,2", 10); norm != "1-3,5" {
		t.Errorf("normalized %q", norm)
	}
}

func testCatalog() Catalog {
	return Catalog{MinOrder: 0, Accepts: []string{"pdf", "docx", "xlsx", "pptx", "jpg", "png"}, MaxTotal: 10000000,
		Services: map[int64]Service{
			1: {ID: 1, Kind: "print", NameKK: "Басып шығару", NameRU: "Печать", Active: true, Online: true, Pricing: Pricing{Sizes: map[string]SizePrice{
				"A4": {BW: 2000, Color: 10000, BWDuplex: 1500, ColorDuplex: 9000}, "A3": {BW: 4000}}}},
			2: {ID: 2, Kind: "binding", NameKK: "Түптеу", NameRU: "Переплёт", Active: true, Online: true, Pricing: Pricing{Tiers: []Tier{{MaxSheets: 50, Price: 50000}, {MaxSheets: 150, Price: 80000}}}},
			3: {ID: 3, Kind: "lamination", NameKK: "Ламинация", NameRU: "Ламинация", Active: true, Online: true, Pricing: Pricing{Sheet: map[string]int64{"A4": 15000}}},
			4: {ID: 4, Kind: "unit", NameKK: "Сканерлеу", NameRU: "Скан", Active: true, Online: true, Pricing: Pricing{Price: 5000, UnitKK: "бет", UnitRU: "стр."}},
			5: {ID: 5, Kind: "unit", NameKK: "Визитка", NameRU: "Визитки", Active: true, Online: false, Pricing: Pricing{Price: 300000, UnitKK: "100 дана", UnitRU: "100 шт."}},
			6: {ID: 6, Kind: "print", NameKK: "Ескі", NameRU: "Старая", Active: false, Online: true, Pricing: Pricing{Sizes: map[string]SizePrice{"A4": {BW: 100}}}},
		},
		Goods: map[int64]Goods{
			10: {ID: 10, NameKK: "Папка", NameRU: "Папка", Price: 30000, InStock: true},
			11: {ID: 11, NameKK: "Қалам", NameRU: "Ручка", Price: 10000, InStock: false},
		},
	}
}

func ip(n int) *int { return &n }

func TestQuoteComesOnlyFromTheCatalogue(t *testing.T) {
	cat := testCatalog()
	files := map[int64]FileInfo{
		100: {ID: 100, Kind: "pdf", DisplayName: "a.pdf", Pages: ip(10)},
		101: {ID: 101, Kind: "docx", DisplayName: "b.docx", PagesHint: ip(12)},
		102: {ID: 102, Kind: "png", DisplayName: "c.png", Pages: ip(1)},
	}
	// 10 pages b/w A4 duplex, 3 copies, bound: 10 × 15 ₸ × 3 + 3 × 500 ₸
	q, err := Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A4", Duplex: true, Copies: 3, BindingID: 2}}})
	if err != nil || q.Review || q.Total != 10*1500*3+3*50000 || q.Lines[0].Sheets != 5 || len(q.Lines[0].Parts) != 2 {
		t.Fatalf("duplex bound: %v %+v", err, q)
	}
	// colour pages 1-3 and 5, laminated: 4 × 100 ₸ + 4 sheets × 150 ₸
	q, err = Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A4", Color: true, Copies: 1, Range: "1-3,5", LaminationID: 3}}})
	if err != nil || q.Total != 4*10000+4*15000 || q.Lines[0].Range != "1-3,5" {
		t.Fatalf("colour range: %v %+v", err, q)
	}
	// an Office file: the student's declared pages, the whole quote is a review
	q, err = Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 101, ServiceID: 1, Size: "A4", Copies: 2, Pages: 12}}})
	if err != nil || !q.Review || !q.Lines[0].PagesDeclared || q.Total != 12*2000*2 {
		t.Fatalf("declared: %v %+v", err, q)
	}
	// goods and a unit service; the minimum order tops up
	cat.MinOrder = 100000
	q, err = Quote(cat, files, QuoteInput{Goods: []QtyInput{{ID: 10, Qty: 2}}, Services: []QtyInput{{ID: 4, Qty: 3}}})
	if err != nil || q.Total != 100000 || q.Lines[len(q.Lines)-1].Kind != "minimum" || q.Lines[len(q.Lines)-1].Total != 100000-60000-15000 {
		t.Fatalf("minimum: %v %+v", err, q)
	}
	cat.MinOrder = 0
	// thick documents need the bigger binding tier; past the last tier binding is refused
	files[103] = FileInfo{ID: 103, Kind: "pdf", DisplayName: "d.pdf", Pages: ip(400)}
	if _, err := Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 103, ServiceID: 1, Size: "A4", Copies: 1, BindingID: 2}}}); err != errBinding {
		t.Errorf("thick binding: %v", err)
	}
	q, _ = Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 103, ServiceID: 1, Size: "A4", Duplex: true, Copies: 1, Range: "1-200", BindingID: 2}}})
	if q == nil || q.Lines[0].Parts[1].Amount != 80000 {
		t.Errorf("100 sheets bound: %+v", q)
	}
	cases := map[string]struct {
		in   QuoteInput
		want error
	}{
		"nothing":             {QuoteInput{}, errNoLines},
		"someone else's file": {QuoteInput{Files: []FileInput{{FileID: 999, ServiceID: 1, Size: "A4", Copies: 1}}}, errFile},
		"same file twice":     {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A4", Copies: 1}, {FileID: 100, ServiceID: 1, Size: "A4", Copies: 1}}}, errDuplicates},
		"inactive service":    {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 6, Size: "A4", Copies: 1}}}, errService},
		"binding as print":    {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 2, Size: "A4", Copies: 1}}}, errService},
		"unpriced size":       {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A0", Copies: 1}}}, errSize},
		"no colour on A3":     {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A3", Color: true, Copies: 1}}}, errColor},
		"no duplex on A3":     {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A3", Duplex: true, Copies: 1}}}, errDuplex},
		"lamination on A3":    {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A3", Copies: 1, LaminationID: 3}}}, errSize},
		"zero copies":         {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A4", Copies: 0}}}, errCopies},
		"101 copies":          {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A4", Copies: 101}}}, errCopies},
		"range past the end":  {QuoteInput{Files: []FileInput{{FileID: 100, ServiceID: 1, Size: "A4", Copies: 1, Range: "9-11"}}}, errRange},
		"office, no pages":    {QuoteInput{Files: []FileInput{{FileID: 101, ServiceID: 1, Size: "A4", Copies: 1}}}, errPages},
		"image range":         {QuoteInput{Files: []FileInput{{FileID: 102, ServiceID: 1, Size: "A4", Copies: 1, Range: "1"}}}, errRange},
		"out of stock":        {QuoteInput{Goods: []QtyInput{{ID: 11, Qty: 1}}}, errGoods},
		"counter-only":        {QuoteInput{Services: []QtyInput{{ID: 5, Qty: 1}}}, errService},
		"too many goods":      {QuoteInput{Goods: []QtyInput{{ID: 10, Qty: 51}}}, errTooMuch},
	}
	for name, c := range cases {
		if _, err := Quote(cat, files, c.in); err != c.want {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	// a format the shop does not accept
	cat.Accepts = []string{"pdf"}
	if _, err := Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 102, ServiceID: 1, Size: "A4", Copies: 1}}}); err != errFormat {
		t.Errorf("format: %v", err)
	}
	// a single page cannot be duplex: it is printed (and priced) simplex
	cat.Accepts = testCatalog().Accepts
	q, err = Quote(cat, files, QuoteInput{Files: []FileInput{{FileID: 102, ServiceID: 1, Size: "A4", Duplex: true, Copies: 2}}})
	if err != nil || q.Lines[0].Duplex || q.Total != 2*2000 {
		t.Errorf("one-page duplex: %v %+v", err, q)
	}
}

func TestValidPricing(t *testing.T) {
	good := map[string]Pricing{
		"print":      {Sizes: map[string]SizePrice{"A4": {BW: 2000}}},
		"binding":    {Tiers: []Tier{{MaxSheets: 20, Price: 30000}, {MaxSheets: 100, Price: 60000}}},
		"lamination": {Sheet: map[string]int64{"A4": 10000}},
		"unit":       {Price: 5000, UnitKK: "бет", UnitRU: "стр."},
	}
	for k, p := range good {
		if err := ValidPricing(k, p); err != nil {
			t.Errorf("%s refused", k)
		}
	}
	bad := map[string][2]any{
		"fractional tenge": {"print", Pricing{Sizes: map[string]SizePrice{"A4": {BW: 2050}}}},
		"unknown size":     {"print", Pricing{Sizes: map[string]SizePrice{"B5": {BW: 2000}}}},
		"duplex w/o bw":    {"print", Pricing{Sizes: map[string]SizePrice{"A4": {Color: 2000, BWDuplex: 1000}}}},
		"tiers unordered":  {"binding", Pricing{Tiers: []Tier{{MaxSheets: 100, Price: 30000}, {MaxSheets: 20, Price: 60000}}}},
		"unit no name":     {"unit", Pricing{Price: 5000}},
		"mixed":            {"lamination", Pricing{Sheet: map[string]int64{"A4": 10000}, Price: 100}},
		"unknown kind":     {"fax", Pricing{Price: 100}},
	}
	for name, c := range bad {
		if ValidPricing(c[0].(string), c[1].(Pricing)) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// ---------- database fixtures ----------

var seq atomic.Int64

func bg() context.Context { return context.Background() }

func newUser(t *testing.T) int64 {
	t.Helper()
	testdb.Need(t)
	n := seq.Add(1)
	var id int64
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		return tx.QueryRow(bg(), `insert into users (telegram_id, name, role, consent_at, bot_inactive) values ($1, $2, 'student', now(), true) returning id`,
			880000+n+time.Now().UnixNano()%100000*10, fmt.Sprintf("Студент%d Тестов", n)).Scan(&id)
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
	shop, print, binding, folder, operator, manager int64
}

// newShop: open every day 08:00–20:00, A4 b/w 20 ₸, colour 100 ₸, duplex 15 ₸ a side; binding; a folder.
func newShop(t *testing.T, mode string) fixture {
	t.Helper()
	var f fixture
	f.shop = scalar[int64](t, `insert into print_shops (name_kk, name_ru, place_kk, place_ru, open_time, close_time, open_days, slot_minutes, lead_minutes, active, payment_mode)
		values ('Тест полиграфия', 'Тест полиграфия', '1-қабат', '1 этаж', '08:00', '20:00', '{1,2,3,4,5,6,7}', 15, 0, true, $1) returning id`, mode)
	f.print = scalar[int64](t, `insert into print_services (shop_id, kind, name_kk, name_ru, pricing) values ($1, 'print', 'Басып шығару', 'Печать',
		'{"sizes":{"A4":{"bw":2000,"color":10000,"bwDuplex":1500,"colorDuplex":9000}}}') returning id`, f.shop)
	f.binding = scalar[int64](t, `insert into print_services (shop_id, kind, name_kk, name_ru, pricing) values ($1, 'binding', 'Түптеу', 'Переплёт',
		'{"tiers":[{"maxSheets":100,"price":50000}]}') returning id`, f.shop)
	f.folder = scalar[int64](t, `insert into print_goods (shop_id, name_kk, name_ru, price) values ($1, 'Папка', 'Папка', 30000) returning id`, f.shop)
	f.operator, f.manager = newUser(t), newUser(t)
	exec(t, `insert into print_staff (shop_id, user_id, role) values ($1, $2, 'operator'), ($1, $3, 'manager')`, f.shop, f.operator, f.manager)
	if mode != "pickup" {
		ct, err := payments.Encrypt(payments.Session{TokenSN: "T", VtokenSecret: "V", ProfileID: "1"}, payments.SessionKey())
		if err != nil {
			t.Fatal(err)
		}
		exec(t, `insert into print_kaspi_sessions (shop_id, ciphertext, phone_hash) values ($1, $2, repeat('c', 64))`, f.shop, ct)
	}
	return f
}

func handler() http.Handler {
	m := http.NewServeMux()
	Routes(m)
	cafe.Routes(m) // the sidecar's webhook lives there
	return m
}

func authed(t *testing.T, r *http.Request, uid int64) {
	t.Helper()
	r.Header.Set("Origin", "http://localhost")
	if uid != 0 {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
	}
}

func do(t *testing.T, method, path string, uid int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	authed(t, r, uid)
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
	return w
}

func upload(t *testing.T, uid int64, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", name)
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest("POST", "/api/print/uploads", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	authed(t, r, uid)
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

func uploaded(t *testing.T, uid int64, name string, data []byte) Upload {
	t.Helper()
	w := upload(t, uid, name, data)
	if w.Code != 201 {
		t.Fatalf("upload %s: %d %s", name, w.Code, w.Body)
	}
	return decode[struct{ Upload Upload }](t, w).Upload
}

var keySeq atomic.Int64

func key() string { return fmt.Sprintf("printkey%d", keySeq.Add(1)) }

type jobReply struct {
	Job       Job  `json:"job"`
	Duplicate bool `json:"duplicate"`
}

func place(t *testing.T, f fixture, uid int64, payment string, files ...map[string]any) Job {
	t.Helper()
	w := do(t, "POST", "/api/print/jobs", uid, map[string]any{"shopId": f.shop, "payment": payment, "key": key(), "files": files, "total": 100})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	return decode[jobReply](t, w).Job
}

func signHook(raw []byte) string {
	m := hmac.New(sha256.New, []byte(webhookSecret))
	m.Write(raw)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func webhookCall(t *testing.T, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(payload)
	r := httptest.NewRequest("POST", "/api/cafe/kaspi/webhook", bytes.NewReader(raw))
	r.Header.Set("X-Webhook-Signature", signHook(raw))
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
	return w
}

func getRaw(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

// ---------- flows ----------

// realClock: these flows compare the API clock with the database's now() (ready_at, closed_at).
func realClock(t *testing.T) {
	prev := Now
	Now = time.Now
	t.Cleanup(func() { Now = prev })
}

func TestUploadPriceCounterQueueReadyPickedUp(t *testing.T) {
	testdb.Need(t)
	realClock(t)
	useFakes()
	f := newShop(t, "pickup")
	student, other := newUser(t), newUser(t)
	up := uploaded(t, student, "Курстық жұмыс.pdf", makePDF(10))
	if up.Pages == nil || *up.Pages != 10 || up.Kind != "pdf" || up.Name != "Курстық жұмыс.pdf" || up.URL == "" {
		t.Fatalf("upload: %+v", up)
	}
	// the stored file has a random name and nothing of the original
	var stored string
	stored = scalar[string](t, `select file_name from print_files where id = $1`, up.ID)
	if !reFileName.MatchString(stored) {
		t.Fatalf("stored name %q", stored)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, "print", stored)); err != nil {
		t.Fatal(err)
	}
	file := map[string]any{"fileId": up.ID, "serviceId": f.print, "size": "A4", "duplex": true, "copies": 2, "bindingId": f.binding}
	// the quote is the server's; a client total is ignored
	w := do(t, "POST", "/api/print/quote", student, map[string]any{"shopId": f.shop, "files": []any{file}, "goods": []any{map[string]any{"id": f.folder, "qty": 1}}, "total": 1})
	q := decode[struct{ Quote Quoted }](t, w).Quote
	want := int64(10*1500*2 + 2*50000 + 30000)
	if w.Code != 200 || q.Total != want || q.Review {
		t.Fatalf("quote: %d %s", w.Code, w.Body)
	}
	// someone else cannot quote or order with this file
	if w := do(t, "POST", "/api/print/quote", other, map[string]any{"shopId": f.shop, "files": []any{file}}); w.Code != 409 {
		t.Fatalf("foreign file: %d %s", w.Code, w.Body)
	}
	j := place(t, f, student, "pickup", file)
	if j.Total != 10*1500*2+2*50000 || j.Status != "new" || j.PriceState != "final" || len(j.Files) != 1 || len(j.PickupCode) != 3 {
		t.Fatalf("job: %+v", j)
	}
	// the file is now in the job: it cannot be placed twice or deleted alone
	if w := do(t, "POST", "/api/print/jobs", student, map[string]any{"shopId": f.shop, "payment": "pickup", "key": key(), "files": []any{file}}); w.Code != 409 {
		t.Fatalf("file twice: %d %s", w.Code, w.Body)
	}
	if w := do(t, "DELETE", fmt.Sprintf("/api/print/uploads/%d", up.ID), student, nil); w.Code != 409 {
		t.Fatalf("delete placed upload: %d", w.Code)
	}
	eventually(t, "staff notified", func() bool { return sentTo(f.operator, fmt.Sprintf("print:new:%d", j.ID)) == 1 })
	// RLS: another student sees nothing; another shop's staff see nothing
	if w := do(t, "GET", fmt.Sprintf("/api/print/jobs/%d", j.ID), other, nil); w.Code != 404 {
		t.Fatalf("other student: %d", w.Code)
	}
	g := newShop(t, "pickup")
	if w := do(t, "GET", fmt.Sprintf("/api/print/staff/jobs?shop=%d", f.shop), g.operator, nil); w.Code != 403 {
		t.Fatalf("other shop board: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/printing", j.ID), g.operator, nil); w.Code != 404 {
		t.Fatalf("other shop action: %d", w.Code)
	}
	// the student's view: queue position
	got := decode[jobReply](t, do(t, "GET", fmt.Sprintf("/api/print/jobs/%d", j.ID), student, nil)).Job
	if got.Ahead == nil || *got.Ahead != 0 {
		t.Fatalf("ahead: %+v", got.Ahead)
	}
	// the operator's board: the job, with a signed link to the file for the operator
	board := decode[struct {
		Jobs []Job `json:"jobs"`
	}](t, do(t, "GET", "/api/print/staff/jobs", f.operator, nil))
	if len(board.Jobs) != 1 || board.Jobs[0].ID != j.ID || len(board.Jobs[0].Files) != 1 || board.Jobs[0].Customer == nil || strings.Contains(*board.Jobs[0].Customer, "Тестов") {
		t.Fatalf("board: %+v", board)
	}
	link := board.Jobs[0].Files[0].URL
	w = getRaw(t, link)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), makePDF(10)) || w.Header().Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("download: %d %v", w.Code, w.Header())
	}
	// a tampered link, another viewer's id, an expired link
	u, _ := url.Parse(link)
	qv := u.Query()
	qv.Set("s", strings.Repeat("0", 64))
	if w := getRaw(t, u.Path+"?"+qv.Encode()); w.Code != 403 {
		t.Fatalf("tampered: %d", w.Code)
	}
	foreign, _ := SignedURL(up.ID, other, Now())
	if w := getRaw(t, foreign); w.Code != 404 {
		t.Fatalf("validly signed for a stranger: %d", w.Code)
	}
	old, _ := SignedURL(up.ID, f.operator, Now().Add(-10*time.Minute))
	if w := getRaw(t, old); w.Code != 410 {
		t.Fatalf("expired: %d", w.Code)
	}
	// printing → ready: the student is told «Дайын»
	for _, a := range []string{"printing", "ready"} {
		if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/%s", j.ID, a), f.operator, nil); w.Code != 200 {
			t.Fatalf("%s: %d %s", a, w.Code, w.Body)
		}
	}
	eventually(t, "student told ready", func() bool { return sentTo(student, fmt.Sprintf("print:ready:%d", j.ID)) == 1 })
	// cannot cancel any more
	if w := do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/cancel", j.ID), student, nil); w.Code != 409 {
		t.Fatalf("late cancel: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/no-show", j.ID), f.operator, nil); w.Code != 409 {
		t.Fatalf("early no-show: %d", w.Code)
	}
	w = do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/picked-up", j.ID), f.operator, nil)
	done := decode[jobReply](t, w).Job
	if w.Code != 200 || done.Status != "picked_up" || done.PaymentStatus != "paid" {
		t.Fatalf("picked up: %d %s", w.Code, w.Body)
	}
	kinds := []string{}
	err := db.Tx(bg(), func(tx pgx.Tx) error {
		rows, err := tx.Query(bg(), `select kind from print_job_events where job_id = $1 order by id`, j.ID)
		if err != nil {
			return err
		}
		kinds, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil || strings.Join(kinds, ",") != "created,printing,ready,picked_up" {
		t.Fatalf("audit: %v %v", kinds, err)
	}
	// retention: N days after pickup the bytes go, the link answers 410, the audit stays
	exec(t, `update print_jobs set closed_at = now() - interval '4 days' where id = $1`, j.ID)
	res, err := Retain(bg())
	if err != nil || res.Files < 1 {
		t.Fatalf("retain: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, "print", stored)); !os.IsNotExist(err) {
		t.Fatalf("file survived retention: %v", err)
	}
	fresh, _ := SignedURL(up.ID, f.operator, Now())
	if w := getRaw(t, fresh); w.Code != 410 {
		t.Fatalf("after retention: %d", w.Code)
	}
}

func TestOfficeFileWaitsForTheOperatorAndTheStudentApprovesANewPrice(t *testing.T) {
	testdb.Need(t)
	useFakes()
	f := newShop(t, "both")
	student := newUser(t)
	up := uploaded(t, student, "slides.docx", makeOffice(t, "docx", nil))
	if up.Pages != nil || up.PagesHint == nil || *up.PagesHint != 12 {
		t.Fatalf("office upload: %+v", up)
	}
	file := map[string]any{"fileId": up.ID, "serviceId": f.print, "size": "A4", "copies": 1}
	if w := do(t, "POST", "/api/print/jobs", student, map[string]any{"shopId": f.shop, "payment": "kaspi", "key": key(), "files": []any{file}}); w.Code != 400 {
		t.Fatalf("office without pages: %d %s", w.Code, w.Body)
	}
	file["pages"] = 10
	j := place(t, f, student, "kaspi", file)
	if j.PriceState != "review" || j.Total != 10*2000 {
		t.Fatalf("review job: %+v", j)
	}
	// on the board at once (the operator must look), but not printable, and Kaspi waits for the price
	eventually(t, "staff told to check", func() bool { return sentTo(f.operator, fmt.Sprintf("print:new:%d", j.ID)) == 1 })
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/printing", j.ID), f.operator, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "price_pending") {
		t.Fatalf("print before price: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/pay", j.ID), student, nil); w.Code != 409 || !strings.Contains(w.Body.String(), "price_pending") {
		t.Fatalf("pay before price: %d %s", w.Code, w.Body)
	}
	// the file really has 14 pages: a new price needs a reason
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/price", j.ID), f.operator, map[string]any{"total": 28000}); w.Code != 400 {
		t.Fatalf("no reason: %d %s", w.Code, w.Body)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/price", j.ID), f.operator, map[string]any{"total": 28050, "reason": "14 бет"}); w.Code != 400 {
		t.Fatalf("fractional tenge: %d", w.Code)
	}
	w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/price", j.ID), f.operator, map[string]any{"total": 28000, "reason": "Файлда 14 бет"})
	pj := decode[jobReply](t, w).Job
	if w.Code != 200 || pj.PriceState != "proposed" || pj.ProposedTotal == nil || *pj.ProposedTotal != 28000 || pj.Total != 20000 {
		t.Fatalf("propose: %d %s", w.Code, w.Body)
	}
	eventually(t, "student asked", func() bool { return sentTo(student, fmt.Sprintf("print:price_proposed:%d", j.ID)) == 1 })
	// still not payable or printable until the student answers; only the student answers
	if w := do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/pay", j.ID), student, nil); w.Code != 409 {
		t.Fatalf("pay while proposed: %d", w.Code)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/price", j.ID), f.operator, map[string]any{"accept": true}); w.Code != 404 {
		t.Fatalf("operator answers for the student: %d", w.Code)
	}
	w = do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/price", j.ID), student, map[string]any{"accept": true})
	aj := decode[jobReply](t, w).Job
	if w.Code != 200 || aj.PriceState != "final" || aj.Total != 28000 || aj.ProposedTotal != nil {
		t.Fatalf("approve: %d %s", w.Code, w.Body)
	}
	// now Kaspi, in the shop's name; paid through the café's webhook, exactly once
	w = do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/pay", j.ID), student, nil)
	paying := decode[jobReply](t, w).Job
	if w.Code != 200 || paying.PaymentStatus != "pending" || paying.Pay == nil {
		t.Fatalf("pay: %d %s", w.Code, w.Body)
	}
	op := scalar[string](t, `select kaspi_operation_id from print_jobs where id = $1`, j.ID)
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/price", j.ID), f.operator, map[string]any{"total": 30000, "reason": "ещё"}); w.Code != 409 {
		t.Fatalf("price change while paying: %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		w := webhookCall(t, map[string]any{"event": "payment.success", "paymentId": op, "status": "Processed", "amount": 280})
		if w.Code != 200 {
			t.Fatalf("webhook: %d %s", w.Code, w.Body)
		}
	}
	if n := scalar[int](t, `select count(*) from print_job_events where job_id = $1 and kind = 'paid'`, j.ID); n != 1 {
		t.Fatalf("paid %d times", n)
	}
	if o := scalar[string](t, `select outcome from cafe_kaspi_webhooks where operation_id = $1`, op); o != "print:paid" {
		t.Fatalf("webhook outcome %q", o)
	}
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/printing", j.ID), f.operator, nil); w.Code != 200 {
		t.Fatalf("print after pay: %d %s", w.Code, w.Body)
	}
}

func TestRejectedPriceCancelsAndConfirmedPriceNeedsNoAnswer(t *testing.T) {
	testdb.Need(t)
	realClock(t)
	useFakes()
	f := newShop(t, "pickup")
	student := newUser(t)
	up := uploaded(t, student, "t.xlsx", makeOffice(t, "xlsx", nil))
	j := place(t, f, student, "pickup", map[string]any{"fileId": up.ID, "serviceId": f.print, "size": "A4", "copies": 1, "pages": 3})
	// the operator counted 3 pages too: confirming keeps the price, nothing to approve
	w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/price", j.ID), f.operator, map[string]any{"total": j.Total})
	if c := decode[jobReply](t, w).Job; w.Code != 200 || c.PriceState != "final" {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	up2 := uploaded(t, student, "t2.pptx", makeOffice(t, "pptx", nil))
	j2 := place(t, f, student, "pickup", map[string]any{"fileId": up2.ID, "serviceId": f.print, "size": "A4", "copies": 1, "pages": 7})
	do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/price", j2.ID), f.operator, map[string]any{"total": 50000, "reason": "Слайдтар түрлі түсті"})
	w = do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/price", j2.ID), student, map[string]any{"accept": false})
	if c := decode[jobReply](t, w).Job; w.Code != 200 || c.Status != "cancelled" || c.CancelReason == nil || *c.CancelReason != "price_rejected" {
		t.Fatalf("reject: %d %s", w.Code, w.Body)
	}
	// a cancelled job's files are deleted almost at once
	exec(t, `update print_jobs set closed_at = now() - interval '1 hour' where id = $1`, j2.ID)
	if _, err := Retain(bg()); err != nil {
		t.Fatal(err)
	}
	if !scalar[bool](t, `select deleted_at is not null from print_files where id = $1`, up2.ID) {
		t.Fatal("cancelled job's file kept")
	}
	if scalar[bool](t, `select deleted_at is not null from print_files where id = $1`, up.ID) {
		t.Fatal("open job's file deleted")
	}
}

func TestQuotasAndCancelRefund(t *testing.T) {
	testdb.Need(t)
	k := useFakes()
	f := newShop(t, "kaspi")
	student := newUser(t)
	t.Setenv("PRINT_DAILY_FILES", "2")
	uploaded(t, student, "1.png", makePNG())
	uploaded(t, student, "2.png", makePNG())
	if w := upload(t, student, "3.png", makePNG()); w.Code != 429 || !strings.Contains(w.Body.String(), "quota_files") {
		t.Fatalf("daily quota: %d %s", w.Code, w.Body)
	}
	t.Setenv("PRINT_DAILY_FILES", "1000")
	if w := upload(t, student, "evil.pdf", []byte("MZ this is an exe")); w.Code != 415 {
		t.Fatalf("exe: %d", w.Code)
	}
	up := uploaded(t, student, "photo.png", makePNG())
	j := place(t, f, student, "kaspi", map[string]any{"fileId": up.ID, "serviceId": f.print, "size": "A4", "color": true, "copies": 3})
	if j.Total != 3*10000 {
		t.Fatalf("total %d", j.Total)
	}
	// a Kaspi job is not on the counter's queue until paid
	if w := do(t, "POST", fmt.Sprintf("/api/print/staff/jobs/%d/printing", j.ID), f.operator, nil); w.Code != 409 {
		t.Fatalf("print unpaid kaspi: %d", w.Code)
	}
	do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/pay", j.ID), student, nil)
	k.mu.Lock()
	k.state = payments.StatePaid
	k.mu.Unlock()
	got := decode[jobReply](t, do(t, "GET", fmt.Sprintf("/api/print/jobs/%d", j.ID), student, nil)).Job
	if got.PaymentStatus != "paid" {
		t.Fatalf("status read: %+v", got.PaymentStatus)
	}
	// cancelled before printing: refunded through the shop's own session
	w := do(t, "POST", fmt.Sprintf("/api/print/jobs/%d/cancel", j.ID), student, nil)
	if c := decode[jobReply](t, w).Job; w.Code != 200 || c.Status != "cancelled" || c.PaymentStatus != "refunded" {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	if len(k.refunds) != 1 {
		t.Fatalf("refunds: %v", k.refunds)
	}
}

func TestAccountDeletionHookRemovesPrintFiles(t *testing.T) {
	testdb.Need(t)
	student := newUser(t)
	up := uploaded(t, student, "x.pdf", makePDF(1))
	name := scalar[string](t, `select file_name from print_files where id = $1`, up.ID)
	err := db.Tx(bg(), func(tx pgx.Tx) error { return deleteUserFiles(bg(), tx, student) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, "print", name)); !os.IsNotExist(err) {
		t.Fatalf("file survived: %v", err)
	}
}
