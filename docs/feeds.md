# Мүмкіндіктер ағыны (ИИ сүзгісімен)

Telegram арналары мен сайттардан стартап, хакатон, грант, стипендия, тағылымдама, академиялық ұтқырлық,
Erasmus+, халықаралық бағдарлама, байқау, конференция, еріктілік туралы посттарды жинап, ИИ арқылы
сүзіп, құрылымдап, модерациядан кейін сайтқа шығарады. Бәрі `FEATURE_FEEDS` қосқышының артында (әдепкі: өшірулі).

```
sources.yaml ─► feeds-collect (30 мин сайын) ─► feed_posts ─► кілт сөз сүзгісі ─► 1 ИИ шақыруы ─► тексеру ─► қайталануды табу
                                                                                                     │
                     /api/content (opportunities, id feed-<n>) ◄── published ◄── /admin/queue (feed_item) ◄── pending
                     іздеу, мүмкіндіктер тізімі, басты бет, күнтізбе          └── автожариялау (сенімді дереккөз + сенімділік ≥ шек)
```

## Қалай жұмыс істейді

1. **Жинау** — `backend/cmd/feeds-collect`. Тізім `backend/feeds/sources.yaml`-да (төменде). Әр жолы әр дереккөздің
   соңғы ~20 посты оқылады; жаңалары `feed_posts`-қа бір рет қана түседі (`unique (source_id, external_id)`).
   30 күннен ескі посттар сақталады, бірақ ИИ-ге жіберілмейді.
2. **Кілт сөз сүзгісі** (тегін): қазақша/орысша/ағылшынша сөз түбірлері (грант, стипенд, хакатон, стажир, erasmus,
   конкурс, deadline, өтінім...). Жарнама (`#реклама`, промокод, ставки...) және тым қысқа посттар өтпейді.
   `relevant: true` дереккөз (тек мүмкіндік жазатын сайт) бұл қадамды өткізеді.
3. **ИИ** — бір `chat/completions` шақыруы, `response_format: json_schema, strict` — жалпақ схема, барлық өріс міндетті,
   `anyOf` жоқ: `is_relevant, category, title_kk, title_ru, summary_kk, summary_ru, organizer, country, city, online,
   deadline, event_date, eligibility, funding, apply_url, source_url, language, confidence, reason`.
   Промпт ойдан қосуға тыйым салады: постта мерзім жоқ болса — `deadline` бос.
4. **Тексеру** (`internal/feeds/validate.go`), ИИ-ге сенбейміз:
   - күн `ЖЖЖЖ-АА-КК` болуы, ақылға қонымды болуы (−1 жыл … +3 жыл) және **посттың мәтінінде жазылуы** керек
     (`20 ноября`, `желтоқсанның 5-і`, `20.11.2026`, `November 30, 2026`...). Жазылмаса — өріс өшіріледі;
   - `apply_url` http(s) болуы және посттың мәтінінде не сілтемелерінде болуы керек; әйтпесе өшіріледі;
   - `source_url` әрқашан посттың өз сілтемесі (ИИ жазғаны елемейді);
   - бір өріс өшірілсе, `confidence` 0.6-дан аспайды, яғни мұндай жазба өздігінен жарияланбайды;
   - белгісіз санат не атаусыз жауап — жазба жасалмайды (`failed`).
5. **Қайталану**: бірдей `apply_url` (utm, www, соңғы `/` есепке алынбайды; жалаң домен не канал түбірі кілт емес),
   бірдей атау + мерзім, не мәтіні дерлік бірдей (simhash, 64 бит, ≤12 бит айырмашылық). Ертерек жазба қалады,
   кейінгі дереккөз `extra_sources`-қа қосылып, сайтта «Басқа дереккөздер» болып көрінеді.
6. **Шешім**: әдепкіде бәрі `/admin/queue` кезегіне түседі (`feed_item`). `FEEDS_AUTOPUBLISH_MIN_CONFIDENCE=0.85`
   қойылса, `trust: high` дереккөздің сенімділігі шектен жоғары жазбасы бірден жарияланады.
7. **Мерзім**: `deadline` өтсе (Астана уақыты) — `expired`, сайттан жоғалады; мерзімсіз болса — `event_date` өткенде;
   екеуі де жоқ болса — 45 күннен кейін (`FEEDS_UNDATED_TTL_DAYS`). Кезектегі мерзімі өткен жазба кезектен
   автоматты түрде шығады.

