package mentors

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/notify"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func newMeeting(t *testing.T, uni string, startsIn time.Duration, capacity int) int64 {
	t.Helper()
	start := time.Now().Add(startsIn)
	return scalar[int64](t, `insert into events (title_kk, title_ru, cat, starts_at, ends_at, university, capacity, status, place_text)
		values ('Сессияға дайындық', 'Подготовка к сессии', 'mentoring', $1, $2, $3, $4, 'published', '1-корпус') returning id`,
		start, start.Add(time.Hour), uni, capacity)
}

// 07-017: a new group meeting reaches the waiting mentees of that university once, as a
// digest-type message (one bot message a day), and always lands in the inbox.
func TestGroupMeetingNotifiesWaitingStudents(t *testing.T) {
	testdb.Need(t)
	uni := "Meet Uni"
	waiting, waitingTG := newUser(t, userOpt{university: uni})
	minor, minorTG := newUser(t, userOpt{university: uni, adult: &no})
	busy, busyTG := newUser(t, userOpt{university: uni})
	already, alreadyTG := newUser(t, userOpt{university: uni})
	noRequest, noRequestTG := newUser(t, userOpt{university: uni})
	elsewhere, elsewhereTG := newUser(t, userOpt{university: "Other Meet Uni"})
	for _, id := range []int64{waiting, minor, busy, already, elsewhere} {
		request(t, id, "study")
	}
	_ = noRequest
	ev := newMeeting(t, uni, 48*time.Hour, 10)
	newMeeting(t, uni, -time.Hour, 10)          // already started: never announced
	full := newMeeting(t, uni, 72*time.Hour, 5) // no seats left
	exec(t, `insert into event_regs (event_id, user_id, ticket) select $1, id, 'full'||id from users order by id desc limit 5`, full)
	exec(t, `insert into event_regs (event_id, user_id, ticket) values ($1, $2, 'already-registered')`, ev, already)
	now := time.Now()
	exec(t, `insert into bot_daily_sends (user_id, day) values ($1, $2::date)`, busy, notify.Day(now))

	count := func(tg int64) int {
		n := 0
		for _, m := range sent(tg) {
			if strings.Contains(m, "/event/"+strconv.FormatInt(ev, 10)) {
				n++
			}
		}
		return n
	}
	if _, err := RunCron(ctx(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := notify.Flush(ctx(), now); err != nil {
		t.Fatal(err)
	}
	if count(waitingTG) != 1 || count(minorTG) != 1 || !strings.Contains(sent(waitingTG)[len(sent(waitingTG))-1], "Сессияға дайындық") {
		t.Fatalf("waiting students not told: %q %q", sent(waitingTG), sent(minorTG))
	}
	if count(busyTG) != 0 || count(alreadyTG) != 0 || count(noRequestTG) != 0 || count(elsewhereTG) != 0 {
		t.Fatal("told the wrong students")
	}
	if n := scalar[int](t, `select count(*) from mentor_meeting_notices where user_id = any($1) and event_id <> $2`, []int64{waiting, minor, busy}, ev); n != 0 {
		t.Fatalf("started or full meeting announced: %d", n)
	}
	// the busy student has it in the inbox already, with a link to the meeting
	if n := scalar[int](t, `select count(*) from notifications where user_id = $1 and category = 'mentors' and link = $2`, busy, "/event/"+strconv.FormatInt(ev, 10)); n != 1 {
		t.Fatalf("inbox for the postponed student: %d", n)
	}
	// same day: nobody hears twice
	if _, err := RunCron(ctx(), now); err != nil {
		t.Fatal(err)
	}
	if count(waitingTG) != 1 || count(busyTG) != 0 {
		t.Fatal("told twice on the same day")
	}
	// tomorrow the busy student gets it; still once per meeting for the others
	if _, err := RunCron(ctx(), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := notify.Flush(ctx(), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if count(busyTG) != 1 || count(waitingTG) != 1 || count(minorTG) != 1 {
		t.Fatalf("next day: busy %d waiting %d minor %d", count(busyTG), count(waitingTG), count(minorTG))
	}
	if n := scalar[int](t, `select count(*) from notifications where user_id = $1 and link = $2`, busy, "/event/"+strconv.FormatInt(ev, 10)); n != 1 {
		t.Fatalf("inbox duplicated by the retry: %d", n)
	}
	if n := scalar[int](t, `select count(*) from mentor_meeting_notices where event_id = $1`, ev); n != 3 {
		t.Fatalf("notices: %d", n)
	}
}

// 07-011: the match message lists both usernames when present; without one the bot
// introduces the pair (tg://user link), and without Telegram the pair page is the way.
func TestIntroContacts(t *testing.T) {
	both := introText(person{Name: "Mentee", Username: "mentee_u", TelegramID: 1, Lang: "kk"},
		person{Name: "Mentor", Username: "mentor_u", TelegramID: 2}, "study", false)
	if !strings.Contains(both, "@mentor_u") || !strings.Contains(both, "@mentee_u") || strings.Contains(both, "tg://") {
		t.Fatalf("both usernames: %s", both)
	}
	noName := introText(person{Name: "Mentor", Username: "mentor_u", TelegramID: 2, Lang: "ru"},
		person{Name: "Аня <b>", TelegramID: 777}, "study", true)
	if !strings.Contains(noName, `<a href="tg://user?id=777">Аня &lt;b&gt;</a>`) || !strings.Contains(noName, "@mentor_u") ||
		!strings.Contains(noName, "/mentors/me") {
		t.Fatalf("partner without username: %s", noName)
	}
	self := introText(person{Name: "Mentee", TelegramID: 5, Lang: "kk"}, person{Name: "Mentor", Username: "mentor_u", TelegramID: 2}, "study", false)
	if !strings.Contains(self, "@mentor_u") || !strings.Contains(self, "username орнатыңыз") {
		t.Fatalf("reader without username: %s", self)
	}
	email := introText(person{Name: "Mentee", Lang: "kk"}, person{Name: "Mentor", TelegramID: 9}, "study", false)
	if !strings.Contains(email, "tg://user?id=9") || !strings.Contains(email, "профильде Telegram-ды қосыңыз") {
		t.Fatalf("email-only reader: %s", email)
	}
	none := introText(person{Name: "Mentor", Username: "mentor_u", TelegramID: 2, Lang: "kk"}, person{Name: "Mentee"}, "study", true)
	if !strings.Contains(none, "жұп бетінен сіздің username-іңізді көріп") || strings.Contains(none, "tg://") {
		t.Fatalf("partner without Telegram: %s", none)
	}
	for _, m := range []string{both, noName, self, email, none} {
		if strings.Contains(m, "+7") || !strings.Contains(m, "15") {
			t.Fatalf("intro: %s", m)
		}
	}
}
