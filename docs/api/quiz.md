# Quiz API — Quizizz-style quizzes, AI from notes, live games

Server: `pkg/quiz`, wired in `pkg/server/routes_quiz.go`. Schema: `supabase/migrations/20261028000019_quiz.sql`.
Ported from the owner's quiz module in `my-new_project` (handlers/services/repository/hub), rewritten for
this repo (stdlib ServeMux, pgx, plain SQL, RLS via `db.WithUser`). The teacher "classroom / assign quiz"
part is **not** ported. Not ported question types: `drawing`, `voice_response`, `video` (they store
student recordings / external video).

## Conventions

- Switch `FEATURE_QUIZ` (off → every route below, WebSocket included, is `404 not_found`; `/api/config`
  shows `features.quiz`).
- Session cookie or `Authorization: Bearer` (Mini App) as everywhere. Every POST/PUT/DELETE goes through
  `auth.Write`: allowed `Origin`, shared per-IP/per-user write budget.
- Errors: `{"error": {"code", "message"}}`, message in kk/ru (`?lang=` / `Accept-Language`). Branch on `code`.
- Writing quizzes, uploads and AI need the consent screen accepted (`403 consent_required`).
- IDs are numbers. Media URLs are relative (`/api/quiz/media/<32 hex>.jpg|.mp3`) and go through the
  `/api/*` rewrite like every other call.

## Question model

One JSON shape is used by the editor (`POST/PUT`), Excel import and the owner's `GET`. Fields not used by a
type are omitted.

```json
{
  "id": 41,                       // existing question (PUT keeps its stats); omit for new ones
  "type": "mcq",
  "prompt": "2+2?",               // ≤1000 chars, newlines allowed
  "options": ["3", "4", "5"],
  "correctIndex": 1,
  "explanation": "…",              // shown in results / after the live reveal
  "sourceQuote": "…",              // AI only: verbatim support from the notes
  "image": "<32hex>.jpg",          // from POST /api/quiz/media (kind=image)
  "audio": "<32hex>.mp3",          // from POST /api/quiz/media (kind=audio)
  "timeLimit": 20                  // seconds 5–300; 0/omitted = quiz timePerQuestion
}
```

| `type` | Editor fields (answer key in **bold**) | Player answer (`answer`) | Scoring |
|---|---|---|---|
| `mcq` | `options` 2–6, **`correctIndex`** | index `1` | exact |
| `mcq_multi` | `options` 3–6, **`correctIndexes`** `[0,2]` | `[0,2]` (order/dupes ignored) | exact set |
| `true_false` | **`correctBool`** | `true` | exact |
| `fill_blank` | **`answers`** 1–5 accepted strings | `"су"` | case, spaces, trailing `.!?;:,` ignored |
| `reorder` | **`items`** 2–10 in the correct order | `["a","b","c"]` (texts) | exact order |
| `matching` | **`pairs`** 2–8 `{left,right}` | `{"left":"right",…}` | all pairs |
| `categorization` | **`pairs`** 2–12 `{left:item,right:category}`, 2–4 categories | `{"item":"category",…}` | all items |
| `dropdown` | `options` 2–6, **`correctIndex`** | index | exact |
| `poll` | `options` 2–6 | index | not graded (no points, not in max score) |
| `hotspot` | `image` required, `zones` 1–10 `{id,x,y,w,h}` (percent), **`correctZone`** | `"zoneId"` | exact |
| `labeling` | `image` required, `zones` 2–10 with **`label`** | `{"zoneId":"label"}` | all labels, case-insensitive |
| `comprehension` | `passage` ≤5000, `subQuestions` 1–10 `{id,type: mcq/true_false/fill_blank,prompt,options,`**`correctIndex/correctBool/answers`**`}` | `{"subId": value}` | **partial**: share of right sub-answers |

