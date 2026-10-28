# Market API — «Отдам даром» (prompt 08)

Server: `pkg/market`, wired in `pkg/server/routes_market.go`. Schema: `supabase/migrations/20261028000009_market.sql`.
Spec: `docs/prompts/08-market.md`.

## Conventions

Same as `docs/api/mentors.md`: switch `FEATURE_MARKET` (off → every route below, photos included, is `404 not_found`); session cookie or Bearer; writes need an allowed `Origin` and are rate limited; errors are `{"error": {"code", "message"}}` with kk/ru messages — branch on `code`.

**Free only.** There is no price field anywhere; `POST/PATCH` with unknown fields (e.g. `price`) answer `400 bad_request`. Selling is handled through reports.

### Enums

| Name | Values (UI label) |
|---|---|
| `category` | `books` (кітап-конспект), `dorm` (жатақхана-тұрмыс), `tech` (техника), `clothes` (киім), `sport` (спорт), `other` (басқа) |
| `condition` | `new` (жаңа), `good` (жақсы), `used` (қолданылған) |
| `status` | `pending` (young account's first item, in the queue), `active`, `reserved` (брондалды), `given` (берілді ✅), `hidden`, `expired` |
| `hiddenReason` | `reports` (auto-hidden, reversible), `admin` (hidden for good) — only in owner/admin views |
| report `reason` | `selling` (сатып жатыр), `prohibited` (тыйым салынған зат), `scam` (алаяқтық), `other` (басқа) |

## Endpoints

| Method & path | Who | Purpose |
|---|---|---|
| `GET /api/market/items` | anyone | Browse grid / search |
| `GET /api/market/items/{id}` | anyone (owner/admin for non-public states) | Item page |
| `POST /api/market/photos` | signed in, consented | Upload one photo (before creating the item) |
| `GET /api/market/photos/{name}` | anyone for public items; uploader/admin otherwise | Photo bytes |
| `GET /api/market/me` | signed in | Can I publish? Why not? |
| `POST /api/market/rules/accept` | signed in, consented | One-time rules screen |
| `POST /api/market/items` | publisher (see rules) | Publish |
| `GET /api/market/mine` | signed in | My items, all states |
| `PATCH /api/market/items/{id}` | owner, item active/reserved | Edit, reserve / un-reserve |
| `POST /api/market/items/{id}/given` | owner, item active/reserved | "Берілді ✅" |
| `POST /api/market/items/{id}/extend` | owner, item active/reserved | +30 days from now |
| `DELETE /api/market/items/{id}` | owner | Delete with photos |
| `POST /api/market/items/{id}/report` | signed in, consented, not the owner | Report |
| `GET /api/admin/market/items?status=` | admin | Reported / hidden / pending / active items |
| `POST /api/admin/market/items/{id}/hide` | admin | Hide for good (reason, audited) |
| `POST /api/admin/market/items/{id}/restore` | admin | Reports groundless → visible again (reason, audited) |
| `POST /api/admin/market/bans` | admin | Temporary publishing ban (reason, audited) |
| `DELETE /api/admin/market/bans/{userId}` | admin | Lift a ban (reason, audited) |
| `GET /api/cron/market` | `Authorization: Bearer $CRON_SECRET` | Daily job |

Item JSON (all list/detail responses):

```json
{
  "id": 17, "title": "Физика оқулығы", "desc": "1-курсқа", "category": "books", "condition": "good",
  "university": "ENU", "photos": ["/api/market/photos/3f9c…e1.jpg"], "status": "active",
  "createdAt": "...", "expiresAt": "...", "daysAgo": 2, "mine": false,
  "pickup": "4-корпус, кіреберіс", "pickupHint": "university",
  "author": {"name": "Айгерім", "username": "aigerim_k", "telegramUrl": "https://t.me/aigerim_k"}
}
```

- **Anonymous visitors get `"pickup": null` and `"author": null`** — show only the item and the generic "университет аумағында" label (`pickupHint`). Name, @username, "Жазу 💬" (`telegramUrl`) and the exact pickup place are for signed-in students only.
- `hiddenReason` and `givenAt` appear only for the owner (and admin).

### GET /api/market/items

Query: `university` (default: the viewer's own; `all` = every university; anonymous default = all), `category`, `condition`, `q` (search in title/description, ≤64 chars — use it for `searchItems()` "Тегін заттар"), `before` (id cursor), `limit` (1–48, default 24).

```json
{"items": [ ... ], "university": "ENU", "season": {"from": "2026-12-15", "to": "2027-01-31", "active": true}, "nextBefore": 5}
```

Only `active`/`reserved`, not expired, newest first. `nextBefore` is present when another page may exist. `season` is the same object as `/api/config` → `marketSeason` (or `null`).

### GET /api/market/items/{id}

`{"item": {...}}`; 404 `item_not_found` when not public and not yours.

### POST /api/market/photos

Send `multipart/form-data` with a field **`photo`** (what `FormData` produces) or the raw image as the body. The server:

- reads at most **600 KB** (`MARKET_PHOTO_MAX_KB`) → 413 `photo_too_large`;
- checks the real type by magic bytes — only JPEG, PNG, WebP; SVG, HTML, GIF, HEIC… → 415 `photo_type` (the client's `Content-Type` and file name are ignored);
- refuses canvases over 12 MP → 400 `photo_dimensions`; undecodable → 400 `photo_invalid`;
- decodes and re-encodes to JPEG, long side ≤ 1280 px, ~≤ 500 KB. **This drops EXIF/GPS and every other metadata block.** The stored name is random (`<32 hex>.jpg`), never derived from the upload.

201 `{"name": "3f9c…e1.jpg", "url": "/api/market/photos/3f9c…e1.jpg", "width": 1280, "height": 960, "bytes": 182311}`. Errors also: 403 `consent_required`, 429 `rate_limited` (30 uploads/hour, `MARKET_PHOTOS_PER_HOUR`).

Client-side resizing is still recommended (spec 08: canvas → 1280 px, ≤ 500 KB) so phone photos fit the 600 KB limit. Upload up to 3 photos, then pass their `name`s to `POST /api/market/items`. Photos not attached to an item within 24 h are deleted.

### GET /api/market/photos/{name}

`image/jpeg`, `Cache-Control: public, max-age=86400` for active/reserved items; `private, no-store` for the uploader's/admin's own view of unattached or non-public photos; 404 otherwise (also for any name that is not `<32 hex>.jpg` — no listing, no traversal). Supports `If-Modified-Since`/`Range`.

### GET /api/market/me

```json
{"canPublish": false, "blockers": ["no_username", "no_rules"], "rulesAccepted": false, "hasUsername": false,
 "banUntil": null, "today": 0, "dailyLimit": 3}
```

`blockers` (in check order) use the same codes as publish errors: `consent_required`, `no_account` (no university in profile), `no_username`, `minor`, `no_rules`, `banned`, `daily_limit`.

### POST /api/market/rules/accept

No body → `{"ok": true}`. Idempotent. Show the rules screen once (free only; prohibited items: medicines, alcohol and tobacco, documents and student cards, animals, weapons, money and payment cards; meet only on campus or in a crowded place).

### POST /api/market/items

```json
{"title": "≤60", "desc": "≤300", "category": "books", "condition": "good", "pickup": "≤120", "photos": ["name1.jpg"]}
```

`photos` ≤ 3, each a name you uploaded and not yet attached. 201 `{"item": {...}, "pending": false}`. `pending: true` → a young account's (< 7 days) first item went to the moderation queue (`market_item`); tell the user it will appear after a check.

| Code | Status | UI |
|---|---|---|
| `no_account` | 403 | fill university in the profile |
| `no_username` | 403 | "Сатып алушы сізге жаза алуы үшін Telegram-да username орнатыңыз" — then sign in again (the username is refreshed at login) |
| `minor` | 403 | publishing is 18+ (see open questions) |
| `no_rules` | 403 | show the rules screen, `POST /rules/accept`, retry |
| `banned` | 403 | temporary ban (`/api/market/me` → `banUntil`) |
| `daily_limit` | 429 | max 3 items per day (deleting does not free a slot) |
| `bad_photo` | 400 | a photo is not yours / already used / expired — re-upload |

### Owner actions

- `GET /api/market/mine` → `{"items": [...]}` every state, with `hiddenReason`.
- `PATCH /api/market/items/{id}` — any of `title, desc, category, condition, pickup, status ("active"|"reserved")` → `{"item": {...}}`. Photos cannot be changed (delete and re-publish).
- `POST /api/market/items/{id}/given` → `{"ok": true, "badgeAwarded": false}`. Photos are deleted. The 3rd given item awards the self-declared `generous` badge ("Жомарт", shown in the portfolio as self-declared, *not* "Расталған").
- `POST /api/market/items/{id}/extend` → `{"item": {...}}` with `expiresAt` = now + 30 days.
- `DELETE /api/market/items/{id}` → `{"deleted": true}`; photos deleted.

All four answer **404 `item_not_found`** for someone else's item and for the owner's `hidden`/`pending`/`expired`/`given` items (RLS: owners edit only their own active or reserved items, and cannot unhide).

### POST /api/market/items/{id}/report

`{"reason": "selling", "text": "optional ≤300"}` → `{"alreadyReported": false, "hidden": false}`. One report per student per item. The first report opens a `market_report` submission and DMs the admins. When **3** (`MARKET_REPORT_HIDE`) *counted* reporters — distinct accounts at least 7 days old (`moderation.CountedReports`) — have reported, the item is hidden (`hiddenReason: reports`) until an admin decides. Errors: 400 `own_item`, 404 `item_not_found`, 403 `consent_required`.

### Admin

- `GET /api/admin/market/items?status=reported|hidden|pending|active&university=` (default `reported` = open reports in any state) → `{"items": [{...item, "ownerId", "reports", "countedReports", "reportReasons": ["selling: 5000 тг сұрады"], "submissionId"}]}`.
- `POST /api/admin/market/items/{id}/hide` `{"reason": "3–500"}` — hidden for good, photos deleted, owner told. Closes the pending `market_report` submission as approved.
- `POST /api/admin/market/items/{id}/restore` `{"reason": "3–500"}` — reports dismissed (they stop counting), the item is visible again (or `expired` if its time ran out). Closes the pending submission as rejected.
- `POST /api/admin/market/bans` `{"userId": 9, "days": 1-365, "reason": "3–500"}` → `{"userId", "until"}`; 404 `user_not_found`.
- `DELETE /api/admin/market/bans/{userId}` `{"reason": "3–500"}` → `{"userId", "lifted": true}`; 404 `not_banned`.

All admin actions write `audit_log` (`market_hide`, `market_restore`, `market_ban`, `market_unban`). 400 `reason_required` without a reason.

### Queue kinds (shared `submissions`)

| kind | payload | approve | reject (reason required) |
|---|---|---|---|
| `market_item` | `{itemId, title, desc, category, photos, reason: "new_account"}` | item `active` (+30 days), owner told | item and photos deleted, owner told with reason |
| `market_report` | `{itemId, title, ownerId, reason, text}` (first report; all reports are in `reports`) | item hidden for good, photos deleted, owner told | reports dismissed, item visible again |

### GET /api/cron/market

Bearer `CRON_SECRET`. Daily, after the bot's `/api/cron`. Expires items after 30 days (photos deleted); sends the owner one digest when an item has ≤ 3 days left ("әлі бересің бе? ұзарту / жабу" → `#/market/mine`); deletes photos never attached within 24 h; during `MARKET_SEASON` sends the "Көшіп жатырсың ба?" digest once per student per season (batch `MARKET_SEASON_BATCH`, default 1000 per run). Digests use the bot's one-message-per-day claim. Response `{"expired", "reminded", "postponed", "orphanPhotos", "seasonSent", "seasonPending"}`.

## Configuration

| Env | Default | |
|---|---|---|
| `FEATURE_MARKET` | off | switch |
| `UPLOAD_DIR` | `./uploads` (server: `/opt/studenthub/uploads`) | photos live in `$UPLOAD_DIR/market/` |
| `MARKET_PHOTO_MAX_KB` | 600 | upload limit |
| `MARKET_PHOTOS_PER_HOUR` | 30 | per user |
| `MARKET_DAILY_LIMIT` | 3 | items per day |
| `MARKET_REPORT_HIDE` | 3 | counted reporters to auto-hide |
| `MARKET_SEASON` | — | `YYYY-MM-DD..YYYY-MM-DD[,…]` (Almaty days, inclusive) → `/api/config` `marketSeason: {from, to, active}` or `null` |
| `MARKET_SEASON_BATCH` | 1000 | seasonal digests per cron run |

Account deletion (`DELETE /api/me`) removes the user's items, photo rows, rules, counters and bans by cascade, and a `me.RegisterDeleteHook` deletes every photo file the user uploaded. `GET /api/me/export` includes `marketItems`, `marketPhotos`, `marketRules`, `marketDaily`, `marketBans`.

## Frontend checklist (screens the spec asks for)

- [ ] Entry points: the "Университет қызметтері" board and search — **no feed on the home page**. Add market items to `searchItems()` as a separate "Тегін заттар" category (`GET /api/market/items?q=`).
- [ ] `#/market` — photo-card grid; filters: university (default own, "all"), category, condition. Card: photo, title, condition, "N күн бұрын". Signed-in: author name, @username, "Жазу 💬" (`telegramUrl`), exact pickup place; anonymous: only "университет аумағында" + sign-in prompt. Reserved items carry a "брондалды" badge. Empty state: "Әзірге зат жоқ. Семестр соңында көп болады — керек емес заттарыңды бірінші болып бер" + publish button. Seasonal banner "Көшіп жатырсың ба? Заттарыңды жаңа студенттерге бер" when `marketSeason.active`.
- [ ] Item page: same visibility rules; "Шағымдану" with the 4 reasons (+ optional text).
- [ ] `#/market/new` — account required; if `/api/market/me` has `no_username`, show the explanation and block the form; rules screen once (`/rules/accept`); up to 3 photos (resize in the browser first, upload each to `/api/market/photos`, show errors by code); fields title ≤60, description ≤300, category, condition, pickup ≤120; **no price field**; "тексерілуде" state when `pending`; daily-limit and ban messages.
- [ ] `#/market/mine` — my items with status; buttons "Брондалды" (PATCH status), "Берілді ✅", "Ұзарту" (extend), "Өшіру" (delete); hidden/pending/expired explanations; "Жомарт" badge toast when `badgeAwarded`.
- [ ] Admin: reported/hidden/pending list with report counts and reasons; "Жасыру" and "Қайта көрсету (негізсіз)" with a reason; ban/unban a student with days + reason. `market_item` / `market_report` also appear in `#/admin/queue` (06).
- [ ] Portfolio (09): `user_badges` row `generous` (not verified) → "Жомарт" as a self-declared badge.
