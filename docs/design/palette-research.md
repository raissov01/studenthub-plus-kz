# Пастель палитра: зерттеу және шешім

Сұраныс: «нежные цвета — фиолетовый, розовый, изумрудный» + ұқсас дизайндарды талдау.
Тұрақты ережелер: flat (тұтас түс, 1px жиек), градиент / blur / түсті жұмсақ көлеңке / эмодзи жоқ,
мәтін WCAG AA (4.5:1, ірі мәтін/иконка/жиек 3:1) жарық және қараңғы тақырыпта.

## 1. Референстер

Түстер App Store-дағы ашық скриншоттардан **өлшенген** (әр қосымшаның 3 скриншоты, 600px,
median-cut квантизация, 10 түс; пайыз — кадрдағы үлесі). Ескерту: бұл маркетинг кадрлары — фон
кейде жарнама плакаты, қосымшаның өз UI түсі емес; ондай жерлер белгіленген.

| # | Референс | Өлшенген түстер | Неліктен жұмсақ, бірақ оқылады | Біз не алдық |
| --- | --- | --- | --- | --- |
| 1 | [Tiimo](https://apps.apple.com/us/app/tiimo-to-do-list-planner/id1480220328) | `#D0C2FE` лаванда 11%, `#DFE4FE`, `#F1F1FE` фон, `#292537` мәтін | Лаванда тек үлкен беттерде, мәтін — дерлік қара күлгін (`#292537`), ақ карточкалар | Беттің фоны — өте әлсіз лаванда, мәтін — қою күлгін-қара `#1D1930` |
| 2 | [Structured](https://apps.apple.com/us/app/structured-daily-planner-todo/id1499198946) | `#FAE4E6` қызғылт 42% (плакат фоны), `#FFFFFF`, `#8D8787` | Әр тапсырманың өз пастель түсі, бірақ белгі (иконка) ішіндегі түс қанық | «Жұмсақ фон + қою сия» жұбы әр санатқа |
| 3 | [Finch](https://apps.apple.com/us/app/finch-self-care-pet/id1528595748) | `#A9E1FF` аспан 9%, `#E56AA9` қызғылт, `#E7A1C2`, `#569E54` жасыл | Иллюстрация жарқын, UI тақталары ақ — түс мәтінге түспейді | Қызғылт пен аспан түсі бөлек отбасы ретінде |
| 4 | [Daylio](https://apps.apple.com/us/app/daylio-journal-mood-tracker/id1194023242) | `#00B98D` изумруд 12%, `#C7FFE6` жалбыз, `#FF7170`, `#019BE3` | Көңіл-күй түстері тек дөңгелек белгілерде, мәтін бейтарап | Изумруд → `--c2 #1D8A66`, жалбыз фон `#DCF3E9` |
| 5 | [Notion Calendar](https://apps.apple.com/us/app/notion-calendar/id1607562761) | `#FFFFFF` 37%, `#E4F1FA` 22%, `#F7F8F8` | Күнтізбе оқиғалары — бозғылт фон + сол түстің қою жиегі/мәтіні | Күнтізбе нүктелері мен белгілері тек санат түсімен, тор ақ |
| 6 | [Duolingo](https://apps.apple.com/us/app/duolingo-learn-languages/id570060128) | `#FFFFFF` 56%, `#CE82FF` күлгін, `#32D1A5` жалбыз, `#FF800B` | Ашық түс үлкен батырмада ғана, қалғаны ақ; flat, градиенсіз | Бір ғана «батыл» элемент — негізгі батырма |
| 7 | [Headspace](https://apps.apple.com/us/app/headspace-sleep-meditation/id493145008) | `#FFE14F` 20%, `#FF7300`, `#5431A5` күлгін, `#101340` түнгі фон | Түнгі режим — қара емес, қою күлгін-көк | Қараңғы тақырып фоны `#14111D` (күлгін реңкті), таза қара емес |
| 8 | [Me+](https://apps.apple.com/us/app/me-lifestyle-routine/id1596403446) | `#F6DFFF` сирень, `#E5D3F3`, `#FFE087`, `#FECD36`, `#FFFCF7` | Пастель карточкалар + қою қоңыр/қара мәтін | Шабдалы (peach) отбасы `#FDEADC` / `#964212` |
| 9 | [Todoist](https://apps.apple.com/us/app/todoist-to-do-list-calendar/id572688855) | `#FFEFE5` шабдалы, `#FFF5DA`, `#EAF5EF` жалбыз, `#F7E8E2` | Пастель тек плакатта, UI ақ; басымдық түстері қою | Тег түстері: бозғылт фон + қою мәтін |
| 10 | [Studo](https://apps.apple.com/us/app/studo-university-student-app/id1095630119) (студент қосымшасы) | `#FFFFFF` 37%, `#F74A63` / `#FA4E5D` қызыл | Сабақ кестесі ақ торда бір түспен | Тұзақ: бір қанық түс бүкіл кестеде шаршатады — біз 5 жұмсақ отбасын алдық |
| 11 | [Kaspi.kz](https://apps.apple.com/us/app/kaspi-kz-%D1%81%D1%83%D0%BF%D0%B5%D1%80%D0%BF%D1%80%D0%B8%D0%BB%D0%BE%D0%B6%D0%B5%D0%BD%D0%B8%D0%B5/id1195076505) (жергілікті контекст) | `#F14635` 51% (плакат), UI ішінде `#FFFFFF`, `#F4F4F4` | Қазақстандық пайдаланушыға таныс: UI ақ-сұр, түс тек бренд/батырмада | Қызылдан аулақ (Kaspi-мен шатасады, «қате» сияқты), бренд — күлгін |
| 12 | [Bear](https://apps.apple.com/us/app/bear-markdown-notes-journal/id1016366447) | `#2E3235` 20%, `#363845`, `#FFFFFF` | Қараңғы тақырып — жылы қою сұр, мәтін ақ емес, сәл сұр | Қараңғыда мәтін `#EDE9F7`, таза ақ емес |

### Не жұмсақ етеді
- Үлкен беттер өте бозғылт (L ≈ 95–98%), қанық түс тек кішкене белгіде немесе бір батырмада.
- Әр түстің **екі нұсқасы**: фон үшін пастель, мәтін/иконка үшін сол реңктің қою «сиясы».
- Мәтін таза қара емес, бренд реңкі бар қою түс (күлгін-қара).
- Қараңғы тақырып — қою күлгін-қара фон, пастель сиялар ашық, ал фондары қою әрі әлсіз қаныққан.

### Тұзақтар
- **Пастель мәтін.** `#CE82FF` немесе `#32D1A5` ақ фонда 2.5–3:1 ғана — мәтінге жарамайды. Мәтін әрқашан
  сол реңктің қою нұсқасымен (`--p-*-ink`), ол 5.5:1+ береді.
- **Бәрі бірдей көрінеді.** Бес пастель бір экранда болса, иерархия жоғалады. Шешім: бет — бейтарап
  ақ/лаванда, карточкалар ақ + 1px жиек, түс тек белгі/тег/плиткада, бір ғана қою батырма.
- **Ақ мәтін пастельде.** Бұрын белгілерде (`.mk`, `.nico`) ақ әріп қанық түсте тұрды; пастельге ауысқанда
  ақ әріп оқылмайды → белгілер «бозғылт фон + қою әріп» болды.
- **Қараңғы тақырыпта сол пастельдер.** Ашық пастель фон қараңғыда жарқырайды → қараңғыда отбасы
  фондары қою (`#2B2443` т.б.), ал сиялар ашық (`#CDBFFF` т.б.).
- **Қызыл/қызғылт = қате.** Раушан (rose) тегі «ескерту» мағынасында ғана; жүрек/санауыш — раушан, бірақ
  қате мәтіні бөлек `--danger`.

## 2. Палитра

Негізгі түс (`--primary`) көктен **қою күлгінге** (`#5B45C9`) ауысты: сұраныстағы «фиолетовый» бренд
болды, ақ логотип онда 6.8:1. Логотип иконкалары (`public/icons/*`, `icon.svg`, `favicon.ico`) жаңа түспен
қайта салынды; `manifest.webmanifest`, `layout.tsx`, `offline.html` theme-color → `#F7F5FC`.

Токен атаулары өзгерген жоқ (компоненттер соларға сүйенеді). Жаңа: `--p-violet|rose|mint|peach|sky` және
олардың `-ink` жұптары, `--on-heart`, `--map-*`. Ескі плитка атаулары бүркеншік ат:
`--t-sage` = violet, `--t-mauve` = rose, `--t-taupe` = mint, `--t-ice` = sky, `--t-mist` = peach.

| Токен | Жарық | Қараңғы | Қайда |
| --- | --- | --- | --- |
| `--bg` | `#F7F5FC` | `#14111D` | бет фоны |
| `--surface` | `#FFFFFF` | `#1C1828` | карточка |
| `--surface-2` | `#F1EEF9` | `#252036` | басылған жол, инпут |
| `--tint` | `#EEE9FD` | `#2B2443` | әлсіз бояу |
| `--line` | `#E3DEF0` | `#372F4D` | 1px жиек |
| `--text` | `#1D1930` | `#EDE9F7` | мәтін |
| `--muted` | `#615B78` | `#ABA4C2` | екінші мәтін |
| `--primary` | `#5B45C9` | `#BBA9FF` | батырма, сілтеме, белсенді таб |
| `--primary-press` | `#4A35AE` | `#CDBFFF` | басылған батырма |
| `--on-primary` | `#FFFFFF` | `#1B1238` | батырма мәтіні |
| `--primary-soft` | `#EEE9FD` | `#2B2443` | таңдалған чип, аватар |
| `--accent` / `--accent-soft` | `#FCE7EF` / `#FDF2F6` | `#3A2231` / `#2A1D26` | екінші акцент (раушан) |
| `--deep` / `--on-deep` / `--on-deep-muted` | `#FFFFFF` / `#1D1930` / `#615B78` | `#1C1828` / `#EDE9F7` / `#ABA4C2` | тақырып жолағы |
| `--danger` / `--danger-soft` | `#B92D4E` / `#FCE6EB` | `#F3A6B8` / `#3E1D27` | қате |
| `--heart` / `--on-heart` | `#C2386E` / `#FFFFFF` | `#F18DB3` / `#2A0F1C` | санауыш, жүрек |
| `--p-violet` / `-ink` (= `--t-sage`) | `#EEE9FD` / `#5A3FC0` | `#2B2443` / `#CDBFFF` | күлгін отбасы |
| `--p-rose` / `-ink` (= `--t-mauve`) | `#FCE7EF` / `#A3285A` | `#3A2231` / `#F6B7CF` | раушан отбасы |
| `--p-mint` / `-ink` (= `--t-taupe`) | `#DCF3E9` / `#0E6B4D` | `#17302A` / `#97DDBF` | изумруд/жалбыз отбасы |
| `--p-peach` / `-ink` (= `--t-mist`) | `#FDEADC` / `#964212` | `#382819` / `#F4C19E` | шабдалы отбасы |
| `--p-sky` / `-ink` (= `--t-ice`) | `#E1EEFB` / `#225E9B` | `#1A2A3C` / `#A8CDF2` | аспан отбасы |
| `--c0` … `--c4` | `#6A52D4` `#C6457E` `#1D8A66` `#C2652A` `#2F79BF` | `#A996FF` `#EE8FB6` `#5FCB9F` `#EDA374` `#7FB6EC` | күнтізбе нүктелері, hero пішіндері |
| `--sw-*` (екі тақырыпта бірдей) | `#EEE9FD` `#FCE7EF` `#DCF3E9` `#FDEADC` `#E1EEFB`, сия `#1D1930` | — | ескі swatch-тар |
| `--map-*` (екі тақырыпта бірдей) | study `#2D5CB8`, dorm `#5A3FC0`, other `#964212`, ent `#0E6B4D`, rose `#A3285A`, ink `#1D1930` | — | кампус картасы: фото екі тақырыпта бірдей, реңктер фотодағы ғимараттарға сай (көк оқу, күлгін жатақхана, қызғылт сары басқа) |

Күнтізбе түрлері (`--cal-*`, календарь агентінің токендері, отбасы сияларына бағытталған): сабақ — `--muted`,
мүмкіндік — аспан, вакансия — изумруд, іс-шара — күлгін, емтихан — раушан, өз дедлайным — шабдалы
(жарықта `#225E9B #0E6B4D #5A3FC0 #A3285A #964212`, қараңғыда `#A8CDF2 #97DDBF #CDBFFF #F6B7CF #F4C19E`).
`e2e/calendar.spec.ts` іс-шара түсін тексереді — мәні `rgb(90, 63, 192)` болып жаңартылды.
Hero WebGL пішіндері `--c0…--c4` оқиды.

Компонент деңгейіндегі өзгерістер тек `app/globals.css` соңындағы «pastel palette» бөлімінде (тек түс,
layout жоқ): hero мен баннер — қанық көк блок орнына лаванда карточка + 1px жиек; тізім белгілері
(`.mk`, `.nico`, `.thumb`) — пастель фон + қою әріп; тегтер; карта түйреуіштері; санауыштар.
Telegram Mini App: `features/telegram/webapp.ts` бұрынғыдай Telegram-ның өз фон/мәтін/батырма
түстерін қояды (нативті сезім); пастель отбасылары `data-theme` бойынша сақталады.

## 3. Контраст тексерісі

`node tools/contrast.mjs` (`--md` — кесте) `globals.css` токен блоктарын оқып, `var()` тізбегін шешеді
де, қолданыстағы әр жұпты тексереді; AA-дан төмен болса exit 1.

### light

| foreground | background | ratio | min | | used for |
| --- | --- | ---: | ---: | --- | --- |
| `--text` #1D1930 | `--bg` #F7F5FC | 15.74 | 4.5 | pass | body text on the page |
| `--text` #1D1930 | `--surface` #FFFFFF | 17.02 | 4.5 | pass | card text |
| `--text` #1D1930 | `--surface-2` #F1EEF9 | 14.86 | 4.5 | pass | text on pressed rows / inputs |
| `--muted` #615B78 | `--bg` #F7F5FC | 5.93 | 4.5 | pass | secondary text on the page |
| `--muted` #615B78 | `--surface` #FFFFFF | 6.41 | 4.5 | pass | secondary text in cards |
| `--muted` #615B78 | `--surface-2` #F1EEF9 | 5.60 | 4.5 | pass | secondary text on surface-2 |
| `--muted` #615B78 | `--p-violet` #EEE9FD | 5.41 | 4.5 | pass | hero subtitle |
| `--text` #1D1930 | `--p-violet` #EEE9FD | 14.35 | 4.5 | pass | hero title, tile labels (violet) |
| `--text` #1D1930 | `--p-rose` #FCE7EF | 14.44 | 4.5 | pass | tile labels (rose) |
| `--text` #1D1930 | `--p-mint` #DCF3E9 | 14.62 | 4.5 | pass | tile labels (mint) |
| `--text` #1D1930 | `--p-peach` #FDEADC | 14.57 | 4.5 | pass | tile labels (peach) |
| `--text` #1D1930 | `--p-sky` #E1EEFB | 14.44 | 4.5 | pass | tile labels (sky) |
| `--text` #1D1930 | `--primary-soft` #EEE9FD | 14.35 | 4.5 | pass | geo status, hero stats |
| `--text` #1D1930 | `--accent` #FCE7EF | 14.44 | 4.5 | pass | .btn.sun / accent |
| `--primary` #5B45C9 | `--surface` #FFFFFF | 6.69 | 4.5 | pass | links, text buttons, active tab |
| `--primary` #5B45C9 | `--bg` #F7F5FC | 6.19 | 4.5 | pass | links on the page |
| `--primary` #5B45C9 | `--primary-soft` #EEE9FD | 5.64 | 4.5 | pass | selected chip, avatar letter, .ico |
| `--on-primary` #FFFFFF | `--primary` #5B45C9 | 6.69 | 4.5 | pass | primary button, today, date badge |
| `--on-primary` #FFFFFF | `--primary-press` #4A35AE | 8.71 | 4.5 | pass | pressed primary button |
| `--primary` #5B45C9 | `--surface` #FFFFFF | 6.69 | 3 | pass | focus ring, selected day border |
| `--danger` #B92D4E | `--surface` #FFFFFF | 5.92 | 4.5 | pass | error text |
| `--danger` #B92D4E | `--danger-soft` #FCE6EB | 4.98 | 4.5 | pass | error on its tint |
| `--on-heart` #FFFFFF | `--heart` #C2386E | 5.14 | 4.5 | pass | unread counters |
| `--on-deep` #1D1930 | `--deep` #FFFFFF | 17.02 | 4.5 | pass | header |
| `--on-deep-muted` #615B78 | `--deep` #FFFFFF | 6.41 | 4.5 | pass | header secondary |
| `--p-violet-ink` #5A3FC0 | `--p-violet` #EEE9FD | 6.09 | 4.5 | pass | violet tile icon, tag, mark letter |
| `--p-rose-ink` #A3285A | `--p-rose` #FCE7EF | 5.92 | 4.5 | pass | rose tile icon, tag, mark letter |
| `--p-mint-ink` #0E6B4D | `--p-mint` #DCF3E9 | 5.59 | 4.5 | pass | mint tile icon, tag, mark letter |
| `--p-peach-ink` #964212 | `--p-peach` #FDEADC | 5.83 | 4.5 | pass | peach tile icon, tag, mark letter |
| `--p-sky-ink` #225E9B | `--p-sky` #E1EEFB | 5.67 | 4.5 | pass | sky tile icon, tag, mark letter |
| `--sw-ink` #1D1930 | `--sw-sage` #EEE9FD | 14.35 | 4.5 | pass | legacy swatch (violet) |
| `--sw-ink` #1D1930 | `--sw-mauve` #FCE7EF | 14.44 | 4.5 | pass | legacy swatch (rose) |
| `--sw-ink` #1D1930 | `--sw-taupe` #DCF3E9 | 14.62 | 4.5 | pass | legacy swatch (mint) |
| `--sw-ink` #1D1930 | `--sw-mist` #FDEADC | 14.57 | 4.5 | pass | legacy swatch (peach) |
| `--sw-ink` #1D1930 | `--sw-ice` #E1EEFB | 14.44 | 4.5 | pass | legacy swatch (sky) |
| `--c0` #6A52D4 | `--surface` #FFFFFF | 5.57 | 3 | pass | calendar dot: opportunity |
| `--c1` #C6457E | `--surface` #FFFFFF | 4.61 | 3 | pass | calendar dot: event |
| `--c2` #1D8A66 | `--surface` #FFFFFF | 4.30 | 3 | pass | calendar dot: job |
| `--c3` #C2652A | `--surface` #FFFFFF | 4.05 | 3 | pass | calendar dot: own deadline |
| `--c4` #2F79BF | `--surface` #FFFFFF | 4.56 | 3 | pass | category colour 4 |
| `--c0` #6A52D4 | `--surface-2` #F1EEF9 | 4.86 | 3 | pass | calendar icon on surface-2 |
| `--c2` #1D8A66 | `--surface-2` #F1EEF9 | 3.76 | 3 | pass | calendar icon on surface-2 |
| `--c1` #C6457E | `--surface-2` #F1EEF9 | 4.03 | 3 | pass | calendar icon on surface-2 |
| `--c3` #C2652A | `--surface-2` #F1EEF9 | 3.53 | 3 | pass | calendar icon on surface-2 |
| `--cal-class` #615B78 | `--surface` #FFFFFF | 6.41 | 4.5 | pass | calendar kind text/stripe: lessons |
| `--cal-class` #615B78 | `--bg` #F7F5FC | 5.93 | 4.5 | pass | calendar kind on the page: lessons |
| `--surface` #FFFFFF | `--cal-class` #615B78 | 6.41 | 3 | pass | calendar legend swatch glyph: lessons |
| `--cal-opp` #225E9B | `--surface` #FFFFFF | 6.68 | 4.5 | pass | calendar kind text/stripe: opportunity |
| `--cal-opp` #225E9B | `--bg` #F7F5FC | 6.18 | 4.5 | pass | calendar kind on the page: opportunity |
| `--surface` #FFFFFF | `--cal-opp` #225E9B | 6.68 | 3 | pass | calendar legend swatch glyph: opportunity |
| `--cal-job` #0E6B4D | `--surface` #FFFFFF | 6.50 | 4.5 | pass | calendar kind text/stripe: job |
| `--cal-job` #0E6B4D | `--bg` #F7F5FC | 6.01 | 4.5 | pass | calendar kind on the page: job |
| `--surface` #FFFFFF | `--cal-job` #0E6B4D | 6.50 | 3 | pass | calendar legend swatch glyph: job |
| `--cal-event` #5A3FC0 | `--surface` #FFFFFF | 7.22 | 4.5 | pass | calendar kind text/stripe: event |
| `--cal-event` #5A3FC0 | `--bg` #F7F5FC | 6.68 | 4.5 | pass | calendar kind on the page: event |
| `--surface` #FFFFFF | `--cal-event` #5A3FC0 | 7.22 | 3 | pass | calendar legend swatch glyph: event |
| `--cal-exam` #A3285A | `--surface` #FFFFFF | 6.98 | 4.5 | pass | calendar kind text/stripe: exam |
| `--cal-exam` #A3285A | `--bg` #F7F5FC | 6.46 | 4.5 | pass | calendar kind on the page: exam |
| `--surface` #FFFFFF | `--cal-exam` #A3285A | 6.98 | 3 | pass | calendar legend swatch glyph: exam |
| `--cal-own` #964212 | `--surface` #FFFFFF | 6.81 | 4.5 | pass | calendar kind text/stripe: own deadline |
| `--cal-own` #964212 | `--bg` #F7F5FC | 6.30 | 4.5 | pass | calendar kind on the page: own deadline |
| `--surface` #FFFFFF | `--cal-own` #964212 | 6.81 | 3 | pass | calendar legend swatch glyph: own deadline |
| `#FFFFFF` #FFFFFF | `--map-study` #2D5CB8 | 6.30 | 3 | pass | map: white glyph / stroke on study |
| `#FFFFFF` #FFFFFF | `--map-dorm` #5A3FC0 | 7.22 | 3 | pass | map: white on dorm |
| `#FFFFFF` #FFFFFF | `--map-other` #964212 | 6.81 | 3 | pass | map: white on other |
| `#FFFFFF` #FFFFFF | `--map-ent` #0E6B4D | 6.50 | 3 | pass | map: white on entrance / shop |
| `#FFFFFF` #FFFFFF | `--map-rose` #A3285A | 6.98 | 3 | pass | map: white on food / health |
| `--map-ink` #1D1930 | `#FFFFFF` #FFFFFF | 17.02 | 4.5 | pass | map: pin number |
| `#FFFFFF` #FFFFFF | `--map-street` #4E4868 | 8.57 | 4.5 | pass | map: street label halo |

### dark

| foreground | background | ratio | min | | used for |
| --- | --- | ---: | ---: | --- | --- |
| `--text` #EDE9F7 | `--bg` #14111D | 15.61 | 4.5 | pass | body text on the page |
| `--text` #EDE9F7 | `--surface` #1C1828 | 14.54 | 4.5 | pass | card text |
| `--text` #EDE9F7 | `--surface-2` #252036 | 13.15 | 4.5 | pass | text on pressed rows / inputs |
| `--muted` #ABA4C2 | `--bg` #14111D | 7.82 | 4.5 | pass | secondary text on the page |
| `--muted` #ABA4C2 | `--surface` #1C1828 | 7.29 | 4.5 | pass | secondary text in cards |
| `--muted` #ABA4C2 | `--surface-2` #252036 | 6.59 | 4.5 | pass | secondary text on surface-2 |
| `--muted` #ABA4C2 | `--p-violet` #2B2443 | 6.14 | 4.5 | pass | hero subtitle |
| `--text` #EDE9F7 | `--p-violet` #2B2443 | 12.25 | 4.5 | pass | hero title, tile labels (violet) |
| `--text` #EDE9F7 | `--p-rose` #3A2231 | 12.11 | 4.5 | pass | tile labels (rose) |
| `--text` #EDE9F7 | `--p-mint` #17302A | 11.79 | 4.5 | pass | tile labels (mint) |
| `--text` #EDE9F7 | `--p-peach` #382819 | 11.85 | 4.5 | pass | tile labels (peach) |
| `--text` #EDE9F7 | `--p-sky` #1A2A3C | 12.22 | 4.5 | pass | tile labels (sky) |
| `--text` #EDE9F7 | `--primary-soft` #2B2443 | 12.25 | 4.5 | pass | geo status, hero stats |
| `--text` #EDE9F7 | `--accent` #3A2231 | 12.11 | 4.5 | pass | .btn.sun / accent |
| `--primary` #BBA9FF | `--surface` #1C1828 | 8.45 | 4.5 | pass | links, text buttons, active tab |
| `--primary` #BBA9FF | `--bg` #14111D | 9.07 | 4.5 | pass | links on the page |
| `--primary` #BBA9FF | `--primary-soft` #2B2443 | 7.12 | 4.5 | pass | selected chip, avatar letter, .ico |
| `--on-primary` #1B1238 | `--primary` #BBA9FF | 8.60 | 4.5 | pass | primary button, today, date badge |
| `--on-primary` #1B1238 | `--primary-press` #CDBFFF | 10.50 | 4.5 | pass | pressed primary button |
| `--primary` #BBA9FF | `--surface` #1C1828 | 8.45 | 3 | pass | focus ring, selected day border |
| `--danger` #F3A6B8 | `--surface` #1C1828 | 9.05 | 4.5 | pass | error text |
| `--danger` #F3A6B8 | `--danger-soft` #3E1D27 | 7.77 | 4.5 | pass | error on its tint |
| `--on-heart` #2A0F1C | `--heart` #F18DB3 | 7.77 | 4.5 | pass | unread counters |
| `--on-deep` #EDE9F7 | `--deep` #1C1828 | 14.54 | 4.5 | pass | header |
| `--on-deep-muted` #ABA4C2 | `--deep` #1C1828 | 7.29 | 4.5 | pass | header secondary |
| `--p-violet-ink` #CDBFFF | `--p-violet` #2B2443 | 8.70 | 4.5 | pass | violet tile icon, tag, mark letter |
| `--p-rose-ink` #F6B7CF | `--p-rose` #3A2231 | 8.67 | 4.5 | pass | rose tile icon, tag, mark letter |
| `--p-mint-ink` #97DDBF | `--p-mint` #17302A | 8.98 | 4.5 | pass | mint tile icon, tag, mark letter |
| `--p-peach-ink` #F4C19E | `--p-peach` #382819 | 8.73 | 4.5 | pass | peach tile icon, tag, mark letter |
| `--p-sky-ink` #A8CDF2 | `--p-sky` #1A2A3C | 8.80 | 4.5 | pass | sky tile icon, tag, mark letter |
| `--sw-ink` #1D1930 | `--sw-sage` #EEE9FD | 14.35 | 4.5 | pass | legacy swatch (violet) |
| `--sw-ink` #1D1930 | `--sw-mauve` #FCE7EF | 14.44 | 4.5 | pass | legacy swatch (rose) |
| `--sw-ink` #1D1930 | `--sw-taupe` #DCF3E9 | 14.62 | 4.5 | pass | legacy swatch (mint) |
| `--sw-ink` #1D1930 | `--sw-mist` #FDEADC | 14.57 | 4.5 | pass | legacy swatch (peach) |
| `--sw-ink` #1D1930 | `--sw-ice` #E1EEFB | 14.44 | 4.5 | pass | legacy swatch (sky) |
| `--c0` #A996FF | `--surface` #1C1828 | 7.02 | 3 | pass | calendar dot: opportunity |
| `--c1` #EE8FB6 | `--surface` #1C1828 | 7.63 | 3 | pass | calendar dot: event |
| `--c2` #5FCB9F | `--surface` #1C1828 | 8.70 | 3 | pass | calendar dot: job |
| `--c3` #EDA374 | `--surface` #1C1828 | 8.34 | 3 | pass | calendar dot: own deadline |
| `--c4` #7FB6EC | `--surface` #1C1828 | 8.10 | 3 | pass | category colour 4 |
| `--c0` #A996FF | `--surface-2` #252036 | 6.35 | 3 | pass | calendar icon on surface-2 |
| `--c2` #5FCB9F | `--surface-2` #252036 | 7.87 | 3 | pass | calendar icon on surface-2 |
| `--c1` #EE8FB6 | `--surface-2` #252036 | 6.90 | 3 | pass | calendar icon on surface-2 |
| `--c3` #EDA374 | `--surface-2` #252036 | 7.54 | 3 | pass | calendar icon on surface-2 |
| `--cal-class` #ABA4C2 | `--surface` #1C1828 | 7.29 | 4.5 | pass | calendar kind text/stripe: lessons |
| `--cal-class` #ABA4C2 | `--bg` #14111D | 7.82 | 4.5 | pass | calendar kind on the page: lessons |
| `--surface` #1C1828 | `--cal-class` #ABA4C2 | 7.29 | 3 | pass | calendar legend swatch glyph: lessons |
| `--cal-opp` #A8CDF2 | `--surface` #1C1828 | 10.47 | 4.5 | pass | calendar kind text/stripe: opportunity |
| `--cal-opp` #A8CDF2 | `--bg` #14111D | 11.24 | 4.5 | pass | calendar kind on the page: opportunity |
| `--surface` #1C1828 | `--cal-opp` #A8CDF2 | 10.47 | 3 | pass | calendar legend swatch glyph: opportunity |
| `--cal-job` #97DDBF | `--surface` #1C1828 | 11.08 | 4.5 | pass | calendar kind text/stripe: job |
| `--cal-job` #97DDBF | `--bg` #14111D | 11.89 | 4.5 | pass | calendar kind on the page: job |
| `--surface` #1C1828 | `--cal-job` #97DDBF | 11.08 | 3 | pass | calendar legend swatch glyph: job |
| `--cal-event` #CDBFFF | `--surface` #1C1828 | 10.32 | 4.5 | pass | calendar kind text/stripe: event |
| `--cal-event` #CDBFFF | `--bg` #14111D | 11.08 | 4.5 | pass | calendar kind on the page: event |
| `--surface` #1C1828 | `--cal-event` #CDBFFF | 10.32 | 3 | pass | calendar legend swatch glyph: event |
| `--cal-exam` #F6B7CF | `--surface` #1C1828 | 10.40 | 4.5 | pass | calendar kind text/stripe: exam |
| `--cal-exam` #F6B7CF | `--bg` #14111D | 11.17 | 4.5 | pass | calendar kind on the page: exam |
| `--surface` #1C1828 | `--cal-exam` #F6B7CF | 10.40 | 3 | pass | calendar legend swatch glyph: exam |
| `--cal-own` #F4C19E | `--surface` #1C1828 | 10.71 | 4.5 | pass | calendar kind text/stripe: own deadline |
| `--cal-own` #F4C19E | `--bg` #14111D | 11.50 | 4.5 | pass | calendar kind on the page: own deadline |
| `--surface` #1C1828 | `--cal-own` #F4C19E | 10.71 | 3 | pass | calendar legend swatch glyph: own deadline |
| `#FFFFFF` #FFFFFF | `--map-study` #2D5CB8 | 6.30 | 3 | pass | map: white glyph / stroke on study |
| `#FFFFFF` #FFFFFF | `--map-dorm` #5A3FC0 | 7.22 | 3 | pass | map: white on dorm |
| `#FFFFFF` #FFFFFF | `--map-other` #964212 | 6.81 | 3 | pass | map: white on other |
| `#FFFFFF` #FFFFFF | `--map-ent` #0E6B4D | 6.50 | 3 | pass | map: white on entrance / shop |
| `#FFFFFF` #FFFFFF | `--map-rose` #A3285A | 6.98 | 3 | pass | map: white on food / health |
| `--map-ink` #1D1930 | `#FFFFFF` #FFFFFF | 17.02 | 4.5 | pass | map: pin number |
| `#FFFFFF` #FFFFFF | `--map-street` #4E4868 | 8.57 | 4.5 | pass | map: street label halo |

all pairs pass WCAG AA

Ең төмен мәндер (жарық): мәтін — `--danger` / `--danger-soft` 4.98, `--on-heart` / `--heart` 5.14; UI — `--c3` / `--surface-2` 3.53.

Playwright жиынтығындағы axe тексерістері де өтті (төменде «Тексеру»).

## 4. Скриншоттар (390×844)

`docs/design/screens/` — `before-*` (бұрынғы көк палитра) және `after-*` (пастель):
`{light,dark}-{home,search,calendar,map,profile,sheet}.png`. Қайта түсіру:
`npm run -s build && SHOT_TAG=after npx playwright test -c tools/screens`.
