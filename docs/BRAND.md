# SIVA: атау және бренд

Өнімнің ресми атауы — **SIVA** (авторлық құқық өтінішіндегі толық атауы: «SIVA — веб-платформа для объединения
учебных, карьерных и внеучебных возможностей студентов в единой цифровой среде»). Бұрынғы атауы — «Student Hub».

## Атау қалай жазылады

- Әрқашан латынша бас әріппен: `SIVA`. Аудармайды, кириллицаға көшірмейді (en/ru/kk бәрінде `SIVA`).
- Қазақша жалғау «Сива» дыбысталуы бойынша, сызықшамен: `SIVA-ның`, `SIVA-ға`, `SIVA-ны`, `SIVA-да`, `SIVA-дан`.
  Орысша атау септелмейді: «в SIVA», «аккаунт SIVA».
- Қысқа сипаттама (тек мета, манифест, OG, басты беттің сипаттамасы сияқты жерлерде, үнемдеп):
  - kk: «студенттің бір цифрлық кеңістігі»
  - ru: «единая цифровая среда студента»
  - en: «one digital space for students»
- Логотип: бұрынғы белгі (текше, U әрпі, сызбалар) өзгерген жоқ (`frontend/src/lib/logo.ts`, `public/icons/*`);
  жанындағы сөз белгісі енді `SIVA` (`Shell.tsx`, `NavDrawer.tsx`, `SideNav.tsx`, `p/[slug]/page.tsx`). Стиль
  жалпақ: тұтас түс, градиент/көлеңке жоқ. Шрифттер (Geologica 700, Golos Text) S/I/V/A және барлық қазақ
  әріптерін қамтиды (fontTools-пен тексерілген).
- `public/og.png` (1200×630) жаңа атаумен қайта жасалды: белгі + `SIVA`, сипаттама, домен; тек тұтас түстер.

## Неге ішкі атаулар әлі `studenthub`

Төмендегілер — техникалық идентификаторлар. Оларды өзгерту кіруді, сақталған деректерді, деплойды не
сілтемелерді бұзады, ал пайдаланушы оларды көрмейді (домен ғана көрінеді). Сондықтан әдейі қалдырылды:

| Не | Мысал | Өзгертсе не болады |
| --- | --- | --- |
| Домен | `studenthub.studywithraissov.com` | ескі сілтемелер, QR, Telegram login domain, push жазылымдары, OG кэші |
| localStorage / sessionStorage кілттері | `studenthub.v1`, `studenthub.ref`, `studenthub.notes.wal`, … | пайдаланушының құрылғыдағы профилі, жоспарлары жоғалады |
| Service worker кэш аттары | `studenthub-pages-…`, `studenthub-static-…` (`public/sw.js`) | офлайн кэш қайта жүктеледі, ескі кэш тазаланбай қалуы мүмкін |
| Cookie аттары, сессия | `sh_session` (`backend/internal/auth/session.go`) | барлығы аккаунттан шығып қалады |
| Go модулі | `github.com/raissov01/studenthub-plus-kz/backend` | барлық import |
| Дерекқор, рөлдер | `studenthub`, `studenthub_<пакет>_test_*`, `studenthub_browser_*` | dump/restore, бэкап скрипттері |
| Docker сервис/контейнер/том аттары, Compose жобасы | `studenthub-api`, `studenthub-web`, `studenthub-pg`, `studenthub-kaspi-pos` | деплой, бэкап (`/root/backup-dbs.sh`), nginx upstream |
| Сервердегі жолдар | `/opt/studenthub/.env`, `/opt/studenthub/uploads` | env, жүктелген файлдар |
| systemd unit аттары мен `Description=` | `studenthub-cron.service`, … | хосттағы unit-терді қайта орнату керек (пайдаланушыға көрінбейді) |
| Env айнымалылары, API жолдары, CSS кластары | `FEATURE_*`, `/api/*` | конфигурация, клиенттер |
| Репозиторий мен папка аттары | `studenthub-plus-kz`, `frontend/package.json` `name` | CI, worktree-лер |
| Telegram бот username | `@…_bot` (`BOT_USERNAME`) | бот сілтемелері, login widget |
| HTTP User-Agent | `StudentHubBot/1.0` (feeds) | сайттардың robots.txt ережелері |
| ICS `UID`-тері | `event-…@studenthub`, `class-…@studenthub` | бұрын импортталған оқиғалар қайталанады |

