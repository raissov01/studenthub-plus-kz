# Admin content API — мүмкіндіктер, жұмыстар, жеңілдіктер `/admin/content` редакторынан

Сервер: `pkg/admincontent` (`pkg/server/routes_admincontent.go`), тексеру `pkg/content/validate.go`
(`ValidateRecord` — Sheets жолымен **бір код**), миграция `supabase/migrations/20261028000017_admin_content.sql`
(кесте `admin_content`). Нұсқаулық: `docs/CONTENT.md`. Студент ұсыныстары: `docs/SUGGEST.md`.

Жалпы ережелер (басқа API-лармен бірдей): cookie / Bearer сессия; жазу эндпоинттері `auth.Write` (рұқсат етілген
`Origin` + rate limit, `403 bad_origin`, `429`); қате `{"error":{"code","message"}}`, хабарлама `?lang=` /
`Accept-Language` бойынша kk не ru. Feature switch **жоқ** — редактор әрқашан қосулы.

Рұқсат: тек `role = admin`. Кірмеген → `401 unauthorized`; басқа рөл (студент, амбассадор, `university_staff`,
жұмыс беруші) → `403 forbidden`. `university_staff` өз университетінің мүмкіндіктерін бұрынғыдай
`/api/university/posts` арқылы жариялайды (`docs/api/b2b.md`, `university_posts`, `source: "university"`) — бұл
редактор оны қайталамайды.

## Эндпоинттер

| Method | Path | Body | Жауап |
|---|---|---|---|
| GET | `/api/admin/content?kind=&status=all\|published\|hidden&q=&limit=200` | — | `200 {"items":[Item],"counts":{"opportunities":{"published":3,"hidden":1},"jobs":{…},"discounts":{…}}}` |
| GET | `/api/admin/content/{id}` | — | `200 {"item":Item}` |
| POST | `/api/admin/content` | `WriteBody` (`kind` міндетті) | `201 {"item":Item}` |
| POST | `/api/admin/content/validate` | `WriteBody` (`kind` міндетті) | `200 {"ok":true,"record":{…}}` — ештеңе сақталмайды (live-тексеру үшін) |
| PUT | `/api/admin/content/{id}` | `WriteBody` (жазба толық ауыстырылады; `kind` өзгермейді) | `200 {"item":Item}` |
| POST | `/api/admin/content/{id}/hide` | — | `200 {"item":Item}` (`status: "hidden"`, идемпотентті) |
| POST | `/api/admin/content/{id}/unhide` | — | `200 {"item":Item}` (`status: "published"`) |
| DELETE | `/api/admin/content/{id}` | — | `200 {"deleted":"adm-12"}` |

- `{id}` — `/api/content`-тегі id: `adm-<n>`. Басқа пішін не жоқ жазба → `404 not_found`.
- `kind`: `opportunities` | `jobs` | `discounts`. `q` — `title`/`name`/`org` (kk, ru) бойынша іздеу (ILIKE).
- Тізім `updated_at` бойынша жаңасы бірінші.

### `WriteBody`

```json
{
  "kind": "opportunities",
  "status": "published",
  "no_deadline": false,
  "record": { "…": "data/opportunities.json пішіні" }
}
```

- `status`: `published` (create әдепкісі) | `hidden`. PUT-та берілмесе — бұрынғысы қалады. Басқа мән → `400 invalid_status`.
- `no_deadline`: «Мерзімсіз» белгісі. `record.deadline` пен `no_deadline`-ның **дәл біреуі** керек (Sheets-тегі `deadline` / `no_deadline` ережесі).
- `record` — `data/*.json` жазбасының пішіні. `record.id` пен `record.is_demo` еленбейді: id-ді сервер береді
  (`adm-<n>`, Sheets / `data/*.json` id-лерімен қақтығыспайды — Sheets-те `adm-` префиксі тыйым салынған, ал
  `/api/content` бірдей id-ді екі рет бермейді), `is_demo` әрқашан `false`.
- `record.posted_at` бос болса — бүгінгі күн (Астана).
- Дене ≤ 64 КБ; `record` жоқ не JSON қате → `400 bad_request`.

### `Item`

```json
{
  "id": "adm-12",
  "kind": "opportunities",
  "status": "published",
  "no_deadline": false,
  "expired": false,
  "record": { "id": "adm-12", "type": "intl", "…": "…", "source": "admin" },
  "created_by": 7, "updated_by": 7,
  "created_at": "2026-10-28T09:00:00Z", "updated_at": "2026-10-28T09:05:00Z"
}
```

