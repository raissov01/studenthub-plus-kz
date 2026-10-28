package content

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/raissov01/studenthub-plus-kz/pkg/jsx"
)

// Sheet <-> record mapping for the three content lists.
// One rule for every list cell: items are separated by "|" (written as " | ").
// Bilingual fields are two columns: <name>_kk and <name>_ru.

const ListSep = " | "

var (
	Kinds    = []string{"opportunities", "jobs", "discounts"}
	Statuses = []string{"draft", "published", "hidden"}

	// Keep in sync with the frontend lists (OPP_TYPES, CITIES, DEAL_CATS, per handling).
	OppTypes = []string{"mobility", "intl", "company", "grant"}
	JobPer   = []string{"shift", "month", "hour"}
	EnLevels = []string{"A1", "A2", "B1", "B2", "C1", "C2"}
	DealCats = []string{"food", "cinema", "books", "sport", "clothes", "lang", "edu", "cowork", "print"}
	Cities   = []string{"astana", "almaty", "shymkent", "karaganda", "aktobe", "taraz", "turkistan", "pavlodar",
		"oskemen", "semey", "kostanay", "atyrau", "aktau", "oral", "kyzylorda", "petropavl", "taldykorgan",
		"kokshetau", "other", "remote"}
)

var commonTail = []string{"source_url", "posted_at", "verified_at", "verified_by",
	"contact_phone", "contact_email", "contact_site", "is_demo", "deadline", "no_deadline"}

// Columns is the sheet header row of each list, in template order.
var Columns = map[string][]string{
	"opportunities": append([]string{"id", "status", "type", "funded", "title_kk", "title_ru", "org_kk", "org_ru",
		"req_course", "req_gpa", "req_en", "req_skills", "spec", "desc_kk", "desc_ru",
		"docs_kk", "docs_ru", "steps_kk", "steps_ru", "city"}, commonTail...),
	"jobs": append([]string{"id", "status", "today", "title_kk", "title_ru", "org_kk", "org_ru", "city", "pay", "per", "hours",
		"sched_kk", "sched_ru", "tags", "skills", "spec", "desc_kk", "desc_ru", "req_kk", "req_ru"}, commonTail...),
	"discounts": append([]string{"id", "status", "cat", "pct", "name_kk", "name_ru", "what_kk", "what_ru", "cond_kk", "cond_ru",
		"city"}, commonTail...),
}

// ---------- records (JSON shape of data/*.json) ----------

// Record is one list entry: a validated sheet row or an entry from data/*.json.
type Record interface{ deadlineValue() any }

type Bi struct {
	KK string `json:"kk"`
	RU string `json:"ru"`
}

type Contact struct {
	Phone string `json:"phone"`
	Email string `json:"email"`
	Site  string `json:"site"`
}

type OppReq struct {
	Course *float64 `json:"course,omitempty"`
	GPA    *float64 `json:"gpa,omitempty"`
	EN     string   `json:"en,omitempty"`
	Skills []string `json:"skills,omitempty"`
}

type Opportunity struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Funded     bool     `json:"funded"`
	Title      Bi       `json:"title"`
	Org        Bi       `json:"org"`
	Req        OppReq   `json:"req"`
	Spec       []string `json:"spec"`
	Desc       Bi       `json:"desc"`
	Docs       []Bi     `json:"docs"`
	Steps      []Bi     `json:"steps"`
	SourceURL  string   `json:"source_url"`
	PostedAt   *string  `json:"posted_at"`
	VerifiedAt *string  `json:"verified_at"`
	VerifiedBy string   `json:"verified_by"`
	Contact    Contact  `json:"contact"`
	IsDemo     bool     `json:"is_demo"`
	Deadline   *string  `json:"deadline"`
	City       string   `json:"city"`
}

