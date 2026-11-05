# Қауымдастық: табылған заттар, сұрақ-жауап, пәтерлес

Код: `backend/internal/community`, миграция `backend/migrations/20261105120041_community.sql`,
фронт: `frontend/src/components/views/{lostfound,qa,roommates,community-ui}.tsx`, `frontend/src/lib/community.ts`.
Әр функцияның өз ажыратқышы бар, бәрі әдепкі бойынша **өшірулі**; өшірулі функцияның API-і белгісіз
маршрутпен бірдей 404 қайтарады, фронтта мәзір пункті мен бет жоқ.

| Ажыратқыш | Функция | Бет |
| --- | --- | --- |
| `FEATURE_LOSTFOUND=true` | «Табылған заттар» | `/lostfound`, `?id=new:lost`, `?id=new:found`, `?id=mine`, `?id=<n>` |
| `FEATURE_COURSEQA=true` | «Пән бойынша сұрақ-жауап» | `/qa`, `?id=s:<пән>`, `?id=new[:<пән>]`, `?id=<n>` |
| `FEATURE_ROOMMATES=true` | «Пәтерлес» | `/roommates`, `?id=new`, `?id=requests`, `?id=<n>` |

## Қосу

1. Сервердің env файлына керегін қос: `FEATURE_LOSTFOUND=true`, `FEATURE_COURSEQA=true`, `FEATURE_ROOMMATES=true`.
2. Миграция (`cd backend && go run ./cmd/migrate`) — `docker/` deploy-ы оны өзі қолданады.
3. API-ді қайта іске қос. `GET /api/config` → `features.lostfound/courseqa/roommates: true`.
4. Ұпай беру үшін `FEATURE_POINTS=true` та керек (өшік болса әрекет жүреді, ұпай берілмейді).
5. 30 күндік мұрағат күнделікті `/api/cron` ішінде жүреді (`cronrun.OnDaily`) — жаңа timer керек емес.

Қосымша шектер (env, әдепкі): `LOSTFOUND_DAILY_POSTS` 5, `LOSTFOUND_DAILY_CLAIMS` 10, `QA_DAILY_QUESTIONS` 5,
`QA_DAILY_ANSWERS` 30, `ROOMMATES_DAILY_REQUESTS` 10, `ROOMMATES_DAILY_LISTINGS` 3, шағым шегі `REPORT_HIDE_AFTER` 3.
Бәрі Алматы күні бойынша; әр жазу қосымша `auth.Write` (IP + қолданушы) шегінен өтеді.

## Ортақ ережелер

- **Жазу — тек кірген, келісім берген аккаунт.** Табылған заттар мен пәтерлесте профильдегі университет те керек.
  Оқу: табылған заттар мен сұрақ-жауап — кірмей-ақ; пәтерлес — тек кірген (жыныс ережесі көрушіге байланысты).
- **Байланыс тек келісіммен.** Бірде-бір кестеде телефон, @username, email, сілтеме жоқ. Байланыс `users`-та
  ғана, `app_user` тек өз жолын оқиды. Сервер оны тек расталған өтінімнің / қабылданған сұраудың екі жағына
  береді (`Contact{name, username, telegramUrl, email}`; email тек расталған болса). Ашылу үшін әр тарапта
  Telegram username не расталған email болуы керек (`403 no_contact`).
- **Мәтіндегі байланыс тыйым салынған** (`400 has_contact`): табылған заттар мен пәтерлесте телефон (7+ цифр,
  бос орын/сызықшасыз), `@handle`, email, `t.me/`, `wa.me/`, `http(s)://`, `www.`, `x.kz` секілді домен.
  Сұрақ-жауапта тек телефон мен `@handle` (оқу сілтемесіне рұқсат). Ереже Bagdar-дың `has_contacts`-ынан.
- **Ұзындық шектері** — әр өріс серверде де, Postgres CHECK-те де (төмендегі кестелерде).
- **Шағым** — ортақ `POST /api/reports {kind, id, reason}`: `lostfound_post`, `qa_question`, `qa_answer`,
  `roommate_listing`. 7 күннен асқан `REPORT_HIDE_AFTER` аккаунт шағымданса — жазба жасырылады және ортақ
  кезекке (`/admin/queue`) `community_hidden` болып түседі; онда **нақты автор** көрінеді (аноним сұрақ та).
  Бекіту = жасырын қалады (`hidden_reason='admin'`), қайтару = шағымдар өшіп, жазба қайта көрінеді.
  `/admin/reports` тізімінде де бар. AI модерация жоқ (Bagdar себебі: «сауда ма» классификаторы жалдауға жарамайды).
