# Деплой: шолу

## Production архитектурасы

- Сервер: ортақ VPS `85.137.91.47` (balu-fit, aismebel, qadam, bagdar, асхана кассасы да осында — жад ортақ).
- Оркестрация: **Docker Compose** (`/opt/studenthub/docker/docker-compose.yml`, project `studenthub`). Swarm бұл
  серверде өшік және өшік қалады.
- Фронтенд: `web` — Next.js standalone, `127.0.0.1:8112`.
- Бэкенд: `api` — Go, `127.0.0.1:8110`; миграциялар бір реттік `api /app/migrate` контейнерімен.
- Дерекқор: `postgres` — бұрынғы `studenthub-postgres` контейнері мен `studenthub-pg` томы.
- Reverse proxy / TLS: хосттағы nginx, `docker/nginx.host.conf` → `/etc/nginx/conf.d/studenthub.conf`, certbot.
- Жоспарлы жұмыстар: хосттағы systemd timer-лер (`docker/systemd/`).
- Image-дер: ноутбукта (не GitHub Actions runner-де) жиналады, серверге `docker save | ssh docker load`.

## Нұсқалар

| Файл | Не үшін | Қай жерде |
| --- | --- | --- |
| `docker/docker-compose.yml` | production | ортақ VPS (әдепкі) |
| `docker/nginx.host.conf` | хосттағы nginx | ортақ VPS |
| `docker/smart-deploy.sh` | өзгергенін ғана жинап, жіберіп, қолдану | ноутбук |
| `docker/deploy.sh` | толық деплой (smart-deploy, бәрі қосулы) | ноутбук |
| `docker/vps-apply.sh` | серверде орындалатын бөлігі | VPS (ssh арқылы) |
| `docker/docker-compose.local.yml` | бүкіл стек бастапқы кодтан | ноутбук |
| `docker/docker-compose.override.yml` + `nginx.local.conf` | бір origin, nginx контейнері `:8088` | ноутбук |
| `docker/stack.yml` + `nginx.conf` + `swarm-init.sh` + `swarm-deploy.sh` | Swarm нұсқасы, өз nginx контейнері 80/443-те | **тек** SIVA 80/443-ті иеленетін сервер |
| `.github/workflows/deploy.yml` | CI-дан smart deploy (қолмен іске қосу) | GitHub Actions, secrets әлі қосылмаған |
| `.github/workflows/ci.yml` | go vet/test, tsc, eslint, unit, build | әр push |

## Әдеттегі деплой (ноутбуктан)

```bash
git switch main && git pull
DRY_RUN=1 bash docker/smart-deploy.sh     # серверде қай sha тұр, не өзгерді, не жүреді — ештеңеге тимейді
bash docker/smart-deploy.sh
```

Smart deploy:

1. Серверден `DEPLOYED_SHA`-ны оқып, `scripts/detect-smart-deploy.sh` арқылы `HEAD`-пен салыстырады.
2. Тек құжат / тест өзгерсе — ештеңе жасамайды.
3. Өзгерген image-ді ғана жинайды (`--platform linux/amd64`, tag = git sha), серверде жоқ болса ғана жібереді.
4. `vps-apply.sh`: миграция файлдары өзгерсе ғана `/app/migrate`, сосын тек өзгерген сервисті қайта қосады,
   nginx/timer файлдары өзгерсе ғана оларды орнатады.
5. Health: `127.0.0.1:8110/api/health`, `127.0.0.1:8112/`, `https://studenthub.studywithraissov.com/api/health`.
6. Сәтті болса `DEPLOYED_SHA` жазылады; әр image-тің соңғы 3 tag-ы rollback үшін қалады.

Ережелердің толық тізімі: [smart-deploy.md](./smart-deploy.md). Сервер, env, бірінші көшу, rollback:
[vps.md](./vps.md).

## Деплой алдында жергілікті тексеру

```bash
cd backend && go vet ./... && go test ./...
cd frontend && npx tsc --noEmit && npx eslint . && npm run test:ts && npm run build
```

## Swarm нұсқасы (міндетті емес)

StudyWithRaissov Swarm-да жүреді; SIVA-да да сол нұсқа бар, бірақ ортақ VPS үшін **әдепкі емес**: онда
хосттағы nginx 80/443-ті ұстап тұр және Swarm өшік. SIVA жеке серверге көшсе:

```bash
ssh root@<server> 'bash -s' < docker/swarm-init.sh   # swarm init, Postgres құпия сөзі secret ретінде
bash docker/swarm-deploy.sh                          # MODE=swarm, бәрі: build, ship, docker stack deploy
MODE=swarm bash docker/smart-deploy.sh               # кейін: тек өзгергені (docker service update)
```

`stack.yml` сол `studenthub-pg` томын (`external`) қолданады; nginx контейнері `nginx.conf`-пен 80/443-те, host
mode (клиенттің нақты IP-і API-дің rate limit-іне жетеді).

## CI (GitHub Actions)

- `ci.yml` — әр push пен PR: Postgres service-пен `go vet` + `go test`, `tsc`, `eslint`, `test:ts`, `next build`.
- `deploy.yml` — тек `workflow_dispatch`. Detect → runner-де image жинау → ssh арқылы `docker load` →
  `vps-apply.sh`. Іске қосу үшін repo secrets керек: `VPS_HOST` (`85.137.91.47`), `VPS_USER` (`root`),
  `VPS_SSH_KEY` (деплойға арналған жеке кілт; оның public бөлігі серверде `authorized_keys`-та). Secrets
  қосылғанша workflow қолмен де іске қосылмайды (бірінші қадамда тоқтайды).