Жарияланған жазба `/api/content` ішінде кәдімгі мүмкіндік: `id: feed-<n>`, `source: "feed"`, түрі санаттан
(`hackathon/startup/grant/scholarship/competition → grant`, `mobility/exchange → mobility`, `internship → company`,
қалғаны `intl`), плюс `feed` блогы (дереккөз аты, түпнұсқа, өтінім сілтемесі, өтетін күні, кімге, қаржыландыру,
басқа дереккөздер). Сайт карточкада «ИИ · <дереккөз>» белгісін, бетінде **«ИИ қорытындысы, түпнұсқасын
тексеріңіз»** жазуы бар блокты және түпнұсқа постқа сілтемені көрсетеді. Іздеу, мүмкіндіктер тізімі, басты бет
жаңалықтары және күнтізбедегі мерзімдер бұл жазбаларды басқаларымен бірдей алады.

## Әкімшіге

- `/admin/feeds` — бүгінгі ИИ шығыны (сұраныс, токен / лимит), **Күтуде** (бір басуда «Жариялау», «Түзету»,
  «Қабылдамау» + себеп), **Жарияланған** («Сайттан алу»), **Дереккөздер** (қосу/өшіру, соңғы жинау, қате).
- Сол жазбалар `/admin/queue`-да «Мүмкіндіктер ағыны (ИИ)» түрімен де тұр; шешім бір жерден жазылады (`audit_log`).
- Түзетуде тек түпнұсқада жазылғанды енгізіңіз. Өткен мерзім қабылданбайды.

## Telegram-ды оқу

**1. Ашық арналар, әдепкі жол (`kind: tg_public`)** — `https://t.me/s/<username>` веб-көрінісі. Аккаунт, кілт,
арнаға жазылу қажет емес. `User-Agent: StudentHubBot/1.0 (+сайт)`, бір хостқа сұраныстар арасы ≥2 с,
дереккөздер арасы 1.5 с, 30 минутта бір рет. Арна веб-көрінісін өшірсе, «no public preview» қатесі шығады.

**2. MTProto, қосымша (`kind: tg_mtproto`)** — веб-көрінісі жоқ не username-сыз арналар үшін. Кодтың негізі
`~/my-new_project/backend` құралдарынан (`cmd/tg-resolve-chan`, `cmd/tg-chan-read`, gotd/td) алынды. Тек оқиды:
username-ді `contacts.resolveUsername` арқылы табады (бұл арнаға **қосылмайды**), username-сыз арнаны аккаунттың
бұрыннан бар диалогтарынан іздейді, сосын `messages.getHistory`. Ешқашан join/subscribe жасамайды, хабар
жібермейді, «оқылды» деп белгілемейді. Сондықтан `CHANNELS_TOO_MUCH` шегіне әсер етпейді.

Қажет:

| Айнымалы | Мәні |
| --- | --- |
| `TELEGRAM_APP_ID`, `TELEGRAM_APP_HASH` | my.telegram.org қолданбасы (my-new_project `.env`-індегі сол мәндер) |
| `FEEDS_TG_SESSION` | сессия файлының жолы, мысалы `/opt/studenthub/secrets/tg-feeds.session` |

**Сессия файлы — бұл аккаунттың өзі (құпия).** Ережелер: `0600`, иесі `root`, git-тен тыс, ешқашан логқа
жазылмайды. Коллектор файлды бір рет жадқа оқиды, дискіге қайта жазбайды. Серверде файл контейнерге mount
етілмейді: timer оны root ретінде stdin-ге береді (`FEEDS_TG_SESSION=/dev/stdin`).

Аккаунт: **@beknazar_02** (екінші аккаунт; @raissov01 `CHANNELS_TOO_MUCH` шегіне жеткен). Қолмен жасалатын қадам
(серверге мен кірмеймін, мұны иесі өзі жасайды):

