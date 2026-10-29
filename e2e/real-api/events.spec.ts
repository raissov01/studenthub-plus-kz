import { test, expect, label, response } from './helpers';

test('11-009: create → register → rotating QR → organizer check-in → public verified portfolio', async ({ actor, lang, browser, baseURL }) => {
  test.setTimeout(150_000); // The real QR rotates on a server minute boundary.
  const organizer = await actor(`Organizer ${lang}`, { admin: true });
  const student = await actor(`Attendee ${lang}`);
  const qrStudent = await actor(`QR attendee ${lang}`);
  const title = `Browser event ${lang}`;
  const p = organizer.page;
  await p.goto('/organize/new');
  await p.locator('#efTitleKk').fill(title);
  await p.locator('#efTitleRu').fill(title);
  const wallTime = (minutes: number) => new Date(Date.now() + (300 + minutes) * 60_000).toISOString().slice(0, 16);
  await p.locator('#efStart').fill(wallTime(5));
  await p.locator('#efEnd').fill(wallTime(65));
  await p.locator('#efPlace').fill('Campus hall');
  await p.locator('#efUni').fill(`Event university ${lang}`);
  const created = await response(p, '/api/events/create', () => p.getByRole('button', { name: label(lang, 'Жариялау', 'Опубликовать'), exact: true }).click());
  const id = created.event.id;
  await student.page.goto(`/event/${id}`);
  await response(student.page, `/api/events/${id}/register`, () => student.page.getByRole('button', { name: label(lang, 'Тіркелу', 'Записаться'), exact: true }).click());
  await expect(student.page.getByText(label(lang, 'Электрондық билет', 'Электронный билет'), { exact: true })).toBeVisible();
  const detail = await (await student.page.request.get(`/api/events/${id}`)).json();
  const ticket = detail.event.my.ticket;
  await qrStudent.page.goto(`/event/${id}`);
  await response(qrStudent.page, `/api/events/${id}/register`, () => qrStudent.page.getByRole('button', { name: label(lang, 'Тіркелу', 'Записаться'), exact: true }).click());
  await p.goto(`/organize/${id}`);
  const firstLive = p.waitForResponse(r => new URL(r.url()).pathname === `/api/events/${id}/checkin/live` && r.ok());
  const initial = await response(p, `/api/events/${id}/checkin/start`, () => p.getByRole('button', { name: label(lang, 'Тіркеуді бастау', 'Начать отметку'), exact: true }).click());
  const firstToken = (await (await firstLive).json()).token;
  expect(initial.open).toBe(true);
  expect(initial.registered).toBe(2);
  const live = p.getByRole('dialog', { name: label(lang, 'Тірі QR', 'Живой QR') });
  await expect(live.locator('.qr svg')).toBeVisible();
  const firstQr = await live.locator('.qr').innerHTML();
  // Observe the actual browser polling response and rendered QR, without replacing the clock/API.
  await p.waitForResponse(async r => new URL(r.url()).pathname === `/api/events/${id}/checkin/live`
    && r.ok() && (await r.json()).token !== firstToken, { timeout: 70_000 });
  await expect.poll(() => live.locator('.qr').innerHTML()).not.toBe(firstQr);
  await live.getByRole('button', { name: label(lang, 'Жабу', 'Закрыть'), exact: true }).click();
  // A new organizer nonce invalidates the previous QR immediately.
  const restarted = await response(p, `/api/events/${id}/checkin/start`, () => p.getByRole('button', { name: label(lang, 'Тіркеуді бастау', 'Начать отметку'), exact: true }).click());
  const old = await student.page.request.post('/api/events/checkin', { headers: { Origin: baseURL! }, data: { token: initial.token } });
  expect(old.status()).toBe(400);
  const qrCheckin = await response(qrStudent.page, '/api/events/checkin', () => qrStudent.page.goto(`/checkin?t=${encodeURIComponent(restarted.token)}`));
  expect(qrCheckin).toMatchObject({ status: 'checked_in', method: 'qr' });
  await expect(qrStudent.page.getByRole('heading', { name: label(lang, 'Қош келдің!', 'Добро пожаловать!'), exact: true })).toBeVisible();
  await live.getByRole('button', { name: label(lang, 'Жабу', 'Закрыть'), exact: true }).click();
  await p.getByRole('button', { name: label(lang, 'Студент билетін сканерлеу', 'Сканировать билет студента'), exact: true }).click();
  await p.locator('#scanCode').fill(ticket);
  await response(p, `/api/events/${id}/checkin/manual`, () => p.getByRole('button', { name: label(lang, 'Келді деп белгілеу', 'Отметить пришедшим'), exact: true }).click());
  await expect(p.getByRole('dialog').getByRole('status')).toContainText(label(lang, 'Келді', 'Пришёл'));
  await student.page.reload();
  await expect(student.page.getByText(label(lang, 'Келгеніңіз расталды', 'Ваше присутствие подтверждено'), { exact: true })).toBeVisible();
  await student.page.goto('/portfolio');
  // This controlled checkbox opens consent first; it changes only after confirmation.
  await student.page.getByRole('checkbox', { name: label(lang, 'Жария бет', 'Публичная страница') }).click();
  const published = await response(student.page, '/api/portfolio/settings', () => student.page.getByRole('dialog').getByRole('button', { name: label(lang, 'Жоқ', 'Нет'), exact: true }).click(), 'PATCH');
  const guest = await browser.newContext({ baseURL, viewport: { width: lang === 'kk' ? 390 : 360, height: 844 } });
  try {
    const page = await guest.newPage();
    const r = await page.goto(`/p/${published.settings.slug}?lang=${lang}`);
    expect(r?.status()).toBe(200);
    const entry = page.locator('.pf-item').filter({ hasText: title });
    await expect(entry).toContainText(label(lang, 'Расталған', 'Подтверждено'));
    const publicData = await (await page.request.get(`/api/p/${published.settings.slug}`)).json();
    expect(publicData.verified.events).toEqual(expect.arrayContaining([expect.objectContaining({ verified: true })]));
    expect(publicData.selfReported.experience).toEqual([]);
  } finally { await guest.close(); }
});