Validation errors: `400 invalid_quiz` with `"question": <1-based index>` and `"reason"`
(`bad_type`, `bad_prompt`, `bad_options`, `bad_correct`, `bad_answers`, `bad_items`, `bad_pairs`,
`bad_categories`, `image_required`, `bad_zones`, `bad_passage`, `bad_sub_questions`, `bad_image`,
`bad_audio`, `bad_time_limit`, `bad_explanation`, `bad_source_quote`) — highlight that question.
Other: `bad_title`, `no_questions`, `too_many` (>200), `bad_media` (image/audio not yours or used by another quiz).

**Play view** (what players get, `PlayQuestion`): `{id, position, type, prompt, imageUrl, audioUrl, timeLimit,
options, multi, items, lefts, rights, categories, zones, labels, passage, subQuestions}` — never correct
answers or explanations. `reorder` items, `matching` rights, `categorization` items and `labeling` labels
are pre-shuffled; options keep their order (answers are indexes) — shuffle them **visually** on the client
when `quiz.shuffle` is true and map back to the original index.

## Quiz header (`Meta`)

```json
{"id": 7, "title": "…", "description": "", "subject": "Биология", "visibility": "public|unlisted|private",
 "isDraft": false, "shareCode": "Ab3dE5fG7h", "timePerQuestion": 30, "shuffle": true,
 "source": "manual|excel|ai_text|ai_files|clone|cards", "questionCount": 12, "plays": 40, "version": 3,
 "createdAt": "…", "updatedAt": "…", "isMine": true, "hidden": false}
```

`shareCode` is present for the owner and for public/unlisted quizzes. Drafts (`isDraft`) and private
quizzes are visible to the owner only.

## Endpoints

| Method & path | Who | Purpose |
|---|---|---|
| `GET /api/quiz/quizzes?q=&subject=&sort=new\|popular&page=` | anyone | Public library (20/page, `hasMore`) |
| `GET /api/quiz/mine` | signed in | My quizzes incl. drafts |
| `POST /api/quiz/quizzes[?source=excel\|cards]` | consented | Create → `201 {id, quiz}` |
| `GET /api/quiz/quizzes/{id}` | owner | Editor view `{quiz, questions}` with keys |
| `PUT /api/quiz/quizzes/{id}` | owner | Replace (send `version` you loaded → `409 version_conflict` if stale). `isDraft:false` publishes a draft |
| `DELETE /api/quiz/quizzes/{id}` | owner | Delete quiz, attempts, media files; open live games end |
| `GET /api/quiz/quizzes/{id}/play` | anyone for published public; owner always | Play view `{quiz, questions}` |
| `POST /api/quiz/quizzes/{id}/attempts` | anyone who can play (guests too) | Submit → graded result |
| `GET /api/quiz/quizzes/{id}/stats` | owner | Per-question stats |
| `POST /api/quiz/quizzes/{id}/clone` | consented, quiz playable | Private copy (media copied) |
| `POST /api/quiz/quizzes/{id}/share/reset` | owner | New share code, old link dies |
| `GET /api/quiz/s/{code}` | anyone (public/unlisted, published) | Play view by share link |
| `GET /api/quiz/s/{code}/meta` | anyone (public/unlisted, published) | Preview data for `generateMetadata` |
| `POST /api/quiz/s/{code}/clone` | consented | Clone via share link |
| `GET /api/quiz/attempts?quizId=` | signed in | My history (newest first, ≤200) |
| `GET /api/quiz/attempts/{id}` | own attempt | Stored result |
| `GET /api/quiz/attempts/{id}/retry` | own attempt | Play view of the wrong questions only («қате жауаптарды қайталау») |
| `POST /api/quiz/media?kind=image\|audio` | consented | Upload (multipart field `file`, or raw body) |
| `GET /api/quiz/media/{name}` | see below | Media bytes |
| `POST /api/quiz/import/xlsx` | signed in | Parse Excel → questions for the editor (nothing saved) |
| `GET /api/quiz/import/template.xlsx` | anyone | Import template |
| `GET /api/quiz/generate/usage` | signed in | AI budget `{used, limit, textCost, photoCost, enabled}` |
| `POST /api/quiz/generate/from-text` | consented | AI draft quiz from pasted text |
| `POST /api/quiz/generate/from-files` | consented | AI draft quiz from lecture-note files |
| `POST /api/quiz/generate/cards-from-files` | consented | AI flashcards from files (not stored) |
| `POST /api/quiz/live/sessions` | consented | Host a live game |
| `GET /api/quiz/live/sessions/{code}` | anyone | Join-screen check |
| `POST /api/quiz/live/sessions/{code}/host-token` | the host | Fresh host token |
| `POST /api/quiz/live/join` | anyone (session optional) | Join with nickname → WS token |
| `GET /api/quiz/live/ws?token=` | token holder, allowed Origin | Game WebSocket |
| `GET /api/quiz/live/history` | signed in | Games I hosted, with results |
| `POST /api/admin/quiz/quizzes/{id}/hide` · `/restore` | admin | Take a public quiz out of the library (audited) |

