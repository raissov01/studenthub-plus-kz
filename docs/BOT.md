# Student Hub еске салу боты

Боттың бір ғана міндеті бар: студент сайтта сақтаған мүмкіндіктің мерзімін өткізіп алмауы керек.
Сайт `https://t.me/<BOT_USERNAME>?start=remind_<id>` сілтемесін береді, бот мерзімге 7, 3 және 1 күн
қалғанда (Алматы уақыты) еске салады. Бір студентке күніне ең көбі **бір** хабарлама жіберіледі.

Бот Go-да жазылған (`pkg/bot`), деректер Postgres-те (Supabase) сақталады. Ескі Node.js + Upstash Redis
нұсқасы `legacy/` ішінде тек анықтама ретінде қалды — ол енді деплой болмайды, Upstash керек емес.

## Файлдар

| Файл | Не істейді |
| --- | --- |
| `pkg/bot/bot.go` | `POST /api/telegram` (webhook, `X-Telegram-Bot-Api-Secret-Token` тексереді, қате болса 401) және `GET /api/cron` (`Authorization: Bearer <CRON_SECRET>`, қате болса 401) |
| `pkg/bot/handlers.go` | `/start remind_<id>`, `/my`, `/pause`, `/resume`, `/stop`, `/help`, `/stats`, батырмалар |
| `pkg/bot/cron.go` | 7/3/1 күндік еске салу, апталық дайджест |
| `pkg/bot/texts.go` | Боттың барлық мәтіні, қазақша және орысша |
| `pkg/bot/content.go` | Мүмкіндіктерді `SITE_URL/api/content` арқылы алады, болмаса `SITE_URL/data/opportunities.json` (бір шақыруға бір рет) |
| `pkg/bot/telegram.go` | Telegram Bot API клиенті (тек stdlib `net/http`) |
| `pkg/db/queries/bot.sql` | Барлық SQL сұраныстары; Go коды `sqlc generate` арқылы `pkg/db/q/` ішіне жасалады |
| `pkg/server/routes_bot.go` | Ботты ортақ `/api/*` роутерге қосады |
| `supabase/migrations/20261028000003_bot.sql` | Бот үшін қосымша бағандар (`reminders.title_ru`, дайджест күйі) |
| `pkg/bot/bot_test.go` | Тесттер: `go test ./pkg/bot/...` |

Барлық `/api/*` бір ғана Vercel Go функциясы арқылы жүреді (`api/index.go`), бот бөлек функция қоспайды.

## 1. BotFather-де бот жасау

