package content

import (
	"encoding/json"
)

// FilterDemo drops every record marked is_demo. The site shows only real records: sample data is
// never served, whatever its source (Sheets rows, bundled data/*.json, extra sources).
// It works for typed Sheets records and raw records alike, and allocates a new slice: filtering
// must never mutate the loader's cached records.
func FilterDemo(records []Record) []Record {
	out := make([]Record, 0, len(records))
	for _, r := range records {
		b, err := json.Marshal(r)
		var flags struct {
			Demo bool `json:"is_demo"`
		}
		if err == nil && json.Unmarshal(b, &flags) == nil && flags.Demo {
			continue
		}
		out = append(out, r)
	}
	return out
}

func filterDemo(c *Content) {
	c.Opportunities = FilterDemo(c.Opportunities)
	c.Jobs = FilterDemo(c.Jobs)
	c.Discounts = FilterDemo(c.Discounts)
	c.Events = FilterDemo(c.Events)
}