### Attempts (server-side scoring)

`POST /api/quiz/quizzes/{id}/attempts`

```json
{"answers": [{"questionId": 41, "answer": 1, "timeSpent": 7}],
 "questionIds": [41, 42],     // optional subset; default = all questions
 "retryOf": 123,              // optional: retry the wrong answers of attempt 123 (questionIds default to them)
 "share": "Ab3dE5fG7h"}       // needed for unlisted quizzes
```

Only `questionId`/`answer`/`timeSpent` are read. Any client "score", "isCorrect", "points" is ignored;
grading uses the stored answer keys. Unanswered questions count as wrong. Unknown question ids →
`400 bad_answers`.

`201` result (also `GET /api/quiz/attempts/{id}`):

```json
{"attemptId": 9, "quizId": 7, "quizTitle": "…", "mode": "practice|retry", "retryOf": null,
 "score": 3.5, "maxScore": 5, "percentage": 70, "correctCount": 3, "total": 6, "wrongCount": 2,
 "timeSpent": 64, "createdAt": "…",
 "answers": [{"position": 1, "questionId": 41, "type": "mcq", "prompt": "…", "view": {…play view…},
   "imageUrl": "", "explanation": "…", "sourceQuote": "", "given": 0, "correct": 1,
   "credit": 0, "isCorrect": false, "graded": true, "timeSpent": 7}]}
```

`score` is fractional only because of comprehension partial credit. Guests get `attemptId: null` (nothing
stored; show "sign in to keep results"). Answers keep a snapshot of the question, so old results render
after edits. Retry flow: result screen → «Қате жауаптарды қайталау» → `GET /attempts/{id}/retry` →
play → submit with `retryOf` (`409 nothing_to_retry` when everything was right).

`GET /api/quiz/quizzes/{id}/stats` → `{attempts, players, avgPercentage, questions: [{questionId, position,
type, prompt, answered, correct, accuracy, avgTimeSpent, skipped, distribution}]}`; `distribution` (for
mcq/dropdown/poll/true_false/hotspot) maps the JSON-encoded answer (`"0"`, `"true"`, `"\"h\""`) to a count.
Only first-try practice attempts count in `attempts`/`avgPercentage`; answers include retries.

### Sharing

- `visibility`: `public` (library + link), `unlisted` (link only), `private` (owner only). Drafts are never shared.
- Share URL: `${SITE_URL}/quiz/s/{shareCode}` (frontend route). Its page reads `GET /api/quiz/s/{code}`.
- `GET /api/quiz/s/{code}/meta` (cached 5 min, no author data):
  `{title, description, subject, questionCount, plays, imageUrl, url}` → use in `generateMetadata`
  (`imageUrl` = first question image or empty → fall back to the site OG image).
- Clone: `POST /api/quiz/s/{code}/clone` or `/quizzes/{id}/clone` → `201 {id, quiz}`, private copy titled
  "… (көшірме)".

### Media

