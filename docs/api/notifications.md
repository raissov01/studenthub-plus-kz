# Notifications API (F8: «хабарлама тым көп / өшіру мүмкін емес»)

> Әр хабарлама алдымен сайттағы inbox-қа (`notifications`) жазылады — Telegram-ға жетсе де,
> жетпесе де. Inbox API: `docs/api/inbox.md`.

Сервер: `pkg/notify` (`Send`, `Digest`/`DigestAs`, `Flush`, HTTP), бот: `pkg/bot` (`/settings`),
миграция `supabase/migrations/20261028000013_notif_prefs.sql` (`notify_prefs`, `notify_outbox`, `notify_sends`).

Жалпы ережелер `docs/api/events.md`-дағыдай: cookie / Bearer, `auth.Write` (Origin + rate limit),
`{"error":{"code","message"}}` (хабарлама `?lang=` / `Accept-Language` бойынша kk/ru), `401 unauthorized`, `500 internal`.

## Санаттар

| Санат | Не жібереді | Өшіруге бола ма |
|---|---|---|
| `deadlines` | бот: 7/3/1 күн қалғанда мерзім еске салу (`/api/cron`) | иә |
| `digest` | бот: апталық дайджест; `notify.Digest`-ті категориясыз шақыратын кез келген «күніне бір» хабарлама | иә |
| `events` | іс-шара: күту тізімінен орын, еске салу, кейінгі сұрақтар (`events.Notify`) | иә |
| `mentors` | ментор: жұп туралы таныстыру, жұп жабылды, 3-апта/ай сұрақтары (`mentors` cron) | иә |
| `market` | маркет: жарнама мерзімі бітуде, маусымдық ұсыныс (`market` cron) | иә |
| `jobs` | b2b жұмыс ұсыныстары (`pkg/b2b` — `notify.Send(..., notify.CatJobs, ...)`) | иә |
| `moderation` | **транзакциялық**: өз өтінімің/затың/өтінішің туралы шешім, шағым нәтижесі, амбассадор мерзімін жаңарту сұрағы, admin-ге шағым хабары | **жоқ** — әрқашан, бірден |

Транзакциялық емес хабарламалар үшін:
- **Тыныш уақыт** — әдепкі 22:00–09:00 Asia/Almaty, өзгертуге не өшіруге болады. Бұл уақытта хабарлама
  **жоғалмайды**: `notify_outbox`-қа түседі де, тыныш уақыт біткен соң сағат сайынғы `GET /api/cron/notify` жібереді.
- **Күндік шек** — әдепкі 3 (1–20), Алматы күні бойынша. Шектен асқаны келесі күнге (тыныш уақыттан кейін) қалады.
- `outbox`-та 72 сағаттан ұзақ тұрған хабарлама ескірген саналып тасталады (`dropped`); санат кейін
  өшірілсе — жіберілмей тасталады. Telegram қатесі: 1 сағаттан кейін қайталау, 5 реттен соң тасталады.
- `Digest`/`DigestAs` (күніне бір, `bot_daily_sends`) ережесі өзгерген жоқ; оған қоса санат ажыратқышы,
  тыныш уақыт және күндік шек тексеріледі. Жіберілмесе — `DigestOff` (`"off"`) не `DigestDeferred` (`"deferred"`),
  шақырушы бұрынғыдай «`DigestSent` емес = кейінге қалды» деп қарайды.
- Ботың өз күндік cron-ы (10:00 Алматы, әдепкі тыныш уақыттан тыс, күніне ең көбі бір хабарлама) тек
  `deadlines`/`digest` ажыратқыштарын тексереді; жіберілгені күндік шекке есептеледі (`notify_sends`).

## Go API (feature пакеттер үшін)

```go
// Санатқа сай жіберу: Sent | Queued (тыныш уақыт/шек → outbox) | Off (санат өшік)
res, err := notify.Send(ctx, userID, notify.CatEvents, text, notify.Opts{})
// func(ctx, userID, text) error түріндегі айнымалылар үшін (тесттер оны алмастырады):
var Notify = notify.For(notify.CatEvents)
// Күніне бір, санатпен:
st, err := notify.DigestAs(ctx, userID, notify.CatMarket, now, text)
// Бұрынғы қолтаңбалар өзгерген жоқ:
notify.Direct(ctx, userID, text)          // ештеңе тексермейді — тек транзакциялық үшін (inbox санаты moderation)
notify.DirectAs(ctx, userID, notify.CatJobs, text) // сол, басқа inbox санатымен; DirectFor(c) — айнымалыға
// Inbox жазбасына сайт жолы: notify.Opts{Link: "/event/12"} не notify.WithLink(ctx, "/event/12")
notify.Digest(ctx, userID, now, text)     // = DigestAs(..., notify.CatDigest, ...)
```

