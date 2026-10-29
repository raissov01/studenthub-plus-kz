# Events API (05-промпт: іс-шаралар және QR арқылы қатысу)

Сервер: `pkg/events`, миграция `supabase/migrations/20261028000006_events.sql`.
Барлық эндпоинт `FEATURE_EVENTS=true` кезінде ғана жұмыс істейді; ажыратқыш өшік болса —
кез келген жол `404 {"error":{"code":"not_found"}}` (жоқ бет сияқты).

## Жалпы ережелер

- Кіру: `sh_session` cookie немесе Mini App ішінде `Authorization: Bearer <token>` (`docs/ACCOUNTS.md`).
- Жазу сұраныстары (POST/PATCH/DELETE) `auth.Write` арқылы өтеді: `Origin` `ALLOWED_HOSTS` ішінде болуы керек
  (әйтпесе `403 bad_origin`), лимит `RATE_WRITE_PER_MIN` (`429 rate_limited`, `Retry-After`).
- Қате пішіні: `{"error":{"code":"<code>","message":"<kk|ru>"}}`; тіл `?lang=ru` не `Accept-Language`.
- Барлық кілттер `snake_case`, уақыт — RFC 3339 (UTC), күн — `YYYY-MM-DD`.
- Жалпы қателер: `401 unauthorized` (кіру керек), `403 consent_required` (келісім экраны өтпеген),
  `403 forbidden`, `404 not_found`, `400 bad_request`, `500 internal`.
- `POST /api/events` — бұл **аналитика трекері** (`pkg/analytics`), сондықтан іс-шара құру `POST /api/events/create`.

### Рөлдер

| Кім | Не істей алады |
|---|---|
| anonymous | тізім, бет, `.ics`, «кім барады» (тек саны), QR сканерлеу (claim жасалады) |
| student (consent бар) | тіркелу / бас тарту, өз тіркелулері, QR арқылы келу, пікір |
| ambassador, university_staff | тек **өз университетінің** (`users.university`) іс-шарасын құру, өзгерту, тіркеу экраны, тізім, CSV, аяқтау |
| admin | бәрі, кез келген университет |
| mentor (07) | TODO: `events.ExtraManager` арқылы тек `cat = mentoring` (07-промптта қосылады) |

### Event объектісі

```json
{
  "id": 12, "title_kk": "Мансап жәрмеңкесі", "title_ru": "Ярмарка карьеры",
  "desc_kk": "...", "desc_ru": null, "cat": "career",
  "starts_at": "2026-11-02T06:00:00Z", "ends_at": "2026-11-02T08:00:00Z",
  "university": "ENU", "building_id": "b1", "place_text": "Акт залы",
  "capacity": 120, "walk_in_allowed": false, "gives_cert": true, "teams_enabled": false,
  "status": "published", "created_by": 5, "checkin_started_at": null, "finished_at": null,
  "created_at": "...",
  "registered": 87, "waitlist": 0, "checked_in": 0, "spots_left": 33,
  "my": {"status": "registered", "ticket": "Vq3...", "checked_in_at": null, "team_no": null, "feedback_score": null},
  "can_manage": false, "is_demo": false
}
```

- `cat`: `career, youth, volunteer, sport, training, conf, holiday, mentoring, other`.
- `status`: `draft` (тек ұйымдастырушыға көрінеді) → `published` → `finished` | `cancelled`.
- `capacity: null` = шексіз; `spots_left: null` = шексіз.
- `is_demo: true` — үлгі іс-шара; сайт оны барлық жерде «үлгі» / «пример» белгісімен көрсетеді (11-025).
- `my` тек кірген адам тіркелген болса; `ticket` — электрондық билеттің коды (QR-ға салу үшін).
- `building_id` — фронттағы `BUILDINGS` картасының id-і (`b1`, `b2` …) → «Кампус картасынан көру».

## Студент жағы

