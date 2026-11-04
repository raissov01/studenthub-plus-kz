# Ұпайлар / Баллы API (XP, серия, монета, сыйлықтар)

Сервер: `pkg/points` (+ `pkg/server/glue.go` ішіндегі квиз және іс-шара хуктары), миграция
`supabase/migrations/20261104000030_gamification.sql`. Фронт: `components/views/points.tsx`, `lib/points.ts`.
Қосқыш: `FEATURE_POINTS=true` — өшірулі кезде ештеңе берілмейді, барлық маршрут `404` (белгісіз бет сияқты).

Жалпы ережелер `docs/api/events.md`-дағыдай: cookie / Bearer, жазу маршруттары `auth.Write` (Origin + ортақ rate limit),
қате `{"error":{"code","message"}}` (kk/ru), `401 unauthorized`, `403 consent_required`, `429 rate_limited` (+ `Retry-After`).

## Ақша (міндетті түрде оқы)

**Монета — ақша емес.** Ақшалай төлем, монетаны ақшаға айырбастау, студенттер арасында аудару **жоқ және
жасалмайды**. Сыйлықтар — тек серіктес жеңілдігі (промокод), университет мерчі, қосымша басып шығару квотасы
(placeholder), іс-шараға басым тіркелу (placeholder).

TODO (жасалмаған, әдейі): монетаны ақшаға айналдыру керек болса — алдымен лицензиясы бар төлем серіктесі және
заңгер қорытындысы (ҚР төлем/электрондық ақша заңнамасы, салық, кәмелетке толмағандар). Оған дейін API-де
ақшаға қатысты бірде-бір өріс жоқ.

## Деректер және кепілдіктер

| кесте | не | RLS |
|---|---|---|
| `points_wallets` | кэштелген баланс (`coins`, CHECK `coins >= 0`), `xp`, серия, қорғаныс, `study_group`, `leaderboard_opt_out` | app_user: тек өзінікін оқиды |
| `xp_events` | XP журналы, `unique (user_id, reason, ref_type, ref_id)` | тек өзінікі, оқу |
| `coin_ledger` | монета журналы (append-only триггер), сол unique | тек өзінікі, оқу |
| `rewards` | каталог | app_user: тек `active` |
| `reward_redemptions` | өтінімдер, `unique (user_id, idem_key)` | тек өзінікі, оқу |
| `reward_codes` | промокод қоры | app_user-ге мүлде жоқ |

- **Барлық жазу серверде** (owner рөлі): клиент ешқашан сан жібермейді — тек әрекет id-і, сыйлық id-і және
  идемпотенттік кілт.
- **Бір әрекет бір рет төленеді**: екі журналда да `unique (user_id, reason, ref_type, ref_id)`.
- **Баланс = журнал қосындысы**: әр өзгеріс бір транзакцияда журнал жолы + `points_wallets.coins`. Журналды
  `UPDATE/DELETE` жасау триггермен тыйым салынған (аккаунт өшкенде cascade және admin `actor_id → null` қана рұқсат).
- **Параллель redeem**: әмиян жолы `SELECT … FOR UPDATE` арқылы бүкіл транзакцияға құлыпталады, соңғы қорғаныс —
  `CHECK (coins >= 0)`. Тест: `TestParallelRedeemsCannotOverspend` (12 параллель сұраныс, 100 монета, баға 30 → дәл 3).
- Аккаунт өшсе — әмиян, журналдар, өтінімдер cascade өшеді; басқа студенттің жолындағы admin `actor_id`/`decided_by`
  null болады. Экспорт бөлімдері: `points`, `xpEvents`, `coinLedger`, `rewardRedemptions`.

## Ұпай ережелері (`pkg/points.Rules`)

Күн — Asia/Almaty күнтізбелік күні. Апта — дүйсенбі 00:00 Almaty.

| reason | қайдан | XP | монета | шек |
|---|---|---|---|---|
| `daily_open` | `POST /api/points/daily` (күннің бірінші ашылуы) | 5 | 1 | 1/күн |
| `quiz` | квиз нәтижесі серверде бағаланады (`pkg/quiz`, `OnAttempt`) | 10, 80 %+ болса +5 | 2 (50 %+ болса ғана) | 5/күн, бір квиз — бір рет |
| `event_checkin` | іс-шараға келу (QR, walk-in, ұйымдастырушы қолмен; `pkg/events`, `OnCheckin`) | 30 | 15 | 3/күн, бір іс-шара — бір рет |
| `roadmap_step` | даму жоспарының қадамы (құрылғыда ғана сақталады) | 5 | **0** | 1/күн, 3/апта |

