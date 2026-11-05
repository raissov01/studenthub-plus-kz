package content

import (
	"encoding/json"
	"testing"
)

// Sample records never reach the site, however many real records there are, and filtering does
// not mutate the source slice.
func TestFilterDemoAlwaysDropsDemoRecords(t *testing.T) {
	yesterday, today := "2026-10-27", "2026-10-28"
	kinds := [][]Record{
		{&Opportunity{IsDemo: true}, &Opportunity{}, &Opportunity{Deadline: &yesterday}},
		{&Job{IsDemo: true}, &Job{}, &Job{}},
		{&Discount{IsDemo: true}, &Discount{IsDemo: true}},
		{RawRecord(json.RawMessage(`{"is_demo":true}`), ""), RawRecord(json.RawMessage(`{"is_demo":false}`), ""), RawRecord(json.RawMessage(`{}`), "")},
	}
	for i, recs := range kinds {
		active := FilterActive(recs, today)
		shown := FilterDemo(active)
		want := []int{1, 2, 0, 2}[i]
		if len(shown) != want {
			t.Errorf("kind %d: %d, want %d", i, len(shown), want)
		}
		if len(FilterDemo(active)) != want || len(active) != len(FilterActive(recs, today)) {
			t.Errorf("kind %d source mutated", i)
		}
	}
}
