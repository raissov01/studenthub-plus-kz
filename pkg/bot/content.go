package bot

// Opportunity data from the site: /api/content first, /data/opportunities.json as fallback.
// Loaded at most once per invocation (one webhook update or one cron run).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Text is a kk/ru pair; a plain JSON string fills both.
type Text struct{ KK, RU string }

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		t.KK, t.RU = s, s
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		t.KK, _ = m["kk"].(string)
		t.RU, _ = m["ru"].(string)
	}
	return nil
}

// pick returns the text in lang, falling back to the other language.
func (t Text) pick(lang string) string {
	if lang == "ru" && t.RU != "" {
		return t.RU
	}
	if lang == "kk" && t.KK != "" {
		return t.KK
	}
	if t.KK != "" {
		return t.KK
	}
	return t.RU
}

// Opp is the part of an opportunity the bot uses.
type Opp struct {
	ID       string
	Title    Text
	Deadline string // YYYY-MM-DD, or "" when the opportunity has none
	Docs     []Text
	City     string
	Spec     []string
	PostedAt string
}

func (o *Opp) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID       json.RawMessage `json:"id"`
		Title    Text            `json:"title"`
		Deadline any             `json:"deadline"`
		Docs     []Text          `json:"docs"`
		City     any             `json:"city"`
		Spec     any             `json:"spec"`
		PostedAt any             `json:"posted_at"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*o = Opp{Title: raw.Title, Docs: raw.Docs}
	var s string
	var n json.Number
	switch {
	case json.Unmarshal(raw.ID, &s) == nil:
		o.ID = s
	case json.Unmarshal(raw.ID, &n) == nil:
		o.ID = n.String()
	}
	o.Deadline, _ = raw.Deadline.(string)
	o.City, _ = raw.City.(string)
	o.PostedAt, _ = raw.PostedAt.(string)
	if arr, ok := raw.Spec.([]any); ok {
		o.Spec = []string{}
		for _, v := range arr {
			switch x := v.(type) {
			case string:
				o.Spec = append(o.Spec, x)
			case float64:
				o.Spec = append(o.Spec, strconv.FormatFloat(x, 'f', -1, 64))
			}
		}
	}
	return nil
}

type content struct {
	b    *Bot
	once sync.Once
	opps []Opp
	err  error
}

func (b *Bot) newContent() *content { return &content{b: b} }

func (c *content) load(ctx context.Context) ([]Opp, error) {
	c.once.Do(func() { c.opps, c.err = c.fetch(ctx) })
	return c.opps, c.err
}

func (c *content) find(ctx context.Context, id string) (*Opp, error) {
	list, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, nil
}

func (c *content) fetch(ctx context.Context) ([]Opp, error) {
	site := c.b.Cfg.ContentURL
	if site == "" {
		site = c.b.Cfg.SiteURL
	}
	if body, err := c.b.getJSON(ctx, site+"/api/content"); err == nil {
		var data struct {
			Opportunities json.RawMessage `json:"opportunities"`
		}
		if json.Unmarshal(body, &data) == nil {
			if list, ok := decodeOpps(data.Opportunities); ok {
				return list, nil
			}
		}
	} // not deployed yet or failing: use the static file
	body, err := c.b.getJSON(ctx, site+"/data/opportunities.json")
	if err != nil {
		return nil, err
	}
	if list, ok := decodeOpps(body); ok {
		return list, nil
	}
	var data struct {
		Opportunities json.RawMessage `json:"opportunities"`
	}
	if json.Unmarshal(body, &data) == nil {
		if list, ok := decodeOpps(data.Opportunities); ok {
			return list, nil
		}
	}
	return []Opp{}, nil
}

// decodeOpps reads a JSON array, skipping malformed entries; ok is false if it is not an array.
func decodeOpps(raw json.RawMessage) ([]Opp, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	out := make([]Opp, 0, len(items))
	for _, it := range items {
		var o Opp
		if json.Unmarshal(it, &o) == nil {
			out = append(out, o)
		}
	}
	return out, true
}

func (b *Bot) getJSON(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := b.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%s: HTTP %d", u, res.StatusCode)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(http.MaxBytesReader(nil, res.Body, 8<<20)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *Bot) oppURL(id string) string { return b.Cfg.SiteURL + "/opportunity/" + url.PathEscape(id) }
