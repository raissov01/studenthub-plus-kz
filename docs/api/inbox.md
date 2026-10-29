# Inbox API — сайттағы хабарламалар (04-012, 04-033, F8)

Сервер: `pkg/notify` (`inbox.go`), миграция `supabase/migrations/20261029000021_parity_inbox.sql`
(`notifications`). Жалпы ережелер `docs/api/events.md`-дағыдай: cookie / Bearer, `auth.Write`
(Origin + rate limit), қате пішіні `{"error":{"code","message"}}`, `401 unauthorized`, `500 internal`.

## Не жазылады

Сервер студентке айтатын **әр хабарлама** алдымен `notifications` кестесіне жазылады, содан кейін
ғана ботқа жіберіледі (`docs/api/notifications.md` ережелерімен). Сондықтан Telegram-ға жетпеген
хабар жоғалмайды:

| Жағдай | Бот | Inbox |
|---|---|---|
| Telegram байланбаған (тек email аккаунт) | жоқ | ✓ |
| Ботты бұғаттаған (`users.bot_inactive`) | жоқ | ✓ |
| Санат өшірілген (`notify_prefs`) | жоқ | ✓ |
| Тыныш уақыт / күндік шек | кейін (`notify_outbox`) | ✓ бірден |
| `digest` (күніне бір, cron қайталайды) | бір рет | ✓ бір рет (source key бойынша, receipt 90 күн сақталады) |

Go: `notify.Send`, `notify.Direct` / `DirectAs` / `DirectFor`, `notify.Digest` / `DigestAs` — бәрі inbox-қа
жазады; Telegram-ға бәрі ортақ dispatcher арқылы өтеді: direct ≤3, digest ≤1, тыныш уақыт 22:00–08:00.
Модерация да осы ережелерге бағынады; санат өшсе — тек inbox.

- `text` — студенттің тіліндегі (kk/ru) сол мәтін, бірақ **жай мәтін**: бот HTML тегтері алынып,
  `&amp;` т.б. ашылған. Сайт оны бәрібір `esc()` арқылы шығарады.
- `category` — `deadlines | digest | events | mentors | market | jobs | community | moderation`.
- `link` — сайт ішіндегі жол (`/event/12`, `/mentors/me`, `/jobs`) немесе `null`. Ол не
  `notify.Opts{Link}` / `notify.WithLink(ctx, path)` арқылы беріледі, не мәтіндегі бірінші
  `SITE_URL/...` сілтемесінен алынады. Тек `/`-пен басталатын жол (`//host`, `https://…` емес) сақталады.
- Сақтау мерзімі: **оқылған** жазба 90 күннен соң күнделікті `GET /api/cron` кезінде өшеді
  (`pkg/cronrun`). Оқылмағаны өшпейді. Аккаунт өшсе — бәрі cascade өшеді; экспортта `notifications` бөлімі.

## `GET /api/me` — `inboxUnread`

Бұрынғы жауапқа бір өріс қосылды (қоңырау белгісі үшін, бөлек сұраныс керек емес):
```json
{"user": {...}, "needsConsent": false, "inboxUnread": 3}
```
Кірмеген адамға `"inboxUnread": 0`.

## `GET /api/me/inbox` — signed in

Query (бәрі міндетті емес):

| param | мәні |
|---|---|
| `limit` | 1–50, әдепкі 20 |
| `before` | алдыңғы беттің `nextBefore` мәні (id) |
| `unread` | `1` / `true` — тек оқылмағандар |

```json
{
  "items": [
    {"id": 41, "category": "events", "text": "Орын босады: «Career Day» … https://…/event/12",
     "link": "/event/12", "createdAt": "2026-10-29T10:00:00+05:00", "readAt": null, "read": false}
  ],
  "unread": 3,
  "nextBefore": 41
}
```
Жаңасы бірінші. `nextBefore` — `null` болса, келесі бет жоқ. `400 bad_request` — `limit`/`before` қате.

## `POST /api/me/inbox/read` — signed in, `auth.Write`

Дене — біреуі ғана:
```json
{"ids": [41, 40]}
```
```json
{"all": true}
```
Жауап: `200 {"ok": true, "marked": 2, "unread": 1}`. Бөтен немесе жоқ id-лер үнсіз еленбейді (RLS).
`400 bad_request`: бос дене, екеуі бірге, 100-ден көп id, id ≤ 0. `403 bad_origin`, `429 rate_limited` — `auth.Write`-тан.

## Frontend checklist

- [ ] Қоңырау белгісі (header): `GET /api/me` → `inboxUnread`; 0 болса бейдж жоқ, 9-дан көп болса «9+».
      Inbox экранынан қайтқанда және `POST /api/me/inbox/read` жауабындағы `unread`-пен жаңарт.
- [ ] Inbox экраны (`#/inbox` не профиль ішінде): `GET /api/me/inbox`, тізім жаңасы бірінші; санат
      белгішесі/атауы (kk/ru), уақыты («2 сағ бұрын»), оқылмағаны қалың/нүктемен. Мәтін — `esc()` арқылы.
      «Тағы» батырмасы `?before=<nextBefore>`; «Тек оқылмағандар» сүзгісі `?unread=1`.
- [ ] Бос күй: «Әзірге хабарлама жоқ».
- [ ] Элементті басқанда: `POST /api/me/inbox/read {"ids":[id]}`, `link` болса — сол сайт жолына өт
      (тек `/`-пен басталатын ішкі жол; сырттағы URL ашпа).
- [ ] «Бәрін оқылды деп белгілеу» батырмасы: `{"all": true}`.
- [ ] Бот жаза алмайтын студентке (Telegram байланбаған не бот бұғатталған) inbox үстінде «Хабарламаларды
      Telegram-да алу үшін ботқа /start жаз» шақыруы (04-012).
- [ ] Mini App: `X-Session-Token` жауап тақырыбын оқып, жадтағы токенді ауыстыр (`docs/ACCOUNTS.md`).
