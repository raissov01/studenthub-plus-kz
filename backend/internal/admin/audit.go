package admin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// The global audit log (/admin/audit): every audit_log row, newest first, with filters.

type auditFilter struct {
	Action        string
	Actor, Target int64
	From, To      *time.Time
	Before        int64
	Limit         int
}

var reAction = regexp.MustCompile(`^[a-z_]{0,40}$`)

func parseAudit(r *http.Request) (auditFilter, bool) {
	q := r.URL.Query()
	f := auditFilter{Action: q.Get("action")}
	var err1, err2, err3 error
	if v := q.Get("actor"); v != "" {
		f.Actor, err1 = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("target"); v != "" {
		f.Target, err2 = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("before"); v != "" {
		f.Before, err3 = strconv.ParseInt(v, 10, 64)
	}
	var okFrom, okTo bool
	f.From, okFrom = parseDate(q.Get("from"))
	f.To, okTo = parseDate(q.Get("to"))
	if f.To != nil {
		t := f.To.AddDate(0, 0, 1)
		f.To = &t
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
	return f, reAction.MatchString(f.Action) && err1 == nil && err2 == nil && err3 == nil && okFrom && okTo
}

const auditWhere = `($1 = '' or l.action = $1) and ($2 = 0 or l.actor_id = $2) and ($3 = 0 or l.target_id = $3)
	and ($4::timestamptz is null or l.created_at >= $4) and ($5::timestamptz is null or l.created_at < $5)`

func (f auditFilter) args() []any { return []any{f.Action, f.Actor, f.Target, f.From, f.To} }

type auditRow struct {
	ID        int64           `json:"id"`
	Action    string          `json:"action"`
	Reason    *string         `json:"reason"`
	Meta      json.RawMessage `json:"meta"`
	CreatedAt time.Time       `json:"createdAt"`
	Actor     *person         `json:"actor"`
	Target    *person         `json:"target"`
}

type person struct {
	ID       int64   `json:"id"`
	Name     *string `json:"name"`
	Username *string `json:"username"`
}

func scanPerson(id *int64, name, username *string) *person {
	if id == nil {
		return nil
	}
	return &person{ID: *id, Name: name, Username: username}
}

// auditLog: GET /api/admin/audit?action=&actor=&target=&from=&to=&before=&limit= → {entries, actions, next}.
func auditLog(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	f, ok := parseAudit(r)
	if !ok {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	entries := []auditRow{}
	type actionCount struct {
		Action string `json:"action"`
		Count  int    `json:"count"`
	}
	actions := []actionCount{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select l.id, l.action, l.reason, l.meta, l.created_at, a.id, a.name, a.tg_username, t.id, t.name, t.tg_username
			from audit_log l left join users a on a.id = l.actor_id left join users t on t.id = l.target_id
			where `+auditWhere+` and ($6 = 0 or l.id < $6) order by l.id desc limit $7`, append(f.args(), f.Before, f.Limit)...)
		if err != nil {
			return err
		}
		entries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (auditRow, error) {
			var e auditRow
			var aID, tID *int64
			var aName, aUser, tName, tUser *string
			err := row.Scan(&e.ID, &e.Action, &e.Reason, &e.Meta, &e.CreatedAt, &aID, &aName, &aUser, &tID, &tName, &tUser)
			e.Actor, e.Target = scanPerson(aID, aName, aUser), scanPerson(tID, tName, tUser)
			return e, err
		})
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `select action, count(*) from audit_log group by action order by count(*) desc, action limit 100`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (actionCount, error) {
			var a actionCount
			return a, row.Scan(&a.Action, &a.Count)
		})
		return err
	})
	if err != nil {
		fail(w, r, "audit", err)
		return
	}
	var next *int64
	if len(entries) == f.Limit {
		next = &entries[len(entries)-1].ID
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"entries": entries, "actions": actions, "next": next})
}
