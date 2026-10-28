# Аналитика (жеке деректерсіз)

## Неге PostHog емес, өз Postgres-іміз
ТЗ PostHog немесе Vercel Web Analytics деп ұсынған. Таңдалғаны — **өз бэкендіміз бен Postgres**:
- Бэкенд пен SQL бәрібір өзімізде (VPS), сыртқы сервис, аккаунт, кілт керек емес.
- Деректер серверден шықпайды — студенттерге «бөтен компанияға ештеңе кетпейді» деп адал айта аламыз.
- Vercel Web Analytics-тің тегін тарифінде өз оқиғалар (custom events) мен retention есебі жоқ, ал бізге дәл сол керек.
- PostHog-ты кейін қосу оңай: клиентте `features/analytics/track.ts` бір ғана жер.

## Не жиналады
Құрылғыға кездейсоқ id (`studenthub.anon`, localStorage) + оқиға атауы + бірнеше санат-өріс. Сервер (`pkg/analytics`) әр оқиғаны тізіммен тексереді, тізімнен тыс өріс үнсіз тасталады.

| Оқиға | Өрістері |
|---|---|
| `visit` | — (тәулігіне бір рет) |
| `onboarding_complete` | `faculty` (мамандық кілті), `course` (1–6), `city` (қала кілті) |
| `opportunity_view`, `job_view` | `id` |
| `share_click` | `kind`, `id`, `channel` |
| `deadline_saved` | `id` |
| `ai_message_sent` | — (тек саны; мәтін ЕШҚАШАН жіберілмейді) |
| `roadmap_item_done` | `item` |
| `telegram_open` | `in_telegram` |
| `feedback` | `score` (1–5), `missing` (≤500 таңба, міндетті емес) |

**Ешқашан жиналмайды:** аты, GPA, телефон, email, AI-чат мәтіні. Тест (`pkg/analytics/analytics_test.go`) соны тексереді: аты/GPA/телефон/чат мәтіні жіберілсе де, базаға түспейді.

## Бөлісу және амбассадорлар
- «Бөлісу» сілтемесіне `?ref=share` қосылады. Бірінші кірісте ол есте сақталады да, URL-ден бірден тазаланады.
- Амбассадор сілтемесі: `https://<сайт>/?ref=amb_<код>` (мысалы `?ref=amb_aru`). Код — латын әріптері, сандар, `-`, `_`, 30 таңбаға дейін.

## Ең маңызды 3 көрсеткіш
Админ ретінде (Telegram арқылы кірген, рөлі `admin`): `GET /api/admin/analytics?days=30` немесе сайттағы `/admin` беті.

1. **Келесі күні қайта оралғандар (day 1):** `returned_day1 / eligible_day1`. Бірінші кірістен кейінгі күні кем дегенде бір рет ашқандар. `eligible_*` — сол күнге жетіп үлгерген құрылғылар ғана (бүгін келгендер әлі есептелмейді).
2. **7-күні қайта оралғандар (day 7):** `returned_day7 / eligible_day7`. Бірінші кірістен дәл 7 күн өткенде ашқандар.
3. **Бірінші мүмкіндікті сақтағандар үлесі:** `saved_first / devices`. Кем дегенде бір мерзімді «Сақтау және бақылау» басқандар.

Қосымша: `from_share` (досынан келгендер), `ambassadors` (код → әкелген құрылғылар), `feedback_avg`, `feedback_recent` (соңғы «не жетіспейді» жауаптары).

SQL-мен тікелей қарау (сервердегі Postgres):
```sql
select first_day, count(*) from analytics_devices group by 1 order by 1 desc limit 14;
select ref, count(*) from analytics_devices where ref is not null group by 1 order by 2 desc;
```