```bash
# ноутбуктан
scp ~/my-new_project/backend/.telegram-session-2 root@85.137.91.47:/opt/studenthub/secrets/tg-feeds.session
ssh root@85.137.91.47 'mkdir -p /opt/studenthub/secrets && chmod 700 /opt/studenthub/secrets && \
  chown root:root /opt/studenthub/secrets/tg-feeds.session && chmod 600 /opt/studenthub/secrets/tg-feeds.session'
# /opt/studenthub/.env:
#   FEEDS_TG_SESSION=/opt/studenthub/secrets/tg-feeds.session
#   TELEGRAM_APP_ID=...   TELEGRAM_APP_HASH=...
```

Сессиясыз `tg_mtproto` дереккөздері жай өткізіледі («no reader» қатесі `/admin/feeds`-те көрінеді), қалғаны жұмыс істей
береді. Бір сессия = бір аккаунт: `tg-aitu-career` тек @beknazar_02 оқи алатын арна (сол аккаунт мүше).

## Сайттар

- `kind: rss` — RSS 2.0, RSS 1.0, Atom; HTML сипаттамасы мәтінге айналады, ішіндегі сілтемелер сақталады.
- `kind: html` — тізім беті + селекторлар (`select.item/title/link/date/text`; `tag.class`, `.class`, `#id`,
  `[attr]`, бос орын — ішіндегі, үтір — балама). Селектор ештеңе таппаса — қате (бет құрылымы өзгерді).
- `robots.txt` сақталады (`*` не `StudentHubBot` тобы), Cloudflare / bot-check бар сайттар алынбайды, кіру (login)
  қажет еткен ештеңе оқылмайды.

## ИИ

Чат пен квиз қолданатын сол кілт пен баптаулар: `OPENAI_API_KEY` (balu-fit кілті), `OPENAI_BASE_URL`,
`OPENAI_MODEL` (әдепкі `gpt-4.1-mini`); тек ағын үшін басқа модель керек болса — `FEEDS_AI_MODEL`.
429 / 5xx / 529 (Overloaded) кезінде 3 рет қайталайды (2 → 4 → 8 с, `Retry-After` ескеріледі); сәтсіз пост
келесі жолы қайта жіберіледі. Тәуліктік лимит `FEEDS_AI_DAILY_LIMIT` (токен, әдепкі 150 000) — жетсе, посттар
ертеңге қалады. Әр шақырудың токендері `feed_ai_usage`-ке және логқа жазылады (кілт, пост мәтіні логқа шықпайды).

**Шамалы шығын** (gpt-4.1-mini тізімдік бағасы $0.40 / 1M кіріс, $1.60 / 1M шығыс): бір шақыру ≈ 1 000–1 500
кіріс + ≈ 250 шығыс токен ≈ **$0.001**. Қосулы дереккөздерден күніне ~15–25 жаңа пост, кілт сөз сүзгісінен
кейін ~10–20 шақыру → **күніне ~$0.01–0.02** (~20–30 мың токен). Ең көбі лимитпен шектеледі: 150 000 токен ≈
**$0.10 / күн**. Алғашқы жол: әр дереккөздің соңғы 30 күндік посттары (бір жолда ең көбі 60 шақыру,
`FEEDS_MAX_AI_PER_RUN`).

## Telegram арнасына жариялау (қосымша)

`FEEDS_PUBLISH_CHANNEL` (мысалы `@studywithme_r` не `-100…`) қойылса, жаңа жарияланған жазбалар (соңғы 2 күн,
бір жолда ≤5) сол арнаға бот арқылы жіберіледі: атауы, қысқаша мазмұны, мерзімі, өтінім сілтемесі, сайттағы бет
және «ИИ қорытындысы, түпнұсқасын тексеріңіз» + түпнұсқа. Әр жазба бір-ақ рет (`channel_posted_at`).
Қосу: Telegram-да арнаңызды ашып → Әкімшілер → Әкімші қосу → сайттың ботын (`BOT_USERNAME`) таңдап, тек
«Хабар жариялау» құқығын беріңіз → `/opt/studenthub/.env`-ге `FEEDS_PUBLISH_CHANNEL=@арнаңыз` → келесі жол.

## Қосу қадамдары

1. Деплойдан кейін (`smart-deploy` миграцияны, API image-ін және timer-лерді өзі қолданады)
   `/opt/studenthub/.env`-ге: `FEATURE_FEEDS=true` (қалағаныңызша `FEEDS_AI_DAILY_LIMIT`,
   `FEEDS_AUTOPUBLISH_MIN_CONFIDENCE=0.85`, `FEEDS_PUBLISH_CHANNEL`, MTProto айнымалылары).
