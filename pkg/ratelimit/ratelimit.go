// Package ratelimit counts requests per key in fixed windows stored in Postgres, so the
// limit holds across all serverless instances. Without a database it falls back to
// an in-memory counter per instance.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

// Allow reports whether one more hit for key fits into limit per window.
func Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration) {
	now := time.Now()
	start := now.Truncate(window)
	retry := start.Add(window).Sub(now)
	p, err := db.Pool(ctx)
	if err != nil {
		return memAllow(key, start, limit, window), retry
	}
	var hits int
	err = p.QueryRow(ctx, `insert into rate_limits (key, window_start, hits) values ($1, $2, 1)
		on conflict (key, window_start) do update set hits = rate_limits.hits + 1 returning hits`, key, start).Scan(&hits)
	if err != nil {
		return memAllow(key, start, limit, window), retry
	}
	return hits <= limit, retry
}

var (
	mu  sync.Mutex
	mem = map[string]int{}
)

func memAllow(key string, start time.Time, limit int, window time.Duration) bool {
	mu.Lock()
	defer mu.Unlock()
	if len(mem) > 10000 {
		mem = map[string]int{}
	}
	k := key + "|" + start.Format(time.RFC3339)
	mem[k]++
	return mem[k] <= limit
}