Ішкі комментарийлер (docker/, scripts/, services/kaspi-pos «Student Hub patch», migrations/*.sql) және тарихи
құжаттар (`docs/prompts/`, `docs/PARITY.md`, `docs/parity.csv`, `docs/research/`, `docs/replica/`,
`docs/design/`, `docs/reviews/`, `archive/`) өзгертілмеді: олар — тарих. Миграция файлдары ешқашан өңделмейді.

Жүктелетін файл аттары пайдаланушыға көрінеді, сондықтан ауыстырылды: `siva-schedule.ics`, `siva-<күн>.ics`,
`siva-data.json`, `siva-stats.csv`, `siva-users.csv`, `siva-audit.csv`, `siva-<түр>-<күн>.csv`,
`siva-quiz-template.xlsx`.

## Иесі қолмен істейтіні (кодтан өзгермейді)

1. **BotFather** (`@BotFather`):
   - `/setname` → `SIVA`;
   - `/setdescription` (бот ашылғанда көрінетін мәтін) және `/setabouttext` (профильдегі қысқа мәтін) —
     «Student Hub» сөзін `SIVA`-ға ауыстыр, мысалы: «SIVA — студенттің бір цифрлық кеңістігі. Сақтаған
     мүмкіндіктеріңнің мерзімін еске саламын.»;
   - Mini App не Web App болса (`/myapps`) — оның title-ы мен сипаттамасы;
   - қаласаң `/setuserpic` (белгі өзгерген жоқ, міндетті емес).
   - **Username-ді өзгертпе**: ол кодта және сілтемелерде (`BOT_USERNAME`, login domain).
2. **Хат жіберуші аты**: серверде `/opt/studenthub/.env` ішіндегі `MAIL_FROM="Student Hub <noreply@…>"` →
   `MAIL_FROM="SIVA <noreply@…>"`, содан кейін `docker compose up -d api`. Хат мәтіндері кодта ауыстырылды,
   ал «Кімнен» аты осы env-тен келеді.
3. **Контент дерегі**: Google Sheets-тегі және әкімші контентіндегі `verified_by` бағаны «Student Hub» болса,
   `SIVA` деп өзгерт. Әкімші контенті үшін бір рет:
   `update admin_content set record = jsonb_set(record, '{verified_by}', '"SIVA"') where record->>'verified_by' = 'Student Hub';`
4. **Kaspi**: Kaspi шоттары кофейня мен полиграфияның өз аттарымен (бізде мерчант аты жоқ) — өзгертетін ештеңе
   жоқ. Resend-те домен аты өзгермейді.
   **Google кіру** (`FEATURE_GOOGLE_LOGIN`, [GOOGLE_LOGIN.md](./GOOGLE_LOGIN.md)): Google Cloud Console → OAuth
   consent screen (Branding) → App name `SIVA` (жоба бұрын «Student Hub» деп жасалған болса); қалғаны өзгермейді.
5. **Орнатылған PWA**: Android Chrome манифест атауын өзі жаңартады (бірнеше күнде); iOS-та «Басты экранға
   қосылған» белгі ескі атпен қалады — қайта қосу керек.
6. Іздеу жүйелері мен Telegram сілтеме алдын ала көрінісі (OG кэші) біраз уақыттан кейін жаңарады; Telegram үшін
   `@WebpageBot`-қа домен сілтемесін жіберсе, бірден жаңарады.
