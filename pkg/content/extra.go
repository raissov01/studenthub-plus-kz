package content

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Extra sources merge database-backed records into /api/content without touching the
// Sheets / data/*.json pipeline (prompt 06: approved ambassador submissions, source =
// "ambassador"). A source returns records per list: "opportunities" and "places". They are
// appended after the sheet records and go through the same deadline filter. A failing source
// is logged and skipped, so /api/content never breaks because of it. With no sources
// registered (or none returning records) the response is exactly what it was before.
type ExtraSource func(ctx context.Context) (map[string][]Record, error)

var (
	extraMu      sync.RWMutex
	extraSources = map[string]ExtraSource{}
)

const extraTimeout = 3 * time.Second

// RegisterExtra adds (or, with fn == nil, removes) a named extra source.
func RegisterExtra(name string, fn ExtraSource) {
	extraMu.Lock()
	defer extraMu.Unlock()
	if fn == nil {
		delete(extraSources, name)
		return
	}
	extraSources[name] = fn
}

// RawRecord wraps an already-shaped JSON object as a Record; deadline is "YYYY-MM-DD" or "".
func RawRecord(raw json.RawMessage, deadline string) Record {
	var d any
	if deadline != "" {
		d = deadline
	}
	return rawRecord{raw: raw, deadline: d}
}

func (l *Loader) loadExtras(ctx context.Context) map[string][]Record {
	extraMu.RLock()
	fns := make(map[string]ExtraSource, len(extraSources))
	for k, v := range extraSources {
		fns[k] = v
	}
	extraMu.RUnlock()
	if len(fns) == 0 {
		return nil
	}
	out := map[string][]Record{}
	for name, fn := range fns {
		func() {
			defer func() {
				if v := recover(); v != nil {
					l.warn(fmt.Sprintf("[content] extra %s: күтпеген қате: %v", name, v))
				}
			}()
			c, cancel := context.WithTimeout(ctx, extraTimeout)
			defer cancel()
			got, err := fn(c)
			if err != nil {
				l.warn(fmt.Sprintf("[content] extra %s: %v", name, err))
				return
			}
			for kind, recs := range got {
				out[kind] = append(out[kind], recs...)
			}
		}()
	}
	return out
}
