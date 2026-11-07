# Резервтік көшірме және қалпына келтіру (11-029)

Нақты дерекқор — VPS `85.137.91.47` ішіндегі `studenthub-postgres` Docker контейнері (`postgres:16-alpine`,
тек `127.0.0.1:5442`, дерек `studenthub-pg` томында, пайдаланушы/база `studenthub`). Оны Docker Compose
басқарады (`/opt/studenthub/docker/docker-compose.yml`, [deployment/vps.md](../deployment/vps.md)); контейнер аты
мен том бұрынғыдай, сондықтан `docker exec studenthub-postgres ...` командалары өзгерген жоқ.

### Не бар

- Серверде `/root/backup-dbs.sh` күн сайын **сервер уақытымен 03:30**-да (cron) іске қосылады. SIVA
  базасын `docker exec studenthub-postgres pg_dump -U studenthub studenthub | gzip` арқылы `/root/backups/`
  ішіне жазады, файлдың бүтіндігін (`gzip -t` және соңындағы «dump complete» белгісі) тексереді, 7 күннен
  ескісін өшіреді. Журнал: `/var/log/db-backup.log`.
- Дамп — кәдімгі SQL: кестелер, деректер, RLS саясаттары, `schema_migrations` (қай миграция орындалғаны).

### Шектері (міндетті түрде біл)

- **Көшірмелер сол серверде ғана жатыр.** Сыртқы көшірме жасайтын орын (бұрынғы дроплет) жоқ, сондықтан
  сервер толық жоғалса, дамптар да жоғалады. Сыртқы орын қосылғанша — апта сайын соңғы дампты өз компьютеріңе
  көшір (төменде).
- **Ең көбі 1 тәулік дерек жоғалады**: соңғы дамптан кейінгі өзгерістер көшірмеде жоқ.
- **Фотолар дампта жоқ.** Маркет пен квиз суреттері `/opt/studenthub/uploads/` ішінде, олар дерекқорда емес.
  Оларды бөлек архивте (төменде).
- **`app_user` рөлі дампқа кірмейді** (Postgres рөлдері базаға емес, серверге тиесілі). Жаңа контейнерге
  қайтарғанда оны алдымен қолмен жасау керек (3-жағдай).
- `.env` (құпиялар) дампта жоқ. Оның көшірмесі әзірлеушінің компьютерінде `.secrets/prod.env`.

### Апта сайын (5 минут)

```bash
ssh root@85.137.91.47 'ls -lt /root/backups/ | grep -i studenthub | head -3; tail -5 /var/log/db-backup.log'
# соңғы дамп бүгінгі не кешегі болуы керек, өлшемі нөл емес

# соңғы дампты және фотолардың архивін өз компьютеріңе көшір
LAST=$(ssh root@85.137.91.47 'ls -t /root/backups/ | grep -i studenthub | head -1')
scp root@85.137.91.47:/root/backups/$LAST ~/studenthub-backups/
ssh root@85.137.91.47 'tar czf - -C /opt/studenthub uploads' > ~/studenthub-backups/uploads-$(date +%F).tar.gz
```

### Қолмен дамп алу (мысалы, қауіпті өзгерістің алдында)

```bash
ssh root@85.137.91.47 'docker exec studenthub-postgres pg_dump -U studenthub studenthub | gzip > /root/backups/studenthub-manual-$(date +%F-%H%M).sql.gz'
```

### Қалпына келтіру

Төмендегі командалар серверде (`ssh root@85.137.91.47`) орындалады. `DUMP` — қайтаратын файл.

**1-жағдай. Деректі кешегі күйге қайтару (контейнер тірі).** Ескі базаны өшірмей, қатар жаңасын тұрғызып,
тексеріп, содан кейін ауыстырамыз — сәтсіз болса, бәрін артқа қайтару оңай.

```bash
DUMP=/root/backups/<файл>.sql.gz
cd /opt/studenthub/docker
systemctl stop studenthub-cron.timer studenthub-notify.timer
docker compose stop api web                                                          # сайт уақытша 502 береді
docker exec studenthub-postgres pg_dump -U studenthub studenthub | gzip > /root/backups/studenthub-before-restore-$(date +%F-%H%M).sql.gz
docker exec studenthub-postgres createdb -U studenthub studenthub_restore
gunzip -c $DUMP | docker exec -i studenthub-postgres psql -U studenthub -d studenthub_restore -v ON_ERROR_STOP=1 -q -o /dev/null
# тексер: сандар күткендей ме (пайдаланушылар, еске салулар, миграциялар саны)
docker exec studenthub-postgres psql -U studenthub -d studenthub_restore -Atc \
  "select (select count(*) from users), (select count(*) from reminders), (select count(*) from schema_migrations)"
# ауыстыру
docker exec studenthub-postgres psql -U studenthub -d postgres \
  -c 'alter database studenthub rename to studenthub_old' -c 'alter database studenthub_restore rename to studenthub'
docker compose start api web
systemctl start studenthub-cron.timer studenthub-notify.timer
curl -sf localhost:8110/api/health && echo ok
```

