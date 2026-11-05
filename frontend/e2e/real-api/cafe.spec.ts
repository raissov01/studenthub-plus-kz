import { test, expect, label, response, sql } from './helpers';
import { kaspiPort } from './settings';

// Real Go API + Postgres + the fake kaspi-pos: the admin connects the café's cashier (SMS code step) and a barista,
// a student pays with Kaspi in the café's name, the payment is reconciled, the counter accepts and readies it.
test('cafe: cashier login → Kaspi order paid to the café → counter accepts and readies', async ({ actor, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const name = `E2E café ${lang}`;
  const admin = await actor(`Cafe admin ${lang}`, { admin: true });
  const barista = await actor(`Cafe barista ${lang}`);
  const student = await actor(`Cafe student ${lang}`);
  const [cafe] = sql<{ id: number }>(`insert into cafes (name_kk, name_ru, open_time, close_time, open_days, slot_minutes, slot_capacity, lead_minutes, active, payment_mode)
    values ($1, $1, '00:00', '23:55', '{1,2,3,4,5,6,7}', 5, 50, 0, true, 'both') returning id`, name);
  sql(`insert into cafe_menu_items (cafe_id, name_kk, name_ru, price) values ($1, 'Американо', 'Американо', 70000)`, cafe.id);

  // admin: a barista and the café's own Kaspi cashier session
  const a = admin.page;
  await a.goto('/admin/cafe');
  const card = a.locator('article.card').filter({ hasText: name });
  await card.getByRole('textbox', { name: t('Пайдаланушы id', 'id пользователя') }).fill(String(barista.id));
  await response(a, `/api/admin/cafes/${cafe.id}/staff`, () => card.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click());
  await card.getByRole('textbox', { name: t('Кассир нөмірі', 'Номер кассира') }).fill('+7 701 123 45 67');
  await response(a, `/api/admin/cafes/${cafe.id}/kaspi/login/start`, () => card.getByRole('button', { name: 'SMS', exact: true }).click());
  await card.getByRole('textbox', { name: t('SMS коды', 'SMS-код') }).fill('1234');
  await response(a, `/api/admin/cafes/${cafe.id}/kaspi/login/verify`, () => card.getByRole('button', { name: t('Растау', 'Подтвердить'), exact: true }).click());
  await expect(card).toContainText('7011***67');
  // the session is stored encrypted, never in clear
  const [row] = sql<{ ciphertext: string }>(`select ciphertext from cafe_kaspi_sessions where cafe_id = $1`, cafe.id);
  expect(row.ciphertext).toMatch(/^v1:/);
  expect(row.ciphertext).not.toContain('E2E-TSN');

  // the student orders and pays with Kaspi
  const s = student.page;
  await s.goto(`/cafe?id=${cafe.id}`);
  await s.getByRole('button', { name: t('Американо: себетке қосу', 'Американо: в корзину') }).click();
  await s.locator('#cafeCart').click();
  await expect(s.getByRole('dialog').getByRole('radio', { name: new RegExp(t('Қазір Kaspi арқылы', 'Сейчас через Kaspi')) })).toBeChecked();
  await response(s, '/api/cafe/orders', () => s.locator('#cafeSubmit').click());
  await expect(s).toHaveURL(/\/cafe\/orders\?id=\d+$/);
  await expect(s.locator('#kaspiLink')).toHaveAttribute('href', /^https:\/\/pay\.kaspi\.kz\/pay\/\d+$/);
  const [order] = sql<{ id: number; kaspi_operation_id: string; total: number }>(`select id, kaspi_operation_id, total from cafe_orders where cafe_id = $1`, cafe.id);
  expect(order.total).toBe(70000);
  // paying in the Kaspi app: the status screen picks it up by itself
  expect((await fetch(`http://127.0.0.1:${kaspiPort}/__pay/${order.kaspi_operation_id}`, { method: 'POST' })).ok).toBe(true);
  await expect(s.getByText(t('Kaspi арқылы төленді', 'Оплачено через Kaspi'))).toBeVisible({ timeout: 20_000 });

  // the counter
  const b = barista.page;
  await b.goto('/cafe/staff');
  const ticket = b.locator('.cafe-ticket').filter({ hasText: 'Американо' });
  await response(b, `/api/cafe/staff/orders/${order.id}/accept`, () => ticket.getByRole('button', { name: t('Қабылдау', 'Принять') }).click());
  await b.getByRole('tab', { name: new RegExp(t('Дайындалуда', 'Готовятся')) }).click();
  await response(b, `/api/cafe/staff/orders/${order.id}/ready`, () => b.locator('.cafe-ticket').getByRole('button', { name: t('Дайын', 'Готово'), exact: true }).click());
  await expect(s.getByRole('status').getByText(t('Дайын — алып кетіңіз', 'Готов — заберите'))).toBeVisible({ timeout: 20_000 });

  const kinds = sql<{ kind: string }>(`select kind from cafe_order_events where order_id = $1 order by id`, order.id).map(r => r.kind);
  expect(kinds).toEqual(['created', 'pay_started', 'paid', 'accepted', 'ready']);
  // another student sees nothing of it
  const other = await actor(`Cafe other ${lang}`);
  expect((await other.page.request.get(`/api/cafe/orders/${order.id}`)).status()).toBe(404);
});
