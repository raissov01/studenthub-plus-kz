# Student Hub

Сейфуллин университеті студенттеріне арналған бір файлдық қосымша (`index.html`) + AI-чат серверлік функциясы (`api/chat.js`, Vercel).

Деплой: `vercel deploy --prod --yes` → https://studenthub-plus-kz.vercel.app

## AI-чат: қауіпсіздік және лимиттер

System промпт тек серверде (`api/chat.js` → `systemPrompt()`). Браузер тек `messages`, `lang` және профильдің бірнеше өрісін жібереді; сервер оларды тексеріп, белгілі мәндерден тыс нәрсені тастайды. Клиент `system` жіберсе — еленбейді.

Лимиттер Vercel → Project → Settings → Environment Variables арқылы өзгереді (кодқа тимей), өзгерткен соң қайта деплой керек:

| Айнымалы | Әдепкі | Мағынасы |
|---|---|---|
| `CHAT_LIMIT_PER_MIN` | 10 | бір IP-ден минутына сұраныс |
| `CHAT_LIMIT_PER_DAY` | 100 | бір IP-ден тәулігіне сұраныс (UTC бойынша) |
| `CHAT_MAX_MSG_LEN` | 2000 | бір хабарламаның ең көп таңбасы |
| `CHAT_MAX_HISTORY` | 16 | AI-ға жіберілетін соңғы хабарламалар саны |
| `OPENAI_MAX_TOKENS` | 900 | жауап ұзындығы (қатаң шегі 1500, коддан көтеріледі) |
| `ALLOWED_HOSTS` | `studenthub-plus-kz.vercel.app,localhost,127.0.0.1` | қай домендерден сұраныс қабылданады (үтірмен); жеке домен қоссаңыз, осында жазыңыз |
| `OPENAI_API_KEY` | — | AI кілті, **тек осында**, кодта жоқ |
| `OPENAI_MODEL`, `OPENAI_BASE_URL` | `gpt-4.1-mini`, OpenAI | модель және API мекенжайы |

Rate limit әдепкіде әр Vercel инстансының жадында санайды — бұл шамамен ғана (суық старттан кейін нөлденеді, бірнеше инстанс өз бетінше санайды). Нақты ортақ лимит үшін тегін Upstash Redis қосып, `UPSTASH_REDIS_REST_URL` мен `UPSTASH_REDIS_REST_TOKEN` айнымалыларын беріңіз — код өзі соған ауысады.

Origin/Referer тексеруі браузерден тыс қолдануды қиындатады, бірақ толық қорғаныс емес (curl тақырыпты қолдан қоя алады) — сондықтан негізгі қорғаныс IP лимиті мен `max_tokens` шегі.

### Браузерлік E2E тексеру

```bash
npx playwright install chromium
API_ORIGIN=http://127.0.0.1:4311 npx next build --webpack
npm run e2e
```

Playwright production `next start` арқылы kk/ru тілдерінде 360/390 px өлшемдерде сегіз негізгі студент жолын
тексереді. API жергілікті жасанды деректермен ауыстырылады, Go сервері қажет емес.
Толық нұсқаулық, 320 px тексеруі және нақты API режимі: [docs/E2E.md](docs/E2E.md).

## Апта сайынғы әкімшілік тізім (11-028)

Аптасына бір рет (мысалы, дүйсенбі таңертең), шамамен 30–40 минут. Барлығы admin аккаунтымен сайтта жасалады.

1. **Модерация кезегі** — `/admin/queue`: күтіп тұрған жазбаның бәрін не бекіт, не себебін жазып қайтар.
   Кезек бос болуы керек. Содан кейін `/admin/reports` (шағымдар), `/admin/market` (маркет хабарландырулары),
   `/admin/mentors` (ментор өтінімдері). 7 күннен көп күткен жазба қалмасын.
