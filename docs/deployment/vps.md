# VPS: Docker Compose, env, көшу және rollback

## Архитектура

```text
Internet
   ↓  443 (certbot TLS)
nginx — ХОСТТА (басқа сайттармен ортақ), /etc/nginx/conf.d/studenthub.conf  ← docker/nginx.host.conf
   ├── studenthub.studywithraissov.com
   │     ├── /api/*, wss /api/quiz/live/ws  → api  127.0.0.1:8110  (Go)
   │     ├── /_next/static/*                 → web  127.0.0.1:8112  (immutable, 1 жыл)
   │     └── /*                              → web  127.0.0.1:8112  (Next.js)
   └── studenthub.85-137-91-47.sslip.io      → тек /api/* (ескі хост: Vercel сайты, бот webhook-і)
docker compose (project studenthub, /opt/studenthub/docker)
   ├── postgres  studenthub-postgres  127.0.0.1:5442  том studenthub-pg (external)   160M
   ├── api       studenthub-api       127.0.0.1:8110  /opt/studenthub/uploads        150M
   └── web       studenthub-web       127.0.0.1:8112                                 300M
systemd (хостта): studenthub-cron.timer 10:00, studenthub-notify.timer сағат сайын → curl 127.0.0.1:8110
```

## Сервердегі файлдар

| Жол | Не |
| --- | --- |
| `/opt/studenthub/.env` | қосымшаның баптаулары (бұрынғы файл, өзгеріссіз қолданылады) |
| `/opt/studenthub/docker/docker-compose.yml`, `stack.yml`, `nginx*.conf`, `vps-apply.sh`, `systemd/` | `smart-deploy.sh` көшіреді |
| `/opt/studenthub/docker/.env` | compose айнымалылары: `API_TAG`, `WEB_TAG`, `POSTGRES_PASSWORD`, `STUDENTHUB_UID/GID` — `vps-apply.sh` жазады (үлгі: `docker/.env.example`) |
| `/opt/studenthub/docker/DEPLOYED_SHA` | соңғы сәтті деплойдың git sha-сы (smart deploy осымен салыстырады) |
| `/opt/studenthub/uploads/{market,quiz,certs}` | фотолар, квиз медиасы, портфолио сертификаттары (api контейнеріне mount) |
| `/opt/studenthub/backups/` | көшу алдындағы дамп (`pre-compose-*.sql.gz`) |

## Env (`/opt/studenthub/.env`)

Compose мыналарды өзі береді, файлдағы мәндерін басып тастайды: `API_ADDR=:8110`,
`DATABASE_URL=postgres://studenthub:<құпия сөз>@postgres:5432/studenthub` (құпия сөз файлдағы `DATABASE_URL`-дан
алынады), `UPLOAD_DIR=/app/uploads`, `CONTENT_URL=http://127.0.0.1:8110`.

Жаңа хостқа көшкенде файлда **қолмен** өзгертілетіндер (серверде, көшуге дейін):

| Айнымалы | Жаңа мәні | Неге |
| --- | --- | --- |
| `SITE_URL` | `https://studenthub.studywithraissov.com` | бот сілтемелері, квиз `joinUrl`, email, превью |
| `ALLOWED_HOSTS` | `studenthub.studywithraissov.com,studenthub-plus-kz.vercel.app,studenthub-platform.vercel.app,studenthub.85-137-91-47.sslip.io` | жазу сұраныстарының Origin тексеруі, квиз WS Origin-і; ауысу кезінде Vercel хосттары да қалады |
| `QUIZ_LIVE_WS_URL` | **өшір** (не `wss://studenthub.studywithraissov.com/api/quiz/live/ws`) | бос болса сервер `wss://<сұраныс хосты>/api/quiz/live/ws` береді — бір origin |

Фронтендтің env-і image ішінде: `NEXT_PUBLIC_SITE_URL=https://studenthub.studywithraissov.com` (build arg),
`API_ORIGIN=http://api:8110` (серверлік fetch-тер compose желісі арқылы).