2. `cd /opt/studenthub/docker && docker compose up -d api` (API жаңа айнымалыларды алсын: `/admin/feeds` және
   `/api/content` ішіндегі жазбалар).
3. Бірінші жолды қолмен: `systemctl start studenthub-feeds.service; journalctl -u studenthub-feeds -n 50`
   (соңында `report: {...}` жолы). Сосын `studenthub-feeds.timer` әр 30 минут сайын (`:07`, `:37`) іске қосады.
4. `/admin/feeds` → Күтуде: жазбаларды қарап, жариялаңыз. Дереккөзді сол жерден өшіруге болады.

Өшіру: `FEATURE_FEEDS=false` → timer бірден шығады, API жазбаларды бермейді, әкімші беттері 404.

| Айнымалы | Әдепкі | Не үшін |
| --- | --- | --- |
| `FEATURE_FEEDS` | off | бүкіл мүмкіндік |
| `FEEDS_AI_DAILY_LIMIT` | 150000 | тәулігіне ИИ токен лимиті |
| `FEEDS_AUTOPUBLISH_MIN_CONFIDENCE` | — (өшірулі) | `0.85` сияқты: сенімді дереккөздерге автожариялау |
| `FEEDS_AI_MODEL` | `OPENAI_MODEL` | ағынға бөлек модель |
| `FEEDS_MAX_AI_PER_RUN` | 60 | бір жолдағы ИИ шақыруы |
| `FEEDS_MAX_POST_AGE_DAYS` | 30 | бұдан ескі пост ИИ-ге бармайды |
| `FEEDS_UNDATED_TTL_DAYS` | 45 | мерзімсіз жазба осынша күннен кейін жасырылады |
| `FEEDS_SOURCE_DELAY_MS` | 1500 | дереккөздер арасындағы үзіліс |
| `FEEDS_PUBLISH_CHANNEL` | — | Telegram арнасы |
| `FEEDS_TG_SESSION`, `TELEGRAM_APP_ID`, `TELEGRAM_APP_HASH` | — | MTProto (қосымша) |
| `FEEDS_SOURCES` | `feeds/sources.yaml` | тізім файлы |

## Жергілікті тексеру

```bash
cd backend
go run ./cmd/feeds-collect --dry-run --source tg-astana-hub,rss-opportunity-desk --limit 3   # дерекқорсыз, ИИ-сіз
FEATURE_FEEDS=true DATABASE_URL=postgres://postgres:dev@localhost:54329/studenthub go run ./cmd/feeds-collect --no-ai
go test ./internal/feeds/...      # парсер fixture-лері, тексеру, dedupe, мерзім, автожариялау, RLS (ИИ — mock)
```

## Дереккөздер (`backend/feeds/sources.yaml`)

Әрқайсысы 2026-11-05 күні ашылып тексерілді (пост бар, соңғы күні жақын). Тексерілмеген handle қоспаңыз.
`enabled` тек алғашқы рет қолданылады; кейін `/admin/feeds`-тегі қосқыш шешеді.

