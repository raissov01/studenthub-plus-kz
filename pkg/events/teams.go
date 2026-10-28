package events

import (
	"context"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

// Small teams (05, stage 4): 4–6 people, faculties mixed.
//
// At check-in a person joins a team right away (assignTeam): their previous team-mates' team
// when they asked for "with my previous team", else the open team (< TeamMax) with the fewest
// people of their faculty, then the smallest. A new team opens once every team has at least
// TeamOpenAt people. Arrivals are unpredictable, so POST /api/events/{id}/teams re-splits
// everyone present into ceil(n/6) balanced teams (sizes differ by at most one; 4–6 for any
// n >= 8, and for n = 4..6 one team). Previous-team preferences are kept by assignTeam only.

const (
	TeamMax    = 6
	TeamOpenAt = 5
)

type teamInfo struct {
	no        int16
	size      int
	faculties map[string]int
	prevMates int
}

func assignTeam(ctx context.Context, tx pgx.Tx, eventID, userID int64) (int16, error) {
	var faculty *string
	var prefer bool
	if err := tx.QueryRow(ctx, `select u.faculty, r.prefer_prev_team from event_regs r join users u on u.id = r.user_id
		where r.event_id = $1 and r.user_id = $2`, eventID, userID).Scan(&faculty, &prefer); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `select r.team_no, coalesce(u.faculty, ''), r.user_id from event_regs r join users u on u.id = r.user_id
		where r.event_id = $1 and r.team_no is not null and r.user_id <> $2 order by r.team_no`, eventID, userID)
	if err != nil {
		return 0, err
	}
	teams := map[int16]*teamInfo{}
	members := map[int64]int16{}
	for rows.Next() {
		var no int16
		var fac string
		var uid int64
		if err := rows.Scan(&no, &fac, &uid); err != nil {
			rows.Close()
			return 0, err
		}
		t := teams[no]
		if t == nil {
			t = &teamInfo{no: no, faculties: map[string]int{}}
			teams[no] = t
		}
		t.size++
		t.faculties[fac]++
		members[uid] = no
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if prefer && len(teams) > 0 {
		// team-mates from the most recent earlier event where this person had a team
		mrows, err := tx.Query(ctx, `with last as (
				select r.event_id, r.team_no from event_regs r join events e on e.id = r.event_id
				where r.user_id = $1 and r.event_id <> $2 and r.team_no is not null
				order by e.starts_at desc limit 1)
			select r.user_id from event_regs r join last l on l.event_id = r.event_id and l.team_no = r.team_no
			where r.user_id <> $1`, userID, eventID)
		if err != nil {
			return 0, err
		}
		mates, err := pgx.CollectRows(mrows, pgx.RowTo[int64])
		if err != nil {
			return 0, err
		}
		for _, m := range mates {
			if no, ok := members[m]; ok {
				teams[no].prevMates++
			}
		}
	}
	fac := ""
	if faculty != nil {
		fac = *faculty
	}
	var open []*teamInfo
	allFull := true
	var maxNo int16
	for _, t := range teams {
		maxNo = max(maxNo, t.no)
		if t.size < TeamMax {
			open = append(open, t)
		}
		if t.size < TeamOpenAt {
			allFull = false
		}
	}
	sort.Slice(open, func(i, j int) bool {
		a, b := open[i], open[j]
		if a.prevMates != b.prevMates {
			return a.prevMates > b.prevMates
		}
		if a.faculties[fac] != b.faculties[fac] {
			return a.faculties[fac] < b.faculties[fac]
		}
		if a.size != b.size {
			return a.size < b.size
		}
		return a.no < b.no
	})
	no := maxNo + 1
	if len(open) > 0 && (open[0].prevMates > 0 || !allFull) {
		no = open[0].no
	}
	_, err = tx.Exec(ctx, `update event_regs set team_no = $3 where event_id = $1 and user_id = $2`, eventID, userID, no)
	return no, err
}

// SplitTeams re-splits everyone checked in into ceil(n/6) teams, spreading each faculty
// round-robin across teams. Returns the number of teams.
func SplitTeams(ctx context.Context, tx pgx.Tx, eventID int64) (int, error) {
	rows, err := tx.Query(ctx, `select r.user_id, coalesce(u.faculty, '') from event_regs r join users u on u.id = r.user_id
		where r.event_id = $1 and r.checked_in_at is not null order by r.checked_in_at, r.user_id`, eventID)
	if err != nil {
		return 0, err
	}
	type person struct {
		id  int64
		fac string
	}
	people, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (person, error) {
		var p person
		return p, row.Scan(&p.id, &p.fac)
	})
	if err != nil {
		return 0, err
	}
	n := len(people)
	if n == 0 {
		return 0, nil
	}
	k := (n + TeamMax - 1) / TeamMax
	// group by faculty, biggest faculty first, then deal round-robin
	byFac := map[string][]int64{}
	var facs []string
	for _, p := range people {
		if _, ok := byFac[p.fac]; !ok {
			facs = append(facs, p.fac)
		}
		byFac[p.fac] = append(byFac[p.fac], p.id)
	}
	sort.SliceStable(facs, func(i, j int) bool { return len(byFac[facs[i]]) > len(byFac[facs[j]]) })
	i := 0
	for _, f := range facs {
		for _, id := range byFac[f] {
			if _, err := tx.Exec(ctx, `update event_regs set team_no = $3 where event_id = $1 and user_id = $2`,
				eventID, id, int16(i%k+1)); err != nil {
				return 0, err
			}
			i++
		}
	}
	return k, nil
}

func splitTeams(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var k int
	ok := withEvent(w, r, u, true, func(tx pgx.Tx, ev *Event) error {
		var err error
		k, err = SplitTeams(r.Context(), tx, ev.ID)
		return err
	})
	if ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"teams": k})
	}
}