Бірнеше күн бәрі дұрыс жұмыс істесе: `docker exec studenthub-postgres dropdb -U studenthub studenthub_old`.
Артқа қайтару керек болса — API-ді тоқтатып, екі `rename`-ді керісінше жаса.

**2-жағдай. Бір кестені не бір жолды қайтару.** Дампты 1-жағдайдағыдай `studenthub_restore` базасына тұрғыз
(API-ді тоқтатпай), керек жолдарды сол жерден `psql` арқылы оқып, негізгі базаға қолмен көшір, соңында
`dropdb studenthub_restore`.

**3-жағдай. Контейнер не том жоғалды.** Compose томды өзі жасамайды (`external: true`) — бос базамен
байқаусызда іске қосылмас үшін. Алдымен томды және тек Postgres-ті тұрғыз, бірақ deploy-ды әлі **іске қоспа**:
ол миграцияларды бос базаға орындап, дампты қайтаруға кедергі жасайды.

```bash
docker volume create studenthub-pg
cd /opt/studenthub/docker && docker compose up -d postgres     # POSTGRES_PASSWORD ./.env-тен (DATABASE_URL-дағы құпия сөз)
```

Содан кейін:

```bash
docker exec studenthub-postgres psql -U studenthub -d studenthub -c 'create role app_user nologin' -c 'grant app_user to studenthub'
gunzip -c $DUMP | docker exec -i studenthub-postgres psql -U studenthub -d studenthub -v ON_ERROR_STOP=1 -q -o /dev/null
tar xzf uploads-<күні>.tar.gz -C /opt/studenthub && chown -R studenthub:studenthub /opt/studenthub/uploads
```

Енді `bash docker/deploy.sh` (өз компьютеріңнен) — миграциялар бұрыннан жазылған, сондықтан жаңасы ғана
орындалады; api, web, nginx, timer-лер орнына келеді.

### Қалпына келтіру тексерілді (2026-10-29, жергілікті)

Серверге тимей, дәл сондай `postgres:16-alpine` контейнерлерімен:

1. жаңа контейнер (`studenthub` пайдаланушысы) + `cd backend && go run ./cmd/migrate` (23 миграция, 60 кесте), 25 пайдаланушы,
   10 еске салу, 3 тоқтатылған сессия қосылды;
2. серверлік скрипттегідей дамп: `pg_dump -U studenthub studenthub | gzip`, `gzip -t` өтті, «dump complete» бар;
3. **сол контейнерде жаңа базаға** (1-жағдай) — `ON_ERROR_STOP=1` қатесіз, сандар бірдей:
   `users=25 reminders=10 revoked_sessions=3 schema_migrations=23 tables=60`;
4. **мүлде жаңа контейнерге** (3-жағдай) — `app_user` жасалмай тұрып `role "app_user" does not exist` қатесі
   шықты; рөлді жасағаннан кейін қатесіз қайтты, сандар бірдей, `cd backend && go run ./cmd/migrate` ештеңе орындамады,
   `app_user` рөлімен RLS бойынша оқу жұмыс істеді.

Өзің қайталау үшін (кез келген Docker бар компьютерде):

```bash
docker run -d --rm --name sh-src -e POSTGRES_USER=studenthub -e POSTGRES_PASSWORD=test -e POSTGRES_DB=studenthub -p 127.0.0.1:55442:5432 postgres:16-alpine
until docker exec sh-src pg_isready -U studenthub -q; do sleep 1; done; sleep 2
(cd backend && DATABASE_URL='postgres://studenthub:test@127.0.0.1:55442/studenthub?sslmode=disable' go run ./cmd/migrate)
docker exec sh-src pg_dump -U studenthub studenthub | gzip > /tmp/sh.sql.gz && gzip -t /tmp/sh.sql.gz
docker exec sh-src createdb -U studenthub studenthub_restore
gunzip -c /tmp/sh.sql.gz | docker exec -i sh-src psql -U studenthub -d studenthub_restore -v ON_ERROR_STOP=1 -q -o /dev/null
docker exec sh-src psql -U studenthub -d studenthub_restore -Atc 'select count(*) from schema_migrations'
docker stop sh-src
```