`POST /api/quiz/media?kind=image` — JPEG/PNG/WebP by magic bytes (not name/Content-Type), ≤900 KB
(`QUIZ_IMAGE_MAX_KB`), ≤16 MP; re-encoded to JPEG ≤1280 px, which strips EXIF/GPS/XMP. HEIC →
`415 file_heic`. `kind=audio` — MP3 only, ≤900 KB (`QUIZ_AUDIO_MAX_KB`; nginx's 1 MB body cap), ID3 tags
removed, frames checked. `201 {name, url, kind, width?, height?, bytes}`; put `name` into the question.
Errors: `413 media_too_large`, `415 media_type`, `400 media_dimensions|media_invalid`, 60 uploads/hour.
Unattached uploads are deleted after 24 h (background sweep, no cron). Serving: public for published
public/unlisted quizzes and quizzes in an open live game, otherwise uploader/admin only.

### Excel import

`POST /api/quiz/import/xlsx` (multipart `file`, ≤2 MB, `.xlsx` only) →
`{questions: [Question…], warnings: [{row, code}], imported, skipped}`; `422 xlsx_empty` when nothing
usable, `400 xlsx_invalid` otherwise. Nothing is saved: load the questions into the editor, then
`POST /api/quiz/quizzes?source=excel`.

Columns are found by header (kk/ru/en words; Quizizz exports work): Question · Type · Option A…F (or
Option 1…5) · Correct · Explanation · Time. Without a header row the template order is assumed.
Types: `mcq` (default), `mcq_multi`, `true_false`, `fill_blank`, `reorder`, `poll`. Correct: letter
`a–f`/`а–е`, number `1–6`, option text, or a `*`/`✓`/`(correct)` mark on the option; multi: `a,c`;
true/false: `true/false/иә/жоқ/да/нет`; fill: answers separated by `|`; reorder: options in the right order.

## AI generation

Env as the chat: `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `OPENAI_MODEL`, plus `OPENAI_VISION_MODEL` for photos
(default `OPENAI_MODEL`). Prompts are server-side. **Nothing about the student is sent** (no name, id,
university); only the notes and the options. Limits per student: `QUIZ_AI_PER_MIN` (3) requests/minute →
`429 ai_minute` (with `Retry-After`); daily budget `QUIZ_AI_DAILY_UNITS` (12) per Almaty day → `429 ai_daily`.
A generation costs **1 unit + `QUIZ_AI_VISION_UNITS` (2) per photo**. Failed generations are refunded.
`503 ai_off` without a key.

Options (both endpoints): `count` 1–20 (UI: 5/10/20; cards 1–30), `difficulty` `easy|medium|hard`,
`types` subset of `mcq,mcq_multi,true_false`, `lang` `kk|ru` (default: UI language), `title` (optional).

The model must return strict JSON; every question carries a verbatim `source_quote`. The server checks
each quote really occurs in the notes (case, whitespace, quote marks and dashes folded; ≥12 chars) and
drops questions that fail, are malformed or duplicate. If fewer than 80% of `count` survive, it asks once
more for the rest. Unparseable output or zero valid questions → `502 ai_upstream`.

**`POST /api/quiz/generate/from-text`** `{"text": "…", "count": 10, "difficulty": "medium", "types": ["mcq"], "lang": "kk"}`
— text 200–60 000 chars (`422 text_too_short`, `413 text_too_long`).

**`POST /api/quiz/generate/from-files`** multipart: `files` (repeat, ≤5) + fields `count`, `difficulty`,
`types` (comma list), `lang`, `title`.
- Accepted by content: PDF (text layer, `ledongthuc/pdf`), DOCX, PPTX (zip XML, stdlib), UTF-8 text,
  photos JPEG/PNG/WebP (incl. handwriting → vision model transcribes faithfully, one call per photo).
- Limits: ≤10 MB per file (`413 file_too_large`), ≤5 files (`400 too_many_files`), ≤30 pages total
  (PDF pages, slides, photos; DOCX/TXT count 3000 chars as a page) (`400 too_many_pages`); text sent
  to the model is cut at 60 000 chars (`truncated: true` → show "конспект ұзын, басы ғана қолданылды").
- Rejected: anything else by magic bytes (`415 file_type`), HEIC (`415 file_heic` — "save as JPEG"),
  zip bombs / >2000 entries / broken files (`413 file_too_large` / `400 file_invalid`), PDFs without a
  text layer (`422 pdf_scanned` — "looks scanned, upload the pages as photos"), no text (`422 no_text`).
- Privacy: files are streamed in memory (no temp files), never stored; extracted text is discarded after
  the call. Only the resulting quiz is saved.

Both return `201 {id, quiz, generated, dropped, truncated, files?: [{name, kind, pages, chars}]}` — the
quiz is a **private draft** (`isDraft: true`, `source: ai_text|ai_files`) with `sourceQuote` on every
question. Review it with `GET /api/quiz/quizzes/{id}`, then `PUT` with `isDraft: false`.

**`POST /api/quiz/generate/cards-from-files`** — same form, `count` 1–30 →
`200 {cards: [{q, a, sourceQuote}], dropped, truncated, files}`. Nothing stored: the existing cards tool
keeps decks in the browser (`Deck.cards` are `{q, a}`), so append these to a new local deck.

### Integration with the «Карточкалар және тест» tool

The AI tool (`components/views/ai.tsx`, `cards`) can turn the same text into a quiz with
`POST /api/quiz/generate/from-text` (it creates a private draft owned by the student and returns its id —
open `/quiz/edit/{id}` for review or `/quiz/play/{id}` directly; the owner can play drafts). A local deck
can also become a quiz without AI: `POST /api/quiz/quizzes?source=cards` with one `fill_blank` question per
card (`prompt: card.q, answers: [card.a]`) or `mcq` built by the client. For files, the tool can call
`cards-from-files` (flashcards) or `from-files` (quiz) with the same upload.

## Live games (Kahoot-style)

Vercel rewrites do not proxy WebSockets, so the browser connects **directly** to the API origin:
`wss://studenthub.85-137-91-47.sslip.io/api/quiz/live/ws?token=…` (the response's `wsUrl`;
set `QUIZ_LIVE_WS_URL` in prod). Cookies are not sent there, so the socket uses a **join token**: a signed,
2-minute token from the HTTPS endpoints below (fetch a new one for each (re)connect). The handshake also
requires an `Origin` from `ALLOWED_HOSTS` (`403 bad_origin`), an unexpired token (`401 bad_token`) and an
open game (`404 live_not_found`).

