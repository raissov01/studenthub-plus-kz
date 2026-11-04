package content

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"
)

// Every bundled record passes the editor validation and comes back in the same shape.
func TestValidateRecordRoundTripsDataFiles(t *testing.T) {
	for _, kind := range Kinds {
		for _, v := range readJSONFile(t, kind) {
			m := v.(map[string]any)
			rec, fe := ValidateRecord(kind, m, m["deadline"] == nil)
			if fe != nil {
				t.Fatalf("%s %v: %v", kind, m["id"], fe)
			}
			SetRecordID(rec, m["id"].(string))
			want := maps.Clone(m)
			want["is_demo"] = false
			assertJSONEqual(t, rec, want)
		}
	}
}

// The editor reports the same message a sheet row with the same values gets.
func TestValidateRecordMirrorsSheetRules(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"type": "grant", "funded": true, "title": map[string]any{"kk": "Грант", "ru": "Грант"},
			"org": map[string]any{"kk": "Қор"}, "req": map[string]any{"course": 2.0, "gpa": 3.0, "en": "B2"},
			"spec": []any{"all"}, "desc": map[string]any{"kk": "Сипаттама"}, "docs": []any{}, "steps": []any{},
			"source_url": "https://fund.kz", "contact": map[string]any{"phone": "", "email": "", "site": ""},
			"deadline": "2026-12-31", "city": "astana",
		}
	}
	cases := []struct {
		name       string
		edit       func(m map[string]any)
		noDeadline bool
		field      string
	}{
		{"no title", func(m map[string]any) { m["title"] = map[string]any{"ru": "Грант"} }, false, "title_kk"},
		{"bad type", func(m map[string]any) { m["type"] = "lottery" }, false, "type"},
		{"bad city", func(m map[string]any) { m["city"] = "moscow" }, false, "city"},
		{"http link", func(m map[string]any) { m["source_url"] = "http://fund.kz" }, false, "source_url"},
		{"no link", func(m map[string]any) { delete(m, "source_url") }, false, "source_url"},
		{"bad date", func(m map[string]any) { m["deadline"] = "2026-02-30" }, false, "deadline"},
		{"no deadline", func(m map[string]any) { delete(m, "deadline") }, false, "deadline"},
		{"both", func(m map[string]any) {}, true, "deadline"},
		{"gpa", func(m map[string]any) { m["req"] = map[string]any{"gpa": 5.0} }, false, "req_gpa"},
		{"course", func(m map[string]any) { m["req"] = map[string]any{"course": 2.5} }, false, "req_course"},
		{"email", func(m map[string]any) { m["contact"] = map[string]any{"email": "a b@x.kz"} }, false, "contact_email"},
		{"site", func(m map[string]any) { m["contact"] = map[string]any{"site": "ftp://x"} }, false, "contact_site"},
		{"docs ru longer", func(m map[string]any) { m["docs"] = []any{map[string]any{"ru": "Паспорт"}} }, false, "docs_ru"},
	}
	for _, c := range cases {
		m := base()
		c.edit(m)
		_, fe := ValidateRecord("opportunities", m, c.noDeadline)
		if fe == nil || fe.Field != c.field {
			t.Errorf("%s: %v, want field %s", c.name, fe, c.field)
			continue
		}
		// the same values as a sheet row
		m["id"], m["is_demo"] = "x1", false
		row, err := RecordToRow("opportunities", m)
		if err != nil {
			t.Fatal(err)
		}
		cells := map[string]string{}
		for i, col := range Columns["opportunities"] {
			cells[col] = row[i]
		}
		cells["no_deadline"] = ""
		if c.noDeadline {
			cells["no_deadline"] = "TRUE"
		}
		var warned string
		recs, rejected := BuildRecords("opportunities", []Item{{Row: 2, Cells: cells}}, func(s string) { warned = s })
		if len(recs) != 0 || rejected != 1 || !strings.Contains(warned, fe.Message) {
			t.Errorf("%s: sheet said %q, editor %q", c.name, warned, fe.Message)
		}
	}
	if _, fe := ValidateRecord("jobs", map[string]any{"title": map[string]any{"kk": "Жұмыс"}, "city": "astana", "pay": 1000.0,
		"per": "shift", "source_url": "https://x.kz"}, true); fe == nil || fe.Field != "hours" {
		t.Errorf("shift without hours: %v", fe)
	}
	if _, fe := ValidateRecord("discounts", map[string]any{"name": map[string]any{"kk": "Кафе"}, "cat": "food", "city": "astana",
		"source_url": "https://x.kz"}, true); fe == nil || fe.Field != "pct" {
		t.Errorf("discount without pct: %v", fe)
	}
	if _, fe := ValidateRecord("opportunities", map[string]any{"docs": []any{map[string]any{"kk": "a | b"}}}, true); fe == nil || fe.Field != "list" {
		t.Errorf("pipe in a list item: %v", fe)
	}
	if _, fe := ValidateRecord("places", map[string]any{}, true); fe == nil || fe.Field != "kind" {
		t.Errorf("unknown kind: %v", fe)
	}
}

func TestSheetRowsCannotUseAdminPrefix(t *testing.T) {
	cells := map[string]string{"id": "adm-1", "status": "published", "type": "grant", "title_kk": "Грант", "city": "astana",
		"source_url": "https://x.kz", "no_deadline": "TRUE"}
	var warned string
	recs, rejected := BuildRecords("opportunities", []Item{{Row: 2, Cells: cells}}, func(s string) { warned = s })
	if len(recs) != 0 || rejected != 1 || !strings.Contains(warned, "adm-") {
		t.Fatalf("adm- id in a sheet: %d %d %q", len(recs), rejected, warned)
	}
}

// An extra source can neither replace nor duplicate a sheet / data/*.json id.
func TestExtraIDsDeduplicated(t *testing.T) {
	now := time.Date(2026, 10, 28, 6, 0, 0, 0, time.UTC)
	l := &Loader{Env: func(string) string { return "" }, DataDir: "../../data", Warn: func(string) {}}
	RegisterExtra("test-dup", func(ctx context.Context) (map[string][]Record, error) {
		return map[string][]Record{
			"opportunities": {RawRecord(json.RawMessage(`{"id":"o1","source":"admin"}`), "")},
			"discounts": {RawRecord(json.RawMessage(`{"id":"adm-7","source":"admin"}`), ""),
				RawRecord(json.RawMessage(`{"id":"adm-7","source":"admin"}`), "")},
		}, nil
	})
	defer RegisterExtra("test-dup", nil)
	c := l.Load(context.Background(), now)
	b, _ := json.Marshal(c)
	n := 0
	for _, r := range c.Opportunities {
		if RecordID(r) == "o1" {
			n++
			if raw, _ := json.Marshal(r); strings.Contains(string(raw), `"source":"admin"`) {
				t.Fatal("the extra record replaced data/opportunities.json o1")
			}
		}
	}
	if n != 1 || strings.Count(string(b), `"adm-7"`) != 1 {
		t.Fatalf("o1 x%d, adm-7 x%d", n, strings.Count(string(b), `"adm-7"`))
	}
}
