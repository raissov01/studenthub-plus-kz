package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const apiKey = "k0123456789abcdef0123456789abcdef"

// sidecar is a fake kaspi-pos: it checks the internal key and the session headers like the real one.
func sidecar(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, body map[string]any)) *Kaspi {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Api-Key") != apiKey {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		handle(w, r, body)
	}))
	t.Cleanup(srv.Close)
	return &Kaspi{BaseURL: srv.URL, APIKey: apiKey, Timeout: 2 * time.Second}
}

var merchant = Merchant{CafeID: 1, Session: &Session{TokenSN: "TSN", VtokenSecret: "VT", ProfileID: "42"}}

func TestKaspiCreateInvoiceSendsTengeAndSession(t *testing.T) {
	k := sidecar(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path != "/api/qr/create" || r.Header.Get("X-Token-SN") != "TSN" || r.Header.Get("X-Vtoken-Secret") != "VT" || r.Header.Get("X-Profile-Id") != "42" {
			t.Errorf("request: %s %v", r.URL.Path, r.Header)
		}
		if body["amount"] != float64(1250) {
			t.Errorf("amount in tenge: %v", body["amount"])
		}
		_, _ = w.Write([]byte(`{"StatusCode":0,"Data":{"QrOperationId":123456789012,"QrToken":"https://pay.kaspi.kz/pay/abc","QrOriginalToken":"https://qr.kaspi.kz/abc","ExpireDate":"2026-11-05T12:03:00+05:00"}}`))
	})
	inv, err := k.CreateInvoice(context.Background(), merchant, 7, 125000)
	if err != nil {
		t.Fatal(err)
	}
	if inv.OperationID != "123456789012" || inv.PayURL != "https://pay.kaspi.kz/pay/abc" || inv.QR != "https://qr.kaspi.kz/abc" || inv.ExpiresAt == nil || inv.Amount != 125000 {
		t.Fatalf("%+v", inv)
	}
	if _, err := k.CreateInvoice(context.Background(), merchant, 7, 125050); !errors.Is(err, ErrInvalid) {
		t.Fatal("tiyn amount accepted:", err)
	}
	if _, err := k.CreateInvoice(context.Background(), Merchant{CafeID: 1}, 7, 1000); !errors.Is(err, ErrNoSession) {
		t.Fatal("no session:", err)
	}
}

