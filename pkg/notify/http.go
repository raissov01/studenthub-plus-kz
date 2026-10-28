package notify

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
)

func init() {
	me.RegisterExport("notificationPrefs", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from notify_prefs where user_id = app_user_id()")
	})
	me.RegisterExport("notificationOutbox", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select id, category, text, not_before, state, created_at from notify_outbox where user_id = app_user_id() order by id")
	})
}

var (
	MsgLocked = httpx.Msg{KK: "Модерация шешімдері туралы хабарламаларды өшіруге болмайды.", RU: "Уведомления о решениях модерации отключить нельзя."}
	MsgQuiet  = httpx.Msg{KK: "Тыныш уақыт ҚҚ:ММ форматында болуы керек, басы мен соңы әртүрлі.", RU: "Тихие часы — в формате ЧЧ:ММ, начало и конец должны отличаться."}
	MsgCap    = httpx.Msg{KK: "Күндік шек 1-ден 20-ға дейін болуы керек.", RU: "Дневной лимит — от 1 до 20."}
)

// Routes wires GET/PUT /api/me/notifications and the hourly outbox job GET /api/cron/notify.
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me/notifications", auth.Need(getPrefs))
	mux.HandleFunc("PUT /api/me/notifications", auth.Write(auth.Need(putPrefs)))
	mux.HandleFunc("GET /api/cron/notify", cron)
}

// PrefsJSON is the wire shape of Prefs.
type PrefsJSON struct {
	Categories map[Category]bool `json:"categories"`
	QuietHours QuietJSON         `json:"quietHours"`
	DailyCap   int               `json:"dailyCap"`
	Locked     []Category        `json:"locked"`
}

type QuietJSON struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}

// JSON renders p for the API.
func (p Prefs) JSON() PrefsJSON {
	cats := map[Category]bool{CatModeration: true}
	for _, c := range Categories {
		cats[c] = p.Allows(c)
	}
	return PrefsJSON{
		Categories: cats,
		QuietHours: QuietJSON{Enabled: p.QuietEnabled, Start: Clock(p.QuietStart), End: Clock(p.QuietEnd), Timezone: "Asia/Almaty"},
		DailyCap:   p.DailyCap,
		Locked:     []Category{CatModeration},
	}
}

func fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("notify: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func getPrefs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var p Prefs
	err := db.WithUser(r.Context(), u.ID, func(tx pgx.Tx) error {
		var err error
		p, err = LoadPrefs(r.Context(), tx, u.ID)
		return err
	})
	if err != nil {
		fail(w, r, "get prefs", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"prefs": p.JSON()})
}

// prefsPatch: every field optional; only the given ones change.
type prefsPatch struct {
	Categories map[string]bool `json:"categories"`
	QuietHours *struct {
		Enabled *bool   `json:"enabled"`
		Start   *string `json:"start"`
		End     *string `json:"end"`
	} `json:"quietHours"`
	DailyCap *int `json:"dailyCap"`
}

// apply validates the patch onto p; it returns an error code and message on bad input.
func (b prefsPatch) apply(p *Prefs) (string, httpx.Msg) {
	for k, on := range b.Categories {
		c := Category(k)
		switch {
		case c == CatModeration:
			if !on {
				return "moderation_locked", MsgLocked
			}
		case c.valid():
			p.On[c] = on
		default:
			return "unknown_category", httpx.MsgBad
		}
	}
	if q := b.QuietHours; q != nil {
		if q.Enabled != nil {
			p.QuietEnabled = *q.Enabled
		}
		for _, f := range []struct {
			in  *string
			out *int
		}{{q.Start, &p.QuietStart}, {q.End, &p.QuietEnd}} {
			if f.in == nil {
				continue
			}
			m, ok := ParseClock(*f.in)
			if !ok {
				return "invalid_quiet_hours", MsgQuiet
			}
			*f.out = m
		}
		if p.QuietStart == p.QuietEnd {
			return "invalid_quiet_hours", MsgQuiet
		}
	}
	if b.DailyCap != nil {
		if *b.DailyCap < 1 || *b.DailyCap > MaxDailyCap {
			return "invalid_daily_cap", MsgCap
		}
		p.DailyCap = *b.DailyCap
	}
	return "", httpx.Msg{}
}

func putPrefs(w http.ResponseWriter, r *http.Request, u *auth.User) {
	var body prefsPatch
	if err := httpx.Decode(r, &body, 4<<10); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
		return
	}
	ctx := r.Context()
	var p Prefs
	var bad string
	var badMsg httpx.Msg
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		var err error
		if p, err = LoadPrefs(ctx, tx, u.ID); err != nil {
			return err
		}
		if bad, badMsg = body.apply(&p); bad != "" {
			return nil
		}
		return SavePrefs(ctx, tx, u.ID, p)
	})
	if err != nil {
		fail(w, r, "put prefs", err)
		return
	}
	if bad != "" {
		httpx.Error(w, r, http.StatusBadRequest, bad, badMsg)
		return
	}
	// Held-back messages are re-checked on the next hourly run with the new settings
	// (quiet hours shortened or switched off -> they may go out now).
	if pool, err := db.Pool(ctx); err == nil {
		if err := Reschedule(ctx, pool, u.ID); err != nil {
			log.Printf("notify: reschedule %d: %v", u.ID, err)
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"prefs": p.JSON()})
}

// Reschedule makes the student's held-back messages due now, so the next outbox run
// re-checks them against changed settings. Runs as the connection owner.
func Reschedule(ctx context.Context, q Querier, userID int64) error {
	_, err := q.Exec(ctx, `update notify_outbox set not_before = now() where user_id = $1 and state = 'pending' and not_before > now()`, userID)
	return err
}

func cronAuthorized(r *http.Request) bool {
	want := config.Str("CRON_SECRET", "")
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || want == "" {
		return false
	}
	a, b := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// cron is GET /api/cron/notify (hourly, Authorization: Bearer CRON_SECRET).
func cron(w http.ResponseWriter, r *http.Request) {
	if !cronAuthorized(r) {
		httpx.JSON(w, http.StatusUnauthorized, map[string]bool{"ok": false})
		return
	}
	res, err := Flush(context.WithoutCancel(r.Context()), Now())
	if err != nil {
		log.Printf("notify cron failed: %v", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]bool{"ok": false})
		return
	}
	line, _ := json.Marshal(res)
	log.Printf("notify cron %s", line)
	httpx.JSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
		*FlushResult
	}{true, res})
}
