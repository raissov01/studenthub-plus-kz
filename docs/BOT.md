# Student Hub еске салу боты

Боттың бір ғана міндеті бар: студент сайтта сақтаған мүмкіндіктің мерзімін өткізіп алмауы керек.
Сайт `https://t.me/<BOT_USERNAME>?start=remind_<id>` сілтемесін береді, бот мерзімге 7, 3 және 1 күн
қалғанда (Алматы уақыты) еске салады. Бір студентке күніне ең көбі **бір** хабарлама жіберіледі.

## Файлдар

| Файл | Не істейді |
| --- | --- |
| `api/telegram.js` | Telegram webhook (`X-Telegram-Bot-Api-Secret-Token` тексереді, қате болса 401) |
| `api/cron.js` | Күнделікті жұмыс (`Authorization: Bearer <CRON_SECRET>` тексереді, қате болса 401) |
| `lib/bot/bot.js` | `/start remind_<id>`, `/my`, `/pause`, `/resume`, `/stop`, `/help`, `/stats`, батырмалар |
| `lib/bot/cron.js` | 7/3/1 күндік еске салу, апталық дайджест |
| `lib/bot/store.js` | Сақтау интерфейсі: Upstash Redis REST және тесттерге арналған жадтағы нұсқа |
| `lib/bot/texts.js` | Боттың барлық мәтіні, қазақша және орысша |
| `lib/bot/content.js` | Мүмкіндіктерді `SITE_URL/api/content` арқылы алады, болмаса `SITE_URL/data/opportunities.json` |
| `tests/bot.test.mjs` | Тесттер: `npm test` немесе `node --test tests/*.test.mjs` |

Ескерту: Node 22-де `node --test tests/` (папка) жұмыс істемейді — файл үлгісін (glob) беру керек.

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
| `BOT_TOKEN` | BotFather берген токен |
| `WEBHOOK_SECRET` | кездейсоқ жол, тек `A-Z a-z 0-9 _ -`, 1–256 таңба. Жасау: `openssl rand -hex 32` |
| `CRON_SECRET` | кездейсоқ жол, кемінде 16 таңба: `openssl rand -hex 32`. Vercel Cron оны өзі `Authorization: Bearer ...` тақырыбында жібереді |
| `SITE_URL` | `https://studenthub-plus-kz.vercel.app` |
| `ADMIN_IDS` | `/stats` көре алатын Telegram user id-лер, үтір арқылы: `123456789,987654321`. Өз id-ңді [@userinfobot](https://t.me/userinfobot) арқылы біл |
| `DIGEST_ENABLED` | `false` (әдепкі). Апталық дайджестті қосу үшін `true` |
| `KV_REST_API_URL`, `KV_REST_API_TOKEN` | Upstash-ты Vercel Marketplace арқылы қоссаң, өзі қосылады (3-бөлім) |
| немесе `UPSTASH_REDIS_REST_URL`, `UPSTASH_REDIS_REST_TOKEN` | Upstash консолінен қолмен алсаң |

Айнымалыларды өзгерткеннен кейін **қайта деплой** керек (Deployments → ... → Redeploy).
Сақтау орны бапталмаса, бот әдейі қате береді (жадта сақтап, деректі жоғалтып алмас үшін).

## 3. Upstash Redis қосу (Vercel Marketplace)

1. Vercel → Project → **Storage** → **Create Database** (немесе Marketplace) → **Upstash** → **Upstash for Redis**.
2. Аймақты таңда (Еуропаға жақыны, мысалы `eu-central-1` / Frankfurt), **Free** жоспарын таңда.
3. Дерекқорды осы жобаға **Connect** ет — Vercel `KV_REST_API_URL` және `KV_REST_API_TOKEN` айнымалыларын өзі қосады
   (жаңа нұсқаларында `UPSTASH_REDIS_REST_URL`/`UPSTASH_REDIS_REST_TOKEN` деп те қосуы мүмкін — бот екеуін де оқиды).
4. Қайта деплой жаса.

Redis-тегі кілттер: `users` (id жиыны), `u:<id>` (студент), `r:<id>` (еске салулар), `d:<id>` (бүгін хабарлама
кетті белгісі, 3 күнде өзі өшеді), `ev:submitted` (анонимді «Тапсырдым» оқиғалары, 60 күн сақталады).
`/stop` студенттің `u:`, `r:`, `d:` кілттерін және `users` жиынындағы жазбасын шынымен өшіреді.

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
5. Telegram-да бір хабарлама келеді: атауы, қалған күн, құжаттар тізімі, «Ашу», «Тапсырдым ✅», «Қажет емес».
6. Curl-ды тағы іске қос — хабарлама келмейді, `"alreadySent":1`.
7. «Тапсырдым ✅» бас — батырмалар жоғалады, `/my` бос. Админ ретінде `/stats` жазсаң, «Тапсырдым» саны 1.
8. `/stop` → «Иә, бәрін өшір» — Redis-тен `u:<id>`, `r:<id>`, `d:<id>` жоғалады.

Жергілікті тесттер: `npm test` (Telegram мен сайт fetch-пен имитацияланады, нақты API шақырылмайды).

## 7. Мінез-құлық ережелері

- Күніне бір хабарлама: бір студенттің сол күні мерзімі келген барлық еске салуы бір хабарламаға біріктіріледі,
  ең жақын мерзім бірінші. Хабарлама алдында `d:<id>` белгісі атомды түрде қойылады (`SET ... GET`).
- Студент ботты бұғаттаса (Telegram 403) — `active=false`, келесі cron-дар оны өткізіп жібереді.
  Ботқа қайта жазса, қайта белсенді болады.
- «Тапсырдым ✅» мен «Қажет емес» екеуі де еске салуды тоқтатады; тек «Тапсырдым» статистикаға кіреді.
- Мерзімі өткен еске салулар cron кезінде автоматты өшіріледі. Сайтта мерзім өзгерсе, бот жаңа мерзімді алады.
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
- Hobby-де бір деплойда ең көбі 12 serverless функция: бот тек 2 функция қосады (`api/telegram.js`, `api/cron.js`).

**Upstash Redis Free** ([upstash.com/pricing/redis](https://upstash.com/pricing/redis)):
- айына **500 000 команда**;
- деректер көлемі **256 MB** (беттің бір жерінде 250 MB деп жазылған);
- айына **10 GB** трафик;
- **1 дерекқор**;
- сұраныс көлемі ең көбі 10 MB, жазба 100 MB, секундына 10 000 команда.

Шамамен есеп: cron күніне `2 + 2N` команда жұмсайды (N — студент саны) + әр хабарлама үшін 1, әр webhook жаңартуы
3–5 команда. 1000 студентке айына ~70 000 команда — тегін шектен әлдеқайда төмен.
