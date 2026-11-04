# Student Hub — agent notes

- `frontend/` — Next.js 16 app (sources in `frontend/src/`, `@/` = `frontend/src/`). **Read `frontend/AGENTS.md`
  first**: this Next.js differs from training data; its docs are in `frontend/node_modules/next/dist/docs/`.
- `backend/` — Go module `github.com/raissov01/studenthub-plus-kz/backend` (`cmd/`, `internal/`, `migrations/`,
  `data/`). Run Go commands from `backend/`. Tests need local Postgres at `127.0.0.1:54329` (docs/ACCOUNTS.md).
- `docker/` — images, compose files, host nginx config, smart deploy. Production = the shared VPS, Compose, images
  built on the laptop (never on the server).
- `scripts/detect-smart-deploy.sh` — which services a diff touches.
- The app shell and every tab are static (prerendered); keep them free of request-time data (`cookies()`,
  `headers()`, `connection()`, uncached fetches). The feed loads in the browser (`frontend/src/lib/content-client.ts`).
- Docs: `docs/README.md`.
