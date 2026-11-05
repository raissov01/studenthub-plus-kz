# Клубтар сеиді: ресми дереккөздер (2026-11-05)

`backend/internal/clubs/seed/kazatu.json` (49 жазба) тек `CLUBS_SEED=1` болғанда жүктеледі (`docs/clubs.md`).
Әр жазбада `source_url` пен `verified_at` бар. Демо/үлгі клуб жоқ. Жетекшілердің аты-жөні, телефоны сақталмайды —
тек рөл атауы («Төраға», «Жетекші»). Сипаттамалар дереккөздегі мәтіннің қысқа мазмұны (kk/ru).

## Қалай тексерілді

- Негізгі бет: <https://kazatu.edu.kz/ru/page/studenceskie-kluby> (қазақша:
  <https://kazatu.edu.kz/kz/page/studenceskie-kluby>), «Жастар саясаты және әлеуметтік жұмыс департаменті»
  бетінен сілтеме бар. Әр клубтың атауы, мақсаты, төрағасының рөлі, көбінде Instagram атауы көрсетілген.
- Қосымша: ЖІК (КДМ) беті, спорт клубы беті, «Ғылыми үйірмелер» беті, жеке үйірме беттері (Python, Arduino,
  Formula Student), 2024 жылғы қазан/қарашадағы ЖІК рейтингтері, еріктілер беті.
- kazatu.edu.kz TLS тізбегі толық емес (`unable to verify the first certificate`), сондықтан беттер `curl -k`
  арқылы оқылды; барлығы HTTP 200 қайтарды.

## Кесте

