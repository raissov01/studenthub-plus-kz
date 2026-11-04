package content

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var root = filepath.Join("..", "..")

func readJSONFile(t *testing.T, kind string) []any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "data", kind+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var v []any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func readTemplate(t *testing.T, kind string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "templates", "sheets", kind+".csv"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// normalize round-trips v through JSON so Go records compare with decoded JSON files.
func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	g, w := normalize(t, got), normalize(t, want)
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.MarshalIndent(g, "", " ")
		wb, _ := json.MarshalIndent(w, "", " ")
		t.Fatalf("JSON differs\n got: %s\nwant: %s", gb, wb)
	}
}

// ---------- CSV parser ----------

func TestParseCSV(t *testing.T) {
	cases := []struct {
		name, in string
		want     [][]string
	}{
		{"plain rows, LF and CRLF, no trailing newline", "a,b\r\nc,d\ne,f", [][]string{{"a", "b"}, {"c", "d"}, {"e", "f"}}},
		{"trailing newline does not add an empty row", "a,b\r\n", [][]string{{"a", "b"}}},
		{"quoted fields with commas, newlines and \"\" escapes", "id,text\r\n1,\"hello, \"\"world\"\"\r\nline2\"\r\n2,\"x\ny\"\r\n",
			[][]string{{"id", "text"}, {"1", "hello, \"world\"\r\nline2"}, {"2", "x\ny"}}},
		{"BOM is stripped", "\uFEFFid,x\n1,2", [][]string{{"id", "x"}, {"1", "2"}}},
		{"empty fields and empty quoted fields", ",,\n\"\",a,\"\"", [][]string{{"", "", ""}, {"", "a", ""}}},
		{"lone CR line endings", "a\rb", [][]string{{"a"}, {"b"}}},
		{"unterminated quote keeps the rest as one field", "a,\"b,c\nd", [][]string{{"a", "b,c\nd"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseCSV(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
	t.Run("toCsv -> parseCsv round-trips awkward values", func(t *testing.T) {
		rows := [][]string{{"a", "b, c", `say "hi"`, "x\ny", " pad ", "", "қазақ | тілі"}}
		if got := ParseCSV(ToCSV(rows)); !reflect.DeepEqual(got, rows) {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("rowsToObjects lower-cases headers, skips blank rows, keeps sheet row numbers", func(t *testing.T) {
		headers, items := RowsToObjects(ParseCSV(" ID ,Status\n1,published\n,\n2,draft"))
		if !reflect.DeepEqual(headers, []string{"id", "status"}) {
			t.Fatalf("headers %q", headers)
		}
		var got [][2]any
		for _, i := range items {
			got = append(got, [2]any{i.Row, i.Cells["id"]})
		}
		if !reflect.DeepEqual(got, [][2]any{{2, "1"}, {4, "2"}}) {
			t.Fatalf("items %v", got)
		}
	})
}

// ---------- validation ----------

func row(kind string, over map[string]string) Item {
	base := map[string]map[string]string{
		"opportunities": {"id": "x1", "status": "published", "type": "grant", "title_kk": "Грант",
			"source_url": "https://example.com", "city": "astana", "deadline": "2099-01-01"},
		"jobs": {"id": "x1", "status": "published", "title_kk": "Жұмыс", "city": "astana", "pay": "8 000", "per": "shift",
			"hours": "4", "source_url": "https://example.com", "no_deadline": "TRUE"},
		"discounts": {"id": "x1", "status": "published", "cat": "food", "pct": "−10%", "name_kk": "Кафе", "city": "astana",
			"source_url": "https://example.com", "no_deadline": "TRUE"},
	}[kind]
	cells := maps.Clone(base)
	maps.Copy(cells, over)
	return Item{Row: 2, Cells: cells}
}

func rowAt(kind string, n int, over map[string]string) Item {
	r := row(kind, over)
	r.Row = n
	return r
}

type checked struct {
	records  []Record
	rejected int
	warnings []string
}

func check(kind string, rows ...Item) checked {
	var c checked
	c.records, c.rejected = BuildRecords(kind, rows, func(m string) { c.warnings = append(c.warnings, m) })
	return c
}

func ids(recs []Record) []string {
	out := []string{}
	for _, r := range recs {
		if id := recordID(r); id != "" {
			out = append(out, id)
			continue
		}
		var v struct{ ID string }
		b, _ := json.Marshal(r)
		_ = json.Unmarshal(b, &v)
		out = append(out, v.ID)
	}
	return out
}

func TestValidation(t *testing.T) {
	t.Run("minimal valid rows pass for every kind", func(t *testing.T) {
		for _, kind := range Kinds {
			r := check(kind, row(kind, nil))
			if len(r.records) != 1 || r.rejected != 0 {
				t.Fatalf("%s: %v", kind, r.warnings)
			}
		}
	})
	t.Run("ru falls back to kk, numbers with spaces/commas are parsed", func(t *testing.T) {
		r := check("jobs", row("jobs", map[string]string{"pay": "12 500", "hours": "4,5"}))
		j := r.records[0].(*Job)
		if j.Title != (Bi{"Жұмыс", "Жұмыс"}) || j.Pay != 12500 || j.Hours != 4.5 {
			t.Fatalf("%+v", j)
		}
	})

	bad := []struct {
		name, kind string
		over       map[string]string
		re         string
	}{
		{"missing id", "opportunities", map[string]string{"id": ""}, `"id"`},
		{"missing title_kk", "opportunities", map[string]string{"title_kk": ""}, `title_kk`},
		{"unknown type", "opportunities", map[string]string{"type": "party"}, `type`},
		{"missing source_url", "opportunities", map[string]string{"source_url": ""}, `source_url`},
		{"http:// link", "opportunities", map[string]string{"source_url": "http://example.com"}, `https://`},
		{"http:// contact site", "discounts", map[string]string{"contact_site": "http://shop.kz"}, `contact_site`},
		{"bad date", "opportunities", map[string]string{"deadline": "2026-02-30"}, `deadline`},
		{"bad date format", "opportunities", map[string]string{"posted_at": "2026/01/05"}, `posted_at`},
		{"no deadline and no no_deadline", "opportunities", map[string]string{"deadline": ""}, `no_deadline`},
		{"both deadline and no_deadline", "opportunities", map[string]string{"no_deadline": "TRUE"}, `біреуін`},
		{"job without pay", "jobs", map[string]string{"pay": ""}, `pay`},
		{"job with unknown per", "jobs", map[string]string{"per": "week"}, `per`},
		{"discount without cat", "discounts", map[string]string{"cat": ""}, `cat`},
		{"unknown city", "discounts", map[string]string{"city": "london"}, `city`},
		{"unknown status", "discounts", map[string]string{"status": "publshed"}, `status`},
	}
	for _, b := range bad {
		t.Run("rejects: "+b.name, func(t *testing.T) {
			r := check(b.kind, row(b.kind, b.over), rowAt(b.kind, 3, map[string]string{"id": "ok"}))
			if r.rejected != 1 {
				t.Fatalf("rejected = %d", r.rejected)
			}
			if got := ids(r.records); !reflect.DeepEqual(got, []string{"ok"}) {
				t.Fatalf("one bad row must not affect the others: %v", got)
			}
			if len(r.warnings) != 1 {
				t.Fatalf("warnings %v", r.warnings)
			}
			if !strings.Contains(r.warnings[0], b.kind+": 2-жол") || !regexp.MustCompile(b.re).MatchString(r.warnings[0]) {
				t.Fatalf("warning %q", r.warnings[0])
			}
		})
	}

	t.Run("duplicate id: first kept, second rejected", func(t *testing.T) {
		r := check("jobs", row("jobs", nil), rowAt("jobs", 3, map[string]string{"title_kk": "Басқа"}))
		if len(r.records) != 1 || r.records[0].(*Job).Title.KK != "Жұмыс" || r.rejected != 1 {
			t.Fatalf("%+v", r)
		}
		if !regexp.MustCompile(`3-жол.*қайталанады`).MatchString(r.warnings[0]) {
			t.Fatalf("warning %q", r.warnings[0])
		}
	})
	t.Run("draft, hidden and empty status are skipped silently (not rejected)", func(t *testing.T) {
		r := check("discounts",
			row("discounts", map[string]string{"id": "a", "status": "draft", "name_kk": ""}),
			row("discounts", map[string]string{"id": "b", "status": "Hidden"}),
			row("discounts", map[string]string{"id": "c", "status": ""}),
			row("discounts", map[string]string{"id": "d", "status": "PUBLISHED"}))
		if got := ids(r.records); !reflect.DeepEqual(got, []string{"d"}) || r.rejected != 0 {
			t.Fatalf("%v %d", got, r.rejected)
		}
	})
	t.Run("no_deadline gives deadline null; DD.MM.YYYY is normalised", func(t *testing.T) {
		if d := check("jobs", row("jobs", nil)).records[0].(*Job).Deadline; d != nil {
			t.Fatalf("deadline %v", *d)
		}
		d := check("opportunities", row("opportunities", map[string]string{"deadline": "6.12.2026"})).records[0].(*Opportunity).Deadline
		if d == nil || *d != "2026-12-06" {
			t.Fatalf("deadline %v", d)
		}
	})
	t.Run("expired deadline is dropped, today and no-deadline stay", func(t *testing.T) {
		s := func(v string) *string { return &v }
		recs := []Record{&Job{ID: "old", Deadline: s("2026-10-27")}, &Job{ID: "today", Deadline: s("2026-10-28")}, &Job{ID: "none"}}
		if got := ids(FilterActive(recs, "2026-10-28")); !reflect.DeepEqual(got, []string{"today", "none"}) {
			t.Fatal(got)
		}
	})
	t.Run("Almaty date is UTC+5", func(t *testing.T) {
		if got := AlmatyToday(time.Date(2026, 10, 28, 18, 59, 0, 0, time.UTC)); got != "2026-10-28" {
			t.Fatal(got)
		}
		if got := AlmatyToday(time.Date(2026, 10, 28, 19, 0, 0, 0, time.UTC)); got != "2026-10-29" {
			t.Fatal(got)
		}
	})
}

// ---------- templates round-trip ----------

func TestTemplates(t *testing.T) {
	for _, kind := range Kinds {
		t.Run(kind+".csv round-trips to data/"+kind+".json", func(t *testing.T) {
			data := readJSONFile(t, kind)
			headers, items := RowsToObjects(ParseCSV(readTemplate(t, kind)))
			if !reflect.DeepEqual(headers, Columns[kind]) {
				t.Fatalf("headers %q", headers)
			}
			r := check(kind, items...)
			if len(r.warnings) != 0 || r.rejected != 0 {
				t.Fatalf("warnings %v", r.warnings)
			}
			assertJSONEqual(t, r.records, data)
		})
		t.Run(kind+".csv is in sync with cmd/json-to-csv", func(t *testing.T) {
			var records []map[string]any
			for _, r := range readJSONFile(t, kind) {
				records = append(records, r.(map[string]any))
			}
			got, err := TemplateCSV(kind, records)
			if err != nil {
				t.Fatal(err)
			}
			if got != readTemplate(t, kind) {
				t.Fatal("template differs from generated CSV")
			}
		})
	}
}

// ---------- fallback chain ----------

var now = time.Date(2026, 10, 28, 6, 0, 0, 0, time.UTC)

func sheetCSV(kind string, rows ...Item) string {
	cols := Columns[kind]
	out := [][]string{cols}
	for _, r := range rows {
		line := make([]string, len(cols))
		for i, c := range cols {
			line[i] = r.Cells[c]
		}
		out = append(out, line)
	}
	return ToCSV(out)
}

func sheetsData() map[string]string {
	return map[string]string{
		"/o.csv": sheetCSV("opportunities",
			row("opportunities", map[string]string{"id": "so1"}),
			row("opportunities", map[string]string{"id": "so2", "deadline": "2026-10-01"}),     // expired
			row("opportunities", map[string]string{"id": "bad", "source_url": "http://x.kz"})), // rejected
		"/j.csv": sheetCSV("jobs", row("jobs", map[string]string{"id": "sj1"})),
		"/d.csv": sheetCSV("discounts", row("discounts", map[string]string{"id": "sd1"}),
			row("discounts", map[string]string{"id": "sd2", "status": "draft"})),
	}
}

// sheets is a fake Google Sheets: handler decides the response; it can be swapped mid-test.
type sheets struct {
	srv     *httptest.Server
	mu      sync.Mutex
	handler http.HandlerFunc
	calls   int
}

func newSheets(t *testing.T, h http.HandlerFunc) *sheets {
	s := &sheets{handler: h}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls++
		h := s.handler
		s.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sheets) set(h http.HandlerFunc) { s.mu.Lock(); s.handler = h; s.mu.Unlock() }

func (s *sheets) env(paths map[string]string) func(string) string {
	return func(k string) string {
		if p, ok := paths[k]; ok {
			return s.srv.URL + p
		}
		return ""
	}
}

var allPaths = map[string]string{"SHEET_OPPORTUNITIES_CSV": "/o.csv", "SHEET_JOBS_CSV": "/j.csv", "SHEET_DISCOUNTS_CSV": "/d.csv"}

func okSheets(data map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, ok := data[r.URL.Path]
		if !ok {
			http.Error(w, "unexpected url", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(body))
	}
}

// down closes the connection without a response, like an unreachable host.
func down(w http.ResponseWriter, r *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		conn.Close()
	}
}

func newLoader(s *sheets, env func(string) string, warn func(string)) *Loader {
	if warn == nil {
		warn = func(string) {}
	}
	l := &Loader{Env: env, Warn: warn, DataDir: filepath.Join(root, "data")}
	if s != nil {
		l.Client = s.srv.Client()
	}
	return l
}

func filterJSON(t *testing.T, kind, today string) []Record {
	t.Helper()
	recs, err := readDataFile(filepath.Join(root, "data", kind+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return FilterActive(recs, today)
}

func listOf(c Content, kind string) []Record {
	return map[string][]Record{"opportunities": c.Opportunities, "jobs": c.Jobs, "discounts": c.Discounts}[kind]
}

func TestLoadFallbackChain(t *testing.T) {
	ctx := context.Background()

	t.Run("sheets OK -> source sheets, memory then used when sheets go down", func(t *testing.T) {
		s := newSheets(t, okSheets(sheetsData()))
		var warnings []string
		var mu sync.Mutex
		l := newLoader(s, s.env(allPaths), func(m string) { mu.Lock(); warnings = append(warnings, m); mu.Unlock() })
		a := l.Load(ctx, now)
		if a.Source != "sheets" || a.Rejected != 1 {
			t.Fatalf("%+v", a)
		}
		for kind, want := range map[string][]string{"opportunities": {"so1"}, "jobs": {"sj1"}, "discounts": {"sd1"}} {
			if got := ids(listOf(a, kind)); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: %v", kind, got)
			}
		}
		if a.UpdatedAt == nil || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`).MatchString(*a.UpdatedAt) {
			t.Fatalf("updated_at %v", a.UpdatedAt)
		}

		s.set(down)
		warnings = nil
		b := l.Load(ctx, now)
		if b.Source != "memory" || b.Rejected != 1 || *b.UpdatedAt != *a.UpdatedAt {
			t.Fatalf("%+v", b)
		}
		if !reflect.DeepEqual(ids(b.Opportunities), []string{"so1"}) || !reflect.DeepEqual(ids(b.Jobs), []string{"sj1"}) {
			t.Fatalf("%+v", b)
		}
		if len(warnings) != 3 {
			t.Fatalf("warnings %v", warnings)
		}
		for _, w := range warnings {
			if !strings.Contains(w, "Google Sheets оқылмады (fetch failed) — жадтағы соңғы нұсқа қолданылады") {
				t.Fatalf("warning %q", w)
			}
		}
	})

	t.Run("cold start with sheets down -> data/*.json, deadline-filtered", func(t *testing.T) {
		s := newSheets(t, down)
		l := newLoader(s, s.env(allPaths), nil)
		r := l.Load(ctx, now)
		if r.Source != "fallback" || r.UpdatedAt != nil || r.Rejected != 0 {
			t.Fatalf("%+v", r)
		}
		for _, kind := range Kinds {
			assertJSONEqual(t, listOf(r, kind), filterJSON(t, kind, "2026-10-28"))
		}
		if len(r.Jobs) != len(readJSONFile(t, "jobs")) {
			t.Fatal("jobs filtered unexpectedly")
		}
		// Far in the future every dated opportunity has expired; undated jobs/discounts remain.
		later := l.Load(ctx, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		if len(later.Opportunities) != 0 || len(later.Discounts) != len(readJSONFile(t, "discounts")) {
			t.Fatalf("%d %d", len(later.Opportunities), len(later.Discounts))
		}
	})

	t.Run("unset env -> fallback without calling fetch", func(t *testing.T) {
		s := newSheets(t, okSheets(sheetsData()))
		l := newLoader(s, func(string) string { return "" }, nil)
		r := l.Load(ctx, now)
		if r.Source != "fallback" || len(r.Opportunities) != len(readJSONFile(t, "opportunities")) || s.calls != 0 {
			t.Fatalf("%+v calls=%d", r, s.calls)
		}
	})

	t.Run("mixed: one sheet configured, others fall back", func(t *testing.T) {
		s := newSheets(t, okSheets(sheetsData()))
		l := newLoader(s, s.env(map[string]string{"SHEET_JOBS_CSV": "/j.csv"}), nil)
		r := l.Load(ctx, now)
		if r.Source != "fallback" || !reflect.DeepEqual(ids(r.Jobs), []string{"sj1"}) || len(r.Discounts) != len(readJSONFile(t, "discounts")) {
			t.Fatalf("%+v", r)
		}
	})

	t.Run("HTML page, HTTP error and non-https URL count as failures", func(t *testing.T) {
		html := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><html>login</html>"))
		}
		http500 := func(w http.ResponseWriter, r *http.Request) { http.Error(w, "err", http.StatusInternalServerError) }
		for name, h := range map[string]http.HandlerFunc{"CSV емес, HTML келді": html, "(HTTP 500)": http500} {
			s := newSheets(t, h)
			var warnings []string
			var mu sync.Mutex
			l := newLoader(s, s.env(allPaths), func(m string) { mu.Lock(); warnings = append(warnings, m); mu.Unlock() })
			if r := l.Load(ctx, now); r.Source != "fallback" {
				t.Fatalf("%s: %+v", name, r)
			}
			if len(warnings) != 3 || !strings.Contains(warnings[0], name) {
				t.Fatalf("%s: %v", name, warnings)
			}
		}
		var warnings []string
		var wmu sync.Mutex // the three lists load concurrently
		l := newLoader(nil, func(k string) string { return "http://example.com/x.csv" }, func(m string) { wmu.Lock(); warnings = append(warnings, m); wmu.Unlock() })
		if r := l.Load(ctx, now); r.Source != "fallback" || !slices.ContainsFunc(warnings, func(w string) bool {
			return w == "[content] jobs: SHEET_JOBS_CSV https:// деп басталмайды — data/jobs.json қолданылады"
		}) {
			t.Fatalf("%v", warnings)
		}
	})

	t.Run("timeout aborts after ~5s and falls back", func(t *testing.T) {
		if testing.Short() {
			t.Skip("waits 5s")
		}
		release := make(chan struct{})
		s := newSheets(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		defer close(release)
		var warnings []string
		l := newLoader(s, s.env(map[string]string{"SHEET_JOBS_CSV": "/j.csv"}), func(m string) { warnings = append(warnings, m) })
		t0 := time.Now()
		r := l.Load(ctx, now)
		if time.Since(t0) < 4900*time.Millisecond || r.Source != "fallback" {
			t.Fatalf("%v %+v", time.Since(t0), r)
		}
		if !strings.Contains(strings.Join(warnings, "\n"), "5 с ішінде") {
			t.Fatalf("%v", warnings)
		}
	})
}

// ---------- HTTP handler ----------

func TestHandler(t *testing.T) {
	t.Run("GET revalidates demo setting without CDN caching", func(t *testing.T) {
		l := newLoader(nil, func(string) string { return "" }, nil)
		rec := httptest.NewRecorder()
		Handler(l)(rec, httptest.NewRequest(http.MethodGet, "/api/content", nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %v", rec.Code, rec.Header())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		keys := slices.Sorted(maps.Keys(body))
		if !reflect.DeepEqual(keys, []string{"discounts", "jobs", "opportunities", "rejected", "source", "updated_at"}) || body["source"] != "fallback" {
			t.Fatalf("%v", body)
		}
	})
	t.Run("POST is rejected with 405", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Handler(newLoader(nil, nil, nil))(rec, httptest.NewRequest(http.MethodPost, "/api/content", nil))
		if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%d", rec.Code)
		}
	})
}
