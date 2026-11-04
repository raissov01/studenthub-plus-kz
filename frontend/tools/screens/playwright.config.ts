/* Design screenshots (docs/design/screens): six screens × light/dark at 390×844, against the production build and the
   e2e content fixture. Not part of the e2e suite. Run: SHOT_TAG=after npx playwright test -c tools/screens */
import { defineConfig } from '@playwright/test';
import base from '../../playwright.config';

const port = Number(process.env.E2E_PORT || 4560);
const apiPort = Number(process.env.E2E_API_PORT || 4561);
export default defineConfig({
  ...base,
  testDir: '.', testMatch: 'screens.spec.ts', reporter: 'list', workers: 1,
  use: { ...base.use, baseURL: `http://127.0.0.1:${port}`, viewport: { width: 390, height: 844 }, locale: 'kk-KZ', deviceScaleFactor: 1 },
  projects: [{ name: 'kk-390', metadata: { lang: 'kk' } }],
  webServer: [
    { command: 'npx tsx e2e/server.ts', cwd: '../..', env: { E2E_API_PORT: String(apiPort) }, url: `http://127.0.0.1:${apiPort}/api/content`, reuseExistingServer: false },
    { command: `npx next start --hostname 127.0.0.1 --port ${port}`, cwd: '../..', url: `http://127.0.0.1:${port}`, reuseExistingServer: false, timeout: 120_000, env: { API_ORIGIN: `http://127.0.0.1:${apiPort}` } },
  ],
});
