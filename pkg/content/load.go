package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/jsx"
)

// Content loader: Google Sheets (published CSV) -> last good copy in memory -> data/*.json.

var EnvKeys = map[string]string{
	"opportunities": "SHEET_OPPORTUNITIES_CSV",
	"jobs":          "SHEET_JOBS_CSV",
	"discounts":     "SHEET_DISCOUNTS_CSV",
}

const (
	fetchTimeout = 5 * time.Second
	maxUnits     = 5 * 1024 * 1024 // JS string length (UTF-16 units) limit of one sheet
	// A UTF-16 unit takes at least 1/3 of its UTF-8 bytes, so a body over 3*maxUnits bytes
	// is always over the limit; reading stops there.
	maxBytes = 3 * maxUnits
)

// Content is the /api/content response body.
type Content struct {
	Opportunities []Record `json:"opportunities"`
	Jobs          []Record `json:"jobs"`
	Discounts     []Record `json:"discounts"`
	Events        []Record `json:"events,omitempty"` // optional extra source; omitted when empty
	Places        []Record `json:"places,omitempty"` // only from extra sources (extra.go)
	Source        string   `json:"source"`
	UpdatedAt     *string  `json:"updated_at"`
	Rejected      int      `json:"rejected"`
}

// Loader keeps the last good copy of each sheet while the instance is warm.
type Loader struct {
	Env     func(string) string // os.Getenv when nil
	Client  *http.Client        // http.DefaultClient when nil
	Warn    func(string)        // stderr when nil
	DataDir string              // located on first use when empty (see DataDir)

	mu      sync.Mutex
	memory  map[string]memEntry
	bundled map[string][]Record
}

type memEntry struct {
	records  []Record
	rejected int
	at       string
}

type part struct {
	records  []Record
	rejected int
	at       string
	source   string
}

// Reset forgets the in-memory copies (tests).
func (l *Loader) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.memory, l.bundled = nil, nil
}

