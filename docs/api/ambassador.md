# Ambassador API (06-промпт: амбассадор кабинеті және ортақ модерация кезегі)

Сервер: `pkg/ambassador` (+ ортақ кезек пен шағымдар `pkg/moderation/http.go`), миграция
`supabase/migrations/20261028000007_ambassador.sql` (`submissions`, `reports` — 000005-те).
Барлығы `FEATURE_AMBASSADOR=true` кезінде; өшік болса кез келген жол `404 not_found` — **admin кезегі мен
шағымдар да** осы ажыратқышпен (07/08/10 оны қолданғанда бұл туралы шешім керек, төмендегі сұрақтарды қара).

Жалпы ережелер `docs/api/events.md`-дағыдай: cookie / Bearer, `auth.Write` (Origin + rate limit),
`{"error":{"code","message"}}`, `snake_case`, `401 unauthorized`, `403 consent_required`, `500 internal`.

## Өтінім (1-кезең)

| Method | Path | Who | Body | Response |
|---|---|---|---|---|
| POST | `/api/ambassador/apply` | signed in + consent | `{"university":"ENU","motivation":"10–300 таңба","hours_per_week":"1-2"\|"2-3"\|"3-5"}` (`university` бос болса профильдегісі) | `201 {"application":{"id","university","motivation","hours_per_week","status":"pending","created_at"}}` |
| GET | `/api/ambassador/application` | signed in | — | `{"application":{… ,"status":"pending"\|"approved"\|"rejected","reason","reviewed_at"}\|null}` (ең соңғысы) |

Қателер: `400 invalid_application`, `409 already_pending`, `409 already_ambassador`.

Өтінім `submissions` кезегіне `kind = ambassador_application` болып түседі. Бекітілгенде (бір транзакцияда):
`ambassador_terms` жолы (бос болса `lead`, әйтпесе `member`; университетте ең көбі 1 lead + 3 member,
5-ші → `409 team_full`, өтінім `pending` күйінде қалады), мерзім = бүгіннен семестр соңына дейін
(1 ақпан – 30 маусым аралығында → 30 маусым, әйтпесе → 31 қаңтар), `hours_per_week` = 2/3/5, `ref_code` (бұрын болса — сол код),
`users.role = ambassador` (admin өз рөлін сақтайды), `users.university` = өтінімдегі университет, `audit_log`.
Авторға `direct`: «амбассадорсыз + `#/ambassador` сілтемесі». Қайтарылса — себебімен `direct`.

## Кабинет (2-кезең) — тек белсенді амбассадор

«Белсенді» = `role` ambassador/admin **және** `ambassador_terms.status = active` **және** `term_end >= бүгін`
(Алматы). Мерзімі біткен амбассадор cron жүрмей тұрып та кабинетке кірмейді: `403 not_ambassador`.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/ambassador/me` | — | см. төмен |
| GET | `/api/ambassador/stats` | — (`?university=` тек admin; басқа университет → `403 foreign_university`) | `UniversityStats` |
| POST | `/api/ambassador/pause` | `{"weeks":1\|2}` (міндетті емес) | `{"paused_until":"2026-11-01"}` («Демалыс аптасы», жексенбіге дейін) |
| DELETE | `/api/ambassador/pause` | — | `{"paused_until":null}` |
| POST | `/api/ambassador/renew` | `{"continue":true\|false}` | `{"continue":true,"submission_id":42}`; `409 already_pending` |

`GET /api/ambassador/me`:
```json
{
  "term": {"id":3,"user_id":7,"university":"ENU","role_in_team":"lead","term_start":"…","term_end":"…",
           "hours_per_week":3,"ref_code":"k7m2p9qa","status":"active","paused_until":null,"renew_answer":null,
           "cert_code":null,"cert_issued_at":null},
  "days_left": 63,
  "renewal": {"ask": false, "answer": null},
  "referral": {"code":"k7m2p9qa","ref":"amb_k7m2p9qa","link":"https://…/?ref=amb_k7m2p9qa","joined":12},
  "workload": {"hours_per_week":3,"items_this_week":1,"paused_until":null,"paused":false,"available":true},
  "team": [{"name","username","role_in_team","term_start","term_end","hours_per_week","paused"}],
  "resources": [{"title_kk","title_ru","url"}]
}
```
- `referral.joined` — `users.referred_by = 'amb_<code>'` саны (аналитикадан емес; өзі есептелмейді).
  `referred_by` кіру кезінде жазылады: фронт сақталған `?ref=`-ді `POST /api/auth/telegram/webapp`
  (`{"initData","ref"}`) не `/widget` (`{..., "ref"}`) денесіне қосады; тек `share` не `amb_<code>`,
  тек тіркелу сәтінде (келісім берілмеген аккаунт) және `referred_by` бос болса.
- `renewal.ask` — мерзімге ≤14 күн қалғанда және жауап жоқ кезде «жалғастырасың ба?» сұрағы.
  `continue: true` → `ambassador_renewal` кезекке; admin бекітсе `term_end` келесі семестр соңына ұзарады.
  `false` → мерзім `term_end`-де бітеді.
- `resources` — `AMBASSADOR_RESOURCES` env (JSON массив) не placeholder.
- `workload`: аптасына бір пайдалы әрекет (ұсыныс не іс-шара) ≈ 1 сағат. `available = !paused && items_this_week < hours_per_week`.
  Тапсырма жүйесі әзірге жоқ — болашақтағы кез келген тапсырма/еске салу `ambassador.Available(ctx, userID)`-ді тексеруі керек.
  Амбассадордың өз бастамасымен қосуына шек қойылмайды.

`UniversityStats` (тек жиынтық сандар; 1–4 болса `"< 5"` жолы қайтады):
```json
{"university":"ENU","students":412,"weekly_active":96,"saved_deadlines":230,
 "events":7,"registrations":310,"check_ins":201,"by_faculty":{"IT":120,"Физика":"< 5"}}