Жалпы күндік шек: **150 XP** және **30 монета** (барлық reason бірге). Шектен асса — `capped: true`, артығы берілмейді;
шекке тірелген әрекет келесі күні төленуі мүмкін (әлі төленбегендіктен).

Шешімдер:
- **Квиз**: өз квизің, `retry` (қате жауаптарды қайталау) және 3 сұрақтан аз квиз төленбейді — өз-өзіне квиз жасап
  ұпай жинауды болдырмау үшін. Live ойындар әзірге төленбейді.
- **Check-in кері алынса** (`/checkin/revoke`) XP толық, монета баланс жеткенше кері алынады (`event_checkin_revoked`),
  қайта келгенде қайта төленбейді.
- **Құрылғыдағы әрекеттер** (даму жоспары) серверде тексерілмейді, сондықтан тек аз XP, монета жоқ, қатаң шек.
- **Күнтізбе дедлайндары** төленбейді: олар тек құрылғыда, «орындалды» күйі жоқ — кез келген сыйақы бос басу болар еді.
- **Серия**: белсенді күн = кемінде бір award шақыруы (шекке тірелсе де). Кеше белсенді болса серия +1; бір күн
  өткізіп алса және қорғаныс болса, қорғаныс жұмсалып серия жалғасады; әйтпесе 1-ден басталады.
- **Деңгей**: L деңгейі 50·L·(L−1) XP-тен басталады (0, 100, 300, 600, 1000…).

## Студент маршруттары

### `GET /api/points/me` — signed in
```json
{
  "xp": 135, "level": 2, "levelXp": 100, "nextLevelXp": 300, "coins": 18,
  "streak": {"days": 3, "best": 5, "freezes": 0, "maxFreezes": 1, "freezeCost": 20, "activeToday": true},
  "today": {"xp": 15, "coins": 3, "xpCap": 150, "coinsCap": 30},
  "tasks": [{"id": "daily_open", "xp": 5, "coins": 1, "perDay": 1, "deviceOnly": false, "done": 1, "week": 3}, …],
  "settings": {"leaderboardOptOut": false, "studyGroup": "ИС-21"},
  "recent": [{"delta": 2, "reason": "quiz", "createdAt": "2026-11-04T07:12:03Z"}]
}
```
`recent` — монета журналының соңғы 20 жолы.

### `POST /api/points/daily` — consent, `auth.Write`
Дене жоқ. Бір Almaty күнінде бір рет төлейді; қайталанса `award.duplicate: true`.
`200 {"award": {"xp":5,"coins":1,"reason":"daily_open"}, "me": <Summary>}`.

### `POST /api/points/claim` — consent, `auth.Write` + `POINTS_CLAIMS_PER_HOUR` (20)
`{"action": "roadmap_step", "ref": "internship:2"}` (`ref` = `^[a-z0-9_-]{1,40}:[0-9]{1,2}$`). Басқа әрекет → `400 bad_claim`.
Клиент жіберген `xp`/`coins` өрістері еленбейді. Жауап `daily`-дегідей.

### `POST /api/points/freeze` — consent, `auth.Write`
`{"key": "<8–64 [A-Za-z0-9_-]>"}`. 20 монета, бір уақытта ең көбі 1 қорғаныс. Сол кілтпен қайталау — no-op.
`409 not_enough_coins`, `409 freeze_owned`. `200 {"award": null, "me": …}`.

### `PATCH /api/points/settings` — consent, `auth.Write`
`{"studyGroup": "ис-21"}` (1–24 таңба: әріп, сан, бос орын, `-/._`; сервер бас әріпке айналдырады; `""` өшіреді)
және/немесе `{"leaderboardOptOut": true}`. `400 bad_group`.

### `GET /api/points/leaderboard?scope=group|faculty` — signed in
Тек апталық XP (`xp_events.created_at >= weekStart`). Аумақ — студенттің **өз** университеті + факультеті
(`faculty`) не университеті + `study_group` (`group`). Рейтингтен бас тартқандар ешкімге көрінбейді және санға
кірмейді. Көрінетін мүше 5-тен аз болса — тізім жоқ. Атау қысқартылады: «Аружан С.».
```json
{"scope": "faculty", "status": "ok", "members": 23, "minMembers": 5, "optedOut": false,
 "weekStart": "2026-11-01T19:00:00Z", "resetsAt": "2026-11-08T19:00:00Z",
 "leaders": [{"rank": 1, "name": "Аружан С.", "xp": 120, "me": false}, …]}
```
`status`: `ok` | `too_small` (`leaders` жоқ) | `no_scope` (профильде университет/факультет не топ жоқ). Үздік 20 + өз орның.

