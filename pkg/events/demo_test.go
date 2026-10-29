package events

import (
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// 11-025: the API says which events are samples, so every card can mark them «үлгі».
func TestEventAPIMarksDemo(t *testing.T) {
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
}