| Method | Path | Who | Body | Response |
|---|---|---|---|---|
| GET | `/api/events?scope=upcoming\|past\|all&university=&limit=` | any | — | `{"events":[Event]}` (upcoming = published, ends_at > now, starts_at бойынша) |
| GET | `/api/events/{id}` | any (draft — тек ұйымдастырушы) | — | `{"event":Event}` |
| GET | `/api/events/{id}/attendees` | any | — | `{"count":87}`; тіркелген не ұйымдастырушыға қосымша `"attendees":[{"name","faculty"}]` |
| GET | `/api/events/{id}/ics` | any | — | `text/calendar` файл («Күнтізбеге қосу») |
| POST | `/api/events/{id}/register` | student+consent | `{"with_previous_team":false}` (міндетті емес) | `{"status":"registered"\|"waitlist","ticket":"…","already":false}` |
| DELETE | `/api/events/{id}/register` | тіркелген | — | `{"ok":true}` |
| GET | `/api/events/my` | signed in | — | `{"registrations":[{"event":Event,"status","ticket","checked_in_at","team_no","feedback_score"}]}` |
| GET | `/api/events/my/attendance` | signed in | — | `{"attendance":[{"event_id","title_kk","title_ru","cat","university","starts_at","checked_in_at","has_certificate","verified":true}]}` |
| POST | `/api/events/{id}/feedback` | келген (checked in) | `{"score":1..5,"text":"≤500, міндетті емес"}` | `{"ok":true}` |

Қателер:
- register: `404 not_found`, `409 closed` (published емес не біткен), `403 consent_required`.
  Екінші рет тіркелу қате емес: `200` + `"already": true` (жол қосылмайды).
- unregister: `409 closed` (басталып кеткен), `404 not_registered`. Бос орын шықса, күту тізіміндегі
  бірінші адам автоматты тіркеледі және `direct` хабарлама алады.
- feedback: `409 not_finished`, `403 not_attended`, `400 bad_request` (score 1–5 емес).

Орын / күту тізімі Postgres-тегі `event_register(...)` RPC ішінде іс-шара жолын `for update` бұғаттап
шешіледі — қатар келген тіркелулер `capacity`-ден аспайды (тест: 30 горутина, 5 орын).

`/api/events/my/attendance` — портфолио (09) үшін «Расталған ✓» дереккөзі: тек `checked_in_at` бар жолдар
(`verified_attendance` view). Студент оны қолмен өзгерте алмайды (RLS). Go: `events.VerifiedAttendance(ctx, userID)`.

## QR арқылы келу

| Method | Path | Who | Body | Response |
|---|---|---|---|---|
| POST | `/api/events/checkin` | any (signed in or not) | `{"token":"ci_…"}` | кірген: `200 CheckinResult`; кірмеген: `202 {"status":"login_required","claim":"…","expires_at":"…"}` |
| POST | `/api/events/checkin/claim` | signed in + consent | `{"claim":"…"}` | `200 CheckinResult` |

`CheckinResult`: `{"status":"checked_in"|"already","team_no":3|null,"method":"qr"|"walk_in"|"manual","message":"Қош келдің! ✅"}`.

Қателер: `400 token_invalid` (қолтаңба қате / басқа іс-шара / тіркеу қайта басталған),
`400 token_expired` (скриншот), `409 checkin_closed` (басталуға 30 мин қалғаннан ерте не біткен),
`403 not_registered` (тіркелмеген және `walk_in_allowed = false`), `403 checkin_revoked`
(ұйымдастырушы «растамау» басқан), `410 claim_expired` (claim 10 мин өтті не қолданылған).

Механика:
- QR ішінде: `https://t.me/<BOT_USERNAME>/<MINIAPP_NAME>?startapp=ci_<token>` (`deep_link`).
  `MINIAPP_NAME` бос болса — `https://t.me/<BOT_USERNAME>?startapp=ci_<token>` (бас Mini App);
  `BOT_USERNAME` бос болса — `web_link` (`SITE_URL/#/checkin?t=ci_<token>`).
  `startapp` тек `A-Z a-z 0-9 _ -`, ≤ 64 таңба; біздікі `ci_` + 32 = 35 таңба.
- Token = base64url(event_id 8B | минут терезесі 4B | HMAC-SHA256 12B), кілт `SESSION_SECRET` + іс-шараның
  `checkin_key` (32 кездейсоқ байт, «Тіркеуді бастау» сайын жаңарады). 60 с сайын жаңа token; алдыңғысы
  тағы 30 с жарамды. Скриншот ең көбі 90 с ішінде өледі.
- Mini App `startapp` параметрін алып (`Telegram.WebApp.initDataUnsafe.start_param`), `initData` арқылы кіріп,
  `POST /api/events/checkin` жібереді. Браузерде кірмеген болса — `claim`-ды сақтап, кіргеннен кейін
  `/checkin/claim`-ға жібереді.
- Бір адам — бір рет: қайталау `already` қайтарады.