### `GET /api/points/rewards` — signed in
`{"rewards": [{"id", "kind", "titleKk", "titleRu", "descKk", "descRu", "cost", "stock", "perUserLimit", "discountId", "active", "mine"}]}`
`stock: null` — шектеусіз; `mine` — қабылданбағаннан басқа өз өтінімдерің саны.

### `POST /api/points/redeem` — consent, `auth.Write` + `POINTS_REDEEMS_PER_HOUR` (10)
`{"rewardId": 3, "key": "<idempotency key>"}`. Монета бірден ұсталады, өтінім `pending`.
`201 {"redemption": {…, "status": "pending", "code": null}, "me": …}`; сол кілтпен қайталау → `200` және сол өтінім.
Қателер: `404 reward_unavailable` (жоқ, мұрағатта не таусылды), `409 limit_reached`, `409 not_enough_coins`.

### `GET /api/points/redemptions` — signed in
Өз өтінімдерің (соңғы 50). `code` тек `approved` кезде көрінеді; `note` — әкімші ескертпесі не бас тарту себебі.

## Әкімші маршруттары (`admin`, бәрі `FEATURE_POINTS` астында)

| маршрут | не |
|---|---|
| `GET /api/admin/points/rewards` | бүкіл каталог (+ `codesLeft`), `kinds` |
| `POST /api/admin/points/rewards` | жаңа сыйлық → `201 {"reward"}` |
| `PUT /api/admin/points/rewards/{id}` | өзгерту; мұрағат = `"active": false` (өшірілмейді — тарих сақталады) |
| `GET /api/admin/points/redemptions?status=pending\|approved\|rejected` | кезек (pending — ескісі бірінші), `userId`, `userName`, `userBalance` |
| `POST /api/admin/points/redemptions/{id}/approve` | `{"code"?, "note"?}`; код бос болса — қордан келесі код; `discount` түрі кодсыз → `400 code_required` |
| `POST /api/admin/points/redemptions/{id}/reject` | `{"note": "себеп (3–500)"}`; монета **сол транзакцияда** қайтарылады (`redeem_refund`), қор қалпына келеді |
| `GET /api/admin/points/users/{userId}` | әмиян + `ledgerSum` (кэшпен тең болуы керек) + монета журналы (500 жол) |
| `POST /api/admin/points/users/{userId}/adjust` | `{"delta": -5, "reason": "5–500 таңба", "key"}`; баланс 0-ден төмен түспейді (`409`); `audit_log` (`points_adjust`) |

Сыйлық денесі: `kind` (`discount` / `merch` / `print_quota` / `priority_event`), `titleKk`, `titleRu` (2–120), `descKk`, `descRu` (≤600),
`cost` (1–100000), `stock` (`null` = шектеусіз), `perUserLimit` (1–50), `discountId` (`discount` үшін міндетті, `/api/content`
жеңілдігінің id-і), `active`, `addCodes` (≤500 жол). Қате: `400 {"error":{"code":"invalid","field":"cost",…}}`.

Шешімдер `audit_log`-қа жазылады (`points_reward_save`, `points_redeem_approve`, `points_redeem_reject`, `points_adjust`).
Студентке хабарлама ортақ диспетчер арқылы (`notify.Send`, санат `moderation`, сілтеме `/points`): алдымен inbox,
Telegram — тыныштық/лимит ережелерімен. Промокодтың өзі хабарламаға **салынбайды** — тек сайтта көрінеді.

## Фронтенд

- Басты бет: батырма-жол `#pointsChip` (деңгей · серия · монета), тек кірген студентке және қосқыш қосулы болса;
  күніне бір рет `POST /api/points/daily` (құрылғыда `studenthub.points.daily` = `<userId>:<Almaty күні>`), XP берілсе — toast.
- `/points`: деңгей жолағы, серия және қорғаныс, бүгінгі тапсырмалар, «Рейтинг / Дүкен / Өтінімдерім», монета тарихы.
  Кірмеген студентке — `SignInCard` (жалған жергілікті монета жоқ).
- Квиз нәтижесі, QR check-in және жоспар қадамынан кейін `+10 XP` toast (`awardText`).
- `/admin/points`: кезек, сыйлықтар каталогы, студенттің есепшоты және себеппен түзету.
- Мәзір (`NavDrawer`, «Менікі») және Профиль — «Ұпайлар».
