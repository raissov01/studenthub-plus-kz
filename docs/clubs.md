# Клубтар (FEATURE_CLUBS)

Университет клубтарының бөлімі: каталог (`/clubs`), клуб беті (`/club?id=<id>`), қосылу өтінімі не Telegram
тобына тікелей сілтеме, клуб жетекшілері (`club_admins`, бір клубқа бірнеше адам, бір адам бірнеше клубта),
модерацияланатын клуб жаңалықтары, клубтың іс-шаралары (кәдімгі іс-шара ағыны: QR арқылы келу, күнтізбе, ұпайлар).

Код: `backend/internal/clubs`, `frontend/src/components/views/clubs.tsx`, `frontend/src/lib/clubs.ts`.
Миграция: `backend/migrations/20261105000041_clubs.sql`. Сеид: [`docs/clubs-seed.md`](clubs-seed.md).

## Қосу

1. `FEATURE_CLUBS=true` (API ортасы), қайта іске қосу. Әдепкі бойынша өшік: барлық `/api/clubs*` 404 қайтарады,
   мәзір, «Іс-шаралар» бөлімі мен басты беттегі карточка көрінбейді.
2. Клуб іс-шаралары үшін `FEATURE_EVENTS=true` те керек (өшік болса, клуб бетінде іс-шара бөлімі жоқ).
3. Ресми KazATU клубтарын бір рет жүктеу: `CLUBS_SEED=1` қойып API-ді қайта іске қосу (`cmd/server` басында
   `clubs.Seed`). Қайталап іске қосу қауіпсіз (`seed_key` бойынша), әкімші өзгерткен жазбалар қайта жазылмайды.
   Жүктелген соң айнымалыны алып тастауға болады.
4. `CLUBS_UNIVERSITY` — жаңа клубтың университеті (әдепкі `Сейфуллин университеті`, профильдегі мәнмен бірдей).
   Клуб іс-шарасы осы университетке жазылады.

## Рөлдер мен құпиялылық

| Кім | Не істей алады |
|---|---|
| Кез келген адам | Белсенді клубтар каталогы, клуб беті, жарияланған жаңалықтар, мүшелер **саны** |
| Кірген студент | Өтінім беру / қайтарып алу, клубтан шығу, «Клуб жетекшісімін» өтінімі |
| Клуб жетекшісі (`club_admins`) | Өз клубының бетін өзгерту, өтінімдерді қабылдау/қабылдамау, мүшелер тізімі, мүшені шығару, жаңалық (модерациямен), клуб іс-шаралары |
| Сайт әкімшісі (`admin`) | Клуб құру/өзгерту/архивтеу, жетекші тағайындау/алу (`audit_log`), кез келген клубты басқару |

- **Мүшелер тізімі** тек сол клубтың жетекшілеріне (және сайт әкімшісіне) көрінеді. RLS: `club_members_admin_read`;
  студент кестеден тек өз жолын оқиды.
- Жетекшінің **байланысы** (`leader_contact`) тек `leader_contact_public = true` болса жария; әйтпесе тек жетекшілерге.
  Карточкада тек рөл атауы (`leader_title_*`, мысалы «Төраға») — аты-жөні сақталмайды.
- Клуб жетекшісі атауды, бағытты, мәртебені, университетті және дереккөзді өзгерте алмайды (бағанға grant жоқ).
- Бас тартылған өтінім сақталмайды (жол өшеді). Аккаунт өшсе, мүшелік пен жетекшілік бірге өшеді; экспортта
  `clubMemberships`, `clubAdmin`, `clubPosts` бөлімдері бар.
- Хабарламалар `community` санатымен (баптауда өшіруге болады): жетекшіге — жаңа өтінім, студентке — шешім.

## API

Барлығы `features.Require("clubs")` астында; жазу сұраныстары `auth.Write` (Origin + rate limit).

