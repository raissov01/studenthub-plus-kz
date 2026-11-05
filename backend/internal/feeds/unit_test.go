package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

func post(text string, links ...string) Post {
	return Post{SourceID: "tg-x", ExternalID: "1", URL: "https://t.me/example/1", Text: text, Links: links}
}

func good() Result {
	return Result{IsRelevant: true, Category: "hackathon", TitleKK: "Хакатон", TitleRU: "Хакатон", SummaryKK: "Қысқаша.",
		SummaryRU: "Кратко.", Funding: "none", Confidence: 0.9, Deadline: "2026-11-20", ApplyURL: "https://example.org/apply?slug=x"}
}

func TestDateMentioned(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	cases := []struct {
		text, date string
		want       bool
	}{
		{"Приём заявок до 20 ноября", "2026-11-20", true},
		{"Дедлайн: 2026-11-20", "2026-11-20", true},
		{"deadline 20.11.2026", "2026-11-20", true},
		{"до 05.12", "2026-12-05", true},
		{"Өтінім желтоқсанның 5-іне дейін", "2026-12-05", true},
		{"Deadline: November 30, 2026", "2026-11-30", true},
		{"apply by 3 Dec", "2026-12-03", true},
		{"deadline 20.11.2025", "2026-11-20", false}, // another year
		{"до 15 ноября", "2026-11-05", false},        // 15 is not 5
		{"Seminar on 5 topics", "2026-03-05", false}, // "mar" inside a word
		{"Заявки принимаются до конца месяца", "2026-11-30", false},
		{"Приём заявок до 20 ноября", "2026-12-20", false},
	}
	for _, c := range cases {
		if got := DateMentioned(c.text, d(c.date)); got != c.want {
			t.Errorf("DateMentioned(%q, %s) = %v", c.text, c.date, got)
		}
	}
}

