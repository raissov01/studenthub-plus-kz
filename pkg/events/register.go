package events

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
)

func readBody(r *http.Request, max int64) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, max))
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// randomToken returns n random bytes as base64url (no padding).
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// register: one registration per person, capacity and waitlist enforced by event_register
// (security definer, locks the event row), called as app_user.
func register(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	if u.NeedsConsent() {
		httpx.Error(w, r, http.StatusForbidden, "consent_required", MsgConsent)
		return
	}
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
		return
	}
	var body struct {
		WithPreviousTeam bool `json:"with_previous_team"`
	}
	_ = httpx.Decode(r, &body, 1<<10) // optional
	var result, status, ticket *string
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select result, reg_status, ticket from event_register($1, $2, $3)`,
			id, randomToken(16), body.WithPreviousTeam).Scan(&result, &status, &ticket)
	})
	if err != nil {
		fail(w, r, "register", err)
		return
	}
	switch *result {
	case "not_found":
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
	case "closed":
		httpx.Error(w, r, http.StatusConflict, "closed", MsgClosed)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"status": *status, "ticket": *ticket, "already": *result == "already"})
	}
}

func unregister(w http.ResponseWriter, r *http.Request, u *auth.User) {
	ctx := r.Context()
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
		return
	}
	var result string
	var promoted *int64
	err := db.WithUser(ctx, u.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select result, promoted_user from event_unregister($1)`, id).Scan(&result, &promoted)
	})
	if err != nil {
		fail(w, r, "unregister", err)
		return
	}
	switch result {
	case "not_found":
		httpx.Error(w, r, http.StatusNotFound, "not_found", MsgNoEvent)
		return
	case "closed":
		httpx.Error(w, r, http.StatusConflict, "closed", MsgClosed)
		return
	case "not_registered":
		httpx.Error(w, r, http.StatusNotFound, "not_registered", MsgNotReg)
		return
	}
	if promoted != nil {
		go func(ctx context.Context) {
			var ev *Event
			if err := db.Tx(ctx, func(tx pgx.Tx) error { var err error; ev, err = load(ctx, tx, id, false); return err }); err == nil {
				notifyPromoted(ctx, ev, []int64{*promoted})
			}
		}(context.WithoutCancel(ctx))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// promoteWaitlist fills free seats (after a capacity increase) from the waitlist, oldest first.
func promoteWaitlist(ctx context.Context, eventID int64) ([]int64, error) {
	var out []int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		ev, err := load(ctx, tx, eventID, true)
		if err != nil {
			return err
		}
		if ev.Status != "published" || ev.Waitlist == 0 {
			return nil
		}
		free := -1 // unlimited
		if ev.Capacity != nil {
			free = *ev.Capacity - ev.Registered
			if free <= 0 {
				return nil
			}
		}
		rows, err := tx.Query(ctx, `update event_regs r set status = 'registered'
			where (r.event_id, r.user_id) in (select w.event_id, w.user_id from event_regs w
				where w.event_id = $1 and w.status = 'waitlist' order by w.registered_at, w.user_id
				limit case when $2::int < 0 then null else $2::int end)
			returning r.user_id`, eventID, free)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	return out, err
}

func eventLink(id int64) string { return fmt.Sprintf("%s/#/event/%d", config.SiteURL(), id) }

func userLang(ctx context.Context, tx pgx.Tx, id int64) string {
	lang := "kk"
	_ = tx.QueryRow(ctx, `select lang from users where id = $1`, id).Scan(&lang)
	return lang
}

func send(ctx context.Context, userID int64, kk, ru string) {
	lang := "kk"
	_ = db.Tx(ctx, func(tx pgx.Tx) error { lang = userLang(ctx, tx, userID); return nil })
	text := kk
	if lang == "ru" {
		text = ru
	}
	if err := Notify(ctx, userID, text); err != nil {
		log.Printf("events: notify %d: %v", userID, err)
	}
}

// notifyPromoted: a seat freed up for the first person on the waitlist (direct message).
func notifyPromoted(ctx context.Context, ev *Event, users []int64) {
	if ev == nil {
		return
	}
	title := html.EscapeString(ev.TitleKK)
	for _, id := range users {
		send(ctx, id,
			fmt.Sprintf("🎉 «%s» іс-шарасында орын босады — сіз енді тіркелдіңіз!\n%s", title, eventLink(ev.ID)),
			fmt.Sprintf("🎉 На «%s» освободилось место — вы зарегистрированы!\n%s", title, eventLink(ev.ID)))
	}
}

func notifyCancelled(ctx context.Context, ev *Event) {
	var ids []int64
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select user_id from event_regs where event_id = $1`, ev.ID)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Printf("events: cancelled %d: %v", ev.ID, err)
		return
	}
	title := html.EscapeString(ev.TitleKK)
	for _, id := range ids {
		send(ctx, id, fmt.Sprintf("❌ «%s» іс-шарасы өтпейді (бас тартылды).", title),
			fmt.Sprintf("❌ Мероприятие «%s» отменено.", title))
	}
}
