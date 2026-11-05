/* Web Push settings (docs/PUSH.md) with a mocked PushManager: permission is asked only on the tap, the subscription
   goes to POST /api/me/push, the test button calls POST /api/me/push/test, turning it off sends DELETE. iOS outside the
   home screen gets the note instead of the switch. The service worker side (push / notificationclick) is plain JS in
   public/sw.js and is exercised by the Go push tests only through its payload contract. */
import type { Page } from '@playwright/test';
import { test, expect, label } from './fixtures';
import { config } from './content';

const KEY = 'BPQroD0GsjFfmjLuYgh0efIK24mn5QVCKTLpNdJf5eu-WZ70lNIw73L3hysAuM55xiqAAHzooivhzto0ZtzQfbE'; // a P-256 public key (65 bytes), as /api/config sends it

async function setup(page: Page, lang: string, webPush = true) {
  const calls: { method: string; path: string; body: unknown }[] = [];
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, ...(webPush ? { webPush: { enabled: true, publicKey: KEY } } : {}) } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 601, name: 'Push Test', role: 'student', lang, telegramId: 5, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'IT' }, needsConsent: false, inboxUnread: 0 } }));
  await page.route('**/api/me/notifications', route => route.fulfill({ json: { prefs: {
    categories: { deadlines: true, digest: true, events: true, mentors: true, market: true, jobs: true, community: true, moderation: true },
    quietHours: { enabled: true, start: '22:00', end: '08:00', timezone: 'Asia/Almaty' }, dailyCap: 3, locked: [] } } }));
  await page.route('**/api/me/push**', async route => {
    const req = route.request();
    calls.push({ method: req.method(), path: new URL(req.url()).pathname, body: req.postData() ? req.postDataJSON() : null });
    if (new URL(req.url()).pathname === '/api/me/push/test') return route.fulfill({ json: { delivered: 1, gone: 0, failed: 0 } });
    return route.fulfill({ json: { ok: true } });
  });
  // an onboarded student on this device
  await page.addInitScript(l => {
    if (!localStorage.getItem('studenthub.v1')) localStorage.setItem('studenthub.v1', JSON.stringify({ v: 1, onboarded: true, lang: l, profile: { name: 'Push Test', spec: 'it', course: 2 } }));
  }, lang);
  // a browser with push support whose PushManager is a recorder
  await page.addInitScript(() => {
    // kept in sessionStorage, so a reload finds the same subscription and permission (as in a real browser)
    const saved = JSON.parse(sessionStorage.getItem('__push') || 'null');
    const state = { asked: 0, subscribe: [] as { userVisibleOnly: boolean; keyLength: number }[], subscribed: !!saved?.subscribed, unsubscribed: 0 };
    let perm: NotificationPermission = saved?.perm ?? 'default';
    const save = () => sessionStorage.setItem('__push', JSON.stringify({ subscribed: state.subscribed, perm }));
    const sub = {
      endpoint: 'https://push.example.test/send/abc',
      options: { applicationServerKey: null },
      toJSON: () => ({ endpoint: 'https://push.example.test/send/abc', expirationTime: null, keys: { p256dh: 'BPk', auth: 'AAAA' } }),
      unsubscribe: async () => { state.subscribed = false; state.unsubscribed++; save(); return true; },
    };
    const pushManager = {
      getSubscription: async () => (state.subscribed ? sub : null),
      subscribe: async (o: { userVisibleOnly: boolean; applicationServerKey: Uint8Array }) => {
        state.subscribe.push({ userVisibleOnly: o.userVisibleOnly, keyLength: o.applicationServerKey.byteLength });
        state.subscribed = true;
        save();
        return sub;
      },
    };
    Object.assign(window, { __push: state });
    if (!('PushManager' in window)) Object.assign(window, { PushManager: function PushManager() {} });
    Object.defineProperty(window, 'Notification', { configurable: true, value: class {
      static get permission() { return perm; }
      static async requestPermission() { state.asked++; perm = 'granted'; save(); return perm; }
    } });
    if (navigator.serviceWorker) navigator.serviceWorker.getRegistration = async () => ({ pushManager } as unknown as ServiceWorkerRegistration);
  });
  return calls;
}

const pushState = (page: Page) => page.evaluate(() => (window as unknown as { __push: { asked: number; subscribe: unknown[]; subscribed: boolean; unsubscribed: number } }).__push);

test('browser notifications: asked only on tap, subscribe, test, unsubscribe', async ({ page, lang }) => {
  const calls = await setup(page, lang);
  await page.goto('/settings');
  const toggle = page.locator('#pushToggle');
  await expect(page.locator('#push-settings')).toContainText(label(lang, 'Браузер хабарламалары', 'Уведомления в браузере'));
  await expect(toggle).toBeEnabled();
  await expect(toggle).not.toBeChecked();
  expect((await pushState(page)).asked, 'permission must not be asked on load').toBe(0);

  await toggle.click(); // controlled: it turns on once the subscription is saved
  await expect(toggle).toBeChecked();
  const st = await pushState(page);
  expect(st.asked).toBe(1);
  expect(st.subscribe).toEqual([{ userVisibleOnly: true, keyLength: 65 }]);
  expect(calls).toContainEqual({ method: 'POST', path: '/api/me/push', body: { endpoint: 'https://push.example.test/send/abc', expirationTime: null, keys: { p256dh: 'BPk', auth: 'AAAA' } } });

  await page.locator('#pushTest').click();
  await expect.poll(() => calls.filter(c => c.path === '/api/me/push/test').length).toBe(1);

  // a reload finds the existing subscription
  await page.reload();
  await expect(page.locator('#pushToggle')).toBeChecked();
  expect((await pushState(page)).asked).toBe(0);

  await page.locator('#pushToggle').click();
  await expect(page.locator('#pushToggle')).not.toBeChecked();
  expect(calls).toContainEqual({ method: 'DELETE', path: '/api/me/push', body: { endpoint: 'https://push.example.test/send/abc' } });
});

test('browser notifications: hidden when the server has push off', async ({ page, lang }) => {
  await setup(page, lang, false);
  await page.goto('/settings');
  await expect(page.locator('#notify-settings')).toBeVisible();
  await expect(page.locator('#push-settings')).toHaveCount(0);
});

test.describe('iOS Safari outside the home screen', () => {
  test.use({ userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1' });
  test('shows the install note and no switch to tap', async ({ page, lang }) => {
    await setup(page, lang);
    await page.goto('/settings');
    await expect(page.locator('#pushIos')).toContainText('iOS 16.4');
    await expect(page.locator('#pushToggle')).toBeDisabled();
  });
});
