package auth

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBot = "123456:TEST-bot-token"

func webappFields(id int64, at time.Time) map[string]string {
	return map[string]string{
		"auth_date": strconv.FormatInt(at.Unix(), 10),
		"query_id":  "AAHdF6IQAAAAAN0XohDhrOrc",
		"user":      `{"id":` + strconv.FormatInt(id, 10) + `,"first_name":"Айгерім","last_name":"Н","username":"aigerim","language_code":"ru"}`,
	}
}

func TestVerifyWebApp(t *testing.T) {
	now := time.Now()
	good := SignWebApp(webappFields(42, now.Add(-time.Hour)), testBot)
	u, err := VerifyWebApp(good, testBot, now)
	if err != nil || u.ID != 42 || u.Username != "aigerim" || u.LanguageCode != "ru" {
		t.Fatalf("valid initData: %+v %v", u, err)
	}
	if _, err := VerifyWebApp(good, "999:other-token", now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("other bot token: %v", err)
	}
	tampered := strings.Replace(good, "aigerim", "hacker", 1)
	if _, err := VerifyWebApp(tampered, testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("tampered user: %v", err)
	}
	old := SignWebApp(webappFields(42, now.Add(-25*time.Hour)), testBot)
	if _, err := VerifyWebApp(old, testBot, now); !errors.Is(err, ErrExpired) {
		t.Fatalf("25h old: %v", err)
	}
	// Widget-style key (SHA256 of token) must not validate initData.
	f := webappFields(42, now)
	signed := SignWebApp(f, testBot)
	i := strings.Index(signed, "hash=") + len("hash=")
	q := signed[:i] + SignWidget(f, testBot) + signed[i+64:]
	if _, err := VerifyWebApp(q, testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("widget key on initData: %v", err)
	}
	if _, err := VerifyWebApp(SignWebApp(map[string]string{}, testBot)+"&x=1", testBot, now); err == nil {
		t.Fatal("hash covering other fields accepted")
	}
	if _, err := VerifyWebApp("hash=zz&auth_date=1", testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("garbage: %v", err)
	}
	if _, err := VerifyWebApp(good, "", now); !errors.Is(err, ErrNoToken) {
		t.Fatalf("no token: %v", err)
	}
}

// Current clients add an Ed25519 "signature" field. It is part of the data-check-string for
// the HMAC hash (only "hash" is excluded); older clients send no signature at all.
func TestVerifyWebAppWithSignatureField(t *testing.T) {
	now := time.Now()
	f := webappFields(43, now)
	f["signature"] = "SGVsbG8gZWQyNTUxOSBzaWduYXR1cmUgdGVzdA-_xx"
	withSig := SignWebApp(f, testBot)
	if !strings.Contains(withSig, "signature=") {
		t.Fatal("fixture lacks signature")
	}
	if u, err := VerifyWebApp(withSig, testBot, now); err != nil || u.ID != 43 {
		t.Fatalf("initData with signature: %+v %v", u, err)
	}
	// A hash computed with signature dropped from the check string must not match.
	noSig := SignWebApp(webappFields(43, now), testBot)
	i := strings.Index(noSig, "hash=") + len("hash=")
	j := strings.Index(withSig, "hash=") + len("hash=")
	mixed := withSig[:j] + noSig[i:i+64] + withSig[j+64:]
	if _, err := VerifyWebApp(mixed, testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("hash without signature accepted for data with signature: %v", err)
	}
	if _, err := VerifyWebApp(strings.Replace(withSig, "SGVsbG8", "SGVsbG9", 1), testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("tampered signature: %v", err)
	}
	// Old clients without signature still work.
	if _, err := VerifyWebApp(noSig, testBot, now); err != nil {
		t.Fatalf("initData without signature: %v", err)
	}
}

func widgetBody(t *testing.T, id int64, at time.Time, token string) map[string]any {
	f := map[string]string{
		"id": strconv.FormatInt(id, 10), "first_name": "Ерлан", "username": "erlan",
		"photo_url": "https://t.me/i/userpic/320/x.jpg", "auth_date": strconv.FormatInt(at.Unix(), 10),
	}
	hash := SignWidget(f, token)
	// Send id and auth_date as JSON numbers, the way the widget callback delivers them.
	raw := `{"id":` + f["id"] + `,"first_name":"Ерлан","username":"erlan","photo_url":"` + f["photo_url"] +
		`","auth_date":` + f["auth_date"] + `,"hash":"` + hash + `"}`
	return decodeNumbers(t, raw)
}

func decodeNumbers(t *testing.T, raw string) map[string]any {
	t.Helper()
	out := map[string]any{}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestVerifyWidget(t *testing.T) {
	now := time.Now()
	u, err := VerifyWidget(widgetBody(t, 77, now.Add(-time.Minute), testBot), testBot, now)
	if err != nil || u.ID != 77 || u.FirstName != "Ерлан" {
		t.Fatalf("valid widget: %+v %v", u, err)
	}
	if _, err := VerifyWidget(widgetBody(t, 77, now, "1:wrong"), testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("wrong token: %v", err)
	}
	b := widgetBody(t, 77, now, testBot)
	b["username"] = "someone_else"
	if _, err := VerifyWidget(b, testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("tampered: %v", err)
	}
	if _, err := VerifyWidget(widgetBody(t, 77, now.Add(-24*time.Hour-time.Minute), testBot), testBot, now); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
	// initData-style key must not validate widget data.
	f := map[string]string{"id": "77", "auth_date": strconv.FormatInt(now.Unix(), 10)}
	webKey := hmacSHA256([]byte("WebAppData"), []byte(testBot))
	b2 := map[string]any{"id": "77", "auth_date": f["auth_date"], "hash": hex.EncodeToString(hmacSHA256(webKey, []byte(DataCheckString(f))))}
	if _, err := VerifyWidget(b2, testBot, now); !errors.Is(err, ErrBadHash) {
		t.Fatalf("webapp key on widget: %v", err)
	}
	if _, err := VerifyWidget(map[string]any{"id": true, "hash": "00"}, testBot, now); !errors.Is(err, ErrBadInput) {
		t.Fatalf("non-scalar: %v", err)
	}
}

func TestSessionToken(t *testing.T) {
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	now := time.Now()
	tok, err := Sign(Claims{UserID: 5, Version: 2, Exp: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(tok, now)
	if err != nil || c.UserID != 5 || c.Version != 2 {
		t.Fatalf("roundtrip: %+v %v", c, err)
	}
	p, sig, _ := strings.Cut(tok, ".")
	forged, _ := Sign(Claims{UserID: 6, Version: 2, Exp: now.Add(time.Hour).Unix()})
	fp, _, _ := strings.Cut(forged, ".")
	for name, bad := range map[string]string{
		"swapped payload": fp + "." + sig,
		"flipped sig":     p + "." + map[bool]string{true: "B", false: "A"}[sig[0] == 'A'] + sig[1:],
		"no dot":          p,
		"empty":           "",
	} {
		if _, err := Parse(bad, now); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := Parse(tok, now.Add(2*time.Hour)); err == nil {
		t.Error("expired token accepted")
	}
	t.Setenv("SESSION_SECRET", strings.Repeat("x", 40))
	if _, err := Parse(tok, now); err == nil {
		t.Error("token signed with another secret accepted")
	}
	t.Setenv("SESSION_SECRET", "short")
	if _, err := Sign(Claims{UserID: 1}); !errors.Is(err, ErrNoSecret) {
		t.Errorf("short secret: %v", err)
	}
}