type Job struct {
	ID         string   `json:"id"`
	Today      bool     `json:"today,omitempty"` // present only when true
	Title      Bi       `json:"title"`
	Org        Bi       `json:"org"`
	City       string   `json:"city"`
	Pay        float64  `json:"pay"`
	Per        string   `json:"per"`
	Hours      float64  `json:"hours"`
	Sched      Bi       `json:"sched"`
	Tags       []string `json:"tags"`
	Skills     []string `json:"skills"`
	Spec       []string `json:"spec"`
	Desc       Bi       `json:"desc"`
	Req        []Bi     `json:"req"`
	SourceURL  string   `json:"source_url"`
	PostedAt   *string  `json:"posted_at"`
	VerifiedAt *string  `json:"verified_at"`
	VerifiedBy string   `json:"verified_by"`
	Contact    Contact  `json:"contact"`
	IsDemo     bool     `json:"is_demo"`
	Deadline   *string  `json:"deadline"`
}

type Discount struct {
	ID         string  `json:"id"`
	Cat        string  `json:"cat"`
	Pct        string  `json:"pct"`
	Name       Bi      `json:"name"`
	What       Bi      `json:"what"`
	Cond       Bi      `json:"cond"`
	SourceURL  string  `json:"source_url"`
	PostedAt   *string `json:"posted_at"`
	VerifiedAt *string `json:"verified_at"`
	VerifiedBy string  `json:"verified_by"`
	Contact    Contact `json:"contact"`
	IsDemo     bool    `json:"is_demo"`
	Deadline   *string `json:"deadline"`
	City       string  `json:"city"`
}

func deadlineOf(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func (r *Opportunity) deadlineValue() any { return deadlineOf(r.Deadline) }
func (r *Job) deadlineValue() any         { return deadlineOf(r.Deadline) }
func (r *Discount) deadlineValue() any    { return deadlineOf(r.Deadline) }

// ---------- record -> row (used by cmd/json-to-csv) ----------

// RecordToRow turns one decoded data/*.json entry into a template row.
func RecordToRow(kind string, r map[string]any) ([]string, error) {
	cols, ok := Columns[kind]
	if !ok {
		return nil, fmt.Errorf("unknown kind %s", kind)
	}
	b := func(v any) string {
		if truthy(v) {
			return "TRUE"
		}
		return ""
	}
	or := func(v any) string { // v || ''
		if truthy(v) {
			return jsString(v)
		}
		return ""
	}
	val := func(v any) string { // base[k] ?? ''
		if v == nil {
			return ""
		}
		return jsString(v)
	}
	var listErr error
	joinList := func(v any) string {
		arr, _ := v.([]any)
		parts := make([]string, len(arr))
		for i, s := range arr {
			if s == nil {
				continue // Array#join prints null/undefined as ""
			}
			parts[i] = jsString(s)
			if strings.Contains(parts[i], "|") && listErr == nil {
				listErr = fmt.Errorf(`"|" is the list separator and cannot appear inside an item: %s`, parts[i])
			}
		}
		return strings.Join(parts, ListSep)
	}
	field := func(o any, k string) any {
		if m, ok := o.(map[string]any); ok {
			return m[k]
		}
		return nil
	}
	contact := r["contact"]
	base := map[string]string{
		"id": val(r["id"]), "status": "published",
		"source_url": val(r["source_url"]), "posted_at": or(r["posted_at"]), "verified_at": or(r["verified_at"]),
		"verified_by": or(r["verified_by"]), "contact_phone": or(field(contact, "phone")),
		"contact_email": or(field(contact, "email")), "contact_site": or(field(contact, "site")),
		"is_demo": b(r["is_demo"]), "deadline": or(r["deadline"]), "no_deadline": b(!truthy(r["deadline"])),
		"city": or(r["city"]),
	}
	bi := func(name string, o any) {
		base[name+"_kk"] = or(field(o, "kk"))
		base[name+"_ru"] = or(field(o, "ru"))
	}
	biList := func(name string, v any) {
		arr, _ := v.([]any)
		kk, ru := make([]any, len(arr)), make([]any, len(arr))
		for i, x := range arr {
			kk[i], ru[i] = field(x, "kk"), field(x, "ru")
		}
		base[name+"_kk"] = joinList(kk)
		base[name+"_ru"] = joinList(ru)
	}
	num := func(v any) string { return val(v) }
	switch kind {
	case "opportunities":
		req := r["req"]
		base["type"] = val(r["type"])
		base["funded"] = b(r["funded"])
		base["req_course"] = num(field(req, "course"))
		base["req_gpa"] = num(field(req, "gpa"))
		base["req_en"] = or(field(req, "en"))
		base["req_skills"] = joinList(field(req, "skills"))
		base["spec"] = joinList(r["spec"])
		bi("title", r["title"])
		bi("org", r["org"])
		bi("desc", r["desc"])
		biList("docs", r["docs"])
		biList("steps", r["steps"])
	case "jobs":
		base["today"] = b(r["today"])
		base["pay"] = num(r["pay"])
		base["per"] = val(r["per"])
		base["hours"] = num(r["hours"])
		base["tags"] = joinList(r["tags"])
		base["skills"] = joinList(r["skills"])
		base["spec"] = joinList(r["spec"])
		bi("title", r["title"])
		bi("org", r["org"])
		bi("sched", r["sched"])
		bi("desc", r["desc"])
		biList("req", r["req"])
	case "discounts":
		base["cat"] = val(r["cat"])
		base["pct"] = val(r["pct"])
		bi("name", r["name"])
		bi("what", r["what"])
		bi("cond", r["cond"])
	}
	if listErr != nil {
		return nil, listErr
	}
	row := make([]string, len(cols))
	for i, k := range cols {
		row[i] = base[k]
	}
	return row, nil
}

// TemplateCSV renders the sheet template of one list: the header row plus one row per
// data/*.json entry.
func TemplateCSV(kind string, records []map[string]any) (string, error) {
	rows := [][]string{Columns[kind]}
	for _, r := range records {
		row, err := RecordToRow(kind, r)
		if err != nil {
			return "", err
		}
		rows = append(rows, row)
	}
	return ToCSV(rows), nil
}

// truthy is JS ToBoolean for decoded JSON values.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	}
	return true
}