func TestValidateKeepsStatedFacts(t *testing.T) {
	p := post("Хакатон для студентов. Приём заявок до 20 ноября. Регистрация: https://example.org/apply?slug=x")
	r, err := Validate(good(), p, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Deadline != "2026-11-20" || r.ApplyURL == "" || r.Confidence != 0.9 || len(r.Notes) != 0 {
		t.Errorf("%+v", r)
	}
	if r.SourceURL != p.URL {
		t.Errorf("source_url must be the post: %q", r.SourceURL)
	}
}

func TestValidateDropsGuessedDeadline(t *testing.T) {
	p := post("Хакатон для студентов, регистрация открыта.", "https://example.org/apply?slug=x")
	r, err := Validate(good(), p, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Deadline != "" || r.Confidence != GuessedCap || len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "not written") {
		t.Errorf("guessed deadline kept: %+v", r)
	}
}

func TestValidateRejectsBadDatesAndURLs(t *testing.T) {
	p := post("Хакатон, до 20 ноября. Заявка https://example.org/apply?slug=x")
	r := good()
	r.Deadline, r.EventDate = "20.11.2026", "2031-01-01"
	r.ApplyURL = "javascript:alert(1)"
	got, err := Validate(r, p, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Deadline != "" || got.EventDate != "" || got.ApplyURL != "" || len(got.Notes) != 3 {
		t.Errorf("%+v", got)
	}
	r = good()
	r.ApplyURL = "https://phishing.example.com/form" // valid URL, but not in the post
	got, _ = Validate(r, p, now)
	if got.ApplyURL != "" || got.Confidence > GuessedCap {
		t.Errorf("unknown link kept: %+v", got)
	}
	// the same link with tracking parameters and without www is the same link
	r = good()
	r.ApplyURL = "https://www.example.org/apply?slug=x&utm_source=tg"
	got, _ = Validate(r, p, now)
	if got.ApplyURL == "" {
		t.Errorf("canonical match failed: %+v", got)
	}
}

func TestValidateStructure(t *testing.T) {
	p := post("text")
	r := good()
	r.IsRelevant = false
	if _, err := Validate(r, p, now); !errors.Is(err, ErrIrrelevant) {
		t.Errorf("irrelevant: %v", err)
	}
	r = good()
	r.Category = "party"
	if _, err := Validate(r, p, now); err == nil {
		t.Error("unknown category accepted")
	}
	r = good()
	r.TitleKK, r.TitleRU = "", ""
	if _, err := Validate(r, p, now); err == nil {
		t.Error("no title accepted")
	}
	r = good()
	r.TitleKK, r.Funding, r.Confidence = "", "lots", 7
	got, _ := Validate(r, post("до 20 ноября https://example.org/apply?slug=x"), now)
	if got.TitleKK != "Хакатон" || got.Funding != "unknown" || got.Confidence != 1 {
		t.Errorf("%+v", got)
	}
}

func TestPrefilter(t *testing.T) {
	s := Source{}
	cases := []struct {
		text string
		ok   bool
	}{
		{"Открыт приём заявок на грант для студентов магистратуры, дедлайн 1 декабря.", true},
		{"Erasmus+ exchange semester for bachelor students, apply now!", true},
		{"Сегодня отличная погода, всем хорошего дня и продуктивной недели!", false},
		{"short", false},
		{"#реклама Скидка 50% на курсы английского по промокоду STUDENT, успей!", false},
		{"Реклама: стипендия фонда для студентов, подача заявок до 1 мая, подробности на сайте", true},
		{"Транскрипт и манускрипт: как подготовить документы для поступления в магистратуру", true},
	}
	for _, c := range cases {
		if ok, why := Prefilter(post(c.text), s); ok != c.ok {
			t.Errorf("Prefilter(%q) = %v (%s)", c.text, ok, why)
		}
	}
	if ok, why := Prefilter(post("Грант для студентов, дедлайн 1 декабря.\nhttps://studenthub.studywithraissov.com/opportunity/feed-12"), s); ok || why != "own feed post" {
		t.Error("our own channel post must not come back")
	}
	if ok, _ := Prefilter(post("A long enough post without any of the words at all, really."), Source{Relevant: true}); !ok {
		t.Error("relevant source skips keywords")
	}
}

func TestSimhashNearDuplicates(t *testing.T) {
	a := "Astana Hub объявляет набор в акселератор Silkway для стартапов на ранней стадии. Заявки принимаются до 30 ноября на сайте, участие бесплатное, лучшие команды получат гранты и менторов."
	b := a + " Подписывайтесь на канал!"
	c := "Университет приглашает студентов на летнюю школу по физике в Германии, полностью финансируется, дедлайн 1 марта, требуется английский B2 и мотивационное письмо."
	if d := Hamming(Simhash(a), Simhash(b)); d > NearDuplicate {
		t.Errorf("repost distance %d", d)
	}
	if d := Hamming(Simhash(a), Simhash(c)); d <= NearDuplicate {
		t.Errorf("different posts distance %d", d)
	}
}

func TestCanonicalAndURLKey(t *testing.T) {
	if CanonicalURL("https://www.Example.org/a/?utm_source=x&id=2#top") != "example.org/a?id=2" {
		t.Error(CanonicalURL("https://www.Example.org/a/?utm_source=x&id=2#top"))
	}
	if CanonicalURL("ftp://x.org/a") != "" || CanonicalURL("not a url") != "" {
		t.Error("non-http accepted")
	}
	for _, generic := range []string{"https://astanahub.com/", "https://t.me/astana_hub", "http://hackathon.kz"} {
		if URLKey(generic) != "" {
			t.Errorf("%s is too generic for a dedupe key", generic)
		}
	}
	if URLKey("https://t.me/astana_hub/7290") == "" || URLKey("https://hackathon.kz/apply?slug=ivl") == "" {
		t.Error("specific links must be keys")
	}
}

func TestDecideAutopublishThreshold(t *testing.T) {
	cases := []struct {
		conf      float64
		trust     string
		threshold float64
		want      string
	}{
		{0.95, "high", 0, StatusPending},      // autopublish off
		{0.95, "high", 0.85, StatusPublished}, // trusted and sure
		{0.85, "high", 0.85, StatusPublished}, // exactly the threshold
		{0.84, "high", 0.85, StatusPending},
		{0.99, "medium", 0.85, StatusPending}, // only trusted sources
		{GuessedCap, "high", 0.85, StatusPending},
	}
	for _, c := range cases {
		if got := Decide(c.conf, c.trust, c.threshold); got != c.want {
			t.Errorf("Decide(%v, %s, %v) = %s", c.conf, c.trust, c.threshold, got)
		}
	}
}

func TestExpired(t *testing.T) {
	today := AlmatyToday(now) // 2026-11-05
	old := now.AddDate(0, 0, -50)
	cases := []struct {
		deadline, event string
		created         time.Time
		want            bool
	}{
		{"2026-11-04", "", now, true},
		{"2026-11-05", "", now, false}, // the deadline day itself is still open
		{"2026-12-01", "2026-10-01", now, false},
		{"", "2026-11-01", now, true},
		{"", "", old, true}, // undated and older than the TTL
		{"", "", now.AddDate(0, 0, -10), false},
	}
	for _, c := range cases {
		if got := Expired(c.deadline, c.event, c.created, today, 45, now); got != c.want {
			t.Errorf("Expired(%q, %q, %v) = %v", c.deadline, c.event, c.created, got)
		}
	}
}

func TestRegistryFileIsValid(t *testing.T) {
	list, err := LoadRegistry("../../feeds/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 5 {
		t.Fatalf("only %d sources", len(list))
	}
	enabled := 0
	for _, s := range list {
		if s.Enabled {
			enabled++
		}
		if s.Kind == KindTGPublic && s.URL != "https://t.me/"+s.Handle {
			t.Errorf("%s url %q", s.ID, s.URL)
		}
	}
	if enabled == 0 {
		t.Error("no enabled source")
	}
}

func TestRegistryRejectsBadEntries(t *testing.T) {
	bad := []string{
		"sources:\n  - {id: a1, kind: tg_public, handle: 'bad handle', name: X}\n",
		"sources:\n  - {id: a1, kind: rss, url: 'ftp://x', name: X}\n",
		"sources:\n  - {id: a1, kind: html, url: 'https://x.kz', name: X}\n",
		"sources:\n  - {id: a1, kind: rss, url: 'https://x.kz', name: X, category: party}\n",
		"sources:\n  - {id: a1, kind: rss, url: 'https://x.kz', name: X}\n  - {id: a1, kind: rss, url: 'https://y.kz', name: Y}\n",
		"sources:\n  - {id: a1, kind: rss, url: 'https://x.kz', name: X, typo: 1}\n",
		"sources:\n  - {id: a1, kind: tg_mtproto, name: X}\n",
	}
	for _, b := range bad {
		if _, err := ParseRegistry([]byte(b)); err == nil {
			t.Errorf("accepted:\n%s", b)
		}
	}
	list, err := ParseRegistry([]byte("sources:\n  - {id: a1, kind: tg_mtproto, channel_id: 42, name: X}\n"))
	if err != nil || list[0].Trust != "medium" || list[0].Scope != "KZ" {
		t.Errorf("%v %v", list, err)
	}
}

func TestParseRobots(t *testing.T) {
	body := "User-agent: *\nDisallow: /admin/\nDisallow: /*.pdf\nDisallow: /search*\n\nUser-agent: BadBot\nDisallow: /\n"
	got := ParseRobots(body, "StudentHubBot")
	if strings.Join(got, ",") != "/admin/,/search" {
		t.Errorf("%v", got)
	}
	got = ParseRobots("User-agent: StudentHubBot\nDisallow: /feed\nUser-agent: *\nDisallow:\n", "StudentHubBot")
	if strings.Join(got, ",") != "/feed" {
		t.Errorf("own group: %v", got)
	}
}

func TestWebHonoursRobots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			_, _ = io.WriteString(w, "User-agent: *\nDisallow: /private/\n")
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	w := NewWeb()
	w.MinGap = 0
	if _, err := w.Get(context.Background(), srv.URL+"/private/list"); !errors.Is(err, ErrRobots) {
		t.Errorf("robots ignored: %v", err)
	}
	if b, err := w.Get(context.Background(), srv.URL+"/news"); err != nil || string(b) != "ok" {
		t.Errorf("%s %v", b, err)
	}
}

// ---------- AI client (mock server; the real API is never called in tests) ----------

func fakeAI(t *testing.T, statuses []int, answer Result) (*AI, *int32, *[]map[string]any) {
	t.Helper()
	var calls int32
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(401)
			return
		}
		if int(n) <= len(statuses) {
			w.WriteHeader(statuses[n-1])
			_, _ = io.WriteString(w, `{"error":{"message":"overloaded"}}`)
			return
		}
		content, _ := json.Marshal(answer)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}},
			"usage":   map[string]any{"prompt_tokens": 700, "completion_tokens": 250},
		})
	}))
	t.Cleanup(srv.Close)
	ai := NewAI(func(k string) string {
		return map[string]string{"OPENAI_API_KEY": "test-key", "OPENAI_BASE_URL": srv.URL, "OPENAI_MODEL": "m-test"}[k]
	})
	ai.Sleep = func(context.Context, time.Duration) error { return nil }
	return ai, &calls, &bodies
}

