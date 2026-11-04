# Student Hub — құжаттар

Student Hub үш бөліктен тұрады: Next.js фронтенд (`frontend/`), Go API (`backend/`) және Docker деплой қабаты
(`docker/`). Production — ортақ VPS (`85.137.91.47`), хосттағы nginx артында Docker Compose.

## Түбірлік құрылым

```text
.
├── AGENTS.md                 агенттерге қысқа ереже (frontend/AGENTS.md — Next.js туралы)
├── README.md
├── frontend/                 Next.js 16 қосымшасы
│   ├── src/
│   │   ├── app/              маршруттар: [[...slug]] (барлық таб, статикалық), job/opportunity/event/p (серверде)
│   │   ├── components/       қабық (Shell), экрандар (views/), квиз, UI
│   │   ├── features/         аналитика, кері байланыс, roadmap, Telegram
│   │   └── lib/              күй (store), фид (content-client), маршруттар, PWA, API клиенттері
│   ├── public/               sw.js, offline.html, шрифттер, белгішелер
│   ├── e2e/                  Playwright тесттері (mocked және real-api)
│   ├── tools/                контраст, экран суреттері, квиз тексерулері
│   └── next.config.ts        output: 'standalone', CSP, /api rewrite (тек dev/test)
├── backend/                  Go модулі github.com/raissov01/studenthub-plus-kz/backend
│   ├── cmd/                  server, migrate, routes (SECURITY.md), e2etest, json-to-csv
│   ├── internal/             auth, content, bot, notify, quiz, events, market, b2b, … (бұрынғы pkg/)
│   ├── migrations/           *.sql, аты бойынша ретпен, әрқайсысы бір рет (cmd/migrate)
│   ├── data/                 jobs/opportunities/discounts.json — Google Sheets істемесе API-дің қоры
│   └── templates/sheets/     Google Sheets үлгі CSV-лері
├── docker/
│   ├── backend.Dockerfile    Go → статикалық бинарлар (server + migrate), alpine, non-root
│   ├── frontend.Dockerfile   Next standalone, node:22-alpine, non-root
│   ├── docker-compose.yml    PRODUCTION (ортақ VPS): postgres + api + web
│   ├── docker-compose.local.yml      ноутбукта бүкіл стек, бастапқы кодтан
│   ├── docker-compose.override.yml   + nginx контейнері, бір origin (:8088)
│   ├── nginx.host.conf       VPS хосттағы nginx (сайт + ескі sslip API хосты)
│   ├── nginx.conf            nginx контейнері TLS-пен (Swarm / жеке сервер нұсқасы)
│   ├── nginx.local.conf      nginx контейнері, жергілікті
│   ├── stack.yml             Docker Swarm нұсқасы (міндетті емес)
│   ├── smart-deploy.sh       ноутбуктан: өзгергенін ғана жинау, жіберу, қолдану
│   ├── deploy.sh             толық деплой (smart-deploy, бәрі қосулы)
│   ├── vps-apply.sh          серверде орындалатын бөлігі (build жоқ)
│   ├── swarm-init.sh, swarm-deploy.sh
│   └── systemd/              cron/notify timer-лері (хостта, API-ді 127.0.0.1:8110 арқылы шақырады)
├── scripts/
│   └── detect-smart-deploy.sh   diff → қай сервис қозғалады
├── docs/
└── archive/                  ескі бір файлдық нұсқа (legacy/) және systemd деплойы (deploy-systemd/)
```

## Негізгі бағыттар

- **Жылдамдық.** Қосымшаның қабығы және барлық таб (`/`, `/search`, `/calendar`, `/profile`, …) build кезінде
  алдын ала жасалады. Таб басқанда сервер ештеңе есептемейді: тек дайын статикалық RSC. Фид (вакансиялар,
  мүмкіндіктер, жеңілдіктер) браузерде: жадтағы / localStorage-тағы соңғы көшірме бірден көрінеді,
  `GET /api/content` фонда жаңартады (`frontend/src/lib/content-client.ts`).
- **Бір origin.** Браузер үшін бәрі `https://studenthub.studywithraissov.com`: `/api/*` пен квиз WebSocket-і
  nginx-тен тура Go-ға, қалғаны Next-ке.
- **Сервер build жасамайды.** VPS жады клиент боттарымен ортақ: image-дер ноутбукта (не CI-да) жиналып,
  `docker save | ssh docker load` арқылы барады.

## Құжаттар

| Тақырып | Файл |
| --- | --- |
| Архитектура, диаграмма | [architecture.md](./architecture.md) |
| Жергілікті орта | [setup.md](./setup.md) |
| Деплой шолуы, нұсқалар (Compose / Swarm / CI) | [deployment/overview.md](./deployment/overview.md) |
| VPS, env, systemd → Compose көшу, rollback | [deployment/vps.md](./deployment/vps.md) |
| Smart deploy ережелері | [deployment/smart-deploy.md](./deployment/smart-deploy.md) |
| Резерв көшірме, қалпына келтіру | [operations/backup-restore.md](./operations/backup-restore.md) |
| Апта сайынғы әкімшілік тізім | [operations/admin-weekly.md](./operations/admin-weekly.md) |
| Аккаунттар, сессиялар, env | [ACCOUNTS.md](./ACCOUNTS.md) |
| Telegram бот | [BOT.md](./BOT.md) |
| Контент (Google Sheets, data/*.json) | [CONTENT.md](./CONTENT.md) |
| PWA, офлайн | [PWA.md](./PWA.md) |
| Қауіпсіздік тақырыптары | [HEADERS.md](./HEADERS.md) |
| API маршруттарының аудиті (генерацияланады) | [SECURITY.md](./SECURITY.md) |
| Аналитика | [ANALYTICS.md](./ANALYTICS.md) |
| Браузерлік тесттер | [E2E.md](./E2E.md) |
| API модульдері | [api/](./api/) |
| Промпттар / ТЗ, паритет | [prompts/](./prompts/), [PARITY.md](./PARITY.md) |
| Дизайн | [design/](./design/) |

## Жиі командалар

```bash
# фронтенд
cd frontend && npm ci && npm run dev            # http://localhost:3000, /api → localhost:8080
cd frontend && npx tsc --noEmit && npx eslint . && npm run test:ts && npm run build
# бэкенд
cd backend && go run ./cmd/server               # :8080
cd backend && go vet ./... && go test ./...
# бүкіл стек Docker-де
docker compose -f docker/docker-compose.local.yml up -d --build
# деплой (ноутбуктан)
DRY_RUN=1 bash docker/smart-deploy.sh           # не өзгерді, не жүреді
bash docker/smart-deploy.sh
```
