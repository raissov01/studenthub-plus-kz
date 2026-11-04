import { test, expect, onboard, fits } from './fixtures';

// This tests browser enforcement, not merely whether the header contains a substring.
test('security headers allow campus map, fonts, camera, worker and quiz WS', async ({ page, context, lang }) => {
  await onboard(page, lang);
  const response = await page.goto('/map');
  const headers = response!.headers();
  expect(headers['referrer-policy']).toBe('strict-origin-when-cross-origin');
  expect(headers['x-content-type-options']).toBe('nosniff');
  expect(headers['permissions-policy']).toContain('camera=(self)');
  expect(headers['permissions-policy']).toContain('geolocation=(self)');
  expect(headers['permissions-policy']).toContain('microphone=()');
  expect(headers['content-security-policy']).toContain('frame-ancestors https://web.telegram.org');
  expect(headers['x-frame-options']).toBeUndefined(); // SAMEORIGIN / obsolete ALLOW-FROM would break Web Telegram.
  await context.grantPermissions(['geolocation', 'camera']);
  await context.setGeolocation({ latitude: 51.18692, longitude: 71.41178 });
  await page.locator('#geoBtn').click();
  await expect(page.locator('#geoBtn')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#geoLayer .me-dot')).toHaveCount(1);
  expect(await page.evaluate(async () => {
    const im = new Image(); im.src = '/campus-map.jpg'; await im.decode(); return im.naturalWidth;
  })).toBeGreaterThan(0);
  expect(await page.evaluate(async () => {
    const fonts = await document.fonts.load('700 18px Geologica', 'Қазақша Ә Ғ Қ Ң Ө Ұ Ү Һ І');
    return fonts.length > 0 && fonts.every(f => f.status === 'loaded');
  })).toBe(true);
  expect(await page.evaluate(async () => {
    const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false });
    const live = stream.getVideoTracks()[0]?.readyState;
    stream.getTracks().forEach(t => t.stop());
    return live;
  })).toBe('live');
  await page.routeWebSocket('wss://studenthub.85-137-91-47.sslip.io/**', ws => {
    ws.onMessage(message => ws.send(message));
  });
  expect(await page.evaluate(() => new Promise<string>((resolve, reject) => {
    const ws = new WebSocket('wss://studenthub.85-137-91-47.sslip.io/api/quiz/e2e');
    ws.onopen = () => ws.send('quiz-ready');
    ws.onmessage = event => { resolve(event.data); ws.close(); };
    ws.onerror = () => reject(new Error('WebSocket blocked'));
  }))).toBe('quiz-ready');
  await expect.poll(() => page.evaluate(async () => (await navigator.serviceWorker.getRegistration())?.active?.state), { timeout: 20_000 }).toBe('activated');
  await fits(page);
  // Admin/public portfolio must remain unframeable.
  for (const path of ['/admin', '/p/no-such-e2e-portfolio']) {
    const r = await page.request.get(path);
    expect(r.headers()['x-frame-options']).toBe('DENY');
    expect(r.headers()['content-security-policy']).toContain("frame-ancestors 'none'");
  }
});

