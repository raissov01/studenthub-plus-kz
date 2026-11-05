// Package univoice holds what the «university voice» features share (internal/surveys,
// internal/feedback, internal/wellbeing): who counts as university staff for them, server-side
// keys derived from SESSION_SECRET, small HTTP helpers and the k-anonymity threshold.
// See docs/UNIVOICE.md.
package univoice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/config"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// K is the smallest group whose answers are ever shown: breakdowns, distributions and text
// answers with fewer than K responses stay hidden.
const K = 5

// Scope is what a staff caller may manage.
type Scope struct {
	Admin      bool
	University string // the bound university of a university_staff caller ("" for admin)
}

// Allows reports whether the scope covers univ.
func (s Scope) Allows(univ string) bool {
	return s.Admin || (s.University != "" && s.University == univ)
}

var MsgNotStaff = httpx.Msg{KK: "Бұл бөлім университет қызметкерлеріне арналған.", RU: "Этот раздел для сотрудников университета."}

// StaffScope: admin, or university_staff bound to a university by an admin (university_staff
// table). Anyone else gets 403.
func StaffScope(w http.ResponseWriter, r *http.Request, u *auth.User) (Scope, bool) {
	if u.Role == "admin" {
		return Scope{Admin: true}, true
	}
	if u.Role != "university_staff" {
		httpx.Error(w, r, http.StatusForbidden, "forbidden", httpx.MsgForbid)
		return Scope{}, false
	}
	univ, err := BoundUniversity(r.Context(), u.ID)
	if err != nil {
		Internal(w, r, "staff", err)
		return Scope{}, false
	}
	if univ == "" {
		httpx.Error(w, r, http.StatusForbidden, "not_staff", MsgNotStaff)
		return Scope{}, false
	}
	return Scope{University: univ}, true
}

// BoundUniversity returns the university an admin bound the user to ("" when none).
func BoundUniversity(ctx context.Context, userID int64) (string, error) {
	var univ string
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select s.university from university_staff s
			join users u on u.id = s.user_id and u.deleted_at is null and u.role = 'university_staff'
			where s.user_id = $1`, userID).Scan(&univ)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return univ, err
}

// Key derives a purpose-specific server key from SESSION_SECRET, so a database dump alone cannot
// recompute anything keyed with it.
func Key(label string) ([]byte, error) {
	s := config.Str("SESSION_SECRET", "")
	if len(s) < 32 {
		return nil, errors.New("univoice: SESSION_SECRET missing")
	}
	return MAC([]byte(s), []byte("studenthub/"+label+"/v1")), nil
}

// MAC is HMAC-SHA256.
func MAC(key []byte, parts ...[]byte) []byte {
	m := hmac.New(sha256.New, key)
	for i, p := range parts {
		if i > 0 {
			m.Write([]byte{0})
		}
		m.Write(p)
	}
	return m.Sum(nil)
}

// Internal logs err without request data and answers 500.
func Internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("univoice: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

// Bad answers 400 with code and message.
func Bad(w http.ResponseWriter, r *http.Request, code string, m httpx.Msg) {
	httpx.Error(w, r, http.StatusBadRequest, code, m)
}

// PathID parses a positive integer path value.
func PathID(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil && n > 0
}

// Clean trims s, refuses control characters (newlines only when multiline) and checks the
// length in characters.
func Clean(s string, min, max int, multiline bool) (string, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if !utf8.ValidString(s) {
		return "", false
	}
	for _, c := range s {
		if unicode.IsControl(c) && !(multiline && (c == '\n' || c == '\t')) {
			return "", false
		}
	}
	n := utf8.RuneCountInString(s)
	return s, n >= min && n <= max
}
