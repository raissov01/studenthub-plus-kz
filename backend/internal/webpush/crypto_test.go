package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decodeB64(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291 Appendix A: the complete example, byte for byte.
func TestRFC8291Example(t *testing.T) {
	plaintext := []byte("When I grow up, I want to be a watermelon")
	asKey, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if got := b64.EncodeToString(asKey.PublicKey().Bytes()); got != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Fatalf("as_public %s", got)
	}
	k, err := ParseKeys("BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4", "BTBZMqHH6r4Tts7J_aSIgg")
	if err != nil {
		t.Fatal(err)
	}
	out, err := encrypt(plaintext, k, asKey, mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if got := b64.EncodeToString(out); got != want {
		t.Fatalf("message\n got %s\nwant %s", got, want)
	}
	// and the user agent side of the same example decrypts it
	ua, _ := ecdh.P256().NewPrivateKey(mustB64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"))
	if got := decryptForTest(t, out, ua, k.Auth); string(got) != string(plaintext) {
		t.Fatalf("decrypt %q", got)
	}
}

// decryptForTest is the browser's half of RFC 8291 (used to check our messages end to end).
func decryptForTest(t *testing.T, msg []byte, ua *ecdh.PrivateKey, auth []byte) []byte {
	t.Helper()
	if len(msg) < 86 || msg[20] != 65 || binary.BigEndian.Uint32(msg[16:20]) != recordSize {
		t.Fatalf("bad header %x", msg[:min(len(msg), 21)])
	}
	salt, asPubRaw, ct := msg[:16], msg[21:86], msg[86:]
	asPub, err := ecdh.P256().NewPublicKey(asPubRaw)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ua.ECDH(asPub)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPubRaw...)
	ikm, _ := hkdf.Key(sha256.New, secret, auth, string(info), 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatal("decrypt:", err)
	}
	if plain[len(plain)-1] != 2 {
		t.Fatal("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

type browserKeys struct {
	priv *ecdh.PrivateKey
	auth []byte
	keys Keys
}

func newBrowser(t *testing.T) browserKeys {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	k, err := ParseKeys(b64.EncodeToString(priv.PublicKey().Bytes()), b64.EncodeToString(auth))
	if err != nil {
		t.Fatal(err)
	}
	return browserKeys{priv, auth, k}
}

func TestEncryptRoundTripAndLimits(t *testing.T) {
	b := newBrowser(t)
	msg := []byte(`{"title":"Student Hub","body":"Сәлем 👋"}`)
	out, err := Encrypt(msg, b.keys)
	if err != nil {
		t.Fatal(err)
	}
	if got := decryptForTest(t, out, b.priv, b.auth); !bytes.Equal(got, msg) {
		t.Fatalf("round trip %q", got)
	}
	again, _ := Encrypt(msg, b.keys)
	if bytes.Equal(out, again) {
		t.Fatal("salt and key must be fresh per message")
	}
	if full, err := Encrypt(bytes.Repeat([]byte("x"), MaxPayload), b.keys); err != nil || len(full) != 4096 {
		t.Fatalf("max payload: %d %v", len(full), err)
	}
	if _, err := Encrypt(bytes.Repeat([]byte("x"), MaxPayload+1), b.keys); err == nil {
		t.Fatal("oversized payload accepted")
	}
}

func TestParseKeysAndEndpoint(t *testing.T) {
	b := newBrowser(t)
	pub := b64.EncodeToString(b.keys.P256dh)
	auth := b64.EncodeToString(b.keys.Auth)
	if _, err := ParseKeys(pub+"=", auth+"=="); err != nil {
		t.Fatal("padded keys:", err)
	}
	notOnCurve := append([]byte{4}, bytes.Repeat([]byte{1}, 64)...)
	for _, bad := range [][2]string{{"", auth}, {pub, ""}, {pub[:40], auth}, {pub, auth[:10]}, {b64.EncodeToString(notOnCurve), auth}, {"%%%", auth}} {
		if _, err := ParseKeys(bad[0], bad[1]); err == nil {
			t.Errorf("accepted keys %q", bad)
		}
	}
	for _, ok := range []string{"https://fcm.googleapis.com/fcm/send/abc", "https://updates.push.services.mozilla.com/wpush/v2/x", "https://web.push.apple.com/QG"} {
		if CheckEndpoint(ok) != nil {
			t.Errorf("rejected %s", ok)
		}
	}
	for _, bad := range []string{"", "http://fcm.googleapis.com/x", "https://user:pw@push.example/x", "javascript:alert(1)", "/relative", "https://" + strings.Repeat("a", 1100)} {
		if CheckEndpoint(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func testVAPID(t *testing.T) *VAPID {
	t.Helper()
	pub, priv, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseVAPID(pub, priv, "mailto:test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// verifyVAPID checks an Authorization header like a push service: ES256 signature by k,
// audience, expiry within 24 h and the subject.
func verifyVAPID(t *testing.T, header, aud string) {
	t.Helper()
	rest, ok := strings.CutPrefix(header, "vapid t=")
	jwt, k, ok2 := strings.Cut(rest, ", k=")
	if !ok || !ok2 {
		t.Fatalf("header %q", header)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt %q", jwt)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), mustB64(t, k))
	if err != nil {
		t.Fatal(err)
	}
	sig := mustB64(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("bad VAPID signature")
	}
	var head struct{ Alg, Typ string }
	_ = json.Unmarshal(mustB64(t, parts[0]), &head)
	var claims struct {
		Aud string
		Exp int64
		Sub string
	}
	_ = json.Unmarshal(mustB64(t, parts[1]), &claims)
	if head.Alg != "ES256" || claims.Aud != aud || claims.Sub != "mailto:test@example.com" {
		t.Fatalf("jwt %+v %+v", head, claims)
	}
	if left := time.Until(time.Unix(claims.Exp, 0)); left <= 0 || left > 24*time.Hour {
		t.Fatalf("exp in %v", left)
	}
}

func TestVAPIDKeys(t *testing.T) {
	v := testVAPID(t)
	h, err := v.Authorization("https://push.example.net", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	verifyVAPID(t, h, "https://push.example.net")
	pub, priv, _ := GenerateVAPID()
	other, _, _ := GenerateVAPID()
	if _, err := ParseVAPID(other, priv, "mailto:x@y.z"); err == nil {
		t.Fatal("mismatched pair accepted")
	}
	if _, err := ParseVAPID(pub, priv, "x@y.z"); err == nil {
		t.Fatal("subject without mailto:/https: accepted")
	}
	if _, err := ParseVAPID(pub, "short", "mailto:x@y.z"); err == nil {
		t.Fatal("bad private key accepted")
	}
}

func TestDefaultNeedsFlagAndKeys(t *testing.T) {
	pub, priv, _ := GenerateVAPID()
	t.Setenv("VAPID_PUBLIC_KEY", pub)
	t.Setenv("VAPID_PRIVATE_KEY", priv)
	t.Setenv("VAPID_SUBJECT", "mailto:a@b.c")
	t.Setenv("FEATURE_WEBPUSH", "")
	if Enabled() || PublicConfig()["enabled"] != false || PublicConfig()["publicKey"] != nil {
		t.Fatal("on without FEATURE_WEBPUSH")
	}
	t.Setenv("FEATURE_WEBPUSH", "true")
	if !Enabled() || PublicConfig()["publicKey"] != pub {
		t.Fatalf("config %v", PublicConfig())
	}
	t.Setenv("VAPID_PRIVATE_KEY", "")
	if Enabled() {
		t.Fatal("on without a private key")
	}
	other, _, _ := GenerateVAPID()
	t.Setenv("VAPID_PRIVATE_KEY", priv)
	t.Setenv("VAPID_PUBLIC_KEY", other)
	if Enabled() {
		t.Fatal("on with a mismatched key pair")
	}
}

func TestMessagePayloadFits(t *testing.T) {
	p, err := Message{Title: "T", Body: strings.Repeat("Қ", 5000), URL: "/event/1"}.Payload()
	if err != nil || len(p) > MaxPayload {
		t.Fatalf("%d %v", len(p), err)
	}
	var m Message
	_ = json.Unmarshal(p, &m)
	if m.Icon != "/icons/icon-192.png" || !strings.HasSuffix(m.Body, "…") || m.URL != "/event/1" {
		t.Fatalf("%+v", m)
	}
}

// fakePushService is a push service: it checks the RFC 8030/8292 headers, decrypts the body
// with the browser's key and answers with the status configured for the endpoint path.
type fakePushService struct {
	*httptest.Server
	t        *testing.T
	browsers map[string]browserKeys
	status   map[string]int
	got      map[string][]string
}

func newFakePushService(t *testing.T) *fakePushService {
	f := &fakePushService{t: t, browsers: map[string]browserKeys{}, status: map[string]int{}, got: map[string][]string{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePushService) serve(w http.ResponseWriter, r *http.Request) {
	t := f.t
	b, ok := f.browsers[r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" || r.Header.Get("Urgency") == "" {
		t.Errorf("headers %v", r.Header)
	}
	verifyVAPID(t, r.Header.Get("Authorization"), f.URL)
	body, _ := io.ReadAll(r.Body)
	f.got[r.URL.Path] = append(f.got[r.URL.Path], string(decryptForTest(t, body, b.priv, b.auth)))
	code := f.status[r.URL.Path]
	if code == 0 {
		code = http.StatusCreated
	}
	w.WriteHeader(code)
}

func (f *fakePushService) subscribe(t *testing.T, path string, status int) Subscription {
	b := newBrowser(t)
	f.browsers[path] = b
	f.status[path] = status
	return Subscription{Endpoint: f.URL + path, Keys: b.keys}
}

func TestSendAgainstFakePushService(t *testing.T) {
	f := newFakePushService(t)
	s := &Sender{VAPID: testVAPID(t), Client: f.Client()}
	for _, c := range []struct {
		status int
		want   Outcome
	}{{201, Delivered}, {410, Gone}, {404, Gone}, {400, Rejected}, {413, Rejected}, {429, Retry}, {503, Retry}} {
		path := "/push/" + http.StatusText(c.status)
		sub := f.subscribe(t, path, c.status)
		out, err := s.Send(context.Background(), sub, []byte(`{"title":"x"}`), "")
		if out != c.want || (c.want == Delivered) != (err == nil) {
			t.Errorf("%d: %v %v", c.status, out, err)
		}
		if err != nil && strings.Contains(err.Error(), f.URL) {
			t.Errorf("error leaks the endpoint: %v", err)
		}
		if got := f.got[path]; len(got) != 1 || got[0] != `{"title":"x"}` {
			t.Errorf("%d: push service got %q", c.status, got)
		}
	}
	// a dead host is retryable and the error does not contain the capability URL
	dead := Subscription{Endpoint: "https://127.0.0.1:1/secret-endpoint", Keys: newBrowser(t).keys}
	if out, err := s.Send(context.Background(), dead, []byte("{}"), ""); out != Retry || err == nil || strings.Contains(err.Error(), "secret-endpoint") {
		t.Fatalf("dead host: %v %v", out, err)
	}
}