```
(іс-шаралар — соңғы 90 күн.)

Амбассадордың іс-шаралары: `docs/api/events.md` (`/api/events/manage`, `/api/events/create`).

## Ұсыныстар (мүмкіндік / пайдалы орын)

| Method | Path | Who | Body | Response |
|---|---|---|---|---|
| POST | `/api/submissions` | signed in + consent (студент те, амбассадор да) | `{"kind":"opportunity"\|"place","university":"…?","payload":{…}}` | `201 {"id":42,"status":"pending"}` |
| GET | `/api/submissions/my` | signed in | — | `{"submissions":[{"id","kind","status","reason","university","payload","created_at","reviewed_at"}]}` («Менің ұсыныстарым», барлық түр) |

- Амбассадордың ұсынысы әрқашан өз университетіне; басқа `university` → `403 foreign_university`.
  Студентте `university` — жіберілгені не профильдегісі.
- Автоматты жариялау жоқ: тек admin бекіткеннен кейін `/api/content`-те шығады.
- Валидация қатесі: `400 invalid_<өріс>` (мысалы `invalid_source_url`, `invalid_deadline`).

`opportunity` payload: `type` (`mobility|intl|company|grant`), `title_kk` 3–140, `title_ru`, `org_kk` 2–140, `org_ru`,
`desc_kk` 10–2000, `desc_ru`, `source_url` (**https, міндетті** — ресми дереккөз), `deadline` (`YYYY-MM-DD` ≥ бүгін, міндетті емес),
`city` (`content.Cities`, әдепкі `other`), `funded` bool, `free` bool (ақы сұрамайды).

`place` payload: `cat` (`canteen|cafe|print|copy|library|shop|pharmacy|sport|cowork|other`), `name_kk` 2–120, `name_ru`,
`address` 3–200, `building_id`, `desc_kk/ru` ≤1000, `hours` ≤100, `source_url` (https, міндетті емес).

Бекітілгенде `community_content`-ке көшеді (автор аккаунтын өшірсе не мерзімі бітсе де қалады) және
`GET /api/content` нәтижесіне қосылады:
- `opportunities[]` соңына: Sheets жазбасының пішіні + `"source":"ambassador"`, `"university"`, `"free"`; `id = "amb-<n>"`.
- жаңа `places[]` кілті (тек бекітілген орын болса; әйтпесе кілт жоқ — Sheets/JSON шығысы өзгермейді):
  `{"id":"amb-<n>","cat","name":{"kk","ru"},"desc":{"kk","ru"},"address","building_id","hours","source_url","posted_at","university","source":"ambassador"}`.
- мерзімі өткендер сол `FilterActive` ережесімен түседі; `FEATURE_AMBASSADOR` өшік болса қосылмайды.

## Ортақ модерация кезегі (3-кезең) — тек admin

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/admin/queue?kind=&status=pending\|approved\|rejected&limit=50&after=<id>` | — | `{"items":[QueueItem],"pendingByKind":{"opportunity":3,…}}` |
| POST | `/api/admin/queue/{id}/approve` | `{"reason":"міндетті емес ескертпе ≤500"}` | `{"submission":{…},"notified":true}` |
| POST | `/api/admin/queue/{id}/reject` | `{"reason":"3–500 таңба, МІНДЕТТІ"}` | `{"submission":{…},"notified":true}` |