1. Host: `POST /api/quiz/live/sessions {quizId, share?}` → `201 {code: "482913", sessionId, hostToken,
   expiresAt, wsUrl, joinUrl, questionCount, title}`. Own quiz or any quiz the host can play. ≤2 open games
   per host, ≤`QUIZ_LIVE_MAX_SESSIONS` (50) in total (`503 live_busy`).
   Reconnect: `POST /api/quiz/live/sessions/{code}/host-token`.
2. Join screen: `GET /api/quiz/live/sessions/{code}` → `{code, status, title, questionCount, players, maxPlayers}`.
3. Player: `POST /api/quiz/live/join {code, nickname}` → `{playerId, nickname, rejoinKey, token, expiresAt,
   wsUrl, status, title}`. Nickname 2–24 chars (`400 bad_nickname`, `409 nickname_taken`), ≤`QUIZ_LIVE_MAX_PLAYERS`
   (100) (`409 live_full`), `QUIZ_LIVE_JOINS_PER_MIN` (20)/IP. Signing in is optional (if signed in, the final
   result is linked to the account). Keep `rejoinKey` in `sessionStorage`; after a drop call `join` with
   `{code, rejoinKey}` to get a new token for the same player (score kept). Late joiners start at 0.

### WebSocket protocol

All frames are JSON text `{"type": "...", "data": {...}}` (client → server: `{"type", ...fields}`).

Client → server:

| type | who | fields | effect |
|---|---|---|---|
| `start` | host | — | lobby → first question (needs ≥1 player, else `error no_players`) |
| `next` | host | — | during a question: reveal now; after a reveal: next question or end |
| `end` | host | — | end the game now |
| `kick` | host | `playerId` | remove a player (they get `kicked`, cannot rejoin) |
| `answer` | player | `index`, `answer` (same shapes as attempts) | answer the running question once |
| `ping` | any | — | `pong {serverTime}` (use for clock offset) |

Server → client:

