/* Laptop/phone layout screenshots (docs/design/desktop): the main screens at 390×844, 768×1024, 1280×800 and 1440×900,
   against the production build and the e2e content fixture. Not part of the e2e suite.
   Run: E2E_PORT=4790 E2E_API_PORT=4791 npx playwright test -c tools/desktop */
import { defineConfig } from '@playwright/test';
import base from '../../playwright.config';

const port = Number(process.env.E2E_PORT || 4570);
const apiPort = Number(process.env.E2E_API_PORT || 4571);
const sizes: [number, number][] = [[390, 844], [768, 1024], [1280, 800], [1440, 900]];
export default defineConfig({
  ...base,
  testDir: '.', testMatch: 'screens.spec.ts', reporter: 'list', workers: 1, fullyParallel: false,
  use: { ...base.use, baseURL: `http://127.0.0.1:${port}`, locale: 'kk-KZ', deviceScaleFactor: 1, timezoneId: 'Asia/Almaty' },
  projects: sizes.map(([width, height]) => ({ name: `${width}x${height}`, metadata: { lang: 'kk' }, use: { viewport: { width, height } } })),
  webServer: [
    { command: 'npx tsx tools/desktop/server.ts', cwd: '../..', env: { E2E_API_PORT: String(apiPort) }, url: `http://127.0.0.1:${apiPort}/api/content`, reuseExistingServer: false },
    { command: `npx next start --hostname 127.0.0.1 --port ${port}`, cwd: '../..', url: `http://127.0.0.1:${port}`, reuseExistingServer: false, timeout: 120_000, env: { API_ORIGIN: `http://127.0.0.1:${apiPort}` } },
  ],
});
