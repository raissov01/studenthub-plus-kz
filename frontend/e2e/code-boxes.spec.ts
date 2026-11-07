import { test, expect, onboard, label } from './fixtures';
import { config } from './content';

// the email code: typing goes box to box by itself, backspace walks back, a pasted code fills all six
test('email code boxes move on their own', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.route('**/api/config', async route => {
    // the Node fixture server has no /api/config (404): answer the suite's fixture config with email sign-in on
    await route.fulfill({ json: { ...config, emailAuth: true } });
  });
  await page.route('**/api/auth/email/register', route => route.fulfill({ json: { ok: true, resendAfter: 60 } }));
  const sent: string[] = [];
  await page.route('**/api/auth/email/verify', route => { sent.push(JSON.parse(route.request().postData() || '{}').code); });
  await onboard(page, lang);
  await page.getByRole('button', { name: t('Кіру', 'Вход'), exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('tab', { name: 'Email', exact: true }).click();
  await dialog.getByRole('button', { name: t('Аккаунт ашу', 'Создать аккаунт') }).last().click();
  await dialog.locator('#regEmail').fill('student@example.com');
  await dialog.locator('#regPw').fill('Strong-pass-2026');
  await dialog.getByRole('button', { name: t('Жалғастыру', 'Продолжить') }).click();

  const code = dialog.getByRole('textbox', { name: t('Хаттағы 6 таңбалы код', '6-значный код из письма') });
  await expect(code).toBeFocused();
  const cells = dialog.locator('.code-cell');
  await page.keyboard.type('123');
  await expect(cells).toHaveText(['1', '2', '3', '', '', '']);
  await page.keyboard.press('Backspace');
  await expect(cells).toHaveText(['1', '2', '', '', '', '']);
  await page.keyboard.type('3456');
  await expect(cells).toHaveText(['1', '2', '3', '4', '5', '6']);
  await expect.poll(() => sent).toEqual(['123456']);
});
