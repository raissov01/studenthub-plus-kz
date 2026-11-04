# Smart deploy ережелері

`scripts/detect-smart-deploy.sh --base <серверде тұрған sha> --head HEAD` өзгерген файлдарды бөліктерге бөледі;
`docker/smart-deploy.sh` (ноутбук) мен `.github/workflows/deploy.yml` (CI) сол шешімді орындайды, серверде
`docker/vps-apply.sh` қолданады. Base — серверде `/opt/studenthub/docker/DEPLOYED_SHA`; ол жоқ не жергілікті
тарихта табылмаса — толық деплой.

Ережелер ретпен тексеріледі: алдымен құжат пен тест, сосын runtime бөліктері.

### Деплойсыз (тек құжат / тест / құралдар)

- `docs/**`, `README.md`, `AGENTS.md`, `CLAUDE.md`, `frontend/AGENTS.md`, `archive/**`
- `frontend/e2e/**`, `frontend/tools/**`, `frontend/playwright*.ts`, `*.test.ts`
- `backend/**/*_test.go`, `backend/internal/testdb/**`, `backend/cmd/{e2etest,routes,json-to-csv}/**`,
  `backend/templates/**`, `backend/sqlc.yaml`
- `.github/**`, `scripts/**`, `docker/*.sh`, `docker/docker-compose.local.yml`, `docker/docker-compose.override.yml`,
  `docker/nginx.local.conf`, `docker/*.example`, `.gitignore`-дар

Тек осылар өзгерсе: `Nothing to deploy`, сервер мүлде қозғалмайды.

### Frontend (web)

- `frontend/**`, `docker/frontend.Dockerfile`
- Әрекет: `studenthub-web:<sha>` жиналады, жіберіледі, тек `web` қайта қосылады (`docker compose up -d --no-deps web`).

### Backend (api)

- `backend/**` (соның ішінде `backend/data/*.json` — image ішінде), `docker/backend.Dockerfile`
- Әрекет: `studenthub-api:<sha>`, тек `api` қайта қосылады.

### Миграциялар

- `backend/migrations/**`, `backend/cmd/migrate/**`
- Әрекет: backend деплойы + жаңа image-пен бір реттік `docker compose run --rm --no-deps api /app/migrate`,
  **api қайта қосылмай тұрып**. Басқа өзгерістерде миграция іске қосылмайды.

### Stack (compose / swarm спецификациясы)

- `docker/docker-compose.yml`, `docker/stack.yml`, `docker/nginx.conf`
- Әрекет: бүкіл файл қайта қолданылады (`docker compose up -d --remove-orphans`; Swarm-да `docker stack deploy`),
  өзгермеген image-дер сол tag-тарымен қалады.

### Хосттағы nginx

- `docker/nginx.host.conf`
- Әрекет: `/etc/nginx/conf.d/studenthub.conf`-қа көшіріледі (бұрынғысы `.bak-<уақыт>`), `nginx -t` өтпесе бұрынғысы
  қайтады, өтсе `systemctl reload nginx`. Сертификаты жоқ хост болса — өткізіп жіберіп, ескертеді.

### Timer-лер

- `docker/systemd/**`
- Әрекет: unit-тер `/etc/systemd/system/`-ке, `daemon-reload`, `enable --now` үш timer (cron, notify, feeds).

### Белгісіз файл → қауіпсіз толық деплой

Ешбір ережеге сай келмейтін файл (мысалы, түбірге жаңа бума) болса: web + api + stack + nginx + timers. Маңызды
runtime өзгерісін үнсіз өткізіп алмау үшін.

## Қолмен басқару

```bash
DRY_RUN=1 bash docker/smart-deploy.sh                 # шешімді ғана көрсет (серверден DEPLOYED_SHA-ны оқиды, ештеңе өзгертпейді)
OFFLINE=1 BASE=<sha> DRY_RUN=1 bash docker/smart-deploy.sh   # ssh-сыз
DEPLOY_FRONTEND=true bash docker/smart-deploy.sh      # бір бөлікті мәжбүрлеу (анықтау өткізіледі)
RUN_MIGRATIONS=true bash docker/smart-deploy.sh       # миграция (+ api)
FULL=1 bash docker/smart-deploy.sh                    # бәрі = bash docker/deploy.sh
ALLOW_DIRTY=1 bash docker/smart-deploy.sh             # коммитсіз өзгерістермен (ұсынылмайды)
VPS=root@<басқа сервер> bash docker/smart-deploy.sh
```

## Жергілікті тексеру

```bash
bash scripts/detect-smart-deploy.sh --base HEAD~3 --head HEAD
```