| type | to | data |
|---|---|---|
| `hello` | the new socket | `{code, status: lobby\|question\|reveal\|finished, title, total, index, role, you?: {id, nickname, score, rank, answered}}` |
| `lobby` | all | `{players: [{id, nickname, online}], count, max}` — on join/leave/kick |
| `question` | all | `{index, total, question: PlayQuestion, deadline (unix ms), serverTime (unix ms), timeLimitMs}` |
| `answer_ack` | player | `{index}` — accepted (correctness only at reveal) |
| `answer_rejected` | player | `{index, reason: late\|duplicate\|not_active\|bad_answer}` |
| `progress` | host | `{index, answered, total}` |
| `reveal` | host | `{index, correct, explanation, answered, correctCount, distribution, leaderboard, last}` |
| `reveal` | player | `{index, correct, explanation, leaderboard, last, you: {answered, credit, isCorrect, points, score, rank, streak}}` |
| `ended` | all | `{reason: completed\|host\|idle\|closed, leaderboard (top 50), you?: {rank, score, correct}}` |
| `kicked` | player | — (socket closes) |
| `error` | sender | `{code: rate_limited\|bad_message\|unknown_type\|no_players}` |

`leaderboard` rows: `{rank, id, nickname, score, correct}` (top 10 at reveals). Order: score, then total
time of correct answers, then join order.

Timing is server-authoritative: the deadline is fixed when the question is sent; answers after
`deadline` (+300 ms network grace) get `late`, the question auto-reveals at the deadline, and ends early
when every player answered. Show the countdown from `deadline - (serverTime offset)`, not a local timer
start. Points: `round(1000 × credit × (1 − elapsed/limit/2))` → 500–1000 for a right answer, 0 for wrong
or polls. The host advances with `next` after each reveal.

Limits: 8 KB per frame; 10-message burst then 5/s per socket (`error rate_limited`; more than 20 violations
→ close 1008); 30 upgrades/IP/min; a slow client whose 64-message buffer fills is disconnected; pings every
25 s. Idle cleanup (every 30 s): lobby idle 30 min, a started game with nobody connected for 5 min, or any
game older than 3 h is ended (`ended reason idle`); finished games are forgotten after 2 min. Games live
in the API process memory: a server restart ends them (the rows become `abandoned`).

`GET /api/quiz/live/history` → `{sessions: [{id, quizId, quizTitle, status, players, questionStats:
[{position, answered, correct}], createdAt, finishedAt, results: [{rank, nickname, score, correct}]}]}`.

Deploy: `deploy/studenthub.nginx.conf` has the ws location (HTTP/1.1, `Upgrade`/`Connection`, 1 h
timeouts) and a 52 MB body limit for `/api/quiz/generate/`. Because the server's nginx file is only
written when absent, `deploy/deploy-vps.sh` runs `deploy/nginx-quiz-patch.sh`, which inserts both blocks
if missing (idempotent, `nginx -t` or rollback). Uploads >4.5 MB may hit Vercel's proxy body limit:
downscale photos in the browser (canvas → JPEG ≤1600 px) before sending.

## Analytics

Whitelisted client events (`POST /api/events`, no text content): `quiz_created {source, questions}`,
`quiz_played {mode, questions, percent}`, `quiz_live_hosted {players, questions}`.

## Account data

Export (`GET /api/me/export`): `quizzes` (with questions and keys), `quizAttempts` (with answers),
`quizMedia`, `quizAIUsage`, `quizLiveHosted`, `quizLiveResults`. Deletion: every quiz table cascades from
`users`; media files are deleted by a hook (the deletion fails if a file cannot be removed); games hosted
by the student end.

## Settings

