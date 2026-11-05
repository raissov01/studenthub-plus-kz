// Package shopkit is the small layer the campus shops share: the café (internal/cafe, docs/cafe.md) and the print
// shop (internal/printshop, docs/print.md). It holds what does not depend on what a shop sells: opening hours and
// pickup slots, the pickup code, money formatting, staff short names, the append-only order audit trail, the
// Telegram button markup, the kaspi-pos webhook signature, the shop's Kaspi cashier session store and its admin
// login screens (kaspi.go).
//
// Money never passes through Student Hub: a Kaspi invoice is issued in the shop's own name with the shop's own
// cashier session (internal/cafe/payments); pay-at-the-counter needs no money flow at all.
package shopkit

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
)

// Almaty is the wall clock of every campus shop.
var Almaty = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Almaty"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Almaty", 5*3600)
}()

// DayStart is Almaty midnight of now's day.
func DayStart(now time.Time) time.Time {
	l := now.In(Almaty)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, Almaty)
}

// ---------- opening hours and pickup slots ----------

// Clock parses "HH:MM" into minutes after midnight.
func Clock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// Hours is a shop's week: open days (ISO, 1 = Monday), opening and closing time, the slot step and the lead time.
type Hours struct {
	Active      bool
	OpenDays    []int
	OpenTime    string
	CloseTime   string
	SlotMinutes int
	LeadMinutes int
}

// OpenToday reports whether now's Almaty weekday is an open day.
func (h Hours) OpenToday(now time.Time) bool {
	wd := int(now.In(Almaty).Weekday())
	if wd == 0 {
		wd = 7
	}
	for _, d := range h.OpenDays {
		if d == wd {
			return true
		}
	}
	return false
}

// OpenAt reports whether the shop is open at t (an open day, between opening and closing).
func (h Hours) OpenAt(t time.Time) bool {
	from, ok1 := Clock(h.OpenTime)
	to, ok2 := Clock(h.CloseTime)
	if !h.Active || !ok1 || !ok2 || !h.OpenToday(t) {
		return false
	}
	l := t.In(Almaty)
	m := l.Hour()*60 + l.Minute()
	return m >= from && m < to
}

// Slots are today's pickup times (Asia/Almaty) still to choose: every SlotMinutes from opening to the last slot
// that starts before closing, no earlier than now + LeadMinutes. Empty on a closed day or an inactive shop.
func (h Hours) Slots(now time.Time) []time.Time {
	if !h.Active || !h.OpenToday(now) {
		return nil
	}
	from, ok1 := Clock(h.OpenTime)
	to, ok2 := Clock(h.CloseTime)
	step := h.SlotMinutes
	if !ok1 || !ok2 || step <= 0 {
		return nil
	}
	day := DayStart(now)
	earliest := now.Add(time.Duration(h.LeadMinutes) * time.Minute)
	var out []time.Time
	for m := from; m < to; m += step {
		t := day.Add(time.Duration(m) * time.Minute)
		if !t.Before(earliest) {
			out = append(out, t)
		}
	}
	return out
}

// ValidSlot reports whether at is one of today's slots.
func (h Hours) ValidSlot(now, at time.Time) bool {
	for _, t := range h.Slots(now) {
		if t.Equal(at) {
			return true
		}
	}
	return false
}

// ---------- small helpers ----------

// PickupCode is a random three-digit code (100–999), not checked against anything: use UniqueCode when placing.
func PickupCode() string { return randomCode(3) }

func randomCode(digits int) string {
	lo := int64(1)
	for i := 1; i < digits; i++ {
		lo *= 10
	}
	n, err := rand.Int(rand.Reader, big.NewInt(9*lo))
	if err != nil {
		return strconv.FormatInt(time.Now().UnixNano()%(9*lo)+lo, 10)
	}
	return strconv.FormatInt(n.Int64()+lo, 10)
}

// ErrNoCode: every code of that length is taken (the caller refuses the order; it never reuses a code).
var ErrNoCode = errors.New("shopkit: every pickup code is in use")

// FreeCode picks a random code of digits digits (no leading zero) that is not in used: a few random draws, then a
// walk from a random start, so it always finds the last free code. ok = false when every code is taken.
func FreeCode(used map[string]bool, digits int) (string, bool) {
	if digits < 1 || digits > 6 {
		return "", false
	}
	for i := 0; i < 16; i++ {
		if c := randomCode(digits); !used[c] {
			return c, true
		}
	}
	lo := 1
	for i := 1; i < digits; i++ {
		lo *= 10
	}
	span := 9 * lo
	start, _ := strconv.Atoi(randomCode(digits))
	for i := 0; i < span; i++ {
		c := strconv.Itoa(lo + (start-lo+i)%span)
		if !used[c] {
			return c, true
		}
	}
	return "", false
}

