# Quiz frontend verification

The mock is a standalone, loopback-only HTTP and WebSocket fixture. No application module imports it.
It refuses to start without `QUIZ_MOCK=1` and `NODE_ENV=development`, including in production mode.
It exercises transport, navigation, review and live state; it does **not** implement real document
extraction, AI, authentication, database permissions or production grading.

From this worktree, use separate terminals:

```sh
QUIZ_MOCK=1 QUIZ_MOCK_PORT=18197 NODE_ENV=development node tools/quiz/mock-server.mjs
API_ORIGIN=http://127.0.0.1:18197 NEXT_PUBLIC_API_WS_ORIGIN=ws://127.0.0.1:18197 npx next dev --webpack --hostname 127.0.0.1 --port 18198
```

Generate the fixtures and run the browser checks using an installed Playwright with Chromium:

```sh
python3 tools/quiz/fixtures.py
node tools/quiz/browser-check.mjs
```

If Playwright lives outside this project, set `PLAYWRIGHT_MODULE` to its absolute `index.mjs` path.
The script supports `QUIZ_TEST_URL`, `QUIZ_MOCK_URL`, `QUIZ_FIXTURES`, `QUIZ_ARTIFACTS` and
`QUIZ_CASE=kk-320` (or another language/width pair) for targeted reruns. Defaults use the ports above.
Screenshots and generated documents go to `/tmp/studenthub-quiz-artifacts` and
`/tmp/studenthub-quiz-fixtures`; no binaries need to be committed.

Coverage: kk/ru × 320/360/390; create, edit, paste, PDF/DOCX, camera input, Excel, draft quotes,
delete/edit review questions, all 12 type editors/player controls/results, guest and saved attempts,
retry wrong, history, share/copy/QR, host plus two players, reconnect after answering, leaderboard,
ended session, disabled feature, metadata, consent and sign-in gates, private media Bearer headers,
and a large PNG converted to a bounded JPEG before upload. Layout checks reject horizontal overflow
and clipped text. Console and page exceptions fail the run.

Stop both terminals after verification. Then run the production checks (do not run a build against
an active dev server using the same `.next` directory):

```sh
npx tsc --noEmit
npx eslint .
npx next build --webpack
npm run test:ts
```

On a fresh checkout, `npx next typegen` creates the repository's generated `PageProps`/`LayoutProps`
before the first standalone `tsc` invocation. `node_modules` may be a symlink; use webpack as above.

Production requires `/api/config.features.quiz = true`. HTTP uses the existing same-origin API
rewrite and account transport. Set `NEXT_PUBLIC_API_WS_ORIGIN` at build time for a non-default API
socket origin; the production default is `wss://studenthub.85-137-91-47.sslip.io`, development defaults
to `ws://localhost:8080`. Live connections refresh their short-lived tickets over HTTP on reconnect.
Uploads are capped at 4 MiB total after local photo compression to fit the frontend proxy; large
images are converted to JPEG at up to 2000 px and 800 KiB. Large documents must be split first.