// jsString is String(v) for decoded JSON values.
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return jsx.NumberString(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = jsString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// ---------- row -> record (validation) ----------

type rowError string

func fail(format string, a ...any) { panic(rowError(fmt.Sprintf(format, a...))) }

var (
	trueWords  = []string{"true", "1", "yes", "y", "x", "иә", "ия", "да", "истина", "ақиқат", "✓", "✔"}
	falseWords = []string{"", "false", "0", "no", "n", "жоқ", "нет", "ложь", "жалған"}
)

func str(row Item, key string) string { return jsx.Trim(row.Cells[key]) }

func boolean(row Item, key string) bool {
	v := jsx.Lower(str(row, key))
	if slices.Contains(trueWords, v) {
		return true
	}
	if slices.Contains(falseWords, v) {
		return false
	}
	fail(`"%s" бағанында TRUE немесе бос болуы керек, ал "%s" жазылған`, key, row.Cells[key])
	return false
}

var (
	isoDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	dmyDate = regexp.MustCompile(`^([0-9]{1,2})\.([0-9]{1,2})\.([0-9]{4})$`)
)

// IsValidDate reports whether s is a real calendar date written as YYYY-MM-DD. Years
// below 100 are rejected, as Date.UTC maps them to 19xx.
func IsValidDate(s string) bool {
	if !isoDate.MatchString(s) {
		return false
	}
	y, _ := strconv.Atoi(s[0:4])
	m, _ := strconv.Atoi(s[5:7])
	d, _ := strconv.Atoi(s[8:10])
	if y < 100 || m < 1 || m > 12 || d < 1 {
		return false
	}
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[m-1]
	if m == 2 && (y%4 == 0 && y%100 != 0 || y%400 == 0) {
		days = 29
	}
	return d <= days
}

func pad2(s string) string {
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

// Dates must be YYYY-MM-DD. DD.MM.YYYY is also accepted because Google Sheets
// with a ru/kk locale silently reformats typed dates that way.
func date(row Item, key string) *string {
	v := str(row, key)
	if v == "" {
		return nil
	}
	if m := dmyDate.FindStringSubmatch(v); m != nil {
		v = m[3] + "-" + pad2(m[2]) + "-" + pad2(m[1])
	}
	if !IsValidDate(v) {
		fail(`"%s" күні қате: "%s" (керек формат: ЖЖЖЖ-АА-КК, мысалы 2026-12-31)`, key, row.Cells[key])
	}
	return &v
}

type numOpts struct {
	required, integer bool
	min, max          float64
}

func number(row Item, key string, o numOpts) *float64 {
	raw := strings.Replace(strings.Map(func(r rune) rune {
		if jsx.IsSpace(r) {
			return -1
		}
		return r
	}, str(row, key)), ",", ".", 1)
	if raw == "" {
		if o.required {
			fail(`"%s" толтырылмаған`, key)
		}
		return nil
	}
	n := jsx.ToNumber(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) || (o.integer && n != math.Trunc(n)) || n < o.min || n > o.max {
		fail(`"%s" бағанындағы сан қате: "%s"`, key, row.Cells[key])
	}
	if n == 0 {
		n = 0 // -0 prints as 0 in JSON.stringify
	}
	return &n
}

func httpsURL(row Item, key string, required bool) string {
	v := str(row, key)
	if v == "" {
		if required {
			fail(`"%s" толтырылмаған`, key)
		}
		return ""
	}
	if !hasHTTPSPrefix(v) || !parsesAsURL(v) {
		fail(`"%s" сілтемесі https:// деп басталуы керек: "%s"`, key, v)
	}
	return v
}

func hasHTTPSPrefix(v string) bool { return len(v) >= 8 && strings.EqualFold(v[:8], "https://") }

func oneOf(row Item, key string, allowed []string, required bool) string {
	v := str(row, key)
	if v == "" {
		if required {
			fail(`"%s" толтырылмаған`, key)
		}
		return ""
	}
	if !slices.Contains(allowed, v) {
		fail(`"%s" мәні белгісіз: "%s" (рұқсат етілгені: %s)`, key, v, strings.Join(allowed, ", "))
	}
	return v
}

func list(row Item, key string) []string {
	out := []string{}
	for _, s := range strings.Split(str(row, key), "|") {
		if s = jsx.Trim(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Bilingual text. A missing side copies the other one; requireKK makes kk mandatory.
func bi(row Item, name string, requireKK bool) Bi {
	kk, ru := str(row, name+"_kk"), str(row, name+"_ru")
	if requireKK && kk == "" {
		fail(`"%s_kk" толтырылмаған`, name)
	}
	out := Bi{kk, ru}
	if kk == "" {
		out.KK = ru
	}
	if ru == "" {
		out.RU = kk
	}
	return out
}

func biList(row Item, name string) []Bi {
	kk, ru := list(row, name+"_kk"), list(row, name+"_ru")
	if len(ru) > len(kk) {
		fail(`"%s_ru" тізімінде "%s_kk" тізімінен көп элемент бар (бөлгіш: |)`, name, name)
	}
	out := make([]Bi, len(kk))
	for i, k := range kk {
		out[i] = Bi{k, k}
		if i < len(ru) && ru[i] != "" {
			out[i].RU = ru[i]
		}
	}
	return out
}

type commonFields struct {
	sourceURL            string
	postedAt, verifiedAt *string
	verifiedBy           string
	contact              Contact
	isDemo               bool
	deadline             *string
}

var emailShape = regexp.MustCompile(`^[^@]+@[^@]+\.[^@]+$`)

func common(row Item) commonFields {
	deadline := date(row, "deadline")
	noDeadline := boolean(row, "no_deadline")
	if deadline != nil && noDeadline {
		fail(`"deadline" мен "no_deadline" екеуі бірдей толтырылған — біреуін ғана қалдырыңыз`)
	}
	if deadline == nil && !noDeadline {
		fail(`"deadline" толтырылмаған (мерзімсіз болса no_deadline = TRUE)`)
	}
	email := str(row, "contact_email")
	if email != "" && (strings.IndexFunc(email, jsx.IsSpace) >= 0 || !emailShape.MatchString(email)) {
		fail(`"contact_email" қате: "%s"`, email)
	}
	var c commonFields
	c.sourceURL = httpsURL(row, "source_url", true)
	c.postedAt = date(row, "posted_at")
	c.verifiedAt = date(row, "verified_at")
	c.verifiedBy = str(row, "verified_by")
	c.contact = Contact{Phone: str(row, "contact_phone"), Email: email}
	c.contact.Site = httpsURL(row, "contact_site", false)
	c.isDemo = boolean(row, "is_demo")
	c.deadline = deadline
	return c
}

var idShape = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func idOf(row Item) string {
	id := str(row, "id")
	if id == "" {
		fail(`"id" толтырылмаған`)
	}
	if !idShape.MatchString(id) {
		fail(`"id" тек латын әріптері, сандар, - және _ болуы керек: "%s"`, id)
	}
	return id
}

func specOf(row Item) []string {
	if s := list(row, "spec"); len(s) > 0 {
		return s
	}
	return []string{"all"}
}

// Builders evaluate cells in the same order as the JS object literals did, so the first
// error reported for a row is the same one.
var builders = map[string]func(Item) Record{
	"opportunities": func(row Item) Record {
		id := idOf(row)
		title := bi(row, "title", true)
		typ := oneOf(row, "type", OppTypes, true)
		var req OppReq
		req.Course = number(row, "req_course", numOpts{integer: true, min: 1, max: 7})
		req.GPA = number(row, "req_gpa", numOpts{min: 0, max: 4})
		req.EN = oneOf(row, "req_en", EnLevels, false)
		if skills := list(row, "req_skills"); len(skills) > 0 {
			req.Skills = skills
		}
		city := oneOf(row, "city", Cities, true)
		c := common(row)
		r := &Opportunity{ID: id, Type: typ, Funded: boolean(row, "funded"), Title: title}
		r.Org = bi(row, "org", false)
		r.Req = req
		r.Spec = specOf(row)
		r.Desc = bi(row, "desc", false)
		r.Docs = biList(row, "docs")
		r.Steps = biList(row, "steps")
		r.SourceURL, r.PostedAt, r.VerifiedAt, r.VerifiedBy = c.sourceURL, c.postedAt, c.verifiedAt, c.verifiedBy
		r.Contact, r.IsDemo, r.Deadline, r.City = c.contact, c.isDemo, c.deadline, city
		return r
	},
	"jobs": func(row Item) Record {
		id := idOf(row)
		today := boolean(row, "today")
		title := bi(row, "title", true)
		city := oneOf(row, "city", Cities, true)
		pay := number(row, "pay", numOpts{required: true, min: 0, max: math.Inf(1)})
		per := oneOf(row, "per", JobPer, true)
		hours := number(row, "hours", numOpts{required: per == "shift", min: 0, max: 24 * 7})
		if per == "shift" && !(hours != nil && *hours > 0) {
			fail(`per = shift болса "hours" 0-ден үлкен болуы керек`)
		}
		c := common(row)
		r := &Job{ID: id, Today: today, Title: title, City: city, Pay: *pay, Per: per}
		r.Org = bi(row, "org", false)
		if hours != nil {
			r.Hours = *hours
		}
		r.Sched = bi(row, "sched", false)
		r.Tags = list(row, "tags")
		r.Skills = list(row, "skills")
		r.Spec = specOf(row)
		r.Desc = bi(row, "desc", false)
		r.Req = biList(row, "req")
		r.SourceURL, r.PostedAt, r.VerifiedAt, r.VerifiedBy = c.sourceURL, c.postedAt, c.verifiedAt, c.verifiedBy
		r.Contact, r.IsDemo, r.Deadline = c.contact, c.isDemo, c.deadline
		return r
	},
	"discounts": func(row Item) Record {
		id := idOf(row)
		name := bi(row, "name", true)
		cat := oneOf(row, "cat", DealCats, true)
		pct := str(row, "pct")
		if pct == "" {
			fail(`"pct" толтырылмаған`)
		}
		city := oneOf(row, "city", Cities, true)
		c := common(row)
		r := &Discount{ID: id, Cat: cat, Pct: pct, Name: name}
		r.What = bi(row, "what", false)
		r.Cond = bi(row, "cond", false)
		r.SourceURL, r.PostedAt, r.VerifiedAt, r.VerifiedBy = c.sourceURL, c.postedAt, c.verifiedAt, c.verifiedBy
		r.Contact, r.IsDemo, r.Deadline, r.City = c.contact, c.isDemo, c.deadline, city
		return r
	},
}

func recordID(r Record) string {
	switch x := r.(type) {
	case *Opportunity:
		return x.ID
	case *Job:
		return x.ID
	case *Discount:
		return x.ID
	}
	return ""
}

func buildOne(build func(Item) Record, row Item, seen map[string]bool) (rec Record, msg string) {
	defer func() {
		if v := recover(); v != nil {
			rec = nil
			if e, ok := v.(rowError); ok {
				msg = string(e)
			} else {
				msg = fmt.Sprintf("күтпеген қате: %v", v)
			}
		}
	}()
	rec = build(row)
	if id := recordID(rec); seen[id] {
		fail(`id "%s" қайталанады — бірінші жол ғана алынды`, id)
	}
	return rec, ""
}

// BuildRecords validates sheet rows. It returns only published, valid, unique rows; no
// deadline filtering here (see FilterActive). Every rejected row is reported via warn.
func BuildRecords(kind string, items []Item, warn func(string)) (records []Record, rejected int) {
	build := builders[kind]
	records = []Record{}
	seen := map[string]bool{}
	for _, row := range items {
		label := fmt.Sprintf("[content] %s: %d-жол", kind, row.Row)
		if raw := row.Cells["id"]; raw != "" {
			label += fmt.Sprintf(" (id=%s)", jsx.Trim(raw))
		}
		status := jsx.Lower(str(row, "status"))
		if status == "" {
			status = "draft" // empty status = draft
		}
		if !slices.Contains(Statuses, status) {
			rejected++
			warn(fmt.Sprintf(`%s: "status" мәні белгісіз: "%s" (draft / published / hidden) — жол алынып тасталды`, label, row.Cells["status"]))
			continue
		}
		if status != "published" {
			continue
		}
		rec, msg := buildOne(build, row, seen)
		if rec == nil {
			rejected++
			warn(fmt.Sprintf("%s: %s — жол алынып тасталды", label, msg))
			continue
		}
		seen[recordID(rec)] = true
		records = append(records, rec)
	}
	return records, rejected
}

// FilterActive drops records whose deadline is before today (a deadline equal to today
// stays). Comparison follows JS: !r.deadline || r.deadline >= today.
func FilterActive(records []Record, today string) []Record {
	out := []Record{}
	for _, r := range records {
		d := r.deadlineValue()
		keep := false
		switch {
		case !truthy(d):
			keep = true
		case isString(d):
			keep = d.(string) >= today
		default:
			switch d.(type) {
			case map[string]any, []any: // compared as strings ("[object Object]", Array#join)
				keep = jsString(d) >= today
			} // numbers and booleans compare as NaN: dropped
		}
		if keep {
			out = append(out, r)
		}
	}
	return out
}

func isString(v any) bool { _, ok := v.(string); return ok }
