# Аккаунттар: кіру, сессия, келісім, деректер, рөлдер, ажыратқыштар

Аккаунт міндетті емес: сайт кірмей-ақ толық жұмыс істейді. Аккаунт тек ортақ дерекқор керек
әрекеттерге (тіркелу, жариялау, өтінім т.б.) қажет. Кірудің **екі жолы** бар: Telegram
(Mini App initData не Login Widget) және **email + құпиясөз** (`pkg/auth/email.go`, хаттар —
`pkg/mail`, Resend). Екеуі де бір `users` жолына, бір `sh_session` cookie-ге апарады.
Толық API: [docs/api/email-auth.md](api/email-auth.md). Код: `pkg/auth`, `pkg/me`, `pkg/admin`,
`pkg/features`; маршруттар `pkg/server/routes_auth.go`-та қосылады.

## Эндпоинттер

| Жол | Әдіс | Кім | Rate limit |
|---|---|---|---|
| `/api/auth/telegram/webapp` | POST `{initData}` | кез келген (Mini App) | IP бойынша 30/мин |
| `/api/auth/telegram/widget` | POST (Login Widget өрістері) | кез келген (браузер) | IP бойынша 30/мин |
| `/api/auth/logout` | POST (`{all:true}` — барлық құрылғыдан) | кез келген | IP + қолданушы 30/мин |
| `/api/auth/email/register` | POST `{email, password, lang, ref?}` | кез келген | + хат: IP 20/сағ, адрес 5/сағ |
| `/api/auth/email/verify` | POST `{email, code, password}` | кез келген | + код: IP 30/15мин, адрес 10/15мин |
| `/api/auth/email/login` | POST `{email, password}` | кез келген | + кіру: IP 30/15мин, адрес 10/15мин |
| `/api/auth/email/resend` | POST `{email, purpose?}` | кез келген | + хат шегі |
| `/api/auth/email/reset/request` | POST `{email}` | кез келген | + хат шегі |
| `/api/auth/email/reset/confirm` | POST `{email, code, newPassword}` | кез келген | + код шегі |
| `/api/me/password` | POST `{currentPassword, newPassword}` | кірген, құпиясөзі бар | + қолданушы 10/15мин |
| `/api/me/email` | POST `{email, password \| currentPassword}` | кірген | + хат шегі |
| `/api/me/email/confirm` | POST `{code}` | кірген | + код шегі |
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

`users` кестесінде (бір адам = бір жол, бот пен сайт ортақ):

- `telegram_id` (email арқылы ғана тіркелгенде **null**), `tg_username`, `name` (Telegram-нан,
  кейін өзгертуге болады), `lang` (kk/ru)
- `email` (кіші әріппен, бос орынсыз сақталады; регистрге тәуелсіз бірегей), `email_verified_at`
  (код енгізілгенге дейін null — мұндай «аяқталмаған тіркелу» жолына сессия берілмейді)
- `university`, `faculty`, `course` (1–6), `city` — студент өзі толтырса ғана
- `role`, `consent_at` (келісім уақыты), `is_adult` (18-ге толды ма, сұралмаса null)
- `session_version` («барлық құрылғыдан шығу» үшін), `last_seen_at` (соңғы кіру), `created_at`
- боттың күйі: `bot_started_at`, `bot_inactive`, `paused_until`, `digest_city`, `digest_direction` т.б.

Сонымен қатар: `user_passwords` (argon2id хэші, тек сервер оқиды), `auth_codes` (бір реттік
кодтардың HMAC-ы), `reminders` (еске салулар), `bot_daily_sends` (бот хабарламасы жіберілген күндер),
`bot_events` (белгіледім/жасырдым/тіркелдім оқиғалары), `audit_log` (рөл өзгерістері).

**GPA мен телефон серверде ЕШҚАШАН сақталмайды** — олар тек құрылғыда (`localStorage`).
`PATCH /api/me` оларды жіберсе, 400 `not_stored` қайтарады.

Сессия — `sh_session` cookie: `base64url(JSON{u: id, v: session_version, e: мерзім, i: берілген уақыт,
a: кірген уақыт, s: сессия id}) + "." + base64url(HMAC-SHA256(SESSION_SECRET, payload))`. HttpOnly, Secure
(localhost-тан басқа), SameSite=Lax, Path=/. Әр сұраныста `role`, `session_version` және `deleted_at`
дерекқордан қайта оқылады: рөл алынса немесе аккаунт бұғатталса — бірден күшіне енеді.
**Бір құрылғыдан шығу** (`POST /api/auth/logout`, `all` жоқ): токеннің `s` id-і (кездейсоқ, жаңартуда
сақталады) `revoked_sessions` кестесіне жазылады, сол сессияның кез келген көшірмесі (cookie, Bearer,
жаңартылған `X-Session-Token`) бұдан былай 401 алады; басқа құрылғылар кірген күйі қалады. `s`-сыз ескі
токендер `u<id>.a<кірген уақыт>` атымен тоқтатылады. Мерзімі өткен жолдар келесі шығуда өшеді.
`{all:true}` — `session_version + 1`, барлық құрылғы шығады.
Mini App (Telegram Web iframe-де cookie жетпеуі мүмкін) кіргенде жауапта `token` алады, оны тек
жадта ұстап, `Authorization: Bearer <token>` етіп жібереді.

