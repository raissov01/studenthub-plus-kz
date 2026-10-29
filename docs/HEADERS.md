# Frontend жауап тақырыптары (CSP)

Маршрут аудиті: [SECURITY.md](SECURITY.md) (go run ./cmd/routes автоматты жазады — оны қолмен өзгертпеңіз).
`next.config.ts → headers()` HTML және статикалық файлдар үшін ортақ саясат береді (proxy API жауап тақырыптарын Go сервері басқарады):

- `Content-Security-Policy`: өз origin-іміз, нақты API origin (`API_ORIGIN`), quiz үшін
  `wss://studenthub.85-137-91-47.sslip.io`, Telegram скрипттері/iframe-дары, қолданылып жатқан Google Fonts.
  Суреттерге `self`, API origin, `data:` және `blob:`; worker-ге `self`/`blob:` рұқсат.
  `object-src 'none'`, `base-uri 'self'`, `form-action 'self'`.
- `Referrer-Policy: strict-origin-when-cross-origin` және `X-Content-Type-Options: nosniff`.
- `Permissions-Policy`: картаға `geolocation=(self)`, QR/фотоға `camera=(self)`,
  микрофон, төлем, USB, serial және қозғалыс сенсорлары өшірулі.

Студенттік маршруттар Mini App ішіндегі deep link ретінде ашыла алады. Оларда тек
`frame-ancestors https://web.telegram.org` қолданылады: `/a/` және `/k/` бір origin.
Wildcard `*.telegram.org` берілмейді. Telegram-ның [ресми bridge коды](https://telegram.org/js/telegram-web-app.js)
`postMessage` үшін дәл осы origin-ге сенеді; [Mini App құжаты](https://core.telegram.org/bots/webapps)
ресми скриптті қосуды сипаттайды. `frame-src` — біздің бет аша алатын ішкі iframe-дар:
`https://telegram.org` және login виджетіндегі `https://oauth.telegram.org`.

`/admin`, `/p/*` Telegram-ға ендіруді қажет етпейді: `frame-ancestors 'none'`
және `X-Frame-Options: DENY`. Студенттік маршруттарға `X-Frame-Options` қойылмайды:
`SAMEORIGIN` Telegram-ды бұзады, ал `ALLOW-FROM` ескірген және браузерлерде еленбейді
([MDN](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/X-Frame-Options)).
Cross-origin iframe ішіндегі камера/GPS үшін Telegram ата-анасы да тиісті permission-ды беруі керек;
біздің тақырып тек өз құжатымызға рұқсат береді.

Бұл — үйлесімділікті сақтайтын enforced CSP. `unsafe-inline` Next hydration, бастапқы theme скрипті
және бар inline стильдер үшін қалды. `unsafe-eval` —
[legacy Telegram login виджетінің](https://telegram.org/js/telegram-widget.js?22) `data-onauth` callback-ін
`eval` арқылы құруына байланысты. Бұл XSS-тен толық қорғайтын nonce-only саясат емес.
Осы екі рұқсатты жою үшін rendering мен login интеграциясын бөлек көшіру керек; рұқсатты жай өшіру
кіруді немесе гидратацияны бұзады. Қолданбада жаңа inline обработчиктер не eval қосылған жоқ.

Тексеру: [E2E.md](E2E.md). Тесттер браузердегі CSP бұзылуларын жинайды; нақты құрылғыдағы
Telegram кіруі мен камера/QR, өндірістік quiz WebSocket протоколы бөлек интеграциялық тексеруді қажет етеді.