| seed_key | Атауы (kk / ru) | Бағыты | Instagram | Дереккөз | Тексерілді |
|---|---|---|---|---|---|
| `kazatu-parlament` | Студенттік Парламент / Студенческий Парламент | Басқа | https://www.instagram.com/Parlament_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-senat` | Студенттік Сенат / Студенческий Сенат | Басқа | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-mazhilis` | Студенттік Мәжіліс / Студенческий Мажилис | Басқа | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-kdm` | Жастар ісі жөніндегі комитет (ЖІК) / Комитет по делам молодёжи (КДМ) | Басқа | — | [kdm](https://kazatu.edu.kz/ru/pages/universitet/studenceskaa-zizn/kdm) | 2026-11-05 |
| `kazatu-komek` | «Көмек» еріктілер ұйымы / Волонтёрская организация «Kazatzu komek» | Еріктілер | https://www.instagram.com/kazatzu.komek/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-parasat` | «Парасат» студенттік кәсіподақ ұйымы / Студенческий профсоюзный комитет «Парасат» | Басқа | https://www.instagram.com/kazatzu_parasat/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-zhastar-ruhy` | «Жастар рухы» жастар қанаты / Молодёжное крыло «Жастар рухы» | Басқа | https://www.instagram.com/jastar_ruhy_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-alliance` | ҚАТЗУ студенттерінің Альянсы / Альянс студентов КАТИУ | Басқа | https://www.instagram.com/qsa_kazatu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-ulandary` | «ҚАТЗУ ұландары» ұйымы / Организация «КАТЗУ ұландары» | Басқа | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-sanaly-urpaq` | «Sanaly Urpaq» клубы / Студенческий клуб «Sanaly Urpaq» | Басқа | https://www.instagram.com/sanaly.urpaq_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-mediators` | Медиаторлар ұйымы / Медиаторы | Басқа | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-altyn-belgi` | «Алтын белгі» иегерлері қауымдастығы / Студенческий клуб «Алтын белгі» | Ғылым | https://www.instagram.com/kazatzu_ab/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-shyraq` | «Шырақ» интеллектуалды пікір-сайыс клубы / Дебатный клуб «Шырақ» | Дебат | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-mangilik` | «MANGILIK» дебат клубы / Дебатный клуб «MANGILIK» | Дебат | https://www.instagram.com/dc_mangilik/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-media` | «KATZU Media» клубы / Клуб «KazATZU Media» | Медиа | https://www.instagram.com/kazatzu_media/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-youtube` | «SU YOUTUBE CHANNEL» клубы / «SU YOUTUBE CHANNEL» | Медиа | https://www.instagram.com/kazatzu.channel/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-sheber` | «Шебер қолөнершілер» клубы / Клуб «Мастер ремесленников» | Өнер | https://www.instagram.com/club_sheber_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-qyrmyzy` | «Қырмызы» қыздар клубы / Клуб девушек «Қырмызы» | Мәдениет | https://www.instagram.com/qyrmyzy_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-arrow-crew` | «Arrow crew» заманауи би тобы / Современная танцевальная группа «Arrow crew» | Өнер | https://www.instagram.com/arrow.crew_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-image-girls` | «KATZU IMAGE» қыздар клубы / Клуб девушек «KATZU IMAGE» | Мәдениет | https://www.instagram.com/kazatu_image/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-image-boys` | «Имидж ұлдар» клубы / Клуб юношей «KATZU IMAGE» | Мәдениет | https://www.instagram.com/kazatu_image/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-unity` | «Unity» клубы / Unity club | Еріктілер | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-green-growth` | «Green growth» клубы / Клуб «Green growth» | Еріктілер | https://www.instagram.com/green_growth_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-cyber` | «Cyber KazATZU» клубы / Клуб «Cyber KATZU» | Спорт | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-positive` | «Positive» психологиялық клубы / Психологический клуб «POSITIVE» | Басқа | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-aqzhelken` | «Ақжелкен» студенттік театры / Студенческий театр «Ақ желкен» | Өнер | https://www.instagram.com/Aqjelken_theatre/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-shattyq` | «Шаттық» би ансамблі / Танцевальный ансамбль «Шаттық» | Өнер | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-shabyt` | «Шабыт» ұлт-аспаптар оркестрі / Ансамбль национальных инструментов «Шабыт» | Өнер | https://www.instagram.com/shabyt_kazatzu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-nazqonyr` | «Назқоңыр» вокал үйірмесі / Вокальный кружок «Назконыр» | Өнер | https://www.instagram.com/vocal_kazatzuu/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-ktk` | «КТК» көңілді тапқырлар клубы / Клуб весёлых и находчивых «КТК» | КВН | https://www.instagram.com/Seifullin_quramasy/ | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-dostyq` | «Достық» клубы / Клуб «Достық» | Мәдениет | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-saryarqa` | «Сарыарқа» студенттік танымдық клубы / Студенческий познавательный клуб «Сарыарқа» | Мәдениет | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-miras` | «Мирас» тарихи-танымдық клубы / Историко-познавательный клуб «Мирас» | Мәдениет | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-sakentanu` | «Сәкентану» клубы / Клуб «Сакентану» | Мәдениет | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-demography` | «Демографиялық даму зертханасы» студенттік қоғамдастығы / Студенческое сообщество «Лаборатория демографического развития» | Ғылым | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-german` | Неміс тілінің әуесқой клубы / Клуб любителей немецкого языка | Тілдер | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-french` | Француз тілі әуесқойларының клубы / Клуб любителей французского языка | Тілдер | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-english` | English speaking club / English speaking club | Тілдер | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-literary` | «Литературная гостиная» әдеби-шығармашылық клубы / Литературно-творческий клуб «Литературная гостиная» | Мәдениет | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-birlik` | «Бірлік» клубы / Клуб «Бірлік» | Дебат | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-togyzqumalaq` | Тоғызқұмалақ лигасы / Лига тоғызқұмалақ | Спорт | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-chess` | Шахмат лигасы / Шахматная лига | Спорт | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-basketball` | Баскетбол лигасы / Баскетбольная лига | Спорт | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-volleyball` | Волейбол лигасы / Волейбольная лига | Спорт | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-futsal` | Футбол (футзал) лигасы / Футбольная лига (футзал) | Спорт | — | [studenceskie-kluby](https://kazatu.edu.kz/ru/page/studenceskie-kluby) | 2026-11-05 |
| `kazatu-sport-club` | Спорт клубы / Спортивный клуб КазАТУ | Спорт | — | [sportivnyj-klub](https://kazatu.edu.kz/ru/pages/universitet/studenceskaa-zizn/sportivnyj-klub) | 2026-11-05 |
| `kazatu-python` | «Python тілінде ғылыми-техникалық есептерді шығару» үйірмесі / Кружок «Использование Python для решения научно-технических задач» | IT | — | [kruzok-ispolzovanie-python-dla-resenia-naucno-tehniceskih-zadac](https://kazatu.edu.kz/ru/page/kruzok-ispolzovanie-python-dla-resenia-naucno-tehniceskih-zadac) | 2026-11-05 |
| `kazatu-arduino` | «ARDUINO платформасында цифрлық құрылғыларды жобалау» үйірмесі / Кружок «Проектирование на платформе Arduino цифровых устройств» | IT | — | [kruzok-proektirovanie-na-platforme-arduino-cifrovyh-ustrojstv](https://kazatu.edu.kz/ru/page/kruzok-proektirovanie-na-platforme-arduino-cifrovyh-ustrojstv) | 2026-11-05 |
| `kazatu-formula-student` | «Formula Student» ғылыми-техникалық үйірмесі / Научно-технический кружок «Formula Student» | Ғылым | — | [naucno-tehniceskij-kruzok-formula-student](https://kazatu.edu.kz/ru/page/naucno-tehniceskij-kruzok-formula-student) | 2026-11-05 |

## Тексерілмегені / енгізілмегені

1. **Корпус, аудитория, кездесу кестесі** ешбір ресми бетте жазылмаған (тек спорт клубының 2022–23 оқу жылының
   ескі PDF кестесі бар) — бұл өрістер бос, оларды клуб жетекшісі толтырады.
2. **Telegram**: ешбір клубтың Telegram арнасы ресми беттерде көрсетілмеген (тек университеттің жалпы арнасы
   `t.me/KazATIU`) — `telegram_url` бос.
3. **Instagram** сілтемелері ресми беттегі атаулардан құрастырылды; профильдердің өзі ашылып тексерілмеді.
   `@kazatzu.komek`, `@sanaly.urpaq_kazatzu`, `@green_growth_kazatzu` — еріктілер бетіне ендірілген посттар арқылы
   расталды. Жарамсыз атаулар енгізілмеді: «@shattyq bi ansambli» (бос орын бар), `@Kazatutеch` (кирилл «е»).
4. Тек 2023 жылғы жаңалық тақырыптарында аталған, клубтар бетінде профилі жоқ клубтар енгізілмеді (белсенді
   болмауы мүмкін): «Көшбасшы», «ART KATZU» сурет клубы, «Сымбат» қыздар клубы.
5. Ресми сайтта жеке бет ретінде табылмағандар: бизнес/стартап клубы, бөлек еріктілер орталығы, СҒҚ (студенттік
   ғылыми қоғам) беті.
6. «Ғылыми үйірмелер» бетіндегі ~50 факультеттік үйірме (агрономия, энергетика, ветеринария, экономика т.б.)
   енгізілмеді: тізім ескі факультет атауларымен топталған (университет қазір институттарға көшкен), жеке беттері
   ашылмады. Тек жеке беті бар үш үйірме (Python, Arduino, Formula Student) енгізілді.
7. Жатақхана студенттік кеңестері (клуб емес) және 2016 жылғы КВН командаларының атаулары енгізілмеді.
8. Клубтар бетінде күн жоқ; ЖІК рейтингтері 2024 қазан/қараша, «Сарыарқа» клубы 2024 қазанда басталғаны жазылған —
   бет кемінде сол кезде жаңартылған. «Бірлік» клубы әлі «экономика факультеті» деп жазылған, ескірген болуы мүмкін.
9. Шығармашылық клубтар беті мен мәдени-көпшілік бөлім бетінде театр, вокал және КВН жетекшілері әртүрлі
   көрсетілген — сондықтан тек жалпы «Жетекші» рөлі қойылды.
10. «Литературная гостиная» клубының қазақша атауы ресми бетте де орысша — солай қалдырылды.
