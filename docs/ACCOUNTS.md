# Аккаунттар: кіру, сессия, келісім, деректер, рөлдер, ажыратқыштар

Аккаунт міндетті емес: сайт кірмей-ақ толық жұмыс істейді. Аккаунт тек ортақ дерекқор керек
әрекеттерге (тіркелу, жариялау, өтінім т.б.) қажет. Код: `pkg/auth`, `pkg/me`, `pkg/admin`,
`pkg/features`; маршруттар `pkg/server/routes_auth.go`-та қосылады.

## Эндпоинттер

| Жол | Әдіс | Кім | Rate limit |
|---|---|---|---|
| `/api/auth/telegram/webapp` | POST `{initData}` | кез келген (Mini App) | IP бойынша 30/мин |
| `/api/auth/telegram/widget` | POST (Login Widget өрістері) | кез келген (браузер) | IP бойынша 30/мин |
| `/api/auth/logout` | POST (`{all:true}` — барлық құрылғыдан) | кез келген | IP + қолданушы 30/мин |
| `/api/me` | GET | кез келген (кірмесе `{user:null}`) | — |
| `/api/me/consent` | POST `{isAdult?}` | кірген | IP + қолданушы 30/мин |
| `/api/me` | PATCH | кірген + келісім берген | IP + қолданушы 30/мин |
| `/api/me/export` | GET | кірген | — |
| `/api/me` | DELETE `{confirm:"DELETE"}` | кірген | IP + қолданушы 30/мин |
| `/api/admin/summary` | GET | admin | — |
| `/api/admin/users?q=` | GET | admin | — |
| `/api/admin/role` | POST `{userId\|telegramId, role, reason}` | admin | IP + қолданушы 30/мин |
| `/api/config` | GET | кез келген | — |

Барлық күй өзгертетін сұраныс (POST/PATCH/DELETE) `Origin` (болмаса `Referer`) хосты
`ALLOWED_HOSTS` тізімінде болуын талап етеді — бұл `SameSite=Lax` cookie-ге қосымша CSRF қорғаныс.
Екеуі де жоқ сұраныс қабылданбайды (403 `bad_origin`).

## Не сақталады

`users` кестесінде (бір Telegram аккаунты = бір жол, бот пен сайт ортақ):

- `telegram_id`, `tg_username`, `name` (Telegram-нан, кейін өзгертуге болады), `lang` (kk/ru)
- `university`, `faculty`, `course` (1–6), `city` — студент өзі толтырса ғана
- `role`, `consent_at` (келісім уақыты), `is_adult` (18-ге толды ма, сұралмаса null)
- `session_version` («барлық құрылғыдан шығу» үшін), `last_seen_at` (соңғы кіру), `created_at`
- боттың күйі: `bot_started_at`, `bot_inactive`, `paused_until`, `digest_city`, `digest_direction` т.б.

Сонымен қатар: `reminders` (еске салулар), `bot_daily_sends` (бот хабарламасы жіберілген күндер),
`bot_events` (белгіледім/жасырдым/тіркелдім оқиғалары), `audit_log` (рөл өзгерістері).

**GPA мен телефон серверде ЕШҚАШАН сақталмайды** — олар тек құрылғыда (`localStorage`).
`PATCH /api/me` оларды жіберсе, 400 `not_stored` қайтарады.

Сессия — `sh_session` cookie: `base64url(JSON{u: id, v: session_version, e: мерзім}) + "." +
base64url(HMAC-SHA256(SESSION_SECRET, payload))`. HttpOnly, Secure (localhost-тан басқа),
SameSite=Lax, Path=/, 30 күн. Әр сұраныста `role`, `session_version` және `deleted_at`
дерекқордан қайта оқылады: рөл алынса немесе аккаунт бұғатталса — бірден күшіне енеді.
Mini App (Telegram Web iframe-де cookie жетпеуі мүмкін) кіргенде жауапта `token` алады, оны тек
жадта ұстап, `Authorization: Bearer <token>` етіп жібереді.

## Келісім, экспорт, өшіру

- Келісімсіз (`consent_at` бос) профильді өзгертуге болмайды (403 `consent_required`).
  Экспорт пен өшіру келісімсіз де қолжетімді.
