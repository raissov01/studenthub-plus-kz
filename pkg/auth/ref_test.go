package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/testdb"
)

func TestCleanRef(t *testing.T) {
	for in, want := range map[string]string{
		"share": "share", "SHARE": "share", "amb_abc123": "amb_abc123", "AMB_AbC": "amb_abc",
		"": "", "amb_": "", "amb_a b": "", "amb_<x>": "", "other": "", "amb_" + strings.Repeat("a", 40): "",
	} {
		if got := CleanRef(in); got != want {
			t.Errorf("CleanRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func referredBy(t *testing.T, tg int64) string {
	t.Helper()
	var s *string
	err := db.Tx(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `select referred_by from users where telegram_id = $1`, tg).Scan(&s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func TestLoginRecordsReferralOnceAtSignUp(t *testing.T) {
	testdb.Need(t)
	login := func(tg int64, ref string) {
		t.Helper()
		body := map[string]string{"initData": SignWebApp(webappFields(tg, time.Now()), testBot)}
		if ref != "" {
			body["ref"] = ref
		}
		if w := post(t, "/api/auth/telegram/webapp", body, nil); w.Code != 200 {
			t.Fatalf("login %d: %d %s", tg, w.Code, w.Body)
		}
	}
	login(3101, "amb_Team1")
	if got := referredBy(t, 3101); got != "amb_team1" {
		t.Fatalf("referred_by = %q", got)
	}
	login(3101, "share") // never overwritten
	if got := referredBy(t, 3101); got != "amb_team1" {
		t.Fatalf("overwritten: %q", got)
	}
	login(3102, "amb_<script>") // invalid refs are ignored
	if got := referredBy(t, 3102); got != "" {
		t.Fatalf("invalid ref stored: %q", got)
	}
	login(3103, "") // consented account: a later ref is not a sign-up
	exec(t, `update users set consent_at = now() where telegram_id = 3103`)
	login(3103, "share")
	if got := referredBy(t, 3103); got != "" {
		t.Fatalf("ref after consent stored: %q", got)
	}
	// Widget login: "ref" is ours and is not part of the Telegram hash.
	body := widgetBody(t, 3104, time.Now(), testBot)
	body["ref"] = "share"
	if w := post(t, "/api/auth/telegram/widget", body, nil); w.Code != 200 {
		t.Fatalf("widget with ref: %d %s", w.Code, w.Body)
	}
	if got := referredBy(t, 3104); got != "share" {
		t.Fatalf("widget referred_by = %q", got)
	}
}