func (l *Loader) warn(msg string) {
	if l.Warn != nil {
		l.Warn(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

type sheetError struct{ msg string }

func (e *sheetError) Error() string { return e.msg }

var (
	htmlType    = regexp.MustCompile(`(?i)text/html`)
	errTimedOut = errors.New("timeout")
)

func (l *Loader) fetchSheet(ctx context.Context, kind, url string) ([]Record, int, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, &sheetError{"Failed to parse URL from " + url}
	}
	req.Header.Set("Accept", "text/csv")
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, errTimedOut
		}
		return nil, 0, &sheetError{"fetch failed"}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, 0, &sheetError{fmt.Sprintf("HTTP %d", res.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, errTimedOut
		}
		return nil, 0, &sheetError{"terminated"}
	}
	text := jsx.ValidUTF8(string(body))
	if len(body) > maxBytes || jsx.UTF16Len(text) > maxUnits {
		return nil, 0, &sheetError{"кесте тым үлкен"}
	}
	if htmlType.MatchString(res.Header.Get("Content-Type")) || strings.HasPrefix(strings.TrimLeftFunc(text, jsx.IsSpace), "<") {
		return nil, 0, &sheetError{`CSV емес, HTML келді (кесте "Publish to web → CSV" арқылы жарияланбаған болуы мүмкін)`}
	}
	headers, items := RowsToObjects(ParseCSV(text))
	if !slices.Contains(headers, "id") || !slices.Contains(headers, "status") {
		return nil, 0, &sheetError{`тақырып жолында "id" не "status" бағаны жоқ — сілтеме дұрыс парақты көрсете ме?`}
	}
	records, rejected := BuildRecords(kind, items, l.warn)
	return records, rejected, nil
}

// rawRecord is an entry of data/*.json, served as is.
type rawRecord struct {
	raw      json.RawMessage
	deadline any
}

func (r rawRecord) MarshalJSON() ([]byte, error) { return r.raw, nil }
func (r rawRecord) deadlineValue() any           { return r.deadline }

func (l *Loader) readBundled(kind string) []Record {
	l.mu.Lock()
	if recs, ok := l.bundled[kind]; ok {
		l.mu.Unlock()
		return recs
	}
	l.mu.Unlock()
	recs, err := readDataFile(filepath.Join(l.dataDir(), kind+".json"))
	if err != nil {
		l.warn(fmt.Sprintf("[content] %s: data/%s.json оқылмады: %s", kind, kind, err.Error()))
		return []Record{}
	}
	l.mu.Lock()
	if l.bundled == nil {
		l.bundled = map[string][]Record{}
	}
	l.bundled[kind] = recs
	l.mu.Unlock()
	return recs
}

func readDataFile(file string) ([]Record, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, errors.New("массив емес")
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(b, &raws); err != nil {
		return nil, err
	}
	recs := make([]Record, len(raws))
	for i, raw := range raws {
		var c bytes.Buffer
		if err := json.Compact(&c, raw); err != nil {
			return nil, err
		}
		var deadline any
		if m, ok := arr[i].(map[string]any); ok {
			deadline = m["deadline"]
		}
		recs[i] = rawRecord{raw: c.Bytes(), deadline: deadline}
	}
	return recs, nil
}

var (
	dataDirOnce sync.Once
	dataDirPath string
)

// DataDir locates data/: ./data under the working directory (Vercel runs the function
// with cwd at the project root, as the JS used process.cwd()), else the first data/
// directory found walking up from the executable.
func DataDir() string {
	dataDirOnce.Do(func() {
		cwd, _ := os.Getwd()
		dataDirPath = filepath.Join(cwd, "data")
		if isDir(dataDirPath) {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			return
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		for dir := filepath.Dir(exe); ; dir = filepath.Dir(dir) {
			if d := filepath.Join(dir, "data"); isDir(d) {
				dataDirPath = d
				return
			}
			if filepath.Dir(dir) == dir {
				return
			}
		}
	})
	return dataDirPath
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (l *Loader) dataDir() string {
	if l.DataDir != "" {
		return l.DataDir
	}
	return DataDir()
}

func (l *Loader) loadKind(ctx context.Context, kind string) part {
	getenv := l.Env
	if getenv == nil {
		getenv = os.Getenv
	}
	key := EnvKeys[kind]
	if url := jsx.Trim(getenv(key)); url != "" {
		if !hasHTTPSPrefix(url) {
			l.warn(fmt.Sprintf("[content] %s: %s https:// деп басталмайды — data/%s.json қолданылады", kind, key, kind))
		} else {
			records, rejected, err := l.fetchSheet(ctx, kind, url)
			if err == nil {
				e := memEntry{records, rejected, time.Now().UTC().Format("2006-01-02T15:04:05.000Z")}
				l.mu.Lock()
				if l.memory == nil {
					l.memory = map[string]memEntry{}
				}
				l.memory[kind] = e
				l.mu.Unlock()
				return part{e.records, e.rejected, e.at, "sheets"}
			}
			reason := err.Error()
			if errors.Is(err, errTimedOut) {
				reason = fmt.Sprintf("%d с ішінде жауап бермеді", int(fetchTimeout/time.Second))
			}
			l.mu.Lock()
			mem, ok := l.memory[kind]
			l.mu.Unlock()
			from := "data/" + kind + ".json"
			if ok {
				from = "жадтағы соңғы нұсқа"
			}
			l.warn(fmt.Sprintf("[content] %s: Google Sheets оқылмады (%s) — %s қолданылады", kind, reason, from))
			if ok {
				return part{mem.records, mem.rejected, mem.at, "memory"}
			}
		}
	}
	return part{l.readBundled(kind), 0, "", "fallback"}
}

var rank = map[string]int{"sheets": 0, "memory": 1, "fallback": 2}

// AlmatyToday is the date in Asia/Almaty (UTC+5, no DST) as YYYY-MM-DD.
func AlmatyToday(now time.Time) string {
	return now.UTC().Add(5 * time.Hour).Format("2006-01-02")
}

// Load returns all three lists. Source is the "worst" source among them (sheets <
// memory < fallback); UpdatedAt is the oldest Sheets fetch time among lists that came
// from Sheets or memory, or nil.
func (l *Loader) Load(ctx context.Context, now time.Time) Content {
	today := AlmatyToday(now)
	parts := make([]part, len(Kinds))
	var wg sync.WaitGroup
	for i, kind := range Kinds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if v := recover(); v != nil {
					l.warn(fmt.Sprintf("[content] %s: күтпеген қате: %v", kind, v))
					parts[i] = part{[]Record{}, 0, "", "fallback"}
				}
			}()
			parts[i] = l.loadKind(ctx, kind)
		}()
	}
	wg.Wait()

	out := Content{Source: "sheets"}
	var updatedAt string
	lists := []*[]Record{&out.Opportunities, &out.Jobs, &out.Discounts}
	for i, p := range parts {
		*lists[i] = FilterActive(p.records, today)
		if rank[p.source] > rank[out.Source] {
			out.Source = p.source
		}
		if p.at != "" && (updatedAt == "" || p.at < updatedAt) {
			updatedAt = p.at
		}
		out.Rejected += p.rejected
	}
	if updatedAt != "" {
		out.UpdatedAt = &updatedAt
	}
	if extra := l.loadExtras(ctx); extra != nil {
		if recs := FilterActive(extra["opportunities"], today); len(recs) > 0 {
			out.Opportunities = append(out.Opportunities, recs...)
		}
		if recs := FilterActive(extra["jobs"], today); len(recs) > 0 {
			out.Jobs = append(out.Jobs, recs...)
		}
		if recs := FilterActive(extra["events"], today); len(recs) > 0 {
			out.Events = recs
		}
		if recs := FilterActive(extra["discounts"], today); len(recs) > 0 {
			out.Discounts = append(out.Discounts, recs...)
		}
		if recs := FilterActive(extra["places"], today); len(recs) > 0 {
			out.Places = recs
		}
		out.Opportunities = l.dedupe("opportunities", out.Opportunities)
		out.Jobs = l.dedupe("jobs", out.Jobs)
		out.Discounts = l.dedupe("discounts", out.Discounts)
	}
	l.filterDemo(ctx, &out)
	return out
}