**Адал шектеу** (ұйымдастырушы экранында көрсетілсін, `notice_kk`/`notice_ru`): залдағы біреу тірі QR-ды сол
сәтте сырттағы досына жібере алады. Ұйымдастырушы тірі санақты залдағы адамдармен салыстырады және күмәнді
тіркелуді «Растамау» (`/checkin/revoke`) арқылы алып тастайды.

## Ұйымдастырушы жағы

Барлығы: кірген + `can_manage` (admin / өз университетінің ambassador не university_staff). Бөтен университет → `403 forbidden`.

**university_staff — тек жиынтық сандар (10-004).** Қызметкер өз университетінің іс-шарасын құрып,
QR арқылы тіркеуді жүргізе алады, бірақ жеке студент дерегін еш жерден алмайды:
`/regs` → `regs: []`, `aggregate_only: true` (санағыштар бар, 1–4 → `"< 5"`); `/attendees` → тек `count` (1–4 → `"< 5"`);
`/attendees.csv` → `metric,value` жиынтық CSV (registered, waitlist, checked_in, cancelled,
checkin_revoked, ratings, avg_rating, `checked_in_faculty:<факультет>`; 1–4 → `< 5`; 5-тен аз адамы бар
факультет аталмайды — бәрі бір `checked_in_faculty:other` жолына қосылады, 10-032); `/feedback` →
пікірлер жоқ, бөлу мен орташа баға тек 5 жауаптан бастап, `count` 1–4 болса `"< 5"`;
`/checkin/manual` — тек `ticket` (студент көрсеткен электронды билет коды), `user_id` → `403 aggregate_only`;
`/checkin/revoke` → `403 aggregate_only`. Амбассадор, бекітілген ментор (өз кездесуі) және admin — бұрынғыдай толық тізім.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/events/manage` | — | `{"events":[Event]}` (draft қоса) |
| POST | `/api/events/create` | Event өрістері: міндетті `title_kk` (3–140), `cat`, `starts_at`, `ends_at`; қалғаны міндетті емес: `title_ru`, `desc_kk/ru` (≤3000), `university` (тек admin; басқаларға өз университеті), `building_id`, `place_text`, `capacity`, `walk_in_allowed`, `gives_cert`, `teams_enabled`, `status` (`draft`\|`published`) | `201 {"event":Event}` |
| PATCH | `/api/events/{id}` | жоғарыдағы өрістердің кез келгені; `status`: `draft→published\|cancelled`, `published→cancelled` | `{"event":Event}` |
| POST | `/api/events/{id}/checkin/start` | — | `LiveView` (жаңа кілт — ескі QR өледі) |
| GET | `/api/events/{id}/checkin/live` | — | `LiveView` — экран 3–5 с сайын сұрайды |
| POST | `/api/events/{id}/checkin/manual` | `{"user_id":7}` не `{"ticket":"…"}` | `CheckinResult` (интернет нашар болғанда резерв) |
| POST | `/api/events/{id}/checkin/revoke` | `{"user_id":7}` | `{"ok":true}` («Растамау») |
| GET | `/api/events/{id}/regs` | — | `{"registered","checked_in","waitlist","regs":[{"user_id","name","username","faculty","course","status","ticket_short","registered_at","checked_in_at","checkin_method","checkin_revoked","team_no","feedback_score"}]}` |
| POST | `/api/events/{id}/teams` | — | `{"teams":2}` — келгендерді қайта бөлу |
| POST | `/api/events/{id}/finish` | — | `{"status":"finished","certificates":N}` |
| GET | `/api/events/{id}/attendees.csv` | — | `text/csv` (UTF-8 BOM, формула инъекциясынан қорғалған) |
| GET | `/api/events/{id}/feedback` | — | `{"count","average","distribution":[1★..5★],"comments":[{"score","text","at"}]}` (атсыз) |

`LiveView`: `{"token":"ci_…","deep_link","web_link","rotates_in_sec":42,"open":true,"registered","checked_in","waitlist","notice_kk","notice_ru"}`.
Экран `rotates_in_sec` біткенде (не жай ғана әр 3–5 с) `/live`-ты қайта сұрап, QR-ды жаңартады.

Қателер: `400 invalid_event` (өріс қате, `university` өзгерту, `status: "finished"` — ол тек `/finish`),
`400 invalid_status` (`cancelled` күйінде құру), `409 invalid_status` (рұқсатсыз күй ауысуы, біткен/бас тартылған іс-шараны өзгерту), `403 foreign_university`, `403 no_university` (профильде университет жоқ),
`409 capacity_below_registered`, `409 checkin_not_started`, `404 ticket_not_found`, `404 not_checked_in`, `409 not_started` (finish ерте).

Ескертпелер:
- `capacity` көбейсе — күту тізімі автоматты жоғары жылжиды (`direct` хабарлама). `cancelled` — тіркелгендердің
  бәріне `direct` хабарлама.
- `finish`: `gives_cert` болса, келгендердің бәріне `event_certificates` жолы (портфолио оқиды).
  `checkin_key` өшіріледі — ескі QR жұмыс істемейді.
- Шағын топтар: `teams_enabled` болса, әр келген адам бірден командаға түседі (`team_no`): «бұрынғы командаммен»
  таңдаған болса — бұрынғы командаластары бар топқа, әйтпесе өз факультеті ең аз ашық топқа; барлық топ ≥5
  болғанда жаңа топ ашылады (макс 6). `POST /teams` барлығын `ceil(n/6)` теңгерілген топқа (4–6, n=7-ден басқа)
  факультеттерді араластырып қайта бөледі.

## Күнделікті тапсырма (cron)

`GET /api/cron/events` — `Authorization: Bearer <CRON_SECRET>` (`401` әйтпесе). Go: `events.Daily(ctx, now)`.
- Ертең басталатын іс-шараға тіркелгендерге еске салу (1 күн бұрын).
- Кеше біткен іс-шараға келгендерге бір сұрақ (1–5 + «не ұнады / не жетіспеді?»).
- Ботпен ортақ «күніне 1 хабарлама» ережесі: `bot_daily_sends` жолын алу арқылы; алынбаса — бүгін жіберілмейді.
- Ескі `event_checkin_claims` тазаланады.
Жауап: `{"reminders","reminder_skips","feedback_asks","claims_pruned"}`.
`pkg/bot` өзгертілмеді — бот cron-ы оны шақыруы не Vercel/systemd cron-ға жеке жол қосу керек.

## Деректер және өшіру

- RLS: студент тек өз `event_regs` жолдарын оқиды; ұйымдастырушы — өз университетінің іс-шараларынікін;
  `events.checkin_key` app_user-ге мүлде берілмейді; тіркелу тек RPC арқылы.
- `/api/me/export`: `eventRegistrations`, `eventCertificates` бөлімдері.
- Аккаунт өшсе: `event_regs`, `event_certificates` cascade өшеді; `events.created_by` → `null` (іс-шара қалады).

## Env

| Var | Мағынасы |
|---|---|
| `FEATURE_EVENTS` | ажыратқыш |
| `BOT_USERNAME`, `MINIAPP_NAME` | QR сілтемесі (`t.me/<bot>/<app>?startapp=`) |
| `SESSION_SECRET` | QR token қолтаңбасы (сессиямен ортақ) |
| `CRON_SECRET` | `/api/cron/events` |
| `SITE_URL` | хабарламалардағы сілтемелер |

## Frontend checklist (05)

- [ ] `VIEWS.opps('events')`: `GET /api/events` — бос болса не студент кірмеген болса демо `EVENTS` «үлгі» белгісімен.
- [ ] `VIEWS.event`: бет, «Кампус картасынан көру» (`building_id`), «Кім барады» (сан / тіркелгенге аттар).
- [ ] «Тіркелу» → кірмеген болса кіру ұсынысы; `waitlist` → «Орын жоқ — күту тізіміндесіз».
- [ ] Электрондық билет (`my.ticket` QR), «Тіркеуден бас тарту», «Күнтізбеге қосу» (`/ics`).
- [ ] Ұйымдастырушы: іс-шара құру/өзгерту формасы, «Тіркеуді бастау» → толық экранды QR (`/checkin/live`
      поллинг), тірі санақ тіркелген/келген, адал шектеу мәтіні, «Растамау», резерв тізім (`/regs` + `manual`).
- [ ] Mini App: `start_param` `ci_…` болса — бірден `POST /api/events/checkin` → «Қош келдің! ✅» + команда нөмірі.
- [ ] Браузер: `#/checkin?t=ci_…` → кірмеген болса `claim` сақтап, кіргеннен кейін `/checkin/claim`.
- [ ] Баптауда «Командаларға бөлу»; тіркелу кезінде «бұрынғы командаммен» белгісі; «Қайта бөлу» батырмасы.
- [ ] «Аяқтау», келгендер тізімі + CSV, пікір нәтижесі; студентке пікір формасы (1–5 + мәтін).
- [ ] Портфолио: `/api/events/my/attendance` → «Расталған ✓».
