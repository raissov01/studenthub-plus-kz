package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func extraLoader(t *testing.T) *Loader {
	return &Loader{Env: func(string) string { return "" }, DataDir: t.TempDir(), Warn: func(string) {}}
}

func TestExtraSources(t *testing.T) {
	now := time.Date(2026, 10, 28, 6, 0, 0, 0, time.UTC)
	plain, _ := json.Marshal(extraLoader(t).Load(context.Background(), now))
	if strings.Contains(string(plain), `"places"`) {
		t.Fatalf("places key without extras: %s", plain)
	}

	RegisterExtra("test-empty", func(ctx context.Context) (map[string][]Record, error) { return nil, nil })
	RegisterExtra("test-broken", func(ctx context.Context) (map[string][]Record, error) { return nil, errors.New("db down") })
	defer RegisterExtra("test-empty", nil)
	defer RegisterExtra("test-broken", nil)
	same, _ := json.Marshal(extraLoader(t).Load(context.Background(), now))
	if string(same) != string(plain) {
		t.Fatalf("output changed by empty / failing sources:\n%s\n%s", plain, same)
	}

	RegisterExtra("test-db", func(ctx context.Context) (map[string][]Record, error) {
		return map[string][]Record{
			"opportunities": {
				RawRecord(json.RawMessage(`{"id":"amb-1","source":"ambassador"}`), "2026-11-01"),
				RawRecord(json.RawMessage(`{"id":"amb-2","source":"ambassador"}`), "2026-10-01"), // expired
			},
			"places": {RawRecord(json.RawMessage(`{"id":"amb-3","source":"ambassador"}`), "")},
		}, nil
	})
	defer RegisterExtra("test-db", nil)
	c := extraLoader(t).Load(context.Background(), now)
	b, _ := json.Marshal(c)
	s := string(b)
	if !strings.Contains(s, `"amb-1"`) || strings.Contains(s, `"amb-2"`) || !strings.Contains(s, `"places":[{"id":"amb-3"`) {
		t.Fatalf("merged: %s", s)
	}
	if c.Source != "fallback" {
		t.Fatalf("source changed: %s", c.Source)
	}
}
