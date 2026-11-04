package feeds

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// UserAgent identifies the collector to every site it reads.
const UserAgent = "StudentHubBot/1.0 (+https://studenthub.studywithraissov.com; opportunity feed)"

// Web is the polite HTTP reader shared by every web reader: one User-Agent, a pause between
// requests to the same host, robots.txt honoured (Disallow for * or StudentHubBot), a size cap,
// no cookies, no login.
type Web struct {
	Client   *http.Client
	MinGap   time.Duration // between requests to one host
	MaxBytes int64
	// IgnoreRobots is for tests against a local server only.
	IgnoreRobots bool

	mu     sync.Mutex
	last   map[string]time.Time
	robots map[string][]string // host -> disallowed path prefixes
}

// NewWeb returns a reader with production defaults.
func NewWeb() *Web {
	return &Web{Client: &http.Client{Timeout: 25 * time.Second}, MinGap: 2 * time.Second, MaxBytes: 4 << 20}
}

// ErrRobots: the site's robots.txt disallows the page.
var ErrRobots = errors.New("disallowed by robots.txt")

// Get fetches url and returns the body (status 200 only).
func (w *Web) Get(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("bad url")
	}
	if !w.IgnoreRobots {
		if dis := w.disallowed(ctx, u); dis {
			return nil, ErrRobots
		}
	}
	w.wait(ctx, u.Host)
	return w.get(ctx, raw)
}

func (w *Web) get(ctx context.Context, raw string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "ru,kk;q=0.9,en;q=0.8")
	c := w.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	limit := w.MaxBytes
	if limit <= 0 {
		limit = 4 << 20
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("page larger than %d bytes", limit)
	}
	return b, nil
}

func (w *Web) wait(ctx context.Context, host string) {
	w.mu.Lock()
	if w.last == nil {
		w.last = map[string]time.Time{}
	}
	next := w.last[host].Add(w.MinGap)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	w.last[host] = next
	w.mu.Unlock()
	sleepCtx(ctx, time.Until(next))
}

func (w *Web) disallowed(ctx context.Context, u *url.URL) bool {
	w.mu.Lock()
	if w.robots == nil {
		w.robots = map[string][]string{}
	}
	rules, ok := w.robots[u.Host]
	w.mu.Unlock()
	if !ok {
		w.wait(ctx, u.Host)
		body, err := w.get(ctx, u.Scheme+"://"+u.Host+"/robots.txt")
		if err == nil {
			rules = ParseRobots(string(body), "StudentHubBot")
		} // no robots.txt (404 or error): nothing disallowed
		w.mu.Lock()
		w.robots[u.Host] = rules
		w.mu.Unlock()
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	for _, p := range rules {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// ParseRobots returns the Disallow prefixes that apply to agent: its own group when the file
// has one, else the "*" group. Allow lines are not interpreted (stricter); a wildcard cuts the
// prefix there, and patterns that start with one ("/*.pdf") are skipped.
func ParseRobots(body, agent string) []string {
	agent = strings.ToLower(agent)
	groups := map[string][]string{}
	var current []string
	inAgents := false
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if !inAgents {
				current = nil
			}
			current = append(current, strings.ToLower(v))
			inAgents = true
			for _, a := range current {
				if _, ok := groups[a]; !ok {
					groups[a] = []string{}
				}
			}
		case "disallow":
			inAgents = false
			if v == "" {
				continue
			}
			if i := strings.IndexAny(v, "*$"); i >= 0 {
				if i <= 1 { // "/*.pdf", "/*?sort=": file-type / parameter patterns, not page prefixes
					continue
				}
				v = v[:i]
			}
			for _, a := range current {
				groups[a] = append(groups[a], v)
			}
		default:
			inAgents = false
		}
	}
	if g, ok := groups[agent]; ok {
		return g
	}
	for a, g := range groups {
		if a != "*" && strings.Contains(agent, a) {
			return g
		}
	}
	return groups["*"]
}
