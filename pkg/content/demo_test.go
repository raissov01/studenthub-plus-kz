package content

import (
	"encoding/json"
	"testing"
)

func TestDemoThresholdIsIndependentAndCountsOnlyActiveRecords(t *testing.T) {
	yesterday, today := "2026-10-27", "2026-10-28"
	kinds := [][]Record{
		{&Opportunity{IsDemo: true}, &Opportunity{}, &Opportunity{}, &Opportunity{}, &Opportunity{}, &Opportunity{}, &Opportunity{Deadline: &yesterday}},
		{&Job{IsDemo: true}, &Job{}, &Job{}, &Job{}, &Job{}, &Job{}, &Job{}},
		{&Discount{IsDemo: true}, &Discount{}, &Discount{}, &Discount{}, &Discount{}, &Discount{}},
		{RawRecord(json.RawMessage(`{"is_demo":true}`), ""), RawRecord(json.RawMessage(`{"is_demo":false}`), "")},
	}
	for i, recs := range kinds {
		active := FilterActive(recs, today)
		shown := FilterDemo(active, true)
		want := []int{6, 6, 6, 2}[i]
		if len(shown) != want {
			t.Errorf("kind %d: %d, want %d", i, len(shown), want)
		}
		hidden := FilterDemo(active, false)
		if len(hidden) != len(active)-1 {
			t.Errorf("kind %d toggle: %d", i, len(hidden))
		}
		again := FilterDemo(active, true)
		if len(again) != want {
			t.Errorf("kind %d source mutated", i)
		}
	}
}
