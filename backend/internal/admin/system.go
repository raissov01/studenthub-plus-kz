package admin

import (
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
)

// System facts for /admin/health (admin only), adapted from the owner's StudyWithRaissov admin (System and
// Database pages): process uptime, build revision, memory and goroutines, the connection pool, accounts online
// now (memory, this process), and the database: size, connections by state and the largest tables with their
// dead-row share and last vacuum. Built-in catalog views only; no query texts are returned.

var started = time.Now()

type tableStat struct {
	Name       string     `json:"name"`
	Rows       int64      `json:"rows"`
	TotalBytes int64      `json:"totalBytes"`
	IndexBytes int64      `json:"indexBytes"`
	DeadRows   int64      `json:"deadRows"`
	DeadShare  float64    `json:"deadShare"`
	Vacuumed   *time.Time `json:"vacuumed"`
}

func system(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	rev := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				rev = s.Value[:7]
			}
		}
	}
	out := map[string]any{
		"uptimeSeconds": int64(time.Since(started).Seconds()), "revision": rev, "goVersion": runtime.Version(),
		"goroutines": runtime.NumGoroutine(), "heapMB": mem.HeapAlloc >> 20, "sysMB": mem.Sys >> 20, "gc": mem.NumGC,
		"onlineNow": auth.OnlineCount(time.Now()), "onlineWindowMinutes": int(auth.OnlineWindow.Minutes()),
	}
	if p, err := db.Pool(ctx); err == nil {
		st := p.Stat()
		out["pool"] = map[string]any{"total": st.TotalConns(), "idle": st.IdleConns(), "inUse": st.AcquiredConns(), "max": st.MaxConns(),
			"waitedTotal": st.EmptyAcquireCount(), "canceled": st.CanceledAcquireCount()}
	}
	conns := map[string]int{}
	tables := []tableStat{}
	var size int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select pg_database_size(current_database())`).Scan(&size); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select coalesce(state, 'unknown'), count(*) from pg_stat_activity where datname = current_database() group by 1`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s string
			var n int
			if err := rows.Scan(&s, &n); err != nil {
				rows.Close()
				return err
			}
			conns[s] = n
		}
		rows.Close()
		rows, err = tx.Query(ctx, `select c.relname, coalesce(s.n_live_tup, 0), pg_total_relation_size(c.oid), pg_indexes_size(c.oid),
				coalesce(s.n_dead_tup, 0),
				case when coalesce(s.n_live_tup, 0) + coalesce(s.n_dead_tup, 0) > 0 then s.n_dead_tup::float8 / (s.n_live_tup + s.n_dead_tup) else 0 end,
				greatest(s.last_vacuum, s.last_autovacuum)
			from pg_class c join pg_namespace n on n.oid = c.relnamespace left join pg_stat_all_tables s on s.relid = c.oid
			where c.relkind = 'r' and n.nspname = 'public' order by pg_total_relation_size(c.oid) desc limit 15`)
		if err != nil {
			return err
		}
		tables, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (tableStat, error) {
			var t tableStat
			return t, row.Scan(&t.Name, &t.Rows, &t.TotalBytes, &t.IndexBytes, &t.DeadRows, &t.DeadShare, &t.Vacuumed)
		})
		return err
	})
	if err != nil {
		fail(w, r, "system", err)
		return
	}
	out["database"] = map[string]any{"sizeBytes": size, "connections": conns, "tables": tables}
	httpx.JSON(w, http.StatusOK, out)
}