func TestAIRetriesOverloadedThenParses(t *testing.T) {
	ai, calls, bodies := fakeAI(t, []int{429, 529, 503}, good())
	r, u, err := ai.Classify(context.Background(), post("Хакатон до 20 ноября"), Source{Name: "X"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 4 || r.Category != "hackathon" || u.Total() != 950 {
		t.Errorf("calls %d r %+v u %+v", *calls, r, u)
	}
	b := (*bodies)[0]
	rf := b["response_format"].(map[string]any)
	schema := rf["json_schema"].(map[string]any)["schema"].(map[string]any)
	if rf["type"] != "json_schema" || schema["additionalProperties"] != false || len(schema["required"].([]any)) != len(schema["properties"].(map[string]any)) {
		t.Errorf("schema must be strict and flat: %v", rf)
	}
	raw, _ := json.Marshal(schema)
	if strings.Contains(string(raw), "anyOf") {
		t.Error("anyOf in schema")
	}
	if b["model"] != "m-test" {
		t.Errorf("model %v", b["model"])
	}
}

func TestAIGivesUpAfterRetries(t *testing.T) {
	ai, calls, _ := fakeAI(t, []int{529, 529, 529, 529, 529}, good())
	if _, _, err := ai.Classify(context.Background(), post("x"), Source{}, now); !errors.Is(err, ErrUpstream) {
		t.Errorf("err %v", err)
	}
	if *calls != 4 {
		t.Errorf("calls %d", *calls)
	}
	ai, calls, _ = fakeAI(t, []int{400}, good())
	if _, _, err := ai.Classify(context.Background(), post("x"), Source{}, now); !errors.Is(err, ErrUpstream) || *calls != 1 {
		t.Errorf("400 is not retried: %v %d", err, *calls)
	}
}

func TestFeedModelOverride(t *testing.T) {
	ai := NewAI(func(k string) string {
		return map[string]string{"OPENAI_MODEL": "chat-model", "FEEDS_AI_MODEL": "feed-model"}[k]
	})
	if ai.Model != "feed-model" || ai.Key != "" {
		t.Errorf("%+v", ai)
	}
	_ = os.Getenv
}