| Жол | Кім | Ескерту |
|---|---|---|
| `GET /api/clubs` | кез келген | `{clubs, directions}`; кірген болса әр клубта `my: {status, admin}` |
| `GET /api/clubs/my` | кірген | менің клубтарым: `pending` / `member` / `admin` |
| `GET /api/clubs/{id}` | кез келген | `{club, posts, can_manage, pending_requests?}`; архивтегі клуб тек жетекшілерге |
| `POST /api/clubs/{id}/join` | кірген, келісім берген | `{message?}` (≤300) → `{status:"pending", created}`; `join_mode=link` болса 409 `join_via_link` + `join_link` |
| `DELETE /api/clubs/{id}/join` | кірген | өтінімді қайтару не клубтан шығу |
| `POST /api/clubs/leader-applications` | кірген, келісім берген | `{club_id, proof_text, proof_url}` не жаңа клуб үшін `{name_kk, direction, proof_text, proof_url}` → модерация кезегі (`club_leader`) |
| `PATCH /api/clubs/{id}` | клуб жетекшісі, admin | бет өрістері (төменде); `null` — өрісті тазалау |
| `GET /api/clubs/{id}/members` | клуб жетекшісі, admin | өтінімдер мен мүшелер: аты, username, факультет, курс, хабарлама |
| `POST /api/clubs/{id}/requests/{userId}` | клуб жетекшісі, admin | `{decision:"approve"\|"decline"}`; студентке хабарлама |
| `DELETE /api/clubs/{id}/members/{userId}` | клуб жетекшісі, admin | мүшені шығару |
| `POST /api/clubs/{id}/posts` | клуб жетекшісі | `{body_kk, body_ru?, link_url?}` → модерация кезегі (`club_post`), бекітілген соң жарияланады |
| `GET /api/admin/clubs` | admin | барлық клуб (архив те), жетекшілері, өтінім саны |
| `POST /api/admin/clubs` | admin | `{name_kk, direction, …, source_url?, verified_at?}` |
| `PATCH /api/admin/clubs/{id}` | admin | кез келген өріс, `status: active\|archived` |
| `POST /api/admin/clubs/{id}/admins` | admin | `{userId}` |
| `DELETE /api/admin/clubs/{id}/admins/{userId}` | admin | |

Бет өрістері (жетекші): `summary_kk/ru` (≤300), `about_kk/ru` (≤4000), `building_id` (кампус картасындағы id),
`room`, `schedule_kk/ru`, `leader_title_kk/ru`, `leader_contact`, `leader_contact_public`, `telegram_url`
(`https://t.me/…`), `instagram_url` (`https://instagram.com/…`), `join_mode` (`request`\|`link`), `join_link`
(`link` режимінде міндетті), `recruiting`. Тек әкімші: `name_kk/ru`, `direction`, `university`, `status`,
`source_url`, `verified_at`. Бағыттар: `it, science, debate, sport, art, volunteer, media, business, languages,
kvn, culture, other`. Қате өріс — 400 `invalid_club` + `field`.

### Клуб іс-шаралары

`POST /api/events/create` денесінде `club_id` болса: жасаушы сол клубтың жетекшісі (не сайт әкімшісі) болуы керек,
іс-шара клубтың университетіне жазылады, `events.club_id` қойылады. Одан әрі — кәдімгі ағын (`/organize/<id>`,
QR, тіркелу, күнтізбе, ұпайлар). `events.ClubManager` (`internal/server/glue_clubs.go`) клуб жетекшісіне өз клубының
іс-шараларын басқаруға рұқсат береді; RLS айнасы — `events_club_insert/update/read`, `event_regs_club_read`,
ал `events_club_tag` (restrictive) басқа рөлдердің бөтен клубты белгілеуіне жол бермейді.
`GET /api/events?club=<id>` — клубтың іс-шаралары.

## Жем (feed) үшін ілмек: клуб жаңалығы

`wt-feeds` модулі `club` санатындағы жазбаны клуб жаңалығы етіп тіркей алады, біздің кодқа тәуелсіз:

```go
id, created, err := clubs.AttachFeedItem(ctx, tx, clubID, clubs.FeedItem{
    Ref:     "tg:kazatzu_media/812",          // жемдегі тұрақты id, клуб ішінде бірегей
    BodyKK:  text,                            // 3–2000 таңба
    URL:     "https://t.me/kazatzu_media/812", // міндетті емес
    Publish: false,                           // false: модерация кезегіне (club_post), true: бірден жарияланады
})
```

- `tx` — шақырушының транзакциясы (connection owner). Сол `Ref` қайта келсе, жаңа жол жасалмайды (`created=false`).
- Кесте деңгейінде: `club_posts (source = 'feed', source_ref)`, `unique (club_id, source_ref)`.

## Тесттер

- Go: `cd backend && go test ./internal/clubs` — өтінім ағыны, сілтеме режимі, мүшелер тізімінің RLS-і, бөтен
  клубты өзгертуге тыйым, жетекші өтінімі және жаңалық модерациясы, клуб іс-шаралары, сайт әкімшісінің CRUD-ы,
  сеид, feed ілмегі.
- Frontend: `src/lib/clubs.test.ts` (сүзгілер), `e2e/clubs.spec.ts` (каталог → клуб беті → өтінім → жетекші
  бекітеді → студентке хабарлама; жетекшінің іс-шарасы клуб бетінде және күнтізбеде; бос күй).
