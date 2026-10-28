# Discount checks API (F7: «промокод/жеңілдік жұмыс істемейді»)

Сервер: `pkg/discounts`, миграция `supabase/migrations/20261028000014_discount_checks.sql` (`discount_checks`).
Жеңілдік id — `/api/content` жауабындағы `discounts[].id` жолы (мысалы `d1`); сервер оны жүктелген
контентпен тексереді (`content.Default`, id жиыны 5 минут кэште).

Жалпы ережелер `docs/api/events.md`-дағыдай: cookie / Bearer, `auth.Write` (Origin + rate limit),
`{"error":{"code","message"}}` (kk/ru), `401 unauthorized`, `500 internal`.

## `POST /api/discounts/{id}/check` — signed in + consent, `auth.Write`

Дене: `{"worked": true}` не `{"worked": false}`.

Ереже: бір студент бір жеңілдікке **30 күнде бір рет** дауыс береді (advisory lock — қатар сұраныстарда да).

`201`:
```json
{
  "check": {"discountId": "d1", "worked": true, "createdAt": "2026-10-28T07:12:03Z"},
  "stats": {"worked": 12, "notWorked": 2, "lastConfirmedAt": "2026-10-28T07:12:03Z"}
}
```

Қателер:
| status | code | қашан |
|---|---|---|
| 400 | `bad_request` | `worked` жоқ / boolean емес / JSON емес |
| 401 | `unauthorized` | кірмеген |
| 403 | `consent_required` | келісім берілмеген |
| 403 | `bad_origin` | `auth.Write`: бөтен Origin |
| 404 | `not_found` | id пішіні дұрыс емес (`^[A-Za-z0-9_-]{1,64}$`) не `/api/content`-те мұндай жеңілдік жоқ (мерзімі өткені де) |
| 409 | `already_checked` | соңғы 30 күнде дауыс берген; `error.nextAllowedAt` — қайта болатын уақыт (UTC) |
| 429 | `rate_limited` | `DISCOUNT_CHECKS_PER_HOUR` (әдепкі 20) / сағ бір студентке, не `auth.Write` шегі; `Retry-After` |

## `GET /api/discounts/checks` — public

`Cache-Control: public, s-maxage=300`.
```json
{
  "windowDays": 60,
  "checks": {
    "d1": {"worked": 12, "notWorked": 2, "lastConfirmedAt": "2026-10-27T09:00:00Z"},
    "d2": {"worked": 0, "notWorked": 1, "lastConfirmedAt": null}
  }
}
```
- `worked` / `notWorked` — соңғы 60 күндегі дауыстар.
- `lastConfirmedAt` — ең соңғы «жұмыс істеді» (уақыт шегінсіз); болмаса `null`.
- Ешкім дауыс бермеген жеңілдік картада жоқ — фронт оны «әзірге ешкім растамаған» деп көрсетеді.

## Деректер

`discount_checks (discount_id, user_id, worked, created_at)`; RLS: студент тек өз дауысын қосады/оқиды,
жаңарту/өшіру жоқ. Жалпы сандарды сервер (owner) агрегаттайды — дауыс берушілер ашылмайды.
`user_id ... on delete cascade` — аккаунт өшсе, дауыстары да өшеді. Экспорт бөлімі: `discountChecks`.

## Фронтенд чеклисті (жеңілдік парағы / sheet)

- [ ] Тізім ашылғанда бір рет `GET /api/discounts/checks` (CDN 5 мин кэштейді) → id бойынша карта.
- [ ] Парақта «Жұмыс істеді ме?» / «Сработала ли?» жолы: екі батырма «Жұмыс істеді» / «Жұмыс істемеді»
      («Сработала» / «Не сработала») + сандар `✓ 12 · ✗ 2` (соңғы 60 күн).
- [ ] `lastConfirmedAt` бар болса: «соңғы расталған: N күн бұрын» / «последнее подтверждение: N дн. назад»
      (0 күн → «бүгін» / «сегодня»); жоқ болса — «әзірге ешкім растамаған» / «пока никто не подтвердил».
- [ ] Кірмеген студентке батырма басқанда кіру ұсынысы; келісім жоқ (`403 consent_required`) → келісім терезесі.
- [ ] `201` → жауаптағы `stats`-пен жолды бірден жаңарту, батырмаларды «Рақмет!» деп ауыстыру.
- [ ] `409 already_checked` → «Сіз бұл жеңілдікті жақында бағаладыңыз» (`error.message`), батырмаларды жасыру.
- [ ] `404` → жеңілдік алынып тасталған: парақты жабу / тізімді жаңарту.