- `pending` ескісі бірінші (`created_at, id`; келесі бет `after=<соңғы id>`); шешілгендер — жаңасы бірінші.
- `QueueItem`: `{"id","kind","payload","submitted_by","university","status","reason","created_at","reviewed_at","reviewed_by","age_hours","author":{"id","name","username","role","university"},"checklist":{"hasOfficialSource","noFee","deadlineOk"}}` — `checklist` тек `opportunity`-де (бекітер алдындағы шағын тізім).
- Түрлер: `opportunity`, `place`, `ambassador_application`, `ambassador_renewal`, кейін 07/08/10 өздерінікін
  (`moderation.OnApprove/OnReject/Notice`) қосады — эндпоинт бәріне ортақ.
- Шешім + kind hook + `audit_log` (`moderation_approve|reject`) бір транзакцияда; кейін авторға `notify.Direct`
  (түрге арналған мәтін не жалпы: «бекітілді» / «қайтарылды. Себебі: …»).
- Қателер: `400 reason_required`, `409 not_pending` (бұрын шешілген), `409 <hook code>` (мысалы `team_full`,
  `already_ambassador`, `author_deleted`, `term_not_found`) — бұл кезде ештеңе өзгермейді.

## Шағымдар (ортақ ереже, 08 және 10 қолданады)

| Method | Path | Who | Body | Response |
|---|---|---|---|---|
| POST | `/api/reports` | signed in + consent | `{"kind":"opportunity","id":"amb-12","reason":"3–300"}` | `{"ok":true,"hidden":false}` |
| GET | `/api/admin/reports` | admin | — | `{"reports":[{"kind","id","total","counted","first_at","reasons":[…5],"hide_after","hidden"}]}` |
| POST | `/api/admin/reports/dismiss` | admin | `{"kind","id","reason"}` | `{"dismissed":3,"hidden":false}` |

- Жасыру: `REPORT_HIDE_AFTER` (әдепкі 3) **әртүрлі**, аккаунты **≥7 күндік** шағымдаушылар (`moderation.CountedReports`).
  Бір адам бір нысанға бір рет (қайталау қате емес).
- Бірінші шағымда-ақ барлық admin-ге `direct`.
- `dismiss` — шағымдар негізсіз: олар енді саналмайды және жазба автоматты қайта көрінеді.
- Қателер: `400 unknown_kind`, `404 not_found`, `400 reason_required`.
- Жаңа нысан түрі: `moderation.OnReport(kind, ReportTarget{Exists, SetHidden})`.
  Қазір тіркелгендер: `opportunity`, `place` (тек `amb-<n>` жазбалары; Sheets жазбалары емес).

## Рейтинг, сертификат (4-кезең)

| Method | Path | Who | Response |
|---|---|---|---|
| GET | `/api/ambassador/rating` | any | `{"rating":[{"rank","university","weekly_active","event_check_ins","score"}],"mine":{…}\|null}` |
| GET | `/api/certificates/{code}` | any | `{"name","university","role_in_team","term_start","term_end","issued_at","events_organized","published_suggestions","verify_url"}`; `404 not_found` |

- Рейтинг — командалық (жеке амбассадор емес), тек белсенді командасы бар университеттер:
  `score = апталық белсенді студенттер + соңғы 30 күндегі іс-шараға келгендер`. `mine` — кірген адамның университеті
  («университетіміз 3-орында»). Сандар `"< 5"` ережесімен.
- Сертификат беті `#/certificate/<code>` — `code` кездейсоқ 16 таңба, тек admin берген соң бар; мерзімі біткен соң да тексеріледі.

