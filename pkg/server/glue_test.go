package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/events"
	"github.com/raissov01/studenthub-plus-kz/pkg/mentors"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Main(m, "glue")) }

func TestMentorGroupMeetingsAcrossFeatures(t *testing.T) {
	testdb.Need(t)
	ctx := context.Background()
	var mentor, student, lead, member int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		q := `insert into users (telegram_id, name, university, role, consent_at) values ($1, $2, 'KATU', $3, now()) returning id`
		for _, u := range []struct {
			tg   int64
			name string
			role string
			id   *int64
		}{{901, "Mentor", "student", &mentor}, {902, "Student", "student", &student}, {903, "Lead", "ambassador", &lead}, {904, "Member", "ambassador", &member}} {
			if err := tx.QueryRow(ctx, q, u.tg, u.name, u.role).Scan(u.id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `insert into mentors (user_id, university, areas, status) values ($1, 'KATU', '{career}', 'approved')`, mentor); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into ambassador_terms (user_id, university, role_in_team, status, ref_code, term_start, term_end, hours_per_week)
			values ($1, 'KATU', 'lead', 'active', 'lead1', current_date, current_date + 100, 3),
			       ($2, 'KATU', 'member', 'active', 'mem1', current_date, current_date + 100, 3)`, lead, member)
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	insert := func(uid int64, cat, uni string) error {
		return db.WithUser(ctx, uid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `insert into events (title_kk, cat, starts_at, ends_at, university, status, created_by)
				values ('Топтық кездесу', $1, now() + interval '2 days', now() + interval '2 days 1 hour', $2, 'published', $3)`, cat, uni, uid)
			return err
		})
	}
	if err := insert(mentor, "mentoring", "KATU"); err != nil {
		t.Fatalf("approved mentor creating a mentoring meeting: %v", err)
	}
	if err := insert(mentor, "career", "KATU"); err == nil {
		t.Error("mentor must not create non-mentoring events (RLS)")
	}
	if err := insert(mentor, "mentoring", "OTHER"); err == nil {
		t.Error("mentor must not create events for another university (RLS)")
	}
	if err := insert(student, "mentoring", "KATU"); err == nil {
		t.Error("a plain student must not create events (RLS)")
	}

	list, err := mentors.GroupMeetings(ctx, "KATU", 3)
	if err != nil || len(list) != 1 || list[0].URL == "" {
		t.Fatalf("GroupMeetings = %v, %v", list, err)
	}
	created := mentor
	ev := &events.Event{Cat: "mentoring", University: "KATU", CreatedBy: &created}
	if !events.ExtraManager(ctx, &auth.User{ID: mentor}, ev) {
		t.Error("mentor should manage their own meeting")
	}
	if events.ExtraManager(ctx, &auth.User{ID: student}, ev) {
		t.Error("student must not manage the meeting")
	}
	if ok, _ := mentors.AmbassadorCanMatch(ctx, lead, "KATU"); !ok {
		t.Error("team lead should match mentors")
	}
	if ok, _ := mentors.AmbassadorCanMatch(ctx, member, "KATU"); ok {
		t.Error("a team member must not match mentors")
	}
}

// An ambassador or mentor must not switch university through their own profile to gain
// organizer rights elsewhere.
func TestUniversityLockedForOrganizers(t *testing.T) {
	testdb.Need(t)
	ctx := context.Background()
	var amb, mentor, student int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		q := `insert into users (telegram_id, name, university, role, consent_at) values ($1, 'x', 'KATU', $2, now()) returning id`
		if err := tx.QueryRow(ctx, q, 911, "ambassador").Scan(&amb); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, q, 912, "student").Scan(&mentor); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, q, 913, "student").Scan(&student); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into mentors (user_id, university, areas, status) values ($1, 'KATU', '{study}', 'approved')`, mentor)
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	move := func(uid int64) string {
		var uni string
		err := db.WithUser(ctx, uid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `update users set university = 'ENU' where id = $1 returning university`, uid).Scan(&uni)
		})
		if err != nil {
			t.Fatalf("update %d: %v", uid, err)
		}
		return uni
	}
	if got := move(amb); got != "KATU" {
		t.Errorf("ambassador moved to %s", got)
	}
	if got := move(mentor); got != "KATU" {
		t.Errorf("mentor moved to %s", got)
	}
	if got := move(student); got != "ENU" {
		t.Errorf("a plain student should be able to change university, got %s", got)
	}
}

// Approved mentors open mentoring group meetings through the real HTTP endpoint (5–10 seats);
// a plain student cannot create events at all.
func TestMentorCreatesGroupMeetingOverHTTP(t *testing.T) {
	testdb.Need(t)
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("FEATURE_EVENTS", "true")
	t.Setenv("ALLOWED_HOSTS", "localhost")
	ctx := context.Background()
	var mentor, student int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		q := `insert into users (telegram_id, name, university, role, consent_at) values ($1, 'x', 'KATU', 'student', now()) returning id`
		if err := tx.QueryRow(ctx, q, 921).Scan(&mentor); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, q, 922).Scan(&student); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into mentors (user_id, university, areas, status) values ($1, 'KATU', '{study}', 'approved')`, mentor)
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	post := func(uid int64, cat string, capacity int) int {
		tok, err := auth.Sign(auth.Claims{UserID: uid, Version: 1, Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now().Add(48 * time.Hour).UTC()
		body := fmt.Sprintf(`{"title_kk":"Топтық кездесу","cat":%q,"starts_at":%q,"ends_at":%q,"capacity":%d,"place_text":"4-корпус","status":"published"}`,
			cat, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339), capacity)
		req := httptest.NewRequest("POST", "/api/events/create", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost")
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok})
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, req)
		if w.Code >= 300 {
			t.Logf("uid %d cat %s cap %d -> %d %s", uid, cat, capacity, w.Code, w.Body)
		}
		return w.Code
	}
	if c := post(mentor, "mentoring", 7); c >= 300 {
		t.Errorf("approved mentor creating a 7-seat mentoring meeting: %d", c)
	}
	if c := post(mentor, "mentoring", 20); c != http.StatusBadRequest {
		t.Errorf("mentor meeting with 20 seats: %d, want 400", c)
	}
	if c := post(mentor, "career", 7); c != http.StatusForbidden {
		t.Errorf("mentor creating a career event: %d, want 403", c)
	}
	if c := post(student, "mentoring", 7); c != http.StatusForbidden {
		t.Errorf("plain student creating an event: %d, want 403", c)
	}
}
