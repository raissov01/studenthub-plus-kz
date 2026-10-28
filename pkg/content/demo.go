package content

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/settings"
)

// FilterDemo works for typed Sheets records and raw records from bundled/extra sources.
// It allocates a new slice: filtering must never mutate the loader's cached records.
func FilterDemo(records []Record, show bool) []Record {
	demos := make([]bool, len(records))
	real := 0
	for i, r := range records {
		b, err := json.Marshal(r)
		var flags struct {
			Demo bool `json:"is_demo"`
		}
		if err != nil || json.Unmarshal(b, &flags) != nil {
			continue
		}
		demos[i] = flags.Demo
		if !flags.Demo {
			real++
		}
	}
	out := make([]Record, 0, len(records))
	for i, r := range records {
		if !demos[i] || (show && real <= 5) {
			out = append(out, r)
		}
	}
	return out
}

func (l *Loader) filterDemo(ctx context.Context, c *Content) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	show, err := settings.ShowDemo(ctx)
	if errors.Is(err, db.ErrNoDB) {
		show = true
	} // preserve the standalone, database-free fallback
	if err != nil && !errors.Is(err, db.ErrNoDB) {
		show = false
		l.warn("[content] demo setting unavailable; demos hidden")
	}
	c.Opportunities = FilterDemo(c.Opportunities, show)
	c.Jobs = FilterDemo(c.Jobs, show)
	c.Discounts = FilterDemo(c.Discounts, show)
	c.Events = FilterDemo(c.Events, show)
}