## Бірінші рет: systemd-дан Compose-қа көшу (дерек жоғалмайды)

Қазір серверде: `studenthub-api` systemd сервисі (`/opt/studenthub/studenthub-api`, `127.0.0.1:8110`),
`docker run` арқылы жасалған `studenthub-postgres` (`studenthub-pg` томы), timer-лер, `/etc/nginx/conf.d/studenthub.conf`
(тек sslip API хосты). Compose **сол томды** алады: контейнер қайта жасалады, дерек томда қалады.

Көшу жергілікті түрде қайталанып тексерілді: ескі `docker run` Postgres-ке белгі жазылып, `SWITCH_FROM_SYSTEMD=1`
арқылы Compose-қа ауысқанда белгі орнында болды; кейін `docker compose down` + ескі `docker run` (rollback) —
белгі тағы орнында.

**0. DNS.** `studenthub.studywithraissov.com` → `A 85.137.91.47`. Тексер: `dig +short studenthub.studywithraissov.com`.

**1. Сертификат** (nginx конфигі әлі өзгермейді; certbot уақытша өз конфигін қолданады):

```bash
ssh root@85.137.91.47 'certbot certonly --nginx -d studenthub.studywithraissov.com'
```

**2. Env** (көшірмесімен):

```bash
ssh root@85.137.91.47 'cd /opt/studenthub && cp -p .env .env.bak-$(date +%F) &&
  sed -i "s#^SITE_URL=.*#SITE_URL=https://studenthub.studywithraissov.com#" .env &&
  sed -i "s#^ALLOWED_HOSTS=.*#ALLOWED_HOSTS=studenthub.studywithraissov.com,studenthub-plus-kz.vercel.app,studenthub-platform.vercel.app,studenthub.85-137-91-47.sslip.io#" .env &&
  sed -i "/^QUIZ_LIVE_WS_URL=/d" .env && grep -E "^(SITE_URL|ALLOWED_HOSTS)=" .env'
```

Ескі API бұл файлды қайта іске қосылғанда ғана оқиды, сондықтан 3-қадамға дейін ештеңе өзгермейді. Жаңа
мәндерді `.secrets/prod.env`-ке де көшір.

**3. Көшу** (ноутбуктан, `main`-нің таза көшірмесінен):

```bash
DRY_RUN=1 bash docker/deploy.sh                 # жоспар
SWITCH_FROM_SYSTEMD=1 bash docker/deploy.sh
```

Не болады: image-дер ноутбукта жиналып серверге жүктеледі (ескі сайт әлі жұмыс істеп тұр) → серверде
`vps-apply.sh`: `pg_dump` → `/opt/studenthub/backups/pre-compose-<уақыт>.sql.gz` (бос болса тоқтайды, ештеңеге
тимейді) → `systemctl disable --now studenthub-api` → ескі `studenthub-postgres` контейнері тоқтап өшіріледі (том
қалады) → `docker compose up -d postgres` (сол том) → `/app/migrate` (жаңасы болса ғана) → `api`, `web` →
`nginx.host.conf` орнатылады (`nginx -t` өтпесе бұрынғысы қайтады; бұрынғы файл `studenthub.conf.bak-<уақыт>`) →
timer-лер → health. Тоқтап тұру: шамамен 20–40 секунд (Postgres пен api қайта қосылғанша).

`SWITCH_FROM_SYSTEMD=1` жоқ болса, `vps-apply.sh` ескі сервис жұмыс істеп тұрғанын көріп, бас тартады.

**4. Тексеру:**

```bash
curl -fsS https://studenthub.studywithraissov.com/api/health
curl -fsS -o /dev/null -w '%{http_code} %header{x-nextjs-cache}\n' https://studenthub.studywithraissov.com/search   # 200 HIT
curl -fsS https://studenthub.85-137-91-47.sslip.io/api/health                                                     # ескі хост
ssh root@85.137.91.47 'cd /opt/studenthub/docker && docker compose ps && docker stats --no-stream | grep studenthub &&
  docker exec studenthub-postgres psql -U studenthub -Atc "select count(*) from users" &&
  systemctl list-timers "studenthub-*" --no-pager'
```

