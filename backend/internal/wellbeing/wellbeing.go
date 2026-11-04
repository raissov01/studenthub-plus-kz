// Package wellbeing serves the «Әл-ауқат / Благополучие» psychologist contacts (FEATURE_WELLBEING,
// docs/UNIVOICE.md). The section itself is informational and lives in the frontend (national help
// lines with sources, self-help cards, a device-only mood check-in); the server only stores the
// university's own support contacts, entered by an admin or that university's staff. Until someone
// fills them in the list is empty — the app never shows invented contacts. No mood data, no
// analytics: nothing about a student's wellbeing ever reaches the server.
package wellbeing

import (
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// Feature is the switch: FEATURE_WELLBEING.
const Feature = "wellbeing"

// MaxContacts per university.
const MaxContacts = 12

type Contact struct {
	University string    `json:"university"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	Phone      *string   `json:"phone"`
	Email      *string   `json:"email"`
	Place      string    `json:"place"`
	Hours      string    `json:"hours"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

var (
	rePhone = regexp.MustCompile(`^\+?[0-9 ()-]{3,24}$`)
	reEmail = regexp.MustCompile(`^[^@\s]{1,64}@[^@\s]{1,100}\.[^@\s]{2,20}$`)
)

func Routes(mux httpx.Router) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	h("GET /api/wellbeing/contacts", list)
	h("PUT /api/staff/wellbeing/contacts", auth.Write(auth.Need(replace)))
}

// list: public; ?university= narrows it (the frontend passes the student's university).
func list(w http.ResponseWriter, r *http.Request) {
	univ := r.URL.Query().Get("university")
	if len(univ) > 160 {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	out := []Contact{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select university, name, role, phone, email, place, hours, updated_at from wellbeing_contacts
			where $1 = '' or university = $1 order by university, sort, id limit 200`, univ)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Contact, error) {
			var c Contact
			return c, row.Scan(&c.University, &c.Name, &c.Role, &c.Phone, &c.Email, &c.Place, &c.Hours, &c.UpdatedAt)
		})
		return err
	})
	if err != nil {
		univoice.Internal(w, r, "wellbeing list", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.JSON(w, http.StatusOK, map[string]any{"contacts": out})
}

// replace: PUT {university, contacts:[...]} replaces that university's list. Admin: any
// university; university_staff: only the one they are bound to.
func replace(w http.ResponseWriter, r *http.Request, u *auth.User) {
	sc, ok := univoice.StaffScope(w, r, u)
	if !ok {
		return
	}
	var in struct {
		University string    `json:"university"`
		Contacts   []Contact `json:"contacts"`
	}
	if httpx.Decode(r, &in, 32<<10) != nil || len(in.Contacts) > MaxContacts {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	univ := sc.University
	if sc.Admin {
		var okU bool
		if univ, okU = univoice.Clean(in.University, 1, 160, false); !okU {
			univoice.Bad(w, r, "invalid", httpx.MsgBad)
			return
		}
	}
	for i := range in.Contacts {
		c := &in.Contacts[i]
		var a, b, d, e bool
		c.Name, a = univoice.Clean(c.Name, 2, 120, false)
		c.Role, b = univoice.Clean(c.Role, 0, 120, false)
		c.Place, d = univoice.Clean(c.Place, 0, 160, false)
		c.Hours, e = univoice.Clean(c.Hours, 0, 160, false)
		if c.Phone != nil && *c.Phone == "" {
			c.Phone = nil
		}
		if c.Email != nil && *c.Email == "" {
			c.Email = nil
		}
		if !a || !b || !d || !e || (c.Phone != nil && !rePhone.MatchString(*c.Phone)) || (c.Email != nil && !reEmail.MatchString(*c.Email)) ||
			(c.Phone == nil && c.Email == nil && c.Place == "") {
			httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalid", "index": i,
				"message": map[bool]string{true: "Проверьте контакт", false: "Байланысты тексеріңіз"}[httpx.Lang(r) == "ru"]}})
			return
		}
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `delete from wellbeing_contacts where university = $1`, univ); err != nil {
			return err
		}
		for i, c := range in.Contacts {
			if _, err := tx.Exec(r.Context(), `insert into wellbeing_contacts (university, name, role, phone, email, place, hours, sort, updated_by)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, univ, c.Name, c.Role, c.Phone, c.Email, c.Place, c.Hours, i, u.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		univoice.Internal(w, r, "wellbeing replace", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "university": univ, "count": len(in.Contacts)})
}
