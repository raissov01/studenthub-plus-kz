package events

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

// Sample events never reach students: the public list skips them; the detail still says which one it is.
func TestEventAPIHidesDemo(t *testing.T) {
	testdb.Need(t)
	real := newEvent(t, evOpts{startsIn: 48 * time.Hour})
	demo := newEvent(t, evOpts{startsIn: 48 * time.Hour})
	exec(t, `update events set is_demo = true where id = $1`, demo)
	for id, want := range map[int64]bool{real: false, demo: true} {
		w := do(t, "GET", path(id, ""), 0, nil)
		ev, _ := decode(t, w)["event"].(map[string]any)
		if w.Code != 200 || ev["is_demo"] != want {
			t.Fatalf("event %d: %d %s", id, w.Code, w.Body)
		}
	}
	w := do(t, "GET", "/api/events", 0, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d,`, demo)) || !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d,`, real)) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
}