test('Telegram script and login iframe are allowed; Mini App bridge loads', async ({ page, lang }) => {
  await onboard(page, lang);
  // Exercise the same origin and dynamic-callback mechanism used by the official legacy widget.
  // E2E_TELEGRAM_SCRIPT can supply the actual downloaded telegram-widget.js for a manual compatibility run.
  const { readFile } = await import('node:fs/promises');
  const source = process.env.E2E_TELEGRAM_SCRIPT
    ? await readFile(process.env.E2E_TELEGRAM_SCRIPT, 'utf8')
    : `(() => { const s = document.currentScript; (0, eval)('(function(user){' + s.getAttribute('data-onauth') + '})');
       const f = document.createElement('iframe'); f.src = 'https://oauth.telegram.org/embed/StudentHubTestBot'; s.after(f); })();`;
  await page.route('https://telegram.org/js/telegram-widget.js*', route => route.fulfill({ contentType: 'application/javascript', body: source }));
  await page.route('https://oauth.telegram.org/**', route => route.fulfill({ contentType: 'text/html', body: '<button>Telegram</button>' }));
  await page.goto('/settings');
  await page.getByRole('button', { name: lang === 'kk' ? /Аккаунтқа кіру/ : /Войти в аккаунт/ }).click();
  await expect(page.getByRole('dialog').locator('iframe')).toBeVisible();
  await expect(page.frameLocator('.tg-login iframe').getByRole('button', { name: 'Telegram' })).toBeVisible();
  await page.route('https://telegram.org/js/telegram-web-app.js', route => route.fulfill({ contentType: 'application/javascript', body: 'window.__miniAppScriptLoaded = true;' }));
  await page.goto('/#tgWebAppPlatform=web');
  await expect.poll(() => page.evaluate(() => (window as unknown as { __miniAppScriptLoaded: boolean }).__miniAppScriptLoaded)).toBe(true);
});

test('QR scanner opens camera and submits a decoded ticket with API mocked', async ({ page, lang }) => {
  const { config } = await import('./content');
  await onboard(page, lang);
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, events: true, b2b: false } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 901, name: 'Test organizer', role: 'ambassador', lang, consentAt: '2026-01-01', isAdult: true }, needsConsent: false } }));
  await page.route('**/api/events/101', route => route.fulfill({ json: { event: {
    id: 101, title_kk: 'Тест кездесуі', title_ru: 'Тестовая встреча', starts_at: '2026-11-01T12:00:00Z', ends_at: '2026-11-01T13:00:00Z',
    university: 'Campus', status: 'published', can_manage: true, registered: 1, checked_in: 0, waitlist: 0, teams_enabled: false,
  } } }));
  await page.route('**/api/events/101/regs', route => route.fulfill({ json: { registered: 1, checked_in: 0, waitlist: 0, regs: [] } }));
  await page.route('**/api/events/101/checkin/manual', async route => {
    expect(route.request().postDataJSON()).toEqual({ ticket: 'e2e-scanned-ticket' });
    await route.fulfill({ json: { status: 'checked_in', team_no: null, method: 'manual' } });
  });
  await page.addInitScript(() => {
    // Chromium has no QR decoder on all Linux builds. Keep real camera/video; supply one decoded value.
    Object.assign(window, { BarcodeDetector: class {
      async detect() { return [{ rawValue: 'e2e-scanned-ticket' }]; }
    } });
  });
  await page.goto('/organize/101');
  await page.getByRole('button', { name: lang === 'kk' ? 'Студент билетін сканерлеу' : 'Сканировать билет студента', exact: true }).click();
  const scanner = page.getByRole('dialog');
  await expect(scanner.locator('video')).toBeVisible();
  await expect(scanner.getByRole('status')).toContainText(lang === 'kk' ? 'Келді' : 'Пришёл');
  await scanner.getByRole('button', { name: lang === 'kk' ? 'Жабу' : 'Закрыть', exact: true }).click();
  await expect(scanner).not.toBeVisible();
});

test('only the exact Telegram Web origin may frame student pages', async ({ page, baseURL, lang }) => {
  await onboard(page, lang);
  const parentHtml = `<iframe id="mini" src="${baseURL}/settings" width="320" height="700"></iframe>`;
  await page.route('https://web.telegram.org/k/', route => route.fulfill({ contentType: 'text/html', body: parentHtml }));
  await page.goto('https://web.telegram.org/k/');
  await expect(page.frameLocator('#mini').getByRole('navigation')).toBeVisible();
  // A different Telegram subdomain must not inherit the permission.
  await page.route('https://untrusted.telegram.org/e2e-frame-host', route => route.fulfill({ contentType: 'text/html', body: parentHtml }));
  const denied = page.waitForEvent('console', { predicate: m => /frame-ancestors/.test(m.text()) });
  await page.goto('https://untrusted.telegram.org/e2e-frame-host');
  await denied;
  await expect(page.frameLocator('#mini').getByRole('navigation')).toHaveCount(0);
});
