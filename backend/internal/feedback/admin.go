package feedback

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/univoice"
)

// DefaultRecipients is the suggested list an admin can add for a university in one click. They
// stay inactive for students until at least one staff member is assigned.
var DefaultRecipients = []Recipient{
	{Key: "dean", NameKK: "Деканат", NameRU: "Деканат", SLAHours: 72},
	{Key: "rector", NameKK: "Ректорат", NameRU: "Ректорат", SLAHours: 120},
	{Key: "dorm", NameKK: "Жатақхана", NameRU: "Общежитие", SLAHours: 48},
	{Key: "canteen", NameKK: "Асхана", NameRU: "Столовая", SLAHours: 48},
	{Key: "it", NameKK: "IT қызметі", NameRU: "IT-служба", SLAHours: 48},
	{Key: "student_affairs", NameKK: "Студенттер ісі", NameRU: "Студенческие дела", SLAHours: 72},
}

var reKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,29}$`)

func adminRecipients(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	univ := r.URL.Query().Get("university")
	out := []Recipient{}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, university, key, name_kk, name_ru, sla_hours, active from feedback_recipients
			where $1 = '' or university = $1 order by university, id`, univ)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Recipient, error) {
			var x Recipient
			return x, row.Scan(&x.ID, &x.University, &x.Key, &x.NameKK, &x.NameRU, &x.SLAHours, &x.Active)
		}); err != nil {
			return err
		}
		for i := range out {
			rows, err := tx.Query(r.Context(), `select u.id, u.name, u.role::text from feedback_recipient_staff s join users u on u.id = s.user_id
				where s.recipient_id = $1 order by u.id`, out[i].ID)
			if err != nil {
				return err
			}
			if out[i].Staff, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (StaffMember, error) {
				var m StaffMember
				return m, row.Scan(&m.UserID, &m.Name, &m.Role)
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		univoice.Internal(w, r, "admin recipients", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recipients": out, "defaults": DefaultRecipients})
}

func validRecipient(x *Recipient) bool {
	var ok1, ok2 bool
	x.NameKK, ok1 = univoice.Clean(x.NameKK, 2, 80, false)
	x.NameRU, ok2 = univoice.Clean(x.NameRU, 2, 80, false)
	return ok1 && ok2 && reKey.MatchString(x.Key) && x.SLAHours >= 1 && x.SLAHours <= 720
}

func adminCreate(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	var in struct {
		Recipient
		Defaults bool `json:"defaults"`
	}
	if httpx.Decode(r, &in, 8<<10) != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	univ, ok := univoice.Clean(in.University, 1, 160, false)
	list := []Recipient{in.Recipient}
	if in.Defaults {
		list = DefaultRecipients
	} else if list[0].SLAHours == 0 {
		list[0].SLAHours = 72
	}
	for i := range list {
		if !ok || !validRecipient(&list[i]) {
			univoice.Bad(w, r, "invalid", httpx.MsgBad)
			return
		}
	}
	n := 0
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		for _, x := range list {
			tag, err := tx.Exec(r.Context(), `insert into feedback_recipients (university, key, name_kk, name_ru, sla_hours) values ($1, $2, $3, $4, $5)
				on conflict (university, key) do nothing`, univ, x.Key, x.NameKK, x.NameRU, x.SLAHours)
			if err != nil {
				return err
			}
			n += int(tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		univoice.Internal(w, r, "recipient create", err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"created": n})
}

func adminPatch(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := univoice.PathID(r, "id")
	var in struct {
		NameKK   *string `json:"nameKk"`
		NameRU   *string `json:"nameRu"`
		SLAHours *int    `json:"slaHours"`
		Active   *bool   `json:"active"`
	}
	if !ok || httpx.Decode(r, &in, 4<<10) != nil {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	var cur Recipient
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `select key, name_kk, name_ru, sla_hours, active from feedback_recipients where id = $1 for update`, id).
			Scan(&cur.Key, &cur.NameKK, &cur.NameRU, &cur.SLAHours, &cur.Active); err != nil {
			return err
		}
		if in.NameKK != nil {
			cur.NameKK = *in.NameKK
		}
		if in.NameRU != nil {
			cur.NameRU = *in.NameRU
		}
		if in.SLAHours != nil {
			cur.SLAHours = *in.SLAHours
		}
		if in.Active != nil {
			cur.Active = *in.Active
		}
		if !validRecipient(&cur) {
			return errInvalid
		}
		_, err := tx.Exec(r.Context(), `update feedback_recipients set name_kk = $2, name_ru = $3, sla_hours = $4, active = $5 where id = $1`,
			id, cur.NameKK, cur.NameRU, cur.SLAHours, cur.Active)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
	case errors.Is(err, errInvalid):
		univoice.Bad(w, r, "invalid", httpx.MsgBad)
	case err != nil:
		univoice.Internal(w, r, "recipient patch", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

var errInvalid = errors.New("feedback: invalid")

var MsgStaffUniv = httpx.Msg{
	KK: "Тек осы университетке бекітілген қызметкерді не әкімшіні қосуға болады.",
	RU: "Можно назначить только сотрудника, привязанного к этому университету, или администратора.",
}

// adminStaff replaces the staff list: each user must be an admin or university_staff bound to the
// recipient's university.
func adminStaff(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	id, ok := univoice.PathID(r, "id")
	var in struct {
		UserIDs []int64 `json:"userIds"`
	}
	if !ok || httpx.Decode(r, &in, 4<<10) != nil || len(in.UserIDs) > 20 {
		univoice.Bad(w, r, "bad_request", httpx.MsgBad)
		return
	}
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		var univ string
		if err := tx.QueryRow(r.Context(), `select university from feedback_recipients where id = $1 for update`, id).Scan(&univ); err != nil {
			return err
		}
		for _, uid := range in.UserIDs {
			var fine bool
			if err := tx.QueryRow(r.Context(), `select exists (select 1 from users u where u.id = $1 and u.deleted_at is null and
				(u.role = 'admin' or (u.role = 'university_staff' and exists (select 1 from university_staff s where s.user_id = u.id and s.university = $2))))`,
				uid, univ).Scan(&fine); err != nil {
				return err
			}
			if !fine {
				return errInvalid
			}
		}
		if _, err := tx.Exec(r.Context(), `delete from feedback_recipient_staff where recipient_id = $1`, id); err != nil {
			return err
		}
		for _, uid := range in.UserIDs {
			if _, err := tx.Exec(r.Context(), `insert into feedback_recipient_staff (recipient_id, user_id) values ($1, $2) on conflict do nothing`, id, uid); err != nil {
				return err
			}
		}
		return nil
	})
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
	case errors.Is(err, errInvalid):
		univoice.Bad(w, r, "staff_university", MsgStaffUniv)
	case errors.As(err, &pg):
		univoice.Bad(w, r, "invalid", httpx.MsgBad)
	case err != nil:
		univoice.Internal(w, r, "recipient staff", err)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
