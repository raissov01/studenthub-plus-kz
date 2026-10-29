import { defineConfig } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { botToken, adminTelegramID } from './e2e/real-api/settings';

// Shared by the API process and SQL fixtures; always a fresh local database.
process.env.E2E_DB_NAME ||= `studenthub_browser_${process.pid}_${randomBytes(6).toString('hex')}`;
const port = Number(process.env.E2E_REAL_PORT || 4320);
const apiPort = Number(process.env.E2E_REAL_API_PORT || 4321);

export default defineConfig({
  testDir: './e2e/real-api', testMatch: '**/*.spec.ts',
  fullyParallel: false, workers: 1, retries: 0,
  timeout: 90_000, expect: { timeout: 15_000 }, forbidOnly: !!process.env.CI,
  reporter: [['list'], ['html', { outputFolder: 'playwright-report/real', open: 'never' }]],
  outputDir: 'test-results/real',
  use: {
    baseURL: `http://127.0.0.1:${port}`, browserName: 'chromium',
    serviceWorkers: 'block', trace: 'retain-on-failure', screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'kk-390', metadata: { lang: 'kk' }, use: { viewport: { width: 390, height: 844 }, locale: 'kk-KZ' } },
    { name: 'ru-360', metadata: { lang: 'ru' }, use: { viewport: { width: 360, height: 844 }, locale: 'ru-RU' } },
  ],
  webServer: [
    {
      command: 'mkdir -p .fe-deps && go build -o .fe-deps/real-api ./pkg/e2etest && exec .fe-deps/real-api',
      url: `http://127.0.0.1:${apiPort}/api/health`, reuseExistingServer: false,
      timeout: 120_000, gracefulShutdown: { signal: 'SIGTERM', timeout: 15_000 },
      env: {
        API_ADDR: `127.0.0.1:${apiPort}`, BOT_TOKEN: botToken, BOT_USERNAME: 'local_playwright_bot',
        SESSION_SECRET: 'local-playwright-session-secret-32-characters',
        ADMIN_TELEGRAM_IDS: String(adminTelegramID), ALLOWED_HOSTS: '127.0.0.1',
        RATE_WRITE_PER_MIN: '2000',
        TELEGRAM_API_BASE: 'http://127.0.0.1:1',
        FEATURE_EVENTS: 'true', FEATURE_AMBASSADOR: 'true', FEATURE_MENTORS: 'true',
        FEATURE_MARKET: 'true', FEATURE_PORTFOLIO: 'true', FEATURE_B2B: 'true',
      },
    },
    {
      command: `npx next dev --webpack --hostname 127.0.0.1 --port ${port}`,
      url: `http://127.0.0.1:${port}`, reuseExistingServer: false, timeout: 120_000,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
      env: { API_ORIGIN: `http://127.0.0.1:${apiPort}` },
    },
  ],
});