- `record` — `/api/content` беретін дәл сол объект.
- `expired: true` — `deadline` бүгіннен ерте (Астана): жазба `/api/content`-те жоқ, тізімде «Мерзімі өтті» белгісімен көрсетіңіз.

### `/api/content`-тегі пішін

`published` жазбалар тиісті тізімнің соңына қосылады, `data/*.json` пішінінде + `"source": "admin"`:

```json
{"id":"adm-12","type":"intl","funded":true,"title":{"kk":"Жазғы мектеп","ru":"Летняя школа"},
 "org":{"kk":"…","ru":"…"},"req":{"course":2,"gpa":3.2,"en":"B1","skills":["research"]},"spec":["it","eng"],
 "desc":{"kk":"…","ru":"…"},"docs":[{"kk":"Транскрипт","ru":"Транскрипт"}],"steps":[{"kk":"…","ru":"…"}],
 "source_url":"https://uni.kz/summer","posted_at":"2026-10-20","verified_at":"2026-10-21","verified_by":"Admin",
 "contact":{"phone":"","email":"intl@uni.kz","site":"https://uni.kz"},"is_demo":false,"deadline":"2026-11-27",
 "city":"almaty","source":"admin"}
```

Жұмыс (`jobs`): `id, today? (тек true болса), title, org, city, pay, per, hours, sched, tags, skills, spec, desc, req[], source_url, posted_at, verified_at, verified_by, contact, is_demo, deadline, source`.
Жеңілдік (`discounts`): `id, cat, pct, name, what, cond, source_url, posted_at, verified_at, verified_by, contact, is_demo, deadline, city, source`.
Жасырылған, өшірілген және мерзімі өткен жазбалар шықпайды. `/api/content` кэші — 10 минутқа дейін.

## Қателер

| HTTP | `code` | Қашан |
|---|---|---|
| 400 | `invalid_record` | Жазба ережеге сай емес (төменде) |
| 400 | `invalid_kind` | `kind` белгісіз (create / validate) |
| 400 | `invalid_status` | `status` белгісіз |
| 400 | `bad_request` | Дене JSON емес / `record` жоқ |
| 401 | `unauthorized` | Кірмеген |
| 403 | `forbidden` | admin емес |
| 403 | `bad_origin` | Бөтен Origin (жазу) |
| 404 | `not_found` | Мұндай `adm-<n>` жоқ |

`invalid_record` денесі:

```json
{"error":{"code":"invalid_record","field":"source_url","path":"source_url",
          "message":"\"source_url\" сілтемесі https:// деп басталуы керек: \"http://uni.kz\""}}
```

- `field` — Sheets бағанының атауы (`title_kk`, `contact_email`, `req_gpa`, `docs_ru`, `hours`, `deadline`, …);
  `path` — жазбадағы JSON жолы (`title.kk`, `contact.email`, `req.gpa`, `docs[].ru`, `hours`, `deadline`);
  `kind` / `list` — арнайы (түр өзгертілді / тізім элементінде `|`).
- `message` — Sheets логындағы мәтіннің дәл өзі (kk); `?lang=ru` болса «Проверьте поле «…»: …» деп басталады.
  Формада өз kk/ru мәтіндеріңізді (төмендегі кесте) `path` бойынша көрсетіңіз, `message`-ті қосымша ретінде.
- Бір жауапта бір қате (бірінші сәтсіз өріс, Sheets-тегі ретпен). Live-тексеру фронтта, сақтау алдында `/validate`.

## Аудит

Әр жазу `audit_log`-қа (`actor_id` = admin, `created_at`) түседі:

| `action` | `meta` |
|---|---|
| `content_create` | `{"id","kind","status","title"}` |
| `content_update` | `{"id","kind","title","changed":["title","deadline"],"status"?}` |
| `content_hide` / `content_unhide` | `{"id","kind"}` |
| `content_delete` | `{"id","kind","status","record":{…өшірілген жазбаның толық көшірмесі}}` |
| `moderation_edit` | `{"submission","kind","changed":[…]}` — кезектегі ұсынысты түзету (`PATCH /api/admin/queue/{id}`) |

## Фронт: `/admin/content` чеклисті

Тізім беті:

- [ ] Үш таб: Мүмкіндіктер / Жұмыстар / Жеңілдіктер (`?kind=`), әр табта `counts` (жарияланған · жасырылған).
- [ ] Сүзгі: Барлығы / Жарияланған / Жасырылған (`status`), іздеу (`q`, 300 мс debounce).
- [ ] Жол: атауы (`record.title.kk` / `record.name.kk`), қала, мерзім не «Мерзімсіз», `status` белгісі,
      `expired` → «Мерзімі өтті» белгісі, `updated_at`.
