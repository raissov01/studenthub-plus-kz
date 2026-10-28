package events

import (
	"strings"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

// 10-b2b: university staff run their university's events on counts only — no endpoint gives
// them a student's name, username, id, faculty row or comment. Ambassadors keep the list (05).
func TestUniversityStaffSeesAggregatesOnly(t *testing.T) {
	testdb.Need(t)
	staff := newUser(t, "university_staff", "PRIV", "")
	amb := newUser(t, "ambassador", "PRIV", "")
	ev := newEvent(t, evOpts{univ: "PRIV", startsIn: -10 * time.Minute, walkIn: true})
	var students []int64
	for i := 0; i < 3; i++ {
		s := newUser(t, "student", "PRIV", "Secret Faculty")
		exec(t, `update users set name = 'Private Person', tg_username = 'private_handle' where id = $1`, s)
		if w := do(t, "POST", path(ev, "/register"), s, nil); w.Code != 200 {
			t.Fatalf("register: %d %s", w.Code, w.Body)
		}
		students = append(students, s)
	}
	exec(t, `update event_regs set ticket = 'TICKETPRIV01' where event_id = $1 and user_id = $2`, ev, students[0])
	// the staff member registered as a participant still sees no names
	if w := do(t, "POST", path(ev, "/register"), staff, nil); w.Code != 200 {
		t.Fatalf("staff register: %d %s", w.Code, w.Body)
	}
	leak := func(name, body string) {
		t.Helper()
		pii := []string{"Private Person", "private_handle", "user_id"}
		if name != "csv" { // the CSV has a per-faculty aggregate (as "< 5" here)
			pii = append(pii, "Secret Faculty")
		}
		for _, s := range pii {
			if strings.Contains(body, s) {
				t.Fatalf("%s leaks %q: %s", name, s, body)
			}
		}
	}

	// list endpoints: counts, no rows
	w := do(t, "GET", path(ev, "/regs"), staff, nil)
	out := decode(t, w)
	if w.Code != 200 || len(out["regs"].([]any)) != 0 || out["aggregate_only"] != true || out["registered"].(float64) != 4 {
		t.Fatalf("staff regs: %d %s", w.Code, w.Body)
	}
	leak("regs", w.Body.String())
	w = do(t, "GET", path(ev, "/attendees"), staff, nil)
	if w.Code != 200 || decode(t, w)["attendees"] != nil || decode(t, w)["count"].(float64) != 4 {
		t.Fatalf("staff attendees: %s", w.Body)
	}
	leak("attendees", w.Body.String())

	// check-in: QR and e-ticket code work, user ids do not
	startQR(t, ev, staff)
	if w := do(t, "GET", path(ev, "/checkin/live"), staff, nil); w.Code != 200 {
		t.Fatalf("live: %d", w.Code)
	}
	if w := do(t, "POST", path(ev, "/checkin/manual"), staff, map[string]any{"user_id": students[1]}); w.Code != 403 || errCode(w) != "aggregate_only" {
		t.Fatalf("manual by id: %d %s", w.Code, w.Body)
	}
	w = do(t, "POST", path(ev, "/checkin/manual"), staff, map[string]any{"ticket": "TICKETPRIV01"})
	if w.Code != 200 {
		t.Fatalf("manual by ticket: %d %s", w.Code, w.Body)
	}
	leak("manual", w.Body.String())
	if w := do(t, "POST", path(ev, "/checkin/revoke"), staff, map[string]any{"user_id": students[0]}); w.Code != 403 {
		t.Fatalf("revoke: %d", w.Code)
	}

	// CSV: metric,value only; small groups as "< 5"
	w = do(t, "GET", path(ev, "/attendees.csv"), staff, nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "metric,value") || !strings.Contains(body, "checked_in,< 5") ||
		!strings.Contains(body, "registered,< 5") || !strings.Contains(body, "checked_in_faculty:Secret Faculty,< 5") ||
		strings.Contains(body, "checked_in_at") {
		t.Fatalf("staff csv: %d %q", w.Code, body)
	}
	leak("csv", body)

	// feedback: no comments, nothing below 5 answers
	exec(t, `update event_regs set checked_in_at = now(), feedback_score = 2, feedback_text = 'Private Person was rude', feedback_at = now()
		where event_id = $1`, ev)
	w = do(t, "GET", path(ev, "/feedback"), staff, nil)
	fb := decode(t, w)
	if w.Code != 200 || fb["count"] != "< 5" || fb["average"] != nil || fb["distribution"] != nil || len(fb["comments"].([]any)) != 0 {
		t.Fatalf("staff feedback: %s", w.Body)
	}
	leak("feedback", w.Body.String())

	// the ambassador organizer keeps the attendee list and the full export (05)
	w = do(t, "GET", path(ev, "/regs"), amb, nil)
	if w.Code != 200 || len(decode(t, w)["regs"].([]any)) != 4 || !strings.Contains(w.Body.String(), "private_handle") {
		t.Fatalf("ambassador regs: %s", w.Body)
	}
	if w := do(t, "GET", path(ev, "/attendees.csv"), amb, nil); !strings.Contains(w.Body.String(), "private_handle") {
		t.Fatalf("ambassador csv: %s", w.Body)
	}
	if w := do(t, "GET", path(ev, "/feedback"), amb, nil); len(decode(t, w)["comments"].([]any)) != 4 {
		t.Fatalf("ambassador feedback: %s", w.Body)
	}
}