func TestKaspiErrorMapping(t *testing.T) {
	var reply func(w http.ResponseWriter)
	k := sidecar(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { reply(w) })
	cases := []struct {
		name string
		fn   func(w http.ResponseWriter)
		want error
	}{
		{"session header rejected", func(w http.ResponseWriter) {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"Invalid or expired vtokenSecret"}`))
		}, ErrSessionExpired},
		{"evicted", func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"StatusCode":-101001}`)) }, ErrSessionExpired},
		{"validation", func(w http.ResponseWriter) { w.WriteHeader(400); _, _ = w.Write([]byte(`{"error":"invalid amount"}`)) }, ErrInvalid},
		{"kaspi error", func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"StatusCode":-1,"Message":"x"}`)) }, ErrUpstream},
		{"sidecar 500", func(w http.ResponseWriter) { w.WriteHeader(500) }, ErrUpstream},
	}
	for _, c := range cases {
		reply = c.fn
		if _, err := k.CreateInvoice(context.Background(), merchant, 1, 1000); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	wrongKey := *k
	wrongKey.APIKey = strings.Repeat("x", 32)
	if _, err := wrongKey.CreateInvoice(context.Background(), merchant, 1, 1000); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("wrong internal key must not look like an expired session: %v", err)
	}
	slow := sidecar(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) { time.Sleep(300 * time.Millisecond) })
	slow.Timeout = 50 * time.Millisecond
	if _, err := slow.CreateInvoice(context.Background(), merchant, 1, 1000); !errors.Is(err, ErrTimeout) {
		t.Errorf("timeout: %v", err)
	}
}

func TestKaspiStatusAndRefund(t *testing.T) {
	status := "Processed"
	refundCode := 0
	k := sidecar(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch r.URL.Path {
		case "/api/qr/status":
			if r.URL.Query().Get("qrOperationId") != "555" {
				t.Errorf("op: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"StatusCode": 0, "Data": map[string]any{"Status": status, "Amount": 1250}})
		case "/api/refund/create":
			if body["qrOperationId"] != "555" || body["returnAmount"] != float64(1250) {
				t.Errorf("refund body: %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"StatusCode": refundCode, "Message": "Нет прав"})
		}
	})
	for raw, want := range map[string]State{"Processed": StatePaid, "Wait": StatePending, "QrTokenCreated": StatePending, "Expired": StateExpired, "CancelledByUser": StateFailed, "Whatever": StateFailed} {
		status = raw
		st, err := k.Status(context.Background(), merchant, "555")
		if err != nil || st.State != want || st.Amount != 125000 {
			t.Errorf("%s: %+v %v", raw, st, err)
		}
	}
	if _, err := k.Status(context.Background(), merchant, "../x"); !errors.Is(err, ErrInvalid) {
		t.Error("bad op id accepted")
	}
	if err := k.Refund(context.Background(), merchant, "555", 125000); err != nil {
		t.Fatal(err)
	}
	refundCode = -7
	if err := k.Refund(context.Background(), merchant, "555", 125000); !errors.Is(err, ErrRefundNotAllowed) {
		t.Fatalf("refused refund: %v", err)
	}
}

func TestKaspiLogin(t *testing.T) {
	k := sidecar(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch r.URL.Path {
		case "/api/auth/init":
			_, _ = w.Write([]byte(`{"success":true,"processId":"P1"}`))
		case "/api/auth/send-phone":
			if body["phoneNumber"] != "7011234567" || body["processId"] != "P1" {
				t.Errorf("send-phone: %v", body)
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/api/auth/verify-otp":
			_, _ = w.Write([]byte(`{"success":true,"tokenSN":"T","vtokenSecret":"V","profileId":77,"phone":"7011234567","orgName":"Coffee LLP","organizationId":9}`))
		case "/api/session/org-context":
			if body["organizationId"] != float64(10) || r.Header.Get("X-Token-SN") != "T" {
				t.Errorf("org-context: %v", body)
			}
			_, _ = w.Write([]byte(`{"profileId":88,"orgName":"Second café"}`))
		}
	})
	pid, err := k.LoginStart(context.Background(), "+7 (701) 123-45-67")
	if err != nil || pid != "P1" {
		t.Fatal(pid, err)
	}
	if _, err := k.LoginStart(context.Background(), "12345"); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad phone accepted")
	}
	l, err := k.LoginVerify(context.Background(), "P1", "1234")
	if err != nil || l.Session.ProfileID != "77" || l.OrganizationID != 9 || l.OrgName != "Coffee LLP" {
		t.Fatalf("%+v %v", l, err)
	}
	p, name, err := k.OrgContext(context.Background(), l.Session, 10)
	if err != nil || p != "88" || name != "Second café" {
		t.Fatal(p, name, err)
	}
}

func TestSessionEncryption(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	s := Session{TokenSN: "T", VtokenSecret: "V", ProfileID: "1"}
	ct, err := Encrypt(s, key)
	if err != nil || strings.Contains(ct, "\"T\"") || !strings.HasPrefix(ct, "v1:") {
		t.Fatal(ct, err)
	}
	got, err := Decrypt(ct, key)
	if err != nil || *got != s {
		t.Fatal(got, err)
	}
	if _, err := Decrypt(ct, []byte(strings.Repeat("x", 32))); err == nil {
		t.Fatal("wrong key decrypted")
	}
	if _, err := Decrypt(ct[:len(ct)-4]+"AAAA", key); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	for in, want := range map[string]string{"+7 701 123 45 67": "7011234567", "87011234567": "7011234567", "77011234567": "7011234567", "7011234567": "7011234567", "12345": "", "+1 202 555 0100": ""} {
		if got := NormalizePhone(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
	if MaskPhone("87011234567") != "7011***67" || PhoneHash("+7 701 123 45 67", key) != PhoneHash("87011234567", key) {
		t.Error("mask/hash")
	}
	if n, err := Tenge(125000); n != 1250 || err != nil {
		t.Error("tenge")
	}
	if _, err := Tenge(-100); err == nil {
		t.Error("negative tenge")
	}
}