- [ ] Әрекеттер: Өңдеу, Жасыру / Қайта көрсету, Өшіру (растау терезесімен: «Біржола өшіресіз бе?» / «Удалить навсегда?»).
- [ ] «Қосу» батырмасы → редактор. Бос күй: «Әзірге жазба жоқ» / «Пока нет записей».
- [ ] 401 → кіру; 403 → «Бұл бет тек әкімшіге» / «Страница только для администратора».

Редактор формасы (create = POST, edit = PUT; `record`-ты `Item.record`-тан толтырыңыз, `source` пен `id`-ді жібермеңіз):

Ортақ өрістер (үш түрге де):

| Өріс (`path`) | Міндетті | Ереже | kk | ru |
|---|---|---|---|---|
| `title.kk` (жеңілдікте `name.kk`) | иә | бос емес | Қазақша атауын жазыңыз | Укажите название на казахском |
| `title.ru` / `name.ru` | жоқ | бос болса kk көшіріледі | — | — |
| `city` | иә (жұмыс, жеңілдік, мүмкіндік) | тізімнен: `astana, almaty, shymkent, karaganda, aktobe, taraz, turkistan, pavlodar, oskemen, semey, kostanay, atyrau, aktau, oral, kyzylorda, petropavl, taldykorgan, kokshetau, other, remote` | Қаланы таңдаңыз | Выберите город |
| `source_url` | иә | `https://` + дұрыс URL | Сілтеме https:// деп басталуы керек | Ссылка должна начинаться с https:// |
| `deadline` / `no_deadline` | біреуі | `ЖЖЖЖ-АА-КК`, нақты күн; екеуі бірге емес | Соңғы күнді таңдаңыз не «Мерзімсіз» белгілеңіз | Укажите дедлайн или отметьте «Без срока» |
| `posted_at`, `verified_at` | жоқ | `ЖЖЖЖ-АА-КК` | Күн қате | Неверная дата |
| `verified_by` | жоқ | мәтін | — | — |
| `contact.phone` | жоқ | мәтін | — | — |
| `contact.email` | жоқ | `x@y.z`, бос орынсыз | Email қате | Неверный email |
| `contact.site` | жоқ | `https://` | Сайт https:// деп басталуы керек | Сайт должен начинаться с https:// |
| кез келген тізім элементі | — | ішінде `\|` болмайды | «\|» таңбасын қолданбаңыз | Не используйте символ «\|» |

Мүмкіндік (`opportunities`):

| Өріс | Міндетті | Ереже | kk | ru |
|---|---|---|---|---|
| `type` | иә | `mobility` / `intl` / `company` / `grant` | Түрін таңдаңыз | Выберите тип |
| `funded` | жоқ | bool | — | — |
| `org.kk`, `org.ru` | жоқ | мәтін | — | — |
| `req.course` | жоқ | бүтін сан 1–7 | Курс 1–7 аралығында | Курс от 1 до 7 |
| `req.gpa` | жоқ | 0–4 | GPA 0–4 аралығында | GPA от 0 до 4 |
| `req.en` | жоқ | `A1`–`C2` | Деңгейді таңдаңыз | Выберите уровень |
| `req.skills` | жоқ | кодтар тізімі | — | — |
| `spec` | жоқ | кодтар (`all, econ, acc, it, agro, vet, eng, energy`); бос → `["all"]` | — | — |
| `desc.kk`, `desc.ru` | жоқ | мәтін | — | — |
| `docs[]`, `steps[]` | жоқ | `{kk, ru}` жолдары; ru бос болса kk көшіріледі; **ru-сы бар, kk-сы жоқ элемент болмайды** | Әр элементтің қазақшасын жазыңыз | Заполните казахский текст каждого пункта |

Жұмыс (`jobs`):

| Өріс | Міндетті | Ереже | kk | ru |
|---|---|---|---|---|
| `today` | жоқ | bool («Бүгінгі жұмыс») | — | — |
| `org.kk/ru`, `sched.kk/ru`, `desc.kk/ru` | жоқ | мәтін | — | — |
| `pay` | иә | сан ≥ 0 (теңге) | Төлемді жазыңыз | Укажите оплату |
| `per` | иә | `shift` / `month` / `hour` | Төлем кезеңін таңдаңыз | Выберите период оплаты |
| `hours` | `per = shift` болса иә | 0 < h ≤ 168 | Ауысым сағатын жазыңыз | Укажите часы смены |
| `tags` | жоқ | `noexp, daily, remote, near` | — | — |
| `skills`, `spec` | жоқ | кодтар; `spec` бос → `["all"]` | — | — |
| `req[]` | жоқ | `{kk, ru}` жолдары | Әр талаптың қазақшасын жазыңыз | Заполните казахский текст требования |