- `GET /api/me/export` — студент туралы бәрі бір JSON файлда (`users` жолы толық, еске салулар,
  бот жазбалары, өзі туралы audit жазбалары). Бәрі `db.WithUser` арқылы, яғни RLS аясында.
- `DELETE /api/me` — `users` жолы шынымен өшеді, тәуелді жолдар `on delete cascade` арқылы
  бірге өшеді; `audit_log`/`bot_events`-тегі сілтемелер `null` болады. `audit_log`-қа тек
  анонимді `account_delete` жазбасы қалады (id, аты, себебі жоқ). Cookie тазаланады.

### Жаңа кесте қосқанда (05–10 промпттар)

1. Студенттің жеке жазбасы: `user_id bigint not null references users(id) on delete cascade`.
2. Басқалар тәуелді контенттегі авторлық баған: `created_by bigint references users(id) on delete set null`.
3. RLS қос, `app_user`-ге тек саясат рұқсат ететінін бер.
4. Экспортқа қосу: `me.RegisterExport("events", func(ctx, tx, uid) (any, error) { return me.Rows(ctx, tx, "select * from event_regs where user_id = app_user_id()") })`.
5. Postgres-тен тыс дерек (Storage фотолары, PostHog person): `me.RegisterDeleteHook(...)`.

`pkg/me` тесті (`TestEveryUserReferenceHasDeleteRule`) `users(id)`-ке сілтейтін әр бағанда
`cascade` немесе `set null` бар екенін тексереді, ал `TestDeleteAccount` өшіруден кейін сол
бағандардың бәрінде өшірілген id қалмағанын тексереді — жаңа кестелер автоматты түрде қамтылады.

## Рөлдер

`student` (әдепкі), `ambassador`, `admin`, `university_staff`, `employer`. Рөлді тек admin
береді: `POST /api/admin/role`, себебі міндетті (3–500 таңба), `audit_log`-қа `role_grant`
немесе `role_revoke` (жаңа рөл `student` болса) жазылады: кім, кімге, себеп, `{from, to}`.
Өз рөліңді өзгертуге болмайды. Бірінші admin — `ADMIN_TELEGRAM_IDS`: тізімдегі Telegram id
кіргенде admin болады, бұл `audit_log`-қа бір рет жазылады. Тізімнен алып тастамаса, admin
рөлі алынса да келесі кіруде қайта беріледі.

## Ажыратқыштар (feature flags)

`events, ambassador, mentors, market, portfolio, b2b` — бәрі әдепкі бойынша **өшірулі**.
Қосу: Vercel-де `FEATURE_MARKET=true` (немесе `on`, `1`) қойып, қайта deploy. Код өзгермейді.
Өшірулі функцияның API-і белгісіз маршрутпен бірдей 404 қайтарады:
`features.Require("market", handler)`. Күйлері `GET /api/config` арқылы браузерге жетеді.

## Environment variables

| Айнымалы | Не үшін |
|---|---|
| `DATABASE_URL` | Supabase transaction pooler URL (төменде) |
| `SESSION_SECRET` | cookie қолтаңбасы, кемінде 32 таңба: `openssl rand -base64 48`. Ауыстырсаң — бәрі шығып кетеді |
| `BOT_TOKEN` | initData мен Login Widget hash тексеру (BotFather-ден) |
| `BOT_USERNAME` | `/api/config` арқылы фронтқа (Login Widget `data-telegram-login`) |
| `ADMIN_TELEGRAM_IDS` | үтірмен бөлінген Telegram id, мысалы `123456789,987654321` |
| `FEATURE_EVENTS`, `FEATURE_AMBASSADOR`, `FEATURE_MENTORS`, `FEATURE_MARKET`, `FEATURE_PORTFOLIO`, `FEATURE_B2B` | `true` — функция қосулы |
| `ALLOWED_HOSTS` | Origin тексеру тізімі; әдепкі `studenthub-plus-kz.vercel.app,studenthub-platform.vercel.app,localhost,127.0.0.1` |
| `RATE_WRITE_PER_MIN` | жазу сұраныстарының шегі, әдепкі 30 (IP бойынша да, қолданушы бойынша да) |
| `REPORT_URL`, `SUGGEST_FORM_URL` | `/api/config` арқылы фронтқа |