## Admin: амбассадорлар

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/admin/ambassadors?status=active\|ended\|all&university=` | — | `{"terms":[Term + "name","username","joined"]}` |
| PATCH | `/api/admin/ambassadors/{id}` | `{"term_end":"YYYY-MM-DD","role_in_team":"lead"\|"member","hours_per_week":1..10}` | `{"term":Term}`; `404 term_not_found`, `409 lead_taken` |
| POST | `/api/admin/ambassadors/{id}/end` | `{"reason":"3–500"}` | `{"ended":true}` — рөл `student`-ке, контент қалады |
| POST | `/api/admin/ambassadors/{id}/certificate` | — | `{"cert_code","url"}` (қайта шақырса сол код) |
| POST | `/api/admin/ambassadors/expire` | — | `{"ended":N}` — cron-ды қолмен іске қосу |

## Мерзім бітуі (cron)

`GET /api/cron/ambassador` — `Authorization: Bearer <CRON_SECRET>`. Go: `ambassador.Daily(ctx, now)`:
1. мерзімге ≤14 күн қалғандарға бір рет «жалғастырасың ба?» `direct` хабарламасы;
2. `ambassador.ExpireTerms(ctx, now)`: `term_end < бүгін` → `status = ended`, басқа белсенді мерзімі жоқ болса
   `role = student` + `audit_log role_revoke`. Жасаған контенті (`community_content`, іс-шаралар) орнында қалады.
Жауап: `{"renew_asks","ended"}`. `pkg/bot` өзгертілмеді — cron жоспарына осы жолды қосу керек.

## Деректер және өшіру

- RLS: `ambassador_applications`, `ambassador_terms` — тек өз жолдары (app_user); өтінім тек өзіне, `pending` күйінде
  енгізіледі; `community_content` — app_user-ге жабық (тек сервер оқиды).
- `/api/me/export`: `ambassadorApplications`, `ambassadorTerms`.
- Аккаунт өшсе: өтінімдер мен мерзімдер cascade; `community_content.author_id` → `null` (жарияланған жазба қалады);
  `submissions` (000005) cascade.

## Env

| Var | Мағынасы |
|---|---|
| `FEATURE_AMBASSADOR` | ажыратқыш (кезек пен шағымдарды қоса) |
| `REPORT_HIDE_AFTER` | шағым шегі (әдепкі 3) |
| `AMBASSADOR_RESOURCES` | `[{"title_kk","title_ru","url"}]` |
| `CRON_SECRET`, `SITE_URL`, `BOT_TOKEN` | cron, сілтемелер, `direct` хабарламалар |

## Амбассадорды қалай қабылдаймын (admin нұсқаулығы)

1. Студент `#/ambassador/apply` арқылы өтінім береді (университет, себеп ≤300, сағат).
2. `#/admin/queue` → сүзгі «ambassador_application» → ескісі жоғарыда. Себебін, факультетін, курсын қараңыз.
3. «Бекіту» → жүйе өзі мерзім ашады (бірінші адам — жетекші), рөл береді, кабинет сілтемесін жібереді.
   Команда толық болса (1+3) — `team_full`: алдымен біреудің мерзімін аяқтаңыз (`/api/admin/ambassadors/{id}/end`).
4. «Қайтару» → себеп міндетті, студентке сол күйінде жіберіледі.
5. Жетекшіні ауыстыру / мерзімді не сағатты түзету — `PATCH /api/admin/ambassadors/{id}`.
6. Семестр соңында: «жалғастырамын» деген өтінімдер кезекке `ambassador_renewal` болып түседі — бекітсеңіз ұзарады;
   сертификат беру — `POST /api/admin/ambassadors/{id}/certificate`, сілтемесін амбассадорға беріңіз.

## Frontend checklist (06)

- [ ] `#/ambassador/apply` — форма (университет, себеп 300 таңба санағышымен, 1–2 / 2–3 / 3–5), күйі (`/application`).
- [ ] `#/ambassador` кабинет: «Менің университетім» (`/stats`, `"< 5"`-ті мәтін ретінде көрсету), «Қосу» батырмалары
      (мүмкіндік, іс-шара → events формасы, пайдалы орын), «Менің ұсыныстарым» (күйі + қайтару себебі), «Команда»,
      «Ресурстар», шақыру сілтемесі + «маған қанша студент қосылды», жүктеме + «Демалыс аптасы», «жалғастырасың ба?» баннері.
- [ ] Сайттың кез келген бетінде `?ref=` сақтау (бар аналитика) және кіру кезінде `ref` өрісімен жіберу.
- [ ] «Мүмкіндік ұсыну» формасы студенттерге (`POST /api/submissions`).
- [ ] `/api/content` `places[]` → «Жақын маңда» бөліміне қосу; `source: "ambassador"` белгісі.
- [ ] `#/admin/queue` — түр бойынша сүзгі, ескісі бірінші, payload көрінісі, opportunity checklist, бекіту / қайтару (себеп міндетті).
- [ ] Шағым батырмасы (`POST /api/reports`) + `#/admin` шағымдар тізімі, «негізсіз» батырмасы.
- [ ] Университеттер рейтингі (студенттерге де: «университетіміз N-орында»).
- [ ] `#/certificate/<code>` — басып шығаруға арналған стиль, тексеру сілтемесі.
- [ ] Admin: амбассадорлар тізімі, мерзімді түзету/аяқтау, сертификат беру.