### Сессия мерзімі (04-014…017)

Тұрақтылар `pkg/auth/session.go`-да:

| Рөл | Мерзім | Ұзара ма |
|---|---|---|
| `student`, `ambassador` | `StudentSessionTTL` = 90 күн | **иә** — белсенді студент жүйеден өзінен-өзі шықпайды |
| `admin`, `university_staff`, `employer` | `StaffSessionTTL` = 14 күн | **жоқ** — кірген сәттен 14 күн, белсенділікке қарамай |

- **Сырғымалы сессия.** `auth.Need` (кіруді талап ететін әр эндпоинт) және `GET /api/me` сұранысында
  студент/амбассадор токені өмірінің жартысынан ескі болса (90 күнде — 45 күннен; әрі кемінде
  `RefreshMinInterval` = 1 тәулік) жаңа 90 күндік токен беріледі. Яғни 45 күнде бір рет болса да
  кірген студент ешқашан шығарылмайды. `session_version` сол күйі қалады — «Барлық құрылғыдан шығу»
  жаңартылған токендерді де тоқтатады; `a` (алғашқы кіру уақыты) сақталады.
  - cookie арқылы келсе — жаңа `Set-Cookie` (сол жалаулармен);
  - `Authorization: Bearer` арқылы келсе (Mini App) — жауапта **`X-Session-Token: <жаңа токен>`**
    тақырыбы (`Access-Control-Expose-Headers`-те бар). Frontend: әр API жауабынан осы тақырыпты
    оқып, бар болса жадтағы токенді ауыстырсын (localStorage-ке жазылмайды).
- **Қызметкер рөлдері** 14 күннен соң қайта кіреді. Тексеру токеннің `a` уақытымен рөлге қарай әр
  сұраныста жүреді: студент кезінде алған 90 күндік токенмен admin/staff/employer рөлі берілсе де,
  кіргеніне 14 күннен асса — токен жарамсыз.
- Бұрынғы (`i`/`a` жоқ) токендер 30 күндік ережемен берілген деп саналады: `i = e − 30 күн`.

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
| `FEATURE_EVENTS`, `FEATURE_AMBASSADOR`, `FEATURE_MENTORS`, `FEATURE_MARKET`, `FEATURE_PORTFOLIO`, `FEATURE_B2B`, `FEATURE_QUIZ` | `true` — функция қосулы |
| `ALLOWED_HOSTS` | Origin тексеру тізімі; әдепкі `studenthub-plus-kz.vercel.app,studenthub-platform.vercel.app,localhost,127.0.0.1` |
| `RATE_WRITE_PER_MIN` | жазу сұраныстарының шегі, әдепкі 30 (IP бойынша да, қолданушы бойынша да) |
| `REPORT_URL`, `SUGGEST_FORM_URL` | `/api/config` арқылы фронтқа |
| `RESEND_API_KEY` | Resend API кілті (`re_...`); жоқ болса email арқылы кіру өшік (`emailAuth:false`) |
| `MAIL_FROM` | жіберуші, мысалы `Student Hub <noreply@mail.studenthub.kz>` — Resend-те расталған доменнен |
| `MAIL_DEV_LOG` | `true` — кілт жоқта хатты (кодымен!) сервер логына жазады. **Тек локалды әзірлеу**, әдепкі `false` |
| `RESEND_API_BASE` | тесттерге арналған; әдепкі `https://api.resend.com` |

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

## Email + құпиясөз арқылы кіру

### Ағындар

- **Тіркелу**: `register` → поштаға 6 таңбалы код → `verify {email, code, password}` → сессия
  (Telegram-дағыдай cookie, `needsConsent:true`, яғни келісім экраны міндетті). `verify`
  құпиясөзді де сұрайды: басқа адам бөтен адреске алдын ала құпиясөз қойып қойса да, кодты
  енгізген адам өз құпиясөзін білмей кіре алмайды (pre-hijack қорғанысы).
- **Кіру**: `login`. Адрес расталмаған болса және құпиясөз дұрыс болса — жаңа код жіберіледі,
  403 `email_not_verified` (фронт код экранына өтеді).
- **Құпиясөзді қалпына келтіру**: `reset/request` → код → `reset/confirm`. Сәтті болса
  `session_version + 1` — барлық басқа құрылғылар шығады, осы құрылғыға жаңа cookie беріледі,
  поштаға «құпиясөз өзгерді» хабары кетеді.
- **Құпиясөзді ауыстыру** (кірген): `POST /api/me/password` — ескі құпиясөз керек, басқа
  құрылғылар шығады.
- **Telegram аккаунтына email қосу**: Баптауларда `POST /api/me/email {email, password}` → код
  → `POST /api/me/email/confirm {code}`. Құпиясөзі бар аккаунт адресті ауыстырғанда
  `currentPassword` жібереді; адрес ауысса басқа құрылғылар шығады.
