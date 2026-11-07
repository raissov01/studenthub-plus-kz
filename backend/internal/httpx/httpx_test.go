package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Postgres refuses NUL in text and jsonb: a "\u0000" that reached SQL was a 500 (SQLSTATE 08P01/22P05).
func TestDecodeRefusesNUL(t *testing.T) {
	var v struct{ Code string }
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"code":"a\u0000b"}`))
	if err := Decode(r, &v, 1<<10); err == nil {
		t.Fatalf("NUL accepted: %q", v.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"code":"Қазақ é"} `))
	if err := Decode(r, &v, 1<<10); err != nil || v.Code != "Қазақ é" {
		t.Fatalf("valid body: %v %q", err, v.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"code":"`+strings.Repeat("a", 2000)+`"}`))
	if err := Decode(r, &v, 1<<10); err == nil {
		t.Fatal("body over the limit accepted")
	}
}

func TestHasNUL(t *testing.T) {
	for p, want := range map[string]bool{"/api/quiz/s/%00": true, "/api/market/items?q=%00": true, "/api/x?q=a%2500": false, "/api/x?q=%D0%9A": false} {
		if got := HasNUL(httptest.NewRequest(http.MethodGet, p, nil)); got != want {
			t.Errorf("%s: %v", p, got)
		}
	}
}
