package server

import (
	"context"
	"os"
	"testing"

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
