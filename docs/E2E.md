# Браузерлік тексеру (Playwright)

## Нақты жергілікті Go API: рөлдік ағындар

```bash
# Postgres: postgres://postgres:dev@127.0.0.1:54329/postgres
npm run e2e:real
# Бір тіл / экран:
npm run e2e:real -- --project=kk-390
```

`playwright.real.config.ts` — бөлек жиынтық: kk/390 px және ru/360 px.
Әр іске қосу `studenthub_browser_<pid>_<random>` уақытша базасын жасайды,
`cmd/migrate` миграцияларын қолданады, нақты Go HTTP handler мен `next dev --webpack`
серверлерін ашады. Соңында серверлер тоқтап, база мен жүктелген фотолар өшіріледі.
Порттар: 4320 (Next), 4321 (Go); `E2E_REAL_PORT` / `E2E_REAL_API_PORT` арқылы өзгертуге болады.
`DATABASE_URL` еленбейді: бұл runner тек жергілікті Postgres-ке қосылады.

`e2e/real-api/` ішінде API жауаптары ауыстырылмайды. Telegram widget callback-іне
тест BOT_TOKEN арқылы HMAC-пен қол қойылған деректер беріледі; кәдімгі кіру,
келісім, cookie және рөл тексерістері орындалады. Сыртқы Telegram скрипті ғана
оқшауланған; хабарлама жеткізу жабық жергілікті портқа бағытталады. Сайт ішіндегі
хабарламалар нақты базаға жазылады. Production кіруінде айналма жол жоқ.
Фото файлдары мен SQL көмекшісінің binary-і еленбейтін `.fe-deps/` ішінде.
Қате кезінде trace пен скриншоттар `test-results/real/`, HTML — `playwright-report/real/`.
Бұл жиынтықты production build немесе басқа Next серверімен қатар іске қоспаңыз.
Төмендегі жылдам mocked жиынтықтың конфигурациясы өзгермейді.

Тесттер Chromium-да **production Next.js** арқылы жүреді: `next dev` қолданылмайды.
`@playwright/test` нұсқасы `package.json` және lock-файлда нақты бекітілген.
Go сервері, Postgres, Telegram аккаунты немесе AI кілті қажет емес.

## Іске қосу

```bash
npm ci
npx playwright install chromium
# Linux-та браузер кітапханалары жетіспесе: npx playwright install --with-deps chromium
API_ORIGIN=http://127.0.0.1:4311 npx next build --webpack
npx tsc --noEmit
npx eslint .
npm run test:ts
npm run e2e
```

`node_modules` symlink болса да webpack қолданыңыз. Ортақ symlink нысанындағы пакеттерді басқа
жұмыс ағаштарымен қатар өзгертпеңіз; қажет болса осы жұмыс ағашына бөлек орнатыңыз.
TypeScript-тің `PageProps` / `LayoutProps` типтерін build жасайды, сондықтан таза көшірмеде build бірінші тұрады.

`npm run e2e` екі жергілікті серверді өзі іске қосып, соңында тоқтатады:

- `127.0.0.1:4311`: `e2e/server.ts` — тек SSR-ге арналған `/api/content` жасанды жауабы.
- `127.0.0.1:4310`: `npx next start` — дайын `.next` жинағы.

`API_ORIGIN` build кезінде rewrite/CSP-ға, start кезінде серверлік контент сұраныстарына қолданылады.
Сондықтан build және start үшін бірдей origin керек. Build кезінде fixture серверінің жұмыс істеуі міндетті емес:
контент беттері `connection()` арқылы сұраныс кезінде рендерленеді.
Портты ауыстыру: `E2E_PORT`, `E2E_API_PORT`; build-тағы `API_ORIGIN` портын да сәйкестендіріңіз.
Бар сервер автоматты түрде қайта қолданылмайды — бөтен процесті тестке қосып алмау үшін.

```bash
npm run e2e -- --project=kk-360
npm run e2e -- --project=ru-390 --headed
npx playwright show-report
npx playwright show-trace test-results/ҚАПШЫҚ/trace.zip
```

## Не тексеріледі

Төрт жоба: `kk-360`, `kk-390`, `ru-360`, `ru-390`. Әрқайсысында бірізді сегіз қадам бар:

1. Бос профиль → онбординг, GPA мен телефонсыз аяқтау.
2. Іздеу мәтіні, санат чипі, сүзгі парағы, тәжірибесіз сүзгісінің әдепкі күйі және сақталған таңдау.
3. Мүмкіндік беті, көрсетілмеген GPA, бөлісу сілтемесі, сақтау және жаңартудан кейінгі күй.
4. Даму жоспарын таңдау, қадамды орындау, прогрестің сақталуы.
5. AI-ға жылдам сұрақ; `/api/chat` жауабы `page.route()` арқылы ауыстырылады.
6. Баптаулардағы тілді ауыстыру және жаңартудан кейін тексеру.
7. Нақты service worker тіркелуі, кэш, интернетті өшіру, worker-ден бетті қайта ашу және офлайн жолақ.
8. Аккаунтқа кіру парағын ашу. Нақты авторизация жасалмайды.

Қосымша: үлгі профильді тек анық таңдағанда қосу, `studygo.v1` көшіруі, GPA/телефонды тазалау,
1/2/3-курс сүзгісі, жеңілдік шарты, 320 px экран, қауіпсіздік тақырыптары, карта/GPS, жергілікті қаріптер,
камера арқылы QR сканерлеу (жасанды декодталған билет), WebSocket origin және Telegram скрипт/iframe жүктелуі.
`web.telegram.org` iframe-ы рұқсат етіліп, басқа Telegram subdomain iframe-ы бұғатталатыны нақты браузерде тексеріледі. Браузер қатесі мен CSP бұзылуы тестті құлатады.
320 px суреттері, сәтсіз тест суреті мен trace `test-results/` ішінде, HTML есебі `playwright-report/` ішінде.
Бұл каталогтар git пен ESLint-тен шығарылған.

`e2e/fixtures.ts` браузердегі API сұраныстарын `page.route()` арқылы ұстайды; күтпеген endpoint тестті құлатады.
Сервер компонентінің fetch-ін Playwright ұстай алмайтындықтан, SSR бөлек жергілікті fixture серверін қолданады.
Қолданбада тест үшін жасанды `initData`, авторизация айналма жолы немесе production тест режимі қосылмаған.
Google Fonts пен Telegram әдеттегі тестте жергілікті жауаппен ауыстырылады; жергілікті woff2 нақты жүктеледі.
Камера — Chromium-ның жасанды құрылғысы, QR декодері белгілі билет қайтарады; WebSocket — Playwright эхо-мок. Бұл нақты Telegram кіруін,
физикалық QR тануды немесе тірі quiz серверінің протоколын растау емес.

Нақты Telegram виджетінің кодын CSP астында тексеруге болады (iframe жауабы бәрібір жасанды, аккаунтқа кірмейді):

```bash
mkdir -p test-results
curl -fsSL 'https://telegram.org/js/telegram-widget.js?22' -o test-results/telegram-widget.js
# Playwright test-results ішін тазалайды; файлды жұмыс ағашындағы еленбейтін тәуелділік каталогына көшіріңіз.
mkdir -p .fe-deps
cp test-results/telegram-widget.js .fe-deps/telegram-widget.js
E2E_TELEGRAM_SCRIPT="$PWD/.fe-deps/telegram-widget.js" npm run e2e -- --project=kk-360 --grep 'Telegram script'
```

## Нақты API-ге бағыттау

Бөлек, тек оқитын smoke-тест қолданылады: fixture-тің атаулары нақты контентте болуы міндетті емес.
`API_ORIGIN` мәнін өзіңіздің staging API-іңізге қойыңыз:

```bash
export API_ORIGIN=https://your-staging-api.example
npx next build --webpack
E2E_REAL_API=1 npm run e2e
```

Бұл режимде `/api/config` пен `/api/content` нақты серверден оқылады, алғашқы кіру тексеріледі.
`route()` API моктары мен жергілікті fixture сервері қосылмайды; чат, өтінім, тіркелу, хабарлама жіберілмейді.
Сегіз қадамдық детерминдік тесттің орнына `real-api.spec.ts` жүреді. Нақты рөлдердің рұқсаттарын,
сервердегі оқшаулауды, хабарламалар лимитін және әкімшілік lifecycle-ды бұл frontend жиынтығы дәлелдемейді.