## `GET /api/me/notifications` — signed in

```json
{
  "prefs": {
    "categories": {"deadlines": true, "digest": true, "events": true, "mentors": true,
                   "market": true, "jobs": true, "moderation": true},
    "quietHours": {"enabled": true, "start": "22:00", "end": "09:00", "timezone": "Asia/Almaty"},
    "dailyCap": 3,
    "locked": ["moderation"]
  }
}
```
Жол жоқ болса — әдепкі мәндер (жол тек бірінші `PUT`-та жасалады).

## `PUT /api/me/notifications` — signed in, `auth.Write`

Дене — жоғарыдағы пішіннің кез келген бөлігі; тек берілген өрістер өзгереді:
```json
{"categories": {"market": false}, "quietHours": {"enabled": true, "start": "23:00", "end": "08:00"}, "dailyCap": 5}
```
Жауап: `200 {"prefs": {...}}` (толық жаңа күй). Сақталған соң студенттің outbox-тағы хабарламалары жаңа
баптаулармен келесі сағаттық іске қосылуда қайта тексеріледі (мысалы, тыныш уақытты өшірсе — бірден кетеді).

Қателер (`400`):
| code | қашан |
|---|---|
| `bad_request` | JSON емес / 4 KB-тан үлкен |
| `moderation_locked` | `"categories": {"moderation": false}` (`true` — рұқсат, ештеңе өзгертпейді) |
| `unknown_category` | белгісіз санат |
| `invalid_quiet_hours` | `HH:MM` емес (00:00–23:59) не басы = соңы |
| `invalid_daily_cap` | 1..20 емес |

`403 bad_origin`, `429 rate_limited` — `auth.Write`-тан.

## `GET /api/cron/notify` — `Authorization: Bearer <CRON_SECRET>`

Мерзімі жеткен outbox хабарламаларын жібереді. Жолдар алдымен `pending → sending` болып `for update skip locked`
арқылы ұсталады, сондықтан қатар не қайта іске қосу бір хабарламаны екі рет жібермейді.
`200 {"ok":true,"sent":N,"deferred":N,"dropped":N,"failed":N}`; секрет қате → `401 {"ok":false}`.
VPS: `deploy/studenthub-notify.timer` (сағат сайын, :05) → `deploy/studenthub-notify.service`; `deploy/deploy-vps.sh` көшіріп, қосады.

## Бот: `/settings`

Әр санатқа бір батырма (`✓` қосулы / `✗` өшік, `callback_data = set:<санат>`) және тыныш уақыт
(`set:quiet`, қосу/өшіру). Басқанда `notify_prefs` сақталады, хабарлама орнында қайта сызылады.
`/stop` бот деректерімен бірге `notify_prefs`, `notify_outbox`, `notify_sends` жолдарын да өшіреді.

## Деректер

- `notify_prefs` — студентке бір жол; RLS: тек өз жолын оқиды/жазады (`app_user`: select/insert/update).
- `notify_outbox`, `notify_sends` — тек сервер жазады; студент өзінікін тек оқиды (экспорт).
- Барлығы `on delete cascade` — аккаунт өшсе бірге өшеді. Экспорт: `notificationPrefs`, `notificationOutbox`.

## Фронтенд чеклисті (сайттың «Баптаулар» беті)

- [ ] «Хабарламалар» бөлімі: `GET /api/me/notifications` → әр санатқа ауыстырғыш (kk/ru атаулары: Мерзім еске
      салулары / Напоминания о дедлайнах, Апталық топтама / Подборка недели, Іс-шаралар / Мероприятия,
      Менторлар / Менторы, Маркет / Маркет, Жұмыс ұсыныстары / Вакансии).
- [ ] `moderation` — өшірілмейтін (disabled) ауыстырғыш + түсіндірме «Өз өтінімдерің туралы шешімдер әрқашан келеді».
- [ ] Тыныш уақыт: қосу/өшіру + екі уақыт таңдағыш (`HH:MM`, Астана уақыты), әдепкі 22:00–09:00.
- [ ] Күндік шек: 1–20 (әдепкі 3).
- [ ] Әр өзгеріс — `PUT` тек өзгерген өріспен; жауаптағы `prefs`-пен күйді жаңарту; `400` болса `error.message` көрсету.
- [ ] Ботқа сілтеме: «Telegram-да да өзгертуге болады: /settings».
