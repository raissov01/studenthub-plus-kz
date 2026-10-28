import { defineConfig } from '@playwright/test';

const port = Number(process.env.E2E_PORT || 4310);
const apiPort = Number(process.env.E2E_API_PORT || 4311);
const realApi = process.env.E2E_REAL_API === '1';
const apiOrigin = realApi ? process.env.API_ORIGIN : `http://127.0.0.1:${apiPort}`;
if (realApi && !apiOrigin) throw new Error('E2E_REAL_API=1 requires API_ORIGIN');

export default defineConfig({
  testDir: './e2e',
  testMatch: realApi ? 'real-api.spec.ts' : /^(?!.*real-api).*\.spec\.ts$/,  fullyParallel: true, workers: 2,
  timeout: 60_000, expect: { timeout: 10_000 },
  forbidOnly: !!process.env.CI, retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: `http://127.0.0.1:${port}`, browserName: 'chromium',
    serviceWorkers: 'allow', trace: 'retain-on-failure', screenshot: 'only-on-failure',
    launchOptions: { args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'] },
  },
  projects: ['kk', 'ru'].flatMap(lang => [360, 390].map(width => ({
    name: `${lang}-${width}`, metadata: { lang },
    use: { viewport: { width, height: 844 }, locale: lang === 'kk' ? 'kk-KZ' : 'ru-RU' },
  }))),
  webServer: [
    ...(!realApi ? [{ command: 'npx tsx e2e/server.ts', url: `http://127.0.0.1:${apiPort}/api/content`, reuseExistingServer: false }] : []),
    {
      command: `npx next start --hostname 127.0.0.1 --port ${port}`,
      url: `http://127.0.0.1:${port}`, reuseExistingServer: false, timeout: 120_000,
      env: { API_ORIGIN: apiOrigin! },
    },
  ],
});