- **Әкімші**: `POST /api/admin/{lostfound|qa|roommates}/moderate {kind?, id, hidden, reason}` (audit_log).
- **Аккаунт өшіру**: барлық сілтеме `on delete cascade`; табылған зат фото файлдары `me.RegisterDeleteHook`
  арқылы өшеді. Экспортта: `lostfoundPosts/Photos/Claims`, `qaQuestions/Answers/Votes`, `roommateGender/Listings/Requests`.
- **Хабарлама** — `notify` `community` санаты (Telegram + сайттағы кіріс), сілтемесімен.

## Табылған заттар

Пост авторын **ешкімге** атамайды (әкімші ғана базадан көреді). Санаттар: `documents, cards, keys, phone,
electronics, bag, clothes, books, other`; `documents` пен `cards` — сезімтал:

- фото жоқ (`sensitive_photo`), 6+ цифр қатар жоқ (`has_number` — ЖСН, карта, билет нөмірі), табылғаны
  деканатқа не күзетке тапсырылады: `handedTo: dean|security` міндетті (`handed_required`); фронт ескертеді;
- барлық постта 6+ цифр қатар тыйым салынған (иесі нөмірді өзі айтады).

Өтінім (claim) — постқа жеке хабарлама (10–300 таңба, байланыссыз). «Таптым» постында иесі тек өзі білетін
белгіні жазады; «Жоғалттым» постында тапқан адам не тапқанын айтады. Хабарламаны тек пост авторы мен өтінім
иесі оқиды (RLS). Автор **растайды** → екеуі бір-бірінің байланысын көреді (бір постта бір расталған өтінім).
Екі жақ та «Зат иесіне қайтты» басса → өтінім `returned`, пост `resolved`, **тапқан адам** `lostfound_return`
ұпайын алады (20 XP + 5 монета, күніне 1, аптасына 2; бір өтінім бір рет).
Фото: `POST /api/lostfound/photos` → `market.Process` (қайта кодтау: EXIF/GPS жоқ), `$UPLOAD_DIR/lostfound`;
постқа қосылмаған фото тек жүктеушіге/әкімшіге көрінеді, 1 күннен соң өшеді. 30 күннен соң `archived`.

| Маршрут | Кім | Не |
| --- | --- | --- |
| `GET /api/lostfound/posts?kind=&category=&university=&q=&before=` | кез келген | белсенді посттар |
| `GET /api/lostfound/posts/{id}` | кез келген | пост; автор — `claims`, өтінім иесі — `myClaim` |
| `POST /api/lostfound/posts` | кірген | `{kind, category, title 3–80, desc ≤500, building, room ≤20, happenedOn (90 күн), photo, handedTo}` |
| `POST /api/lostfound/posts/{id}/close` | автор | жабу |
| `POST /api/lostfound/posts/{id}/claims` | кірген, автор емес | `{message}` |
| `POST /api/lostfound/claims/{id}/decide` | пост авторы | `{accept}` |
| `POST /api/lostfound/claims/{id}/returned` | екі тарап | қайтару белгісі |
| `GET /api/lostfound/mine` | кірген | менің посттарым мен өтінімдерім (RLS) |
| `POST /api/lostfound/photos`, `GET /api/lostfound/photos/{name}` | кірген / кез келген | фото |

## Пән бойынша сұрақ-жауап

Пәндер құрылғыда (`lib/subjects.ts`), сондықтан тақта кілті: **университет + `subject_key` + `faculty_key`**.
`subject_key` = пән атауы кіші әріппен, `ё→е`, тек әріп пен цифр, бір бос орын (`NormKey`; «Математика-1»,
«МАТЕМАТИКА 1» — бір тақта). `faculty_key` — сұрақ «тек менің факультетім» болса `users.faculty`-ның сол
нормасы, әйтпесе бос (бүкіл университет). Кірген студент әдепкіде өз университетінің бүкіл-университет пен
өз факультетінің сұрақтарын көреді (`faculty=all` — барлық факультет); кірмеген — бәрін. Топ (`points_wallets.study_group`)
кілтке қосылмаған: ол міндетті емес және жиі бос.

- Сұрақ: пән 2–80, тақырып 8–150, мәтін ≤2000, 5 тегке дейін (2–24 әріп/цифр), `honesty: true` міндетті
  (академиялық адалдық ережесі; емтихан жауаптарын сұрау тыйым салынған).
