// Package quiz is the Quizizz-style quiz module (ported from the owner's other project and
// rewritten for this repo): students create quizzes by hand, from Excel or with AI from pasted
// text or uploaded lecture notes, practise them with server-side scoring, share them, and host
// Kahoot-style live games over a WebSocket. FEATURE_QUIZ; the full API is in docs/api/quiz.md.
//
// Owner writes run through db.WithUser so RLS refuses edits of other people's quizzes. Reads
// that must see answer keys (grading, live) or unlisted quizzes by share code run as the
// connection owner with explicit visibility checks.
package quiz

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"math/big"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/auth"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
	"github.com/raissov01/studenthub-plus-kz/pkg/httpx"
	"github.com/raissov01/studenthub-plus-kz/pkg/me"
)

const Feature = "quiz"

func init() {
	me.RegisterDeleteHook(deleteUserFiles)
	me.RegisterExport("quizzes", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select q.*, coalesce((select jsonb_agg(jsonb_build_object('position', qq.position,
			'type', qq.type, 'prompt', qq.prompt, 'explanation', qq.explanation, 'sourceQuote', qq.source_quote,
			'image', qq.image, 'audio', qq.audio, 'key', k.answer) order by qq.position)
			from quiz_questions qq left join quiz_answer_keys k on k.question_id = qq.id where qq.quiz_id = q.id), '[]') as questions
			from quizzes q where q.user_id = app_user_id() order by q.id`)
	})
	me.RegisterExport("quizAttempts", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, `select a.*, coalesce((select jsonb_agg(to_jsonb(x) - 'attempt_id' order by x.position)
			from quiz_attempt_answers x where x.attempt_id = a.id), '[]') as answers
			from quiz_attempts a where a.user_id = app_user_id() order by a.id`)
	})
	me.RegisterExport("quizMedia", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select name, kind, quiz_id, bytes, created_at from quiz_media where user_id = app_user_id() order by created_at")
	})
	me.RegisterExport("quizAIUsage", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select day, units from quiz_ai_usage where user_id = app_user_id() order by day")
	})
	me.RegisterExport("quizLiveHosted", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select id, quiz_id, status, players, created_at, started_at, finished_at from quiz_live_sessions where host_id = app_user_id() order by id")
	})
	me.RegisterExport("quizLiveResults", func(ctx context.Context, tx pgx.Tx, _ int64) (any, error) {
		return me.Rows(ctx, tx, "select session_id, rank, nickname, score, correct from quiz_live_results where user_id = app_user_id() order by id")
	})
}

// Routes wires /api/quiz/*, the admin hide/restore and the cron job; 404 while FEATURE_QUIZ is off.
func Routes(mux *http.ServeMux) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, features.RequireFunc(Feature, fn)) }
	w := func(fn auth.Handler) http.HandlerFunc { return auth.Write(auth.Need(fn)) }

	// quizzes
	h("GET /api/quiz/quizzes", browse)
	h("GET /api/quiz/mine", auth.Need(mine))
	h("POST /api/quiz/quizzes", w(create))
	h("GET /api/quiz/quizzes/{id}", auth.Need(getForEdit))
	h("PUT /api/quiz/quizzes/{id}", w(update))
	h("DELETE /api/quiz/quizzes/{id}", w(remove))
	h("POST /api/quiz/quizzes/{id}/clone", w(cloneByID))
	h("POST /api/quiz/quizzes/{id}/share/reset", w(resetShare))
	h("GET /api/quiz/quizzes/{id}/play", playByID)
	h("GET /api/quiz/quizzes/{id}/stats", auth.Need(stats))
	h("POST /api/quiz/quizzes/{id}/attempts", auth.Write(submit))
	// share links
	h("GET /api/quiz/s/{code}", playByCode)
	h("GET /api/quiz/s/{code}/meta", shareMeta)
	h("POST /api/quiz/s/{code}/clone", w(cloneByCode))
	// attempts
	h("GET /api/quiz/attempts", auth.Need(history))
	h("GET /api/quiz/attempts/{id}", auth.Need(getAttempt))
	h("GET /api/quiz/attempts/{id}/retry", auth.Need(retryWrong))
	// media, import
	h("POST /api/quiz/media", w(uploadMedia))
	h("GET /api/quiz/media/{name}", serveMedia)
	h("POST /api/quiz/import/xlsx", w(importXLSX))
	h("GET /api/quiz/import/template.xlsx", template)
	// AI
	h("GET /api/quiz/generate/usage", auth.Need(aiUsage))
	h("POST /api/quiz/generate/from-text", w(generateFromText))
	h("POST /api/quiz/generate/from-files", w(generateFromFiles))
	h("POST /api/quiz/generate/cards-from-files", w(cardsFromFiles))
	// live
	h("POST /api/quiz/live/sessions", w(liveCreate))
	h("GET /api/quiz/live/sessions/{code}", liveInfo)
	h("POST /api/quiz/live/sessions/{code}/host-token", w(liveHostToken))
	h("POST /api/quiz/live/join", auth.Write(liveJoin))
	h("GET /api/quiz/live/ws", liveWS)
	h("GET /api/quiz/live/history", auth.Need(liveHistory))
	// admin, cron
	h("POST /api/admin/quiz/quizzes/{id}/hide", auth.Write(auth.NeedRole(adminHide, "admin")))
	h("POST /api/admin/quiz/quizzes/{id}/restore", auth.Write(auth.NeedRole(adminRestore, "admin")))
	h("GET /api/cron/quiz", cronHandler)
}

var msgs = map[string]httpx.Msg{
	"consent_required":  {KK: "Алдымен деректерді өңдеуге келісім беріңіз.", RU: "Сначала дайте согласие на обработку данных."},
	"quiz_not_found":    {KK: "Тест табылмады.", RU: "Тест не найден."},
	"invalid_quiz":      {KK: "Тест дұрыс толтырылмаған — белгіленген сұрақты тексеріңіз.", RU: "Тест заполнен неверно — проверьте отмеченный вопрос."},
	"bad_title":         {KK: "Тест атауын жазыңыз (200 таңбаға дейін).", RU: "Укажите название теста (до 200 символов)."},
	"too_many":          {KK: "Бір тестте ең көбі 200 сұрақ.", RU: "В одном тесте не больше 200 вопросов."},
	"no_questions":      {KK: "Кемінде бір сұрақ қосыңыз.", RU: "Добавьте хотя бы один вопрос."},
	"version_conflict":  {KK: "Тест басқа жерде өзгертілген. Бетті жаңартыңыз.", RU: "Тест изменён в другом месте. Обновите страницу."},
	"bad_media":         {KK: "Файл табылмады — қайта жүктеңіз.", RU: "Файл не найден — загрузите заново."},
	"media_too_large":   {KK: "Файл тым үлкен.", RU: "Файл слишком большой."},
	"media_type":        {KK: "Бұл файл түрі қабылданбайды.", RU: "Этот тип файла не поддерживается."},
	"media_dimensions":  {KK: "Суреттің өлшемі тым үлкен.", RU: "Слишком большое разрешение изображения."},
	"media_invalid":     {KK: "Файлды оқу мүмкін болмады.", RU: "Не удалось прочитать файл."},
	"xlsx_invalid":      {KK: "Excel файлын оқу мүмкін болмады (.xlsx керек).", RU: "Не удалось прочитать Excel-файл (нужен .xlsx)."},
	"xlsx_empty":        {KK: "Файлда сұрақтар табылмады. Үлгіні қолданыңыз.", RU: "В файле нет вопросов. Воспользуйтесь шаблоном."},
	"attempt_not_found": {KK: "Нәтиже табылмады.", RU: "Результат не найден."},
	"nothing_to_retry":  {KK: "Қате жауап жоқ — бәрі дұрыс!", RU: "Ошибок нет — всё верно!"},
	"bad_answers":       {KK: "Жауаптар дұрыс жіберілмеді.", RU: "Ответы отправлены неверно."},
	"ai_off":            {KK: "AI уақытша қолжетімсіз.", RU: "AI временно недоступен."},
	"ai_daily":          {KK: "Бүгінгі AI лимиті таусылды. Ертең қайталаңыз.", RU: "Дневной лимит AI исчерпан. Попробуйте завтра."},
	"ai_minute":         {KK: "Тым жиі. Бір минуттан кейін қайталаңыз.", RU: "Слишком часто. Повторите через минуту."},
	"ai_upstream":       {KK: "AI дұрыс жауап бермеді. Қайталап көріңіз.", RU: "AI ответил некорректно. Попробуйте ещё раз."},
	"text_too_short":    {KK: "Мәтін тым қысқа (кемінде 200 таңба).", RU: "Текст слишком короткий (минимум 200 символов)."},
	"text_too_long":     {KK: "Мәтін тым ұзын.", RU: "Текст слишком длинный."},
	"bad_options":       {KK: "Генерация параметрлері дұрыс емес.", RU: "Неверные параметры генерации."},
	"no_files":          {KK: "Конспект файлын таңдаңыз.", RU: "Выберите файл конспекта."},
	"too_many_files":    {KK: "Ең көбі 5 файл.", RU: "Не больше 5 файлов."},
	"too_many_pages":    {KK: "Ең көбі 30 бет (слайд, фото).", RU: "Не больше 30 страниц (слайдов, фото)."},
	"file_too_large":    {KK: "Файл тым үлкен (ең көбі 10 МБ).", RU: "Файл слишком большой (максимум 10 МБ)."},
	"file_type":         {KK: "Тек PDF, DOCX, PPTX, TXT немесе JPEG/PNG/WebP фото.", RU: "Только PDF, DOCX, PPTX, TXT или фото JPEG/PNG/WebP."},
	"file_heic":         {KK: "HEIC фото қабылданбайды — JPEG ретінде жіберіңіз (камера «Ең үйлесімді» режимі).", RU: "Фото HEIC не принимаются — отправьте как JPEG (камера, режим «Наиболее совместимый»)."},
	"file_invalid":      {KK: "Файлды оқу мүмкін болмады.", RU: "Не удалось прочитать файл."},
	"pdf_scanned":       {KK: "PDF сканерленген сияқты (мәтін қабаты жоқ). Беттерді фото ретінде жүктеңіз.", RU: "PDF похож на скан (нет текстового слоя). Загрузите страницы как фото."},
	"no_text":           {KK: "Файлдардан мәтін табылмады.", RU: "В файлах не найден текст."},
	"live_not_found":    {KK: "Мұндай коды бар ойын жоқ.", RU: "Игры с таким кодом нет."},
	"live_full":         {KK: "Ойында орын қалмады.", RU: "В игре нет мест."},
	"live_started":      {KK: "Ойын басталып кетті.", RU: "Игра уже началась."},
	"live_busy":         {KK: "Қазір тым көп ойын жүріп жатыр. Кейінірек қайталаңыз.", RU: "Сейчас идёт слишком много игр. Попробуйте позже."},
	"bad_nickname":      {KK: "Лақап атыңызды жазыңыз (2–24 таңба).", RU: "Укажите никнейм (2–24 символа)."},
	"nickname_taken":    {KK: "Бұл лақап ат бос емес.", RU: "Этот никнейм занят."},
	"not_host":          {KK: "Бұл ойынның жүргізушісі сіз емессіз.", RU: "Вы не ведущий этой игры."},
}

func fail(w http.ResponseWriter, r *http.Request, code string, status int) {
	m, ok := msgs[code]
	if !ok {
		m = httpx.MsgBad
	}
	httpx.Error(w, r, status, code, m)
}

func internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	log.Printf("quiz: %s: %v", what, err)
	httpx.Error(w, r, http.StatusInternalServerError, "internal", httpx.MsgInternal)
}

func bad(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusBadRequest, "bad_request", httpx.MsgBad)
}

func notFound(w http.ResponseWriter, r *http.Request) { fail(w, r, "quiz_not_found", http.StatusNotFound) }

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

const alnum = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// randomCode returns n characters from alphabet using crypto/rand.
func randomCode(n int, alphabet string) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