Сайтта: кіру (Telegram), табтар, квиз live (WebSocket), маркетке фото жүктеу.

**5. Telegram** (BotFather):

- `/setdomain` → `studenthub.studywithraissov.com` (Telegram Login widget тек бір доменде жұмыс істейді).
- Mini App / мәзір батырмасының URL-і → `https://studenthub.studywithraissov.com`.
- Webhook ескі sslip хостында қала береді; ауыстыру керек болса — [BOT.md](../BOT.md) (`setWebhook`
  `https://studenthub.studywithraissov.com/api/telegram`, сол `WEBHOOK_SECRET`).

**6. Vercel.** Vercel сайты ескі sslip API хосты арқылы жұмыс істей береді. Пайдаланушылар көшкен соң Vercel
жобасын жаңа доменге redirect-ке қой не өшір; содан кейін `ALLOWED_HOSTS`-тан vercel хосттарын алып таста.

### Көшуді кері қайтару (rollback)

Дерек `studenthub-pg` томында, Compose оны ешқашан өшірмейді (`external`). Ескі бинар
`/opt/studenthub/studenthub-api` пен unit файлы орнында қалады.

```bash
ssh root@85.137.91.47
cd /opt/studenthub/docker && docker compose down                    # том қалады; ЕШҚАШАН `down -v` емес
PGPASS=$(grep ^POSTGRES_PASSWORD= .env | cut -d= -f2-)
docker run -d --name studenthub-postgres --restart unless-stopped \
  -e POSTGRES_USER=studenthub -e POSTGRES_PASSWORD=$PGPASS -e POSTGRES_DB=studenthub \
  -p 127.0.0.1:5442:5432 -v studenthub-pg:/var/lib/postgresql/data --memory 160m \
  postgres:16-alpine -c shared_buffers=24MB -c max_connections=20
cp /opt/studenthub/.env.bak-<күні> /opt/studenthub/.env             # керек болса, ескі SITE_URL/ALLOWED_HOSTS
systemctl enable --now studenthub-api
ls /etc/nginx/conf.d/studenthub.conf.bak-*                          # ең соңғысы — көшуге дейінгі файл
cp /etc/nginx/conf.d/studenthub.conf.bak-<уақыт> /etc/nginx/conf.d/studenthub.conf && nginx -t && systemctl reload nginx
curl -fsS localhost:8110/api/health
```

Көшу кезінде жаңа миграция орындалса да, миграциялар тек қосатын түрде жазылады, ескі бинар жаңа кестелерді
елемейді. Ең нашар жағдайда — `pre-compose-*.sql.gz` дампынан қалпына келтіру
([backup-restore.md](../operations/backup-restore.md), 1-жағдай).

## Әдеттегі деплойды кері қайтару

Әр image-тің соңғы 3 tag-ы серверде сақталады:

```bash
ssh root@85.137.91.47
docker images studenthub-api; docker images studenthub-web          # tag = git sha
cd /opt/studenthub/docker
sed -i 's/^API_TAG=.*/API_TAG=<алдыңғы sha>/; s/^WEB_TAG=.*/WEB_TAG=<алдыңғы sha>/' .env
docker compose up -d api web
```

Келесі smart deploy `DEPLOYED_SHA`-дан бастап салыстырады; rollback-тан кейін `FULL=1` не `bash docker/deploy.sh`
сенімдірек.

## Пайдалы командалар

```bash
cd /opt/studenthub/docker
docker compose ps
docker compose logs -f --tail 100 api
docker compose run --rm api /app/migrate             # миграцияларды қолмен
docker compose restart web
docker exec -it studenthub-postgres psql -U studenthub
journalctl -u studenthub-cron -n 50                  # күнделікті жұмыстар
```