2. **Ескірген контент** — `/admin/content`:
   - мерзімі өткен (`expired`) мүмкіндік пен вакансия сайтта көрінбейді, бірақ тізімде тұр: керек болмаса — жасыр;
   - `verified_at` 30 күннен ескі жазбаның дереккөз сілтемесін ашып тексер, шарт өзгерсе — түзет, өзгермесе —
     «Тексерілген күні»-н бүгінге қой; сілтеме ашылмаса — жасыр;
   - үлгі (демо) жазба нақты деректей көрінбеуі керек: «үлгі» / «пример» белгісі тұр ма, қарап шық.
3. **Амбассадорлар** — `/admin/ambassadors`: жаңа өтінімдерге жауап бер; бір аптадан көп белсенділігі жоқ не
   уәде еткен сағатынан көп жұмыс алған амбассадормен сөйлес. `/admin` бетіндегі `amb_<код>` бойынша келген
   құрылғылар санын көр — кім студент әкеліп жатыр.
4. **Үш көрсеткіш** — `/admin` (соңғы 30 күн). Үшеуін де апта сайын бір кестеге (күні + үш сан) жазып отыр:
   - **Келесі күні қайта оралғандар** — 1-күні қайта кірген құрылғылар / 1 күн өткен құрылғылар;
   - **7-күні қайта оралғандар** — 7-күні қайта кірген / 7 күн өткен құрылғылар;
   - **Бірінші мүмкіндікті сақтағандар** — кемінде бір мерзімді сақтаған / барлық жаңа құрылғылар.
   Бір көрсеткіш екі апта қатарынан түссе — сол аптадағы өзгерісті (жаңа нұсқа, контент, қате) іздеу керек.
   Сол жерде «Не жетіспейді?» жауаптарын оқы (аты, телефон, сандар сервердің өзінде `[жасырылды]` болып тұрады).
5. **Жүйе күйі** — `/admin/health`: cron жұмыстарының бірі «Кешіккен» не «Әлі орындалмаған» болса, серверде
   `journalctl -u studenthub-cron -n 50` қара. Қате саны өсіп кетсе — сол бетте көрінеді.
6. **Резерв көшірме** — төмендегі «Резервтік көшірме» бөлімінің «Апта сайын» тармағын орында.

## Резервтік көшірме және қалпына келтіру (11-029)

Нақты дерекқор — VPS `85.137.91.47` ішіндегі `studenthub-postgres` Docker контейнері (`postgres:16-alpine`,
тек `127.0.0.1:5442`, дерек `studenthub-pg` томында, пайдаланушы/база `studenthub`). Оны
`deploy/deploy-vps.sh` жасайды.

### Не бар

- Серверде `/root/backup-dbs.sh` күн сайын **сервер уақытымен 03:30**-да (cron) іске қосылады. Student Hub
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
systemctl stop studenthub-api studenthub-cron.timer studenthub-notify.timer       # сайт уақытша 502 береді
docker exec studenthub-postgres pg_dump -U studenthub studenthub | gzip > /root/backups/studenthub-before-restore-$(date +%F-%H%M).sql.gz
docker exec studenthub-postgres createdb -U studenthub studenthub_restore
gunzip -c $DUMP | docker exec -i studenthub-postgres psql -U studenthub -d studenthub_restore -v ON_ERROR_STOP=1 -q -o /dev/null
# тексер: сандар күткендей ме (пайдаланушылар, еске салулар, миграциялар саны)
docker exec studenthub-postgres psql -U studenthub -d studenthub_restore -Atc \
  "select (select count(*) from users), (select count(*) from reminders), (select count(*) from schema_migrations)"
# ауыстыру
docker exec studenthub-postgres psql -U studenthub -d postgres \
  -c 'alter database studenthub rename to studenthub_old' -c 'alter database studenthub_restore rename to studenthub'