- **Аноним** — курстастардан жасырын (`author: null`), бірақ `user_id` сақталады: әкімші `authorId` көреді,
  кезекте нақты автор. Кірмеген оқырман ешкімнің атын көрмейді; кірген — «Аружан Т.» (тегі бір әріп).
- Жауап 2–2000; сұрақ иесіне хабар барады. Дауыс — тек «пайдалы» (өзіңізге емес, бір рет).
- «Шешілді»: тек сұрақ иесі белгілейді; жауап авторы `qa_accepted` (10 XP + 2 монета, күніне 3, аптасына 10)
  алады, өзіне жауап берсе — жоқ; басқа жауапқа ауысса, бұрынғы сыйақы қайтарылады.
- **Құлып**: әкімші `POST /api/admin/qa/questions/{id}/lock {locked, reason}` (мысалы, емтихан кезінде) —
  жаңа жауап `409 locked`, себебі бетте көрінеді.
- Сілтеме: мәзір («Оқу» → «Сұрақ-жауап») және пән беті («Курстастардан сұрау» → `/qa?id=s:<пән>`).

Маршруттар: `GET /api/qa/questions?subject=&faculty=&university=&tag=&q=&sort=new|open&before=`,
`GET /api/qa/questions/{id}`, `POST /api/qa/questions`, `DELETE /api/qa/questions/{id}`,
`POST /api/qa/questions/{id}/answers`, `DELETE /api/qa/answers/{id}`, `POST /api/qa/questions/{id}/accept {answerId|null}`,
`POST /api/qa/vote {kind, id, up}`, `POST /api/admin/qa/moderate`, `POST /api/admin/qa/questions/{id}/lock`.

## Пәтерлес

Bagdar `/paterles` ережелері (`~/bagdar/README.md`, «Пәтерлес»):

- Хабарландыруда автордың id-і, аты, username-і, байланысы **жоқ**. Түрі (`have_room`/`looking`), `dorm`/`flat`,
  аудан 2–60, бюджет 5 000–1 000 000 ₸, көшу күні (бүгіннен 6 ай), әдеттер (12-ден 8-ге дейін), өзі туралы ≤400.
- **«Тек өз жынысым» — қатаң.** Жыныс тек `roommate_genders`-та (профильде жоқ, иесі ғана оқиды). Мұндай
  хабарландыру басқа (не белгісіз) жыныстағы адамға тізімде көрінбейді, id-мен де, сұрауда да **404**.
  Белсенді қатаң хабарландыру тұрғанда жынысты өзгертуге болмайды. «Маған сай» тек көрушінің өз шартын қосады.
- Бір адам — бір белсенді хабарландыру (unique index), 30 күннен соң `closed`.
- «Қызығамын» сұрауында мәтін жоқ; иесі сұраушының курсын, университетін, хабарландыруының түйінін көреді.
  Иесі «Келісемін» бассa ғана екеуіне байланыс ашылады. Күніне 10 сұрау.
- «Маған сай»: менің хабарландыруыммен бір тұрғын түрі, бюджет қиылысады, `have_room` → тек `looking`;
  ортақ әдет саны, сосын көшу күнінің жақындығы бойынша сұрыпталады.

Маршруттар: `GET /api/roommates/me`, `POST /api/roommates/gender {gender: f|m|null}`,
`GET /api/roommates/listings?kind=&housing=&budgetMax=&match=1&university=&before=`, `GET /api/roommates/listings/{id}`,
`POST /api/roommates/listings`, `POST /api/roommates/listings/{id}/close`, `POST /api/roommates/listings/{id}/requests`,
`POST /api/roommates/requests/{id}/decide {accept}`, `GET /api/roommates/requests`, `POST /api/admin/roommates/moderate`.

## RLS және тесттер

`app_user`-ге тек SELECT: посттар/сұрақтар/жауаптар — белсенді не өзімдікі; өтінімдер мен пәтер сұраулары —
екі тарапқа ғана; пәтер хабарландырулары мен жыныс — тек өзіме; дауыстар — өзімдікі. Жазу құқығы жоқ.
`backend/internal/community/community_test.go`: RLS, келісімге дейін/кейін байланыс, үшінші адам, лимиттер,
сезімтал құжат ережелері, EXIF өшуі, аноним/әкімші, құлып, ұпай мен оны қайтару, шағым → кезек → қалпына келтіру,
қатаң жыныс, сәйкестік реті, 30 күндік мұрағат. Браузер: `frontend/e2e/community.spec.ts`.
