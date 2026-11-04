// Package httpx holds small JSON and error helpers shared by all API handlers.
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Msg is a user-facing message in both UI languages.
type Msg struct{ KK, RU string }

// Lang picks kk or ru from ?lang=, then the Accept-Language header; kk by default.
func Lang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l == "ru" || l == "kk" {
		return l
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ru") {
		return "ru"
	}
	return "kk"
}

// Error writes {"error":{"code","message"}} with the message in the request's language.
func Error(w http.ResponseWriter, r *http.Request, status int, code string, m Msg) {
	text := m.KK
	if Lang(r) == "ru" {
		text = m.RU
	}
	JSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": text}})
}

var (
	MsgBad      = Msg{"Сұраныс дұрыс емес.", "Некорректный запрос."}
	MsgNotFound = Msg{"Мұндай бет жоқ.", "Такой страницы нет."}
	MsgMethod   = Msg{"Бұл әдіс қолданылмайды.", "Метод не поддерживается."}
	MsgInternal = Msg{"Серверде қате шықты. Кейінірек қайталаңыз.", "Ошибка на сервере. Повторите позже."}
	MsgAuth     = Msg{"Алдымен аккаунтқа кіріңіз (Telegram не email).", "Сначала войдите в аккаунт (Telegram или email)."}
	MsgForbid   = Msg{"Бұл әрекетке рұқсат жоқ.", "Нет доступа к этому действию."}
	MsgSlow     = Msg{"Сұраныс тым жиі жіберілді. Сәл күтіп, қайталаңыз.", "Слишком частые запросы. Подождите и повторите."}
)

// Decode reads a JSON body of at most maxBytes into v.
func Decode(r *http.Request, v any, maxBytes int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	return dec.Decode(v)
}

// ClientIP returns the caller's IP as Vercel reports it.
func ClientIP(r *http.Request) string {
	for _, h := range []string{"X-Real-Ip", "X-Forwarded-For"} {
		if v := strings.TrimSpace(strings.Split(r.Header.Get(h), ",")[0]); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