- **Email аккаунтына Telegram қосу**: email-мен кіріп тұрып, қалыпты Telegram кіруін (widget
  не Mini App) жасау жеткілікті — жаңа аккаунт ашылмайды, `telegram_id` осы аккаунтқа жазылады
  (`audit_log`: `telegram_link`). Ол Telegram id басқа жолда тұрса — **409 `telegram_taken`**,
  аккаунттар ешқашан үнсіз біріктірілмейді. Басқа жол тек боттан қалған болса (келісімсіз,
  email-сіз), хабарламада ботқа `/stop` жіберу айтылады — ол бот деректерін өшіреді, содан
  кейін қайта қосуға болады.

### Қауіпсіздік

- Құпиясөз: **argon2id** (m=19 MiB, t=2, p=1, 16 байт тұз, PHC форматы), тексеру constant-time.
  Параметрлер өзгерсе, келесі кіруде хэш жаңа параметрмен қайта жасалады. Бір мезгілде ең
  көбі 4 хэш есептеледі (жадты қорғау). Ережелер: 8–128 таңба, бір ғана таңбаның қайталануы
  емес, жиі кездесетін құпиясөздер тізімінде емес, email-ге (не @ алдындағы бөлігіне) тең емес.
- Хэштер бөлек `user_passwords` кестесінде: `select * from users` (экспорт, `app_user`) оларды
  ешқашан қайтармайды. `user_passwords` пен `auth_codes`-та RLS қосулы, `app_user`-ге бірде-бір
  құқық жоқ. `app_user` өз `email`-ін де өзгерте алмайды.
- Код: `crypto/rand`, 6 сан, **15 минут**, ең көбі **5 қате әрекет** (5-ші қатеден кейін код
  өшеді), бір мақсатқа бір тірі код, жаңа код 60 секундтан ерте жіберілмейді. Базада тек
  `HMAC-SHA256(SESSION_SECRET-тен туындатылған кілт, мақсат|user|email|код)` сақталады.
  Код хат тақырыбында ешқашан болмайды.
- Аккаунт бар-жоғы ашылмайды: бос емес адреспен тіркелу, белгісіз адреске reset/resend бірдей
  `200 {ok:true, resendAfter:60}` қайтарады (адрес иесіне кодсыз ескерту хаты кетеді); қате
  email мен қате құпиясөз — бір `401 invalid_credentials`, белгісіз адрес те бір argon2id
  тексеруін жасайды; қате/ескі/таусылған/жоқ код — бір `400 invalid_code`. Хаттар фонда
  жіберіледі, сондықтан жауап уақыты хат жіберілгенін көрсетпейді.
- Шектеулер `rate_limits` кестесінде; адрестің орнына оның HMAC-ы сақталады. Шектен асса —
  429 + `Retry-After`. Бір адресті біреу қасақана «бұғаттауы» мүмкін (15 минутқа) — бұл
  құпиясөз іріктеуден қорғаудың бағасы; Telegram арқылы кіру сонда да жұмыс істейді.
- Аяқталмаған тіркелу (код енгізілмеген, Telegram жоқ, келісім жоқ) ешқашан сессия алмайды.
  Сол адресті нақты иесі өз аккаунтына қосса, ондай жол өшіріледі.
- `pkg/notify`: Telegram-сыз аккаунтқа ештеңе жіберілмейді (`Direct` үнсіз өткізеді, `Send`
  `off` қайтарады, outbox толмайды); бот cron-ы оларды көрмейді; `/stop` email-і бар аккаунтты
  өшірмейді.
- Аккаунтты өшіру: `users` жолымен бірге `user_passwords`, `auth_codes` cascade арқылы өшеді.

### Resend баптау

1. resend.com → тіркел → **Domains → Add domain**. Негізгі доменнің ішкі доменін ал
   (мысалы `mail.studenthub.kz`), сонда негізгі доменнің беделі қорғалады.
2. Resend көрсеткен DNS жазбаларын доменнің DNS-іне қос: SPF (`TXT` + `MX` — `send.` ішкі
   доменіне), DKIM (`resend._domainkey` `TXT`), ұсынылатын DMARC (`_dmarc` `TXT`,
   мысалы `v=DMARC1; p=none;`). **Verify** басып, статус `Verified` болғанша күт.
3. **API Keys → Create**: рұқсаты `Sending access`, домені — сол домен. Кілтті тек серверде сақта.
4. Серверде (VPS `studenthub-api.service` environment файлы):
   `RESEND_API_KEY=re_...`, `MAIL_FROM="Student Hub <noreply@mail.studenthub.kz>"`.
   `MAIL_DEV_LOG` қойма (не `false`). API-ді қайта іске қос.
5. Тексеру: `GET /api/config` → `"emailAuth": true`; өз адресіңмен тіркеліп, хат келгенін көр.
   Тегін деңгей шектеулерін resend.com/pricing-тен тексер.

Кілт жоқ, `MAIL_DEV_LOG` жоқ болса: `emailAuth:false`, код жіберетін эндпоинттер
`503 email_auth_off` («Email арқылы кіру әзірге қосылмаған») қайтарады. Бұрын расталған
аккаунттар email+құпиясөзбен кіре береді (кіруге хат керек емес).