| id | түрі | дереккөз | ауқым | trust | әдепкі |
| --- | --- | --- | --- | --- | --- |
| `tg-astana-hub` | tg_public | [@astana_hub](https://t.me/s/astana_hub) — Astana Hub технопаркі, 13.9K | KZ | high | қосулы |
| `tg-hackathon-kz` | tg_public | [@hackathon_kz](https://t.me/s/hackathon_kz) — hackathon.kz хабарландырулары | KZ | medium | қосулы |
| `tg-studywithraissov` | tg_public | [@studywithme_r](https://t.me/s/studywithme_r) — жоба иесінің арнасы | KZ | high | қосулы |
| `tg-aitu-career` | tg_mtproto | AITU Career and Employment (username жоқ, @beknazar_02 оқиды) | KZ | medium | қосулы (сессия керек) |
| `html-enic-kz` | html | [enic-kazakhstan.edu.kz/novosti](https://enic-kazakhstan.edu.kz/novosti) — Жоғары білімді дамыту ұлттық орталығы (Erasmus+ ұлттық офисі осында) | KZ | high | қосулы |
| `rss-the-tech-internships` | rss | [the-tech.kz — стажировки](https://the-tech.kz/tag/stazhirovki/feed/) | KZ | medium | қосулы |
| `rss-opportunity-desk` | rss | [opportunitydesk.org](https://opportunitydesk.org/feed/) | халықаралық | medium | қосулы |
| `rss-scholars4dev` | rss | [scholars4dev.com](https://www.scholars4dev.com/feed/) | халықаралық | medium | қосулы |
| `tg-omacademy` | tg_public | @omacademytc — гранттар (көбіне талапкерлерге) | KZ | low | өшірулі |
| `tg-universitetlar` | tg_public | @America_Universitetlari — халықаралық стипендиялар, өзбекше | халықаралық | low | өшірулі |
| `tg-nu-edu-kz` | tg_public | @nu_edu_kz — NU студенттерінің арнасы, сирек | KZ | low | өшірулі |
| `rss-opportunities-corners` | rss | opportunitiescorners.com | халықаралық | low | өшірулі |

### Алынбағандар және себебі

| Үміткер | Себебі |
| --- | --- |
| «Grants KZ» Telegram | `grantskz`, `grants_kz`, `grantykz` жоқ; `grantkz` — 2 жазылушы, «Қайтарымсыз грант» (бизнес), `granty_kz` 2022-ден бері үнсіз. Нақты handle табылмады — иесі нақты сілтемені берсе, қосамыз |
| Astana Hub сайты (astanahub.com/event) | Cloudflare bot-check (бетке «Attention Required»); айналып өтпейміз — Telegram арнасы алынды |
| Болашақ (ЦМП) | ресми Telegram handle табылмады (`bolashak*` жоқ); bolashak.gov.kz қосылымды үзеді |
| Erasmus+ in Kazakhstan Telegram | ENIC сайтында аталады, бірақ handle тексерілмеді (`erasmusplus_kz`, `erasmus_kz`... жоқ); erasmusplus.kz TLS қатесі. Орнына ENIC жаңалықтары (html) |
| DAAD Kazakhstan / Central Asia | daad.kz жауап бермейді (TLS timeout), Telegram handle табылмады |
| Fulbright / АҚШ елшілігі | kz.usembassy.gov білім беті 404, Telegram handle табылмады |
| British Council KZ | britishcouncil.kz DNS-те жоқ |
| Campus France KZ, Stipendium Hungaricum, Türkiye Bursları, MEXT, GKS, Chevening, China CSC | сайттарда RSS не Telegram сілтемесі жоқ; ресми арналар тексерілмеді. Бұл бағдарламалар Opportunity Desk / Scholars4Dev арқылы келеді |
| Youth Opportunities (youthop.com) | `/feed` 404 |
| Opportunities for Youth (opportunitiesforyouth.org) | 403 (bot-check); `@opportunitiesforyouth` Telegram 2026-06-дан бері үнсіз |
| Scholarship Positions | сайт TLS қатесі; `@scholarshippositions` 2020-дан бері үнсіз |
| `@opportunitydesk`, `@youthopportunities` Telegram | ресми емес, 2–27 жазылушы / ескі; сайт RSS-і алынды |
| Devpost | robots.txt тыйым салады + 403 |
| MLH (mlh.io/events) | Cloudflare bot-check |
| Kaggle competitions | тек тіркелген аккаунт + API кілті арқылы; шарттары автоматты жинауға жол бермейді |
| Tech Garden, MOST Ventures | techgarden.kz 522; most.com.kz-те Telegram/RSS жоқ; Telegram handle табылмады |
| UNDP / UN Kazakhstan, волонтерлік | undp.org 403; kazakhstan.un.org-та RSS/Telegram жоқ |
| KazATU / Seifullin ресми | kazatu.edu.kz TLS тізбегі толық емес (сертификат тексерілмейді), kazatu.kz жауап бермейді; `@seifullin` — веб-көрінісі жоқ, арна екені расталмады |
| digitalbusiness.kz, astanatimes.com RSS | жалпы жаңалықтар, мүмкіндік үлесі тым аз |
| Иесінің жазылымдарындағы қалған арналар | пайдасыз (ойын-сауық, спорт, IELTS дайындығы т.б.); тізім жеке, репозиторийге жазылмайды |
