// Package market is prompt 08, "Отдам даром": students give things away for free. There is
// no price field at all. Publishing needs an account with a Telegram @username; items are
// visible at once (a young account's first item goes through the shared moderation queue),
// reports hide an item after N counted reporters (moderation.CountedReports), and photos are
// re-encoded on the server and stored on local disk (UPLOAD_DIR).
//
// The daily limit and publisher checks live in market_item_create() (Postgres). Owner edits
// run through db.WithUser, so RLS refuses edits of other people's items; public reads that
// show the publisher's name run as the owner with explicit status filters.
package market

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/config"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
	"github.com/raissov01/studenthub-plus-kz/pkg/moderation"
)

const (
	Feature    = "market"
	KindItem   = "market_item"   // a young account's first item, waiting for approval
	KindReport = "market_report" // reports on an item (one pending submission per item)
	TargetKind = "market_item"   // reports.target_kind
)

var (
	Categories    = []string{"books", "dorm", "tech", "clothes", "sport", "other"}
	Conditions    = []string{"new", "good", "used"}
	ReportReasons = []string{"selling", "prohibited", "scam", "other"}
)

func dailyLimit() int    { return config.Int("MARKET_DAILY_LIMIT", 3) }
func hideThreshold() int { return config.Int("MARKET_REPORT_HIDE", 3) }
func generousAfter() int { return 3 }
func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func init() {
	moderation.OnApprove(KindItem, approveItem)
	moderation.OnReject(KindItem, rejectItem)
	moderation.OnApprove(KindReport, upholdReport)
	moderation.OnReject(KindReport, dismissReport)
	me.RegisterDeleteHook(deleteUserFiles)
	me.RegisterExport("marketItems", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from market_items where user_id = app_user_id() order by id")
	})
	me.RegisterExport("marketPhotos", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select name, item_id, bytes, created_at from market_photos where user_id = app_user_id() order by created_at")
	})
	me.RegisterExport("marketRules", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from market_rules where user_id = app_user_id()")
	})
	me.RegisterExport("marketDaily", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select * from market_daily where user_id = app_user_id() order by day")
	})
	me.RegisterExport("marketBans", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select until, reason, created_at from market_bans where user_id = app_user_id()")
	})
}

// Routes wires /api/market/*, /api/admin/market/* and the cron job; 404 while FEATURE_MARKET is off.
func Routes(mux *http.ServeMux) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	w := func(fn auth.Handler) http.HandlerFunc { return auth.Write(auth.Need(fn)) }

	h("GET /api/market/items", browse)
	h("GET /api/market/items/{id}", getItem)
	h("POST /api/market/items", w(create))
	h("PATCH /api/market/items/{id}", w(patchItem))
	h("DELETE /api/market/items/{id}", w(deleteItem))
	h("POST /api/market/items/{id}/given", w(markGiven))
	h("POST /api/market/items/{id}/extend", w(extend))
	h("POST /api/market/items/{id}/report", w(report))
	h("GET /api/market/mine", auth.Need(mine))
	h("GET /api/market/me", auth.Need(status))
	h("POST /api/market/rules/accept", w(acceptRules))
	h("POST /api/market/photos", w(upload))
	h("GET /api/market/photos/{name}", servePhoto)

	h("GET /api/admin/market/items", auth.NeedRole(adminItems, "admin"))
	h("POST /api/admin/market/items/{id}/hide", auth.Write(auth.NeedRole(adminHide, "admin")))
	h("POST /api/admin/market/items/{id}/restore", auth.Write(auth.NeedRole(adminRestore, "admin")))
	h("POST /api/admin/market/bans", auth.Write(auth.NeedRole(adminBan, "admin")))
	h("DELETE /api/admin/market/bans/{userId}", auth.Write(auth.NeedRole(adminUnban, "admin")))

	h("GET /api/cron/market", cronHandler)
}

var msgs = map[string]httpx.Msg{
	"consent_required": {KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."},
	"no_account":       {KK: "Жариялау үшін профильде университетіңізді көрсетіңіз.", RU: "Чтобы публиковать, укажите университет в профиле."},
	"no_username":      {KK: "Сізге жаза алуы үшін Telegram-да username орнатыңыз, сосын қайта кіріңіз.", RU: "Установите username в Telegram, чтобы вам могли написать, и войдите снова."},
	"minor":            {KK: "Зат жариялау 18 жастан бастап қолжетімді.", RU: "Публиковать вещи можно с 18 лет."},
	"no_rules":         {KK: "Алдымен маркет ережелерімен танысыңыз.", RU: "Сначала ознакомьтесь с правилами маркета."},
	"banned":           {KK: "Жариялау құқығыңыз уақытша тоқтатылған.", RU: "Публикация временно ограничена."},
	"daily_limit":      {KK: "Күніне ең көбі 3 зат жариялауға болады. Ертең жалғастырыңыз.", RU: "Можно публиковать не больше 3 вещей в день. Продолжите завтра."},
	"bad_photo":        {KK: "Фото табылмады — қайта жүктеңіз.", RU: "Фото не найдено — загрузите заново."},
	"photo_too_large":  {KK: "Фото тым үлкен (ең көбі 600 КБ).", RU: "Фото слишком большое (максимум 600 КБ)."},
	"photo_type":       {KK: "Тек JPEG, PNG немесе WebP фото.", RU: "Только фото JPEG, PNG или WebP."},
	"photo_dimensions": {KK: "Фотоның өлшемі тым үлкен.", RU: "Слишком большое разрешение фото."},
	"photo_invalid":    {KK: "Фотоны оқу мүмкін болмады.", RU: "Не удалось прочитать фото."},
	"item_not_found":   {KK: "Зат табылмады.", RU: "Вещь не найдена."},
	"own_item":         {KK: "Өз затыңызға шағымдана алмайсыз.", RU: "Нельзя пожаловаться на свою вещь."},
	"reason_required":  {KK: "Себебін жазыңыз (3–500 таңба).", RU: "Укажите причину (3–500 символов)."},
	"user_not_found":   {KK: "Мұндай қолданушы табылмады.", RU: "Пользователь не найден."},
	"not_banned":       {KK: "Бұл студенттің жариялауы шектелмеген.", RU: "У этого студента нет ограничения."},
}

func fail(w http.ResponseWriter, r *http.Request, code string, status int) {
	m, ok := msgs[code]
	if !ok {
		m = httpx.MsgBad
	}
	httpx.Error(w, r, status, code, m)
}

func internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("market: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func bad(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "not_found", httpx.MsgNotFound)
}

func raised(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "P0001" {
		return pe.Message
	}
	return ""
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