systemctl start studenthub-api studenthub-cron.timer studenthub-notify.timer
curl -sf localhost:8110/api/health && echo ok
```

Бірнеше күн бәрі дұрыс жұмыс істесе: `docker exec studenthub-postgres dropdb -U studenthub studenthub_old`.
Артқа қайтару керек болса — API-ді тоқтатып, екі `rename`-ді керісінше жаса.

**2-жағдай. Бір кестені не бір жолды қайтару.** Дампты 1-жағдайдағыдай `studenthub_restore` базасына тұрғыз
(API-ді тоқтатпай), керек жолдарды сол жерден `psql` арқылы оқып, негізгі базаға қолмен көшір, соңында
`dropdb studenthub_restore`.

**3-жағдай. Контейнер не том жоғалды.** Алдымен бос контейнерді `deploy/deploy-vps.sh` ішіндегі
`docker run ... studenthub-postgres ...` командасымен (сол құпия сөзбен) жаса, бірақ скриптің өзін әлі
**іске қоспа**: ол миграцияларды бос базаға орындап, дампты қайтаруға кедергі жасайды. Содан кейін:

```bash
docker exec studenthub-postgres psql -U studenthub -d studenthub -c 'create role app_user nologin' -c 'grant app_user to studenthub'
gunzip -c $DUMP | docker exec -i studenthub-postgres psql -U studenthub -d studenthub -v ON_ERROR_STOP=1 -q -o /dev/null
tar xzf uploads-<күні>.tar.gz -C /opt/studenthub && chown -R studenthub:studenthub /opt/studenthub/uploads
```

Енді `bash deploy/deploy-vps.sh` (өз компьютеріңнен) — миграциялар бұрыннан жазылған, сондықтан жаңасы ғана
орындалады; сервис, nginx, timer-лер орнына келеді.

### Қалпына келтіру тексерілді (2026-10-29, жергілікті)

Серверге тимей, дәл сондай `postgres:16-alpine` контейнерлерімен:

1. жаңа контейнер (`studenthub` пайдаланушысы) + `go run ./cmd/migrate` (23 миграция, 60 кесте), 25 пайдаланушы,
   10 еске салу, 3 тоқтатылған сессия қосылды;
2. серверлік скрипттегідей дамп: `pg_dump -U studenthub studenthub | gzip`, `gzip -t` өтті, «dump complete» бар;
3. **сол контейнерде жаңа базаға** (1-жағдай) — `ON_ERROR_STOP=1` қатесіз, сандар бірдей:
   `users=25 reminders=10 revoked_sessions=3 schema_migrations=23 tables=60`;
4. **мүлде жаңа контейнерге** (3-жағдай) — `app_user` жасалмай тұрып `role "app_user" does not exist` қатесі
   шықты; рөлді жасағаннан кейін қатесіз қайтты, сандар бірдей, `go run ./cmd/migrate` ештеңе орындамады,
   `app_user` рөлімен RLS бойынша оқу жұмыс істеді.

Өзің қайталау үшін (кез келген Docker бар компьютерде):

```bash
docker run -d --rm --name sh-src -e POSTGRES_USER=studenthub -e POSTGRES_PASSWORD=test -e POSTGRES_DB=studenthub -p 127.0.0.1:55442:5432 postgres:16-alpine
until docker exec sh-src pg_isready -U studenthub -q; do sleep 1; done; sleep 2
DATABASE_URL='postgres://studenthub:test@127.0.0.1:55442/studenthub?sslmode=disable' go run ./cmd/migrate
docker exec sh-src pg_dump -U studenthub studenthub | gzip > /tmp/sh.sql.gz && gzip -t /tmp/sh.sql.gz
docker exec sh-src createdb -U studenthub studenthub_restore
gunzip -c /tmp/sh.sql.gz | docker exec -i sh-src psql -U studenthub -d studenthub_restore -v ON_ERROR_STOP=1 -q -o /dev/null
docker exec sh-src psql -U studenthub -d studenthub_restore -Atc 'select count(*) from schema_migrations'
docker stop sh-src
```