Жеңілдік (`discounts`):

| Өріс | Міндетті | Ереже | kk | ru |
|---|---|---|---|---|
| `cat` | иә | `food, cinema, books, sport, clothes, lang, edu, cowork, print` | Санатын таңдаңыз | Выберите категорию |
| `pct` | иә | мәтін (`−15%`, `1 500 ₸`) | Жеңілдік мөлшерін жазыңыз | Укажите размер скидки |
| `what.kk/ru`, `cond.kk/ru` | жоқ | мәтін | — | — |

- [ ] Өріс өзгергенде фронттағы ережемен тексеру; «Сақтау» алдында `POST /api/admin/content/validate`;
      `invalid_record` келсе `path` бойынша өрісті белгілеп, `message`-ті астына шығару.
- [ ] «Мерзімсіз» құсбелгісі қосылса `deadline` өрісін тазалап, бұғаттау (`record.deadline = null`, `no_deadline: true`).
- [ ] Тізім өрістері (`docs`, `steps`, `req`) — kk/ru жұп жолдар, қосу / өшіру / ретін ауыстыру.
- [ ] «Жариялау» (`status: published`) және «Жасырын сақтау» (`status: hidden`) батырмалары.
- [ ] Сақталғаннан кейін: «Сақталды. Сайтта 10 минут ішінде шығады» / «Сохранено. Появится на сайте в течение 10 минут».

## Фронт: студенттің «Мүмкіндік ұсыну» формасы

`POST /api/submissions` (`docs/api/ambassador.md`, «Ұсыныстар») — **амбассадор ажыратқышына тәуелсіз**, әрқашан қосулы.
Google Form (`SUGGEST_FORM_URL`) енді қолданылмайды: «Мүмкіндік білесің бе? Ұсын» батырмасы осы формаға апарсын.

- [ ] Кірмеген → кіру терезесі (`401 unauthorized`); келісім жоқ → келісім экраны (`403 consent_required`).
- [ ] Дене: `{"kind":"opportunity","payload":{…}}`; өрістер мен ережелер:

| Өріс | Міндетті | Ереже | kk | ru |
|---|---|---|---|---|
| `type` | иә | `mobility` / `intl` / `company` / `grant` | Түрін таңдаңыз | Выберите тип |
| `title_kk` | иә | 3–140 таңба | Атауы 3–140 таңба | Название 3–140 символов |
| `title_ru` | жоқ | ≤140 | — | — |
| `org_kk` | иә | 2–140 | Ұйымды жазыңыз | Укажите организацию |
| `org_ru` | жоқ | ≤140 | — | — |
| `desc_kk` | иә | 10–2000 | Сипаттама кемінде 10 таңба | Описание не короче 10 символов |
| `desc_ru` | жоқ | ≤2000 | — | — |
| `source_url` | иә | `https://`, бос орынсыз | Ресми сілтеме https:// деп басталуы керек | Официальная ссылка должна начинаться с https:// |
| `deadline` | жоқ | `ЖЖЖЖ-АА-КК`, бүгіннен ерте емес | Өткен күнді таңдауға болмайды | Нельзя выбрать прошедшую дату |
| `city` | жоқ | `content.Cities`, әдепкі `other` | — | — |
| `funded`, `free` | жоқ | bool («Қаржыландырылады», «Ақы сұрамайды») | — | — |

- [ ] Сервер қатесі `400 invalid_<өріс>` (`invalid_title_kk`, `invalid_source_url`, `invalid_deadline`, …) → сол өрісті белгілеу.
- [ ] Сәтті → `201 {"id","status":"pending"}`: «Рақмет! Ұсыныс тексеруге жіберілді» / «Спасибо! Предложение отправлено на проверку».
- [ ] «Менің ұсыныстарым» (`GET /api/submissions/my`): қаралуда / жарияланды / қайтарылды + `reason`.

Admin кезегі (`/admin/queue`, `docs/api/ambassador.md`): `opportunity` ұсынысын бекітер алдында «Түзету» —
`PATCH /api/admin/queue/{id}` `{"payload":{…толық payload…}}` (жоғарыдағы ережелер, `400 invalid_<өріс>`,
`409 not_pending`). Бекітілгенде жазба `/api/content`-те `id = amb-<n>`, `source: "student"` болып шығады, авторға
Telegram хабарламасы барады.
