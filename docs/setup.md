# Жергілікті орта

Керек: Node 22, Go 1.25+, Docker (Postgres үшін және бүкіл стек үшін).

## 1. Тек фронтенд (ең жылдам)

```bash
cd frontend
npm ci
npm run dev                     # http://localhost:3000
```

`/api/*` `next dev` кезінде `http://localhost:8080`-ге rewrite болады (`frontend/next.config.ts`, `API_ORIGIN`
арқылы өзгереді). Go сервері жоқ болса, фид бос болады, ал құрылғыдағы бөлімдер (күнтізбе, конспект, профиль)
бәрібір жұмыс істейді.

## 2. Go API

```bash
# Postgres (тесттер мен локал API үшін бір контейнер)
docker run -d --name studenthub-pg -e POSTGRES_PASSWORD=dev -p 54329:5432 postgres:16
docker exec studenthub-pg createdb -U postgres studenthub

cd backend
DATABASE_URL=postgres://postgres:dev@127.0.0.1:54329/studenthub?sslmode=disable go run ./cmd/migrate
DATABASE_URL=postgres://postgres:dev@127.0.0.1:54329/studenthub?sslmode=disable \
  SESSION_SECRET=local-dev-session-secret-at-least-32-chars go run ./cmd/server     # :8080
```

Env айнымалыларының толық тізімі: [ACCOUNTS.md](./ACCOUNTS.md), [BOT.md](./BOT.md), [CONTENT.md](./CONTENT.md),
[api/](./api/). Фидтің қоры — `backend/data/*.json` (`DATA_DIR` арқылы өзгереді), миграциялар — `backend/migrations`
(`MIGRATIONS_DIR`).

## 3. Бүкіл стек Docker-де (production-ға ең жақын)

```bash
cp docker/local.env.example docker/local.env      # міндетті емес: BOT_TOKEN, OPENAI_API_KEY, FEATURE_*
docker compose -f docker/docker-compose.local.yml up -d --build
curl localhost:8110/api/health                     # api
open http://localhost:8112                         # web
docker compose -f docker/docker-compose.local.yml down          # -v — жергілікті базаны да өшіру
```

`migrate` сервисі API алдында миграцияларды бір рет орындайды. Production сияқты бір origin керек болса
(квиз WebSocket-і, cookie), nginx контейнерін қос:

```bash
docker compose -f docker/docker-compose.local.yml -f docker/docker-compose.override.yml up -d --build
open http://localhost:8088
```

## Тексерулер

```bash
cd backend && go vet ./... && go test ./...        # Postgres 127.0.0.1:54329 керек
cd frontend && npx tsc --noEmit && npx eslint . && npm run test:ts && npm run build
cd frontend && E2E_PORT=4590 E2E_API_PORT=4591 npm run e2e   # build API_ORIGIN=http://127.0.0.1:4591-пен; docs/E2E.md
```

`backend/cmd/routes` API маршруттарының аудитін жасайды; маршрут өзгерсе:
`cd backend && go run ./cmd/routes > ../docs/SECURITY.md` (тест айырманы ұстайды).

## Next.js 16 туралы

Бұл нұсқа оқыту деректеріндегіден өзгеше: код жазбас бұрын `frontend/AGENTS.md` пен
`frontend/node_modules/next/dist/docs/` оқы. Қабық пен табтар статикалық болып қалуы керек — layout-қа не
`[[...slug]]` бетіне `cookies()`, `headers()`, `connection()` не кэшсіз fetch қоспа; build шығысында
`/`, `/search`, `/calendar`, `/profile` `●` (SSG) болып тұруы тиіс.
