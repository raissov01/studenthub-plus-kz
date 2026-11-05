import { test, expect, label, response, sql } from './helpers';
import { kaspiPort } from './settings';

// Real Go API + Postgres + the fake kaspi-pos. (1) The admin adds an operator and the shop's own Kaspi cashier; a
// student uploads a real PDF (the server counts its pages), sees the server's price, pays at the counter, the operator
// downloads it through the signed link, prints, marks it ready, and the student is told «Дайын». (2) A PDF the server
// cannot count: the student declares pages, the operator proposes a new price, the student approves it and pays with
// Kaspi in the shop's name.

function pdf(pages: number): Buffer {
  let out = '%PDF-1.4\n';
  const offs: number[] = [];
  const obj = (body: string) => { offs.push(Buffer.byteLength(out)); out += `${offs.length} 0 obj\n${body}\nendobj\n`; };
  obj('<< /Type /Catalog /Pages 2 0 R >>');
  obj(`<< /Type /Pages /Kids [${Array.from({ length: pages }, (_, i) => `${3 + i} 0 R`).join(' ')}] /Count ${pages} >>`);
  for (let i = 0; i < pages; i++) obj('<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] >>');
  const xref = Buffer.byteLength(out);
  out += `xref\n0 ${offs.length + 1}\n0000000000 65535 f \n` + offs.map(o => `${String(o).padStart(10, '0')} 00000 n \n`).join('');
  out += `trailer\n<< /Size ${offs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(out, 'latin1');
}

test('print: PDF upload → server price → pay at the counter → operator prints → student told «Дайын»', async ({ actor, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const name = `E2E print ${lang}`;
  const admin = await actor(`Print admin ${lang}`, { admin: true });
  const operator = await actor(`Print operator ${lang}`);
  const student = await actor(`Print student ${lang}`);
  const [shop] = sql<{ id: number }>(`insert into print_shops (name_kk, name_ru, open_time, close_time, open_days, slot_minutes, lead_minutes, active, payment_mode)
    values ($1, $1, '00:00', '23:55', '{1,2,3,4,5,6,7}', 15, 0, true, 'both') returning id`, name);
  sql(`insert into print_services (shop_id, kind, name_kk, name_ru, pricing) values ($1, 'print', 'Басып шығару', 'Печать',
    '{"sizes":{"A4":{"bw":2000,"color":10000,"bwDuplex":1500,"colorDuplex":0}}}')`, shop.id);
  sql(`insert into print_goods (shop_id, name_kk, name_ru, price) values ($1, 'Папка', 'Папка', 30000)`, shop.id);

  // admin: an operator and the shop's own Kaspi cashier session (stored encrypted, apart from the cafés')
  const a = admin.page;
  await a.goto('/admin/print');
  const card = a.locator('article.card').filter({ hasText: name });
  await card.getByRole('textbox', { name: t('Пайдаланушы id', 'id пользователя') }).fill(String(operator.id));
  await response(a, `/api/admin/print/shops/${shop.id}/staff`, () => card.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click());
  await card.getByRole('textbox', { name: t('Кассир нөмірі', 'Номер кассира') }).fill('+7 701 123 45 67');
  await response(a, `/api/admin/print/shops/${shop.id}/kaspi/login/start`, () => card.getByRole('button', { name: 'SMS', exact: true }).click());
  await card.getByRole('textbox', { name: t('SMS коды', 'SMS-код') }).fill('1234');
  await response(a, `/api/admin/print/shops/${shop.id}/kaspi/login/verify`, () => card.getByRole('button', { name: t('Растау', 'Подтвердить'), exact: true }).click());
  await expect(card).toContainText('7011***67');
  const [row] = sql<{ ciphertext: string }>(`select ciphertext from print_kaspi_sessions where shop_id = $1`, shop.id);
  expect(row.ciphertext).toMatch(/^v1:/);
  expect(row.ciphertext).not.toContain('E2E-TSN');

  // the student: a 3-page PDF, duplex, one folder; pay at the counter
  const s = student.page;
  await s.goto(`/print?id=${shop.id}`);
  await response(s, '/api/print/uploads', () => s.locator('#printFile').setInputFiles({ name: 'Курстық жұмыс.pdf', mimeType: 'application/pdf', buffer: pdf(3) }));
  const file = s.getByRole('article', { name: 'Курстық жұмыс.pdf' });
  await expect(file).toContainText(t('3 бет', '3 стр.'));
  await file.getByRole('checkbox', { name: t('Екі жақты', 'Двусторонняя') }).check();
  await s.getByRole('group', { name: 'Папка' }).getByRole('button', { name: t('Көбейту', 'Больше') }).click();
  await expect(s.locator('#printTotal')).toHaveText(/345\s₸/); // 3 × 15 ₸ + 300 ₸, priced by the server
  await s.getByRole('radio', { name: new RegExp(t('Алған кезде кассада', 'На кассе при получении')) }).check();
  await response(s, '/api/print/jobs', () => s.locator('#printSubmit').click());
  await expect(s).toHaveURL(/\/print\/jobs\?id=\d+$/);
  const [job] = sql<{ id: number; total: number; price_state: string }>(`select id, total, price_state from print_jobs where shop_id = $1`, shop.id);
  expect(job.total).toBe(34500);
  expect(job.price_state).toBe('final');
  // the stored file has a random name; nothing of it is public
  const [f] = sql<{ id: number; file_name: string; pages: number }>(`select id, file_name, pages from print_files where job_id = $1`, job.id);
  expect(f.file_name).toMatch(/^[0-9a-f]{32}\.pdf$/);
  expect(f.pages).toBe(3);
  expect((await s.request.get(`/api/print/files/${f.id}`)).status()).toBe(403);

  // the operator: the queue, the file by its signed link, printing → ready
  const o = operator.page;
  await o.goto('/print/staff');
  const ticket = o.locator('.print-ticket').filter({ hasText: 'Курстық жұмыс.pdf' });
  const href = await ticket.getByRole('link', { name: /Курстық жұмыс\.pdf/ }).getAttribute('href');
  const dl = await o.request.get(href!);
  expect(dl.status()).toBe(200);
  expect(dl.headers()['content-disposition']).toMatch(/^attachment/);
  expect(dl.headers()['content-security-policy']).toContain('sandbox');
  expect((await dl.body()).subarray(0, 5).toString()).toBe('%PDF-');
  expect((await o.request.get(href!.replace(/s=[0-9a-f]+/, 's=' + '0'.repeat(64)))).status()).toBe(403);
  await response(o, `/api/print/staff/jobs/${job.id}/printing`, () => ticket.getByRole('button', { name: t('Басып жатырмын', 'Печатаю') }).click());
  await o.getByRole('tab', { name: new RegExp(t('Басып жатыр', 'Печатаются')) }).click();
  await response(o, `/api/print/staff/jobs/${job.id}/ready`, () => o.locator('.print-ticket').getByRole('button', { name: t('Дайын', 'Готово'), exact: true }).click());
  await expect(s.getByRole('status').getByText(t('Дайын', 'Готово'), { exact: true })).toBeVisible({ timeout: 20_000 });
  // «Дайын» reached the student's inbox (Telegram delivery itself is closed in this suite)
  await expect.poll(() => sql<{ n: number }>(`select count(*)::int as n from notifications where user_id = $1 and category = 'print' and link = $2`,
    student.id, `/print/jobs?id=${job.id}`)[0].n).toBe(1);
  await o.getByRole('tab', { name: new RegExp(t('Дайын', 'Готовы') + ' ') }).click();
  await response(o, `/api/print/staff/jobs/${job.id}/picked-up`, () => o.locator('.print-ticket').getByRole('button', { name: t('Берілді', 'Выдан') }).click());
  const kinds = sql<{ kind: string }>(`select kind from print_job_events where job_id = $1 order by id`, job.id).map(r => r.kind);
  expect(kinds).toEqual(['created', 'printing', 'ready', 'picked_up']);
  // another student sees nothing of it
  const other = await actor(`Print other ${lang}`);
  expect((await other.page.request.get(`/api/print/jobs/${job.id}`)).status()).toBe(404);

  // (2) a PDF the server cannot read: declared pages, the operator's new price, approval, Kaspi to the shop
  await s.goto(`/print?id=${shop.id}`);
  await response(s, '/api/print/uploads', () => s.locator('#printFile').setInputFiles({ name: 'scan.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.7\nbroken\n') }));
  const scan = s.getByRole('article', { name: 'scan.pdf' });
  await scan.getByLabel(t('Файлдағы бет саны', 'Страниц в файле')).fill('5');
  await expect(s.locator('#printTotal')).toHaveText(/100\s₸/);
  await s.getByRole('radio', { name: new RegExp(t('Kaspi арқылы', 'Через Kaspi')) }).check();
  await response(s, '/api/print/jobs', () => s.locator('#printSubmit').click());
  await expect(s.getByText(t('Оператор бағаны тексеріп жатыр', 'Оператор проверяет цену'))).toBeVisible();
  const [j2] = sql<{ id: number; price_state: string }>(`select id, price_state from print_jobs where shop_id = $1 and id <> $2`, shop.id, job.id);
  expect(j2.price_state).toBe('review');
  await o.getByRole('tab', { name: new RegExp(t('Жаңа', 'Новые')) }).click();
  const t2 = o.locator('.print-ticket').filter({ hasText: 'scan.pdf' });
  await t2.getByRole('button', { name: t('Бағаны өзгерту', 'Изменить цену') }).click({ timeout: 20_000 });
  const sheet = o.getByRole('dialog');
  await sheet.locator('#ppTotal').fill('140');
  await sheet.locator('#ppReason').fill(t('Файлда 7 бет', 'В файле 7 страниц'));
  await response(o, `/api/print/staff/jobs/${j2.id}/price`, () => sheet.getByRole('button', { name: t('Жіберу', 'Отправить') }).click());
  const proposal = s.locator('#printProposal');
  await expect(proposal).toContainText(/140\s₸/, { timeout: 20_000 });
  await expect(proposal).toContainText(t('Файлда 7 бет', 'В файле 7 страниц'));
  await response(s, `/api/print/jobs/${j2.id}/price`, () => proposal.getByRole('button', { name: t('Жаңа бағамен келісемін', 'Согласен с новой ценой') }).click());
  await expect(s.locator('#kaspiLink')).toHaveAttribute('href', /^https:\/\/pay\.kaspi\.kz\/pay\/\d+$/);
  const [paying] = sql<{ kaspi_operation_id: string; total: number }>(`select kaspi_operation_id, total from print_jobs where id = $1`, j2.id);
  expect(paying.total).toBe(14000);
  expect((await fetch(`http://127.0.0.1:${kaspiPort}/__pay/${paying.kaspi_operation_id}`, { method: 'POST' })).ok).toBe(true);
  await expect(s.getByText(t('Kaspi арқылы төленді', 'Оплачено через Kaspi'))).toBeVisible({ timeout: 20_000 });
  const kinds2 = sql<{ kind: string }>(`select kind from print_job_events where job_id = $1 order by id`, j2.id).map(r => r.kind);
  expect(kinds2).toEqual(['created', 'price_proposed', 'price_approved', 'pay_started', 'paid']);
});