## Supabase жобасын қосу

Тегін деңгей шектеулері (supabase.com/pricing, **2026-10-28** тексерілді):

- дерекқор: жобаға 500 MB; файл қоймасы: 1 GB; egress: 5 GB (+5 GB кэштелген)
- белсенді жоба: 2; файл жүктеу: ең көбі 50 MB
- **1 апта (7 күн) белсенділік болмаса жоба тоқтатылады (pause)** — қолмен қайта қосу керек
- **автоматты резервтік көшірме жоқ** — өзің `pg_dump` жасап тұру керек
- Edge Function: 500 000 шақыру; Realtime: бір мезгілде 200 қосылым; MAU: 50 000 (Supabase Auth, біз қолданбаймыз)

Қадамдар:

1. supabase.com → New project. Аймақты Қазақстанға жақын таңда (мысалы, Frankfurt `eu-central-1`).
2. Жоба бетінде жоғарыдағы **Connect** → **Transaction pooler** → URL-ді көшір:
   `postgresql://postgres.<PROJECT-REF>:<PASSWORD>@<POOLER-HOST>:6543/postgres`.
   Құпия сөздегі арнайы таңбаларды percent-encode жаса. Transaction pooler prepared
   statement-терді қолдамайды — `pkg/db` оны ескереді (simple protocol).
3. Vercel → Project → Settings → Environment Variables: `DATABASE_URL` = сол URL. Қалған
   айнымалыларды (жоғарыдағы кесте) қос.
4. Миграцияларды іске қос (репозиторий түбінен):

   ```bash
   DATABASE_URL='postgresql://postgres.<REF>:<PASSWORD>@<POOLER-HOST>:6543/postgres?default_query_exec_mode=simple_protocol' \
     go run ./cmd/migrate
   ```

   `default_query_exec_mode=simple_protocol` — pooler арқылы prepared statement-сіз жұмыс істеу
   үшін. Миграция `schema_migrations` кестесіне жазылады, қайта іске қосу қауіпсіз.
5. Supabase-тің Data API (PostgREST) арқылы кестелерге қатынау керек емес: барлық кестеде RLS
   қосулы және `anon`/`authenticated` рөлдеріне саясат жоқ, яғни бәрі жабық.

Локалды тест: `postgres://postgres:dev@localhost:54329/studenthub` (басқа болса
`TEST_DATABASE_URL`). `go test ./pkg/...` әр пакетке жаңа дерекқор (`studenthub_<пакет>_test_<pid>`)
ашып, барлық миграцияны қолданып, соңында өшіреді; Postgres жоқ болса дерекқор тесттері skip болады.

## Telegram Login Widget: домен баптау

Widget тек BotFather-де тіркелген доменде жұмыс істейді:

1. Telegram-да @BotFather → `/setdomain` → ботты таңда.
2. Доменді жібер: `studenthub-plus-kz.vercel.app` (схемасыз, жолсыз). Бір ботқа бір домен ғана —
   preview deploy-лерде widget жұмыс істемейді, онда Mini App арқылы кір.
3. Фронтта widget скрипті: `data-telegram-login="<BOT_USERNAME>"`, `data-request-access="write"`
   (онсыз бот студентке бірінші жаза алмайды), `data-onauth` callback-те алынған объектіні
   өзгертпей `POST /api/auth/telegram/widget`-ке JSON етіп жібер.
4. Mini App үшін: `Telegram.WebApp.initData` жолын `POST /api/auth/telegram/webapp`-қа
   `{initData}` етіп жібер; `Telegram.WebApp.requestWriteAccess()` сұра.

Тексеру: initData — `secret = HMAC_SHA256(key "WebAppData", BOT_TOKEN)`, Widget —
`secret = SHA256(BOT_TOKEN)`; екеуінде де `hash`-тен басқа барлық өріс (жаңа клиенттердегі
`signature` өрісі де) сұрыпталып, `\n` арқылы біріктіріледі. `auth_date` 24 сағаттан ескі болса — 401.