| Env | Default | Meaning |
|---|---|---|
| `FEATURE_QUIZ` | off | switch |
| `QUIZ_CREATE_PER_DAY` | 50 | quizzes created/cloned per student per day |
| `QUIZ_IMAGE_MAX_KB` / `QUIZ_AUDIO_MAX_KB` | 900 / 900 | upload limits |
| `QUIZ_MEDIA_PER_HOUR` | 60 | uploads per student |
| `QUIZ_AI_PER_MIN` | 3 | AI requests per minute |
| `QUIZ_AI_DAILY_UNITS` | 12 | AI budget per Almaty day |
| `QUIZ_AI_VISION_UNITS` | 2 | extra units per photo |
| `OPENAI_VISION_MODEL` | `OPENAI_MODEL` | model for photo transcription |
| `QUIZ_LIVE_WS_URL` | derived from Host | `wss://studenthub.85-137-91-47.sslip.io/api/quiz/live/ws` in prod |
| `QUIZ_LIVE_MAX_PLAYERS` | 100 | per game |
| `QUIZ_LIVE_MAX_SESSIONS` | 50 | open games per process |
| `QUIZ_LIVE_JOINS_PER_MIN` / `QUIZ_LIVE_WS_PER_MIN` | 20 / 30 | per IP |

## Frontend checklist

- [ ] **Library / my quizzes**: `/quiz` — tabs «Барлығы» (`GET /quizzes`, search, subject filter, new/popular,
  paging) and «Менің тесттерім» (`GET /mine`, draft badge, visibility badge, plays). Cards → play / edit / share / delete.
- [ ] **Editor** `/quiz/edit/{id}` (and `/quiz/new`): title, description, subject, visibility, time per question,
  shuffle; question list with drag-reorder; per-type forms for all 12 types (options with correct marks,
  multi checkboxes, T/F toggle, accepted answers chips, reorder list, pairs, categories, image + zone drawing
  for hotspot/labeling, passage + sub-questions); image/audio upload (`/media`), time override, explanation;
  show `sourceQuote` («жауап конспектіде мына жерде») on AI questions; save with `version`, handle
  `invalid_quiz` by scrolling to `question`, `409` → reload prompt.
- [ ] **Excel import** in the editor: template download, file picker, preview list with row warnings, «Қосу» appends.
- [ ] **AI from text** sheet: textarea (counter, 200–60 000), count 5/10/20, difficulty, types, language; usage
  meter (`/generate/usage`); progress state; → draft review.
- [ ] **AI from notes** upload sheet: file picker (PDF/DOCX/PPTX/TXT/images) + camera capture on phones
  (`accept="image/*" capture="environment"`), client-side downscale to ≤1600 px JPEG, ≤5 files, options as above,
  progress («файлдар оқылуда… / AI сұрақ құруда…»), error texts for `pdf_scanned`, `file_heic`, limits.
- [ ] **Draft review** screen: each question with its source quote, edit/delete inline, «Сақтау» → `PUT isDraft:false`;
  show `dropped`/`truncated` notices. «Осы конспектіден карточкалар» → `cards-from-files` → new local deck.
- [ ] **Play / practice** `/quiz/play/{id}` and `/quiz/s/{code}`: one question per screen, timer per question
  (practice timer is informational), per-type inputs, progress bar; submit once at the end.
- [ ] **Results**: score %, correct/total, per-question review with given vs correct + explanation;
  «Қате жауаптарды қайталау» (retry flow); history list (`/attempts`); guests: "кіріп, нәтижені сақтаңыз".
- [ ] **Owner stats** tab: per-question accuracy bars and option distribution.
- [ ] **Share sheet**: visibility switch, copy link `${SITE_URL}/quiz/s/{code}`, Telegram share, QR, «Сілтемені
  жаңарту» (`share/reset`), clone button on others' quizzes; `generateMetadata` on `/quiz/s/[code]` from `/meta`.
- [ ] **Live host screen** (projector): big 6-digit code + QR of `joinUrl`, player list with kick, «Бастау»;
  question view with countdown from `deadline`, answered counter (`progress`), «Келесі»; reveal with
  distribution chart + top-10 leaderboard; final podium. Reconnect with `host-token`.
- [ ] **Live join + play on phone** `/quiz/live?code=`: code + nickname form, WebSocket to `wsUrl`, waiting room,
  big answer buttons, «жауап қабылданды» after `answer_ack`, reveal (right/wrong, points, rank, streak),
  final rank; reconnect with `rejoinKey`; handle `kicked`, `late`, `ended`.
- [ ] Hide every entry point unless `/api/config` → `features.quiz`.