// UniqueCode is the code for a new order of one shop, inside the transaction that inserts it. The caller holds a
// lock that orders the shop's inserts (its shop row FOR UPDATE), and a partial unique index on (shop, code) among
// the open orders is the database's own guard. A code of an open order never repeats; today's closed orders'
// codes are avoided too while any other is free (a student may still read today's message). table and scope are
// constants of the calling package (cafe_orders/cafe_id, print_jobs/shop_id), never request input.
func UniqueCode(ctx context.Context, tx pgx.Tx, digits int, table, scope string, shopID int64, open []string, now time.Time) (string, error) {
	base := `select pickup_code from ` + table + ` where ` + scope + ` = $1 and `
	for _, q := range []string{
		base + `(status = any($2) or created_at >= $3)`,
		base + `status = any($2) and $3::timestamptz is not null`,
	} {
		rows, err := tx.Query(ctx, q, shopID, open, DayStart(now))
		if err != nil {
			return "", err
		}
		codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return "", err
		}
		used := make(map[string]bool, len(codes))
		for _, c := range codes {
			used[c] = true
		}
		if c, ok := FreeCode(used, digits); ok {
			return c, nil
		}
	}
	return "", ErrNoCode
}

// Tenge formats tiyn as «1 250 ₸».
func Tenge(tiyn int64) string {
	neg := tiyn < 0
	if neg {
		tiyn = -tiyn
	}
	s := strconv.FormatInt(tiyn/100, 10)
	var b strings.Builder
	if neg {
		b.WriteString("−")
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String() + " ₸"
}

// ShortName: «Аружан С.», enough to call out at the counter; the staff never see the full name or a phone.
func ShortName(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	if utf8.RuneCountInString(out) > 20 {
		out = string([]rune(out)[:20])
	}
	if len(parts) > 1 {
		r, _ := utf8.DecodeRuneInString(parts[1])
		out += " " + string(r) + "."
	}
	return out
}

// TextOK: between min and max runes and no surrounding spaces.
func TextOK(s string, min, max int) bool {
	n := utf8.RuneCountInString(s)
	return n >= min && n <= max && strings.TrimSpace(s) == s
}

// Pick is kk or ru by the reader's language.
func Pick(lang, kk, ru string) string {
	if lang == "ru" {
		return ru
	}
	return kk
}

// Trim cuts s to n bytes (for log lines and stored error reasons).
func Trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// VerifySignature checks the sidecar's "sha256=<hex HMAC of the raw body>" in constant time.
func VerifySignature(raw []byte, header, secret string) bool {
	if secret == "" || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	want := hex.EncodeToString(m.Sum(nil))
	return hmac.Equal([]byte(strings.TrimPrefix(header, "sha256=")), []byte(want))
}

// ---------- audit trail ----------

// Event appends one row to an append-only order audit table (cafe_order_events, print_job_events) inside the
// caller's transaction. table and fk are constants of the calling package, never request input.
func Event(ctx context.Context, tx pgx.Tx, table, fk string, orderID int64, actor *int64, kind, name string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into `+table+` (`+fk+`, actor_id, actor_kind, kind, detail) values ($1, $2, $3, $4, $5)`,
		orderID, actor, kind, name, string(raw))
	return err
}

// ---------- Telegram buttons ----------

type Button struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

// Markup is an inline keyboard; empty rows are dropped.
func Markup(rows ...[]Button) json.RawMessage {
	var kb [][]Button
	for _, r := range rows {
		if len(r) > 0 {
			kb = append(kb, r)
		}
	}
	raw, _ := json.Marshal(map[string]any{"inline_keyboard": kb})
	return raw
}

// LinkButton opens a page of the site. Telegram accepts only https URL buttons, so it is empty elsewhere.
func LinkButton(text, path string) []Button {
	site := config.SiteURL()
	if !strings.HasPrefix(site, "https://") {
		return nil
	}
	return []Button{{Text: text, URL: site + path}}
}

// AuditStaff writes the audit_log row of a shop staff change (staff_grant / staff_revoke): who changed whose
// access to which shop ("cafe" / "print") with which role, and the optional reason.
func AuditStaff(ctx context.Context, tx pgx.Tx, actorID int64, action string, targetID int64, shop string, shopID int64, role, reason string) error {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 500 {
		reason = string([]rune(reason)[:500])
	}
	_, err := tx.Exec(ctx, `insert into audit_log (actor_id, action, target_id, reason, meta)
		values ($1, $2, $3, nullif($4, ''), jsonb_build_object('shop', $5::text, 'shopId', $6::bigint, 'role', $7::text))`,
		actorID, action, targetID, reason, shop, shopID, role)
	return err
}