1. Telegram-да [@BotFather](https://t.me/BotFather) ашып, `/newbot` жаз.
2. Атауын (мысалы, `Student Hub`) және username-ін (мысалы, `studenthub_kz_bot`, соңы `bot` болуы шарт) бер.
3. BotFather берген токенді сақта — бұл `BOT_TOKEN`. Оны ешкімге көрсетпе, репозиторийге салма.
4. Қосымша (міндетті емес): `/setdescription`, `/setabouttext`, `/setuserpic`.
5. Сайттағы «Еске салу» батырмасы `https://t.me/<BOT_USERNAME>?start=remind_<id>` сілтемесіне апаруы керек
   (`<BOT_USERNAME>` — `@` белгісіз username).

## 2. Vercel-де орта айнымалылары (Environment Variables)

Vercel → Project → Settings → Environment Variables → **Production** (қаласаң, Preview де):

| Айнымалы | Мәні |
| --- | --- |
| `DATABASE_URL` | Supabase Postgres қосылым жолы (3-бөлім). Бүкіл Go API-мен ортақ |
| `BOT_TOKEN` | BotFather берген токен |
| `WEBHOOK_SECRET` | кездейсоқ жол, тек `A-Z a-z 0-9 _ -`, 1–256 таңба. Жасау: `openssl rand -hex 32` |
| `CRON_SECRET` | кездейсоқ жол, кемінде 16 таңба: `openssl rand -hex 32`. Vercel Cron оны өзі `Authorization: Bearer ...` тақырыбында жібереді |
| `SITE_URL` | `https://studenthub-plus-kz.vercel.app` (соңында `/` жоқ) |
| `ADMIN_IDS` | `/stats` көре алатын Telegram user id-лер, үтір арқылы: `123456789,987654321`. Өз id-ңді [@userinfobot](https://t.me/userinfobot) арқылы біл |
| `DIGEST_ENABLED` | `false` (әдепкі). Апталық дайджестті қосу үшін `true` |
| `TELEGRAM_API_BASE` | қоймау керек (әдепкі `https://api.telegram.org`); тек тесттерге арналған |

Айнымалыларды өзгерткеннен кейін **қайта деплой** керек (Deployments → ... → Redeploy).
`DATABASE_URL` жоқ болса, `/api/telegram` пен `/api/cron` 503 қайтарады (секрет тексерілгеннен кейін) —
бот деректі жадта сақтап, жоғалтып алмас үшін әдейі солай.

## 3. Postgres (Supabase)

1. Supabase жобасында: Project Settings → Database → **Connection string** → **Transaction pooler** (порт `6543`).
   Құпия сөзді қойып, `DATABASE_URL` ретінде Vercel-ге сал. Go API prepared statement қолданбайды, сондықтан
   transaction pooler-мен жұмыс істейді.
2. Миграцияларды қолдан (`supabase/migrations/*.sql` ретімен, әрқайсысы бір рет):

   ```bash
   DATABASE_URL='postgresql://...' go run ./cmd/migrate
   ```

Бот қолданатын кестелер:

| Кесте | Не сақталады |
| --- | --- |
| `users` | студент `telegram_id` бойынша. Тек бот арқылы келген студенттің жолы да осында (`consent_at` бос). Бот бағандары: `bot_started_at`, `bot_inactive`, `paused_until`, `safety_note_sent`, `lang`, `digest_city`, `digest_direction`, `digest_asked`, `digest_week`, `digest_seen` |
| `reminders` | `(user_id, opportunity_id)`, `title` (kk), `title_ru`, `deadline`, `status`: `active` / `submitted` («Тапсырдым») / `dismissed` («Қажет емес») |
| `bot_daily_sends` | `(user_id, day)` — «бүгін хабарлама кетті» белгісі; 3 күннен ескісін cron өзі өшіреді |
| `bot_events` | `submitted`, `dismissed` оқиғалары (`/stats` үшін) |

`/stop` → «Иә, бәрін өшір»: студенттің `reminders` және `bot_daily_sends` жолдары шынымен өшеді, `bot_events`-тегі
сілтемесі алынады (статистика анонимді қалады). Сайтта аккаунты жоқ болса (`consent_at` бос) — `users` жолы да
өшеді. Сайтта аккаунты бар болса — аккаунт қалады, тек бот бағандары тазаланады (аккаунтты өшіру — сайттың өз ісі).

## 4. Webhook орнату

Деплойдан кейін бір рет орында (мәндерді өзіңдікіне ауыстыр):

```bash
BOT_TOKEN='123456:ABC...'
WEBHOOK_SECRET='...'   # Vercel-дегімен бірдей

curl -sS "https://api.telegram.org/bot${BOT_TOKEN}/setWebhook" \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://studenthub-plus-kz.vercel.app/api/telegram",
    "secret_token": "'"${WEBHOOK_SECRET}"'",
    "allowed_updates": ["message", "callback_query", "my_chat_member"],
    "drop_pending_updates": true
  }'
```

Тексеру:

```bash
curl -sS "https://api.telegram.org/bot${BOT_TOKEN}/getWebhookInfo"
```

`url` дұрыс болуы және `last_error_message` бос болуы керек. Секретсіз немесе қате секретпен келген сұранысқа
`/api/telegram` 401 қайтарады.

Команда мәзірі (міндетті емес, бір рет):

```bash
curl -sS "https://api.telegram.org/bot${BOT_TOKEN}/setMyCommands" -H 'Content-Type: application/json' -d '{
  "commands": [
    {"command": "my", "description": "Сақталған тізім"},
    {"command": "pause", "description": "7 күнге кідірту"},
    {"command": "resume", "description": "Қайта қосу"},
    {"command": "stop", "description": "Барлық деректі өшіру"},
    {"command": "help", "description": "Анықтама"}
  ]}'
curl -sS "https://api.telegram.org/bot${BOT_TOKEN}/setMyCommands" -H 'Content-Type: application/json' -d '{
  "language_code": "ru",
  "commands": [
    {"command": "my", "description": "Сохранённый список"},
    {"command": "pause", "description": "Пауза на 7 дней"},
    {"command": "resume", "description": "Включить снова"},
    {"command": "stop", "description": "Удалить все данные"},
    {"command": "help", "description": "Справка"}
  ]}'
```

## 5. Cron

`vercel.json` ішінде: `"crons": [{ "path": "/api/cron", "schedule": "0 5 * * *" }]` — күн сайын 05:00 UTC =
10:00 Алматы (UTC+5). **Hobby жоспарында cron сағат ішінде кез келген уақытта іске қосылуы мүмкін**, яғни
10:00–10:59 аралығында. Cron тек **production** деплойында жұмыс істейді.

Тексеру: Vercel → Project → **Settings → Cron Jobs** — `/api/cron` көрінуі керек; сол жерден **Run** батырмасымен
іске қосып, **Logs** арқылы нәтижені көруге болады (логта `cron {"date":...,"reminders":N,...}` жолы шығады).

Қолмен іске қосу:

```bash
CRON_SECRET='...'   # Vercel-дегімен бірдей
curl -sS -H "Authorization: Bearer ${CRON_SECRET}" https://studenthub-plus-kz.vercel.app/api/cron
# {"ok":true,"date":"2026-11-07","reminders":1,"digests":0,"alreadySent":0,"blocked":0,"failed":0,"cleaned":0}
```

Бір күнде екінші рет іске қоссаң, ешкімге қайта жіберілмейді (`alreadySent` санағы өседі) — бұл әдейі.
Қате секрет → 401.

## 6. Тексеру сценарийі

1. Сайтта мерзіміне дәл **7, 3 немесе 1 күн** қалған мүмкіндікті тап (әйтпесе бүгін еске салатын ештеңе жоқ;
   мысалы, мерзімі 14 қараша болса — 7 қараша күні тексер). «Еске салу» батырмасын бас.
2. Telegram ботты ашады, **Start** бас. Бот «“<атауы>” мерзімі — … . … күн қалғанда еске саламын ✅» деп
   жауап береді, бірінші рет болса — «Бот ешқашан ақша сұрамайды…» ескертуін де жібереді.
3. `/my` жаз — мүмкіндік тізімде, жанында «Өшіру» батырмасы бар.
4. Cron-ды қолмен іске қос (5-бөлімдегі curl). Жауапта `"reminders":1`.
5. Telegram-да бір хабарлама келеді: атауы, қалған күн, құжаттар тізімі, «Ашу» (`SITE_URL/opportunity/<id>`),
   «Тапсырдым ✅», «Қажет емес».
6. Curl-ды тағы іске қос — хабарлама келмейді, `"alreadySent":1`.
7. «Тапсырдым ✅» бас — батырмалар жоғалады, `/my` бос. Админ ретінде `/stats` жазсаң, «Тапсырдым» саны 1.
8. `/stop` → «Иә, бәрін өшір». Supabase SQL Editor-да тексер (`<TG_ID>` — өз Telegram id-ң):

   ```sql
   select count(*) from users where telegram_id = <TG_ID>;          -- 0 (сайтта аккаунт болмаса)
   select count(*) from reminders r join users u on u.id = r.user_id where u.telegram_id = <TG_ID>;  -- 0
   ```

Жергілікті тесттер (Telegram мен сайт `httptest` арқылы имитацияланады, нақты API шақырылмайды):

```bash
go test ./pkg/bot/...
```

Тесттер `postgres://postgres:dev@localhost:54329/studenthub` серверінде (басқасы керек болса — `TEST_DATABASE_URL`)
уақытша `studenthub_bot_test_<pid>` дерекқорын жасап, барлық миграцияны қолданады, соңында өшіреді.
Postgres қолжетімсіз болса, DB тесттері `SKIP` болады.

SQL сұранысын өзгертсең: `pkg/db/queries/bot.sql` → `sqlc generate` (`~/go/bin/sqlc`).

## 7. Мінез-құлық ережелері

- Күніне бір хабарлама: бір студенттің сол күні мерзімі келген барлық еске салуы бір хабарламаға біріктіріледі,
  ең жақын мерзім бірінші. Хабарлама алдында `bot_daily_sends`-ке `(user_id, бүгін)` жолы атомды түрде
  қосылады (`on conflict do nothing`); жол бұрыннан болса — жіберілмейді. Жіберу 403-тен басқа қатемен құласа,
  белгі алынады (сол күні қайталауға болады).
- Студент ботты бұғаттаса (Telegram 403 немесе `my_chat_member` → `kicked`) — `bot_inactive = true`, келесі
  cron-дар оны өткізіп жібереді. Ботқа қайта жазса, қайта белсенді болады.
- «Тапсырдым ✅» мен «Қажет емес» екеуі де еске салуды тоқтатады (`reminders.status` → `submitted` / `dismissed`,
  `bot_events`-ке жазылады); `/stats`-қа тек «Тапсырдым» кіреді.
- Мерзімі өткен белсенді еске салулар cron кезінде автоматты өшіріледі. Сайтта мерзім өзгерсе, бот жаңа мерзімді алады.
- `/pause` — 7 күнге кідірту, `/resume` — қайта қосу.
- Апталық дайджест (`DIGEST_ENABLED=true` болғанда ғана): алғашқы еске салудан кейін студенттен бір рет қала мен
  бағытты сұрайды; бейсенбі күні соңғы 7 күнде қосылған 2–3 сай мүмкіндікті жібереді. Сол күні еске салу кеткен
  болса, дайджест келесі күнге ауысады (аптасына бір рет).
- Жаппай тарату (broadcast) функциясы **жоқ**, әдейі.

## 8. Тегін тарифтің шектері (2026-10-28 тексерілді)

**Vercel Hobby, Cron** ([vercel.com/docs/cron-jobs/usage-and-pricing](https://vercel.com/docs/cron-jobs/usage-and-pricing), бет 2026-07-15 жаңартылған):
- жобаға 100 cron job;
- ең жиі — **күніне бір рет** (жиірек өрнек деплойда қате береді);
- дәлдік — **сағат ішінде (±59 мин)**: `0 5 * * *` 05:00–05:59 UTC аралығында іске қосылады;
- cron кәдімгі Vercel Function-ды шақырады, сондықтан функция шектері мен бағасы қолданылады.
- Бот жаңа функция қоспайды: бүкіл `/api/*` бір Go функциясында.

Postgres жүктемесі аз: cron күніне бірнеше сұраныс + әр студентке бір `insert`, әр webhook жаңартуы 2–4 сұраныс.
