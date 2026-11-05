import { readFile } from 'node:fs/promises';
import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label, deviceState, seedTimetable } from './fixtures';

const almatyToday = () => new Date(Date.now() + 5 * 3600_000).toISOString().slice(0, 10);
const plusDays = (d: string, n: number) => new Date(Date.parse(d + 'T00:00:00Z') + n * 86400_000).toISOString().slice(0, 10);

/** every record of a store in the study database (IndexedDB) */
function idbAll(page: Page, store: string) {
  return page.evaluate(s => new Promise<Record<string, unknown>[]>((resolve, reject) => {
    const r = indexedDB.open('studenthub-study');
    r.onerror = () => reject(r.error);
    r.onsuccess = () => {
      const q = r.result.transaction(s).objectStore(s).getAll();
      q.onsuccess = () => { resolve(q.result); r.result.close(); };
      q.onerror = () => reject(q.error);
    };
  }), store);
}

const AI_ANSWER = [
  '**ҚОРЫТЫНДЫ:**', 'Сұраныс баға өскенде азаяды.', '',
  '**КАРТОЧКАЛАР:**', 'С: Сұраныс заңы | Ж: Баға өссе, сұраныс азаяды', 'С: Икемділік | Ж: Пайыздық жауап',
].join('\n');

test('study core: subject page from the menu, Cornell note survives reload, export, AI cards, spaced repetition', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await seedTimetable(page, [0, 1, 2, 3, 4, 5, 6]); // every day, so the home «next class» card has one in the morning

  await test.step('subject page (no menu entry: the section is reachable by its address only)', async () => {
    await page.goto('/subjects');
    await expect(page).toHaveURL(/\/subjects$/);
    await expect(page.locator('#subjectList .row')).toHaveCount(3);
    await fits(page);
    await page.locator('#subjectList').getByRole('button', { name: /^Микроэкономика/ }).click();
    await expect(page).toHaveURL(/\/subjects\?id=s[0-9a-z]+$/);
    await expect(page.getByRole('heading', { name: 'Микроэкономика', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: t('4-214 аудитория — картада', 'Аудитория 4-214 на карте') })).toBeVisible();
    await expect(page.getByRole('heading', { name: t('Емтихан', 'Экзамен') })).toBeVisible();
    await fits(page);
  });

  const subjectUrl = page.url();

  await test.step('a Cornell note: write in the three zones, saved, survives a reload', async () => {
    await page.getByRole('button', { name: t('Алғашқы конспект', 'Первый конспект') }).click();
    const sheet = page.getByRole('dialog');
    await sheet.getByRole('button', { name: new RegExp('^' + t('Cornell', 'Корнелл')) }).click();
    await expect(page).toHaveURL(/\/notes\?id=n[0-9a-z]+$/);
    await page.getByLabel(t('Конспект тақырыбы', 'Тема конспекта')).fill('Сұраныс заңы');
    await page.getByRole('region', { name: t('Сұрақтар мен кілт сөздер', 'Вопросы и ключевые слова'), exact: true }).getByRole('textbox').fill('Сұраныс неге азаяды?');
    const main = page.getByRole('region', { name: t('Жазба', 'Записи'), exact: true });
    await main.getByRole('textbox').first().fill('Баға өскенде сұраныс көлемі азаяды, басқа жағдайлар өзгермесе. Икемділік — пайыздық жауап.');
    await main.getByRole('button', { name: t('Тізім', 'Список'), exact: true }).click();
    await expect(main.getByRole('textbox')).toHaveCount(2);
    await expect(main.getByRole('textbox').nth(1)).toBeFocused();
    await page.keyboard.type('Тепе-теңдік баға');
    await page.getByRole('region', { name: t('Қорытынды', 'Итог'), exact: true }).getByRole('textbox').fill('Сұраныс пен баға кері байланысты.');
    await expect(page.locator('#noteStatus')).toHaveText(new RegExp('^' + t('Сақталды', 'Сохранено') + ' \\d\\d:\\d\\d$'));
    await fits(page);
    const stored = await idbAll(page, 'notes');
    expect(stored).toHaveLength(1);
    expect(stored[0]).toMatchObject({ template: 'cornell', title: 'Сұраныс заңы' });

    await page.reload();
    await expect(page.getByLabel(t('Конспект тақырыбы', 'Тема конспекта'))).toHaveValue('Сұраныс заңы');
    await expect(page.getByRole('region', { name: t('Жазба', 'Записи'), exact: true }).getByRole('textbox').nth(1)).toHaveValue('Тепе-теңдік баға');
    await expect(page.getByRole('region', { name: t('Қорытынды', 'Итог'), exact: true }).getByRole('textbox')).toHaveValue('Сұраныс пен баға кері байланысты.');
  });

  await test.step('export as Markdown', async () => {
    const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: '.md', exact: true }).click()]);
    expect(download.suggestedFilename()).toBe('сұраныс-заңы.md');
    const md = await readFile(await download.path(), 'utf8');
    expect(md).toContain('# Сұраныс заңы');
    expect(md).toContain('## ' + t('Сұрақтар мен кілт сөздер', 'Вопросы и ключевые слова') + '\n\nСұраныс неге азаяды?');
    expect(md).toContain('- Тепе-теңдік баға');
  });

  await test.step('AI unavailable: the reason is shown, nothing breaks', async () => {
    const off = (route: import('@playwright/test').Route) => route.fulfill({ status: 503, json: { error: { code: 'off', message: t('AI-чат уақытша қолжетімсіз.', 'AI-чат временно недоступен.') } } });
    await page.route('**/api/chat', off);
    await page.getByRole('button', { name: t('Қорытып, карточка жаса', 'Итог и карточки'), exact: true }).click();
    await expect(page.locator('.ai-err')).toHaveText(t('AI-чат уақытша қолжетімсіз.', 'AI-чат временно недоступен.'));
    await page.unroute('**/api/chat', off);
  });

  await test.step('AI summary and cards, edited before saving, become a deck of the subject', async () => {
    let sent = '';
    await page.route('**/api/chat', async route => {
      const body = route.request().postDataJSON();
      sent = body.messages.at(-1).content;
      expect(body.messages).toHaveLength(1);
      await route.fulfill({ json: { text: AI_ANSWER } });
    });
    await page.getByRole('button', { name: t('Қорытып, карточка жаса', 'Итог и карточки'), exact: true }).click();
    await expect(page.getByLabel(t('Сұрақ 1', 'Вопрос 1'))).toHaveValue('Сұраныс заңы');
    expect(sent).toContain('Баға өскенде сұраныс');
    expect(sent.length).toBeLessThanOrEqual(2000);
    await expect(page.locator('#aiSum')).toHaveValue('Сұраныс баға өскенде азаяды.');
    await page.getByLabel(t('Жауап 2', 'Ответ 2')).fill('Пайыздық өзгеріс');
    await fits(page);
    await page.getByRole('button', { name: t('Жиынға сақтау', 'Сохранить в набор') }).click();
    const s = await deviceState(page);
    expect(s.decks).toHaveLength(1);
    expect(s.decks[0].subject).toBe(new URL(subjectUrl).searchParams.get('id'));
    expect(s.decks[0].cards).toEqual([{ q: 'Сұраныс заңы', a: 'Баға өссе, сұраныс азаяды' }, { q: 'Икемділік', a: 'Пайыздық өзгеріс' }]);
    // the summary went into the note's summary zone
    await expect(page.getByRole('region', { name: t('Қорытынды', 'Итог'), exact: true }).getByRole('textbox')).toHaveCount(2);
  });

  await test.step('spaced repetition: «Жақсы» moves the card two days ahead; home shows what is still due', async () => {
    await page.goto(subjectUrl);
    await expect(page.getByText(t('Бүгін қайталау: 2 карточка', 'Повторить сегодня: 2 карточки'))).toBeVisible();
    await page.getByRole('button', { name: t('Қайталауды бастау', 'Начать повторение') }).click();
    await expect(page).toHaveURL(/\/review\?id=/);
    await expect(page.locator('.flash .q')).toHaveText('Сұраныс заңы');
    await page.getByRole('button', { name: t('Жауапты көрсету', 'Показать ответ') }).click();
    await expect(page.locator('.flash .q')).toHaveText('Баға өссе, сұраныс азаяды');
    await fits(page);
    await page.getByRole('group', { name: t('Қаншалықты есте қалды?', 'Насколько хорошо помните?') }).getByRole('button', { name: new RegExp('^' + t('Жақсы', 'Хорошо')) }).click();
    await expect(page.locator('#reviewLeft')).toContainText('1');
    await expect.poll(async () => (await idbAll(page, 'srs')).map(x => x.due)).toEqual([plusDays(almatyToday(), 2)]);

    // the subjects section has no entry points any more: the home «next class» card shows no subject link
    const morning = new Date(); morning.setHours(8, 0, 0, 0);
    await page.clock.setFixedTime(morning);
    await page.goto('/');
    await expect(page.locator('#homeReview')).toHaveText(t('Бүгін қайталау: 1 карточка', 'Повторить сегодня: 1 карточка'));
    await expect(page.locator('#ncSubject')).toHaveCount(0);
  });
});

test('notes: search, a lecture template from the notes list, and offline opening', async ({ page, context, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await seedTimetable(page);
  await page.goto('/notes');
  await page.getByRole('button', { name: t('Жаңа конспект', 'Новый конспект') }).click();
  const sheet = page.getByRole('dialog');
  await sheet.getByLabel(t('Пән', 'Предмет')).selectOption({ label: 'Статистика' });
  await sheet.getByRole('button', { name: new RegExp('^' + t('Лекция', 'Лекция')) }).click();
  await expect(page).toHaveURL(/\/notes\?id=/);
  const main = page.getByRole('region', { name: t('Конспект мәтіні', 'Текст конспекта'), exact: true });
  await expect(main.getByRole('textbox').first()).toHaveValue(t('Негізгі ойлар', 'Главные мысли'));
  await main.getByRole('textbox').nth(1).fill('Медиана — реттелген қатардың ортасы');
  await expect(page.locator('#noteStatus')).toHaveText(new RegExp('^' + t('Сақталды', 'Сохранено')));
  await fits(page);

  await page.goto('/notes');
  await page.getByRole('searchbox', { name: t('Конспектілерден іздеу', 'Поиск по конспектам') }).fill('медиана');
  await expect(page.locator('#noteList .row')).toHaveCount(1);
  await page.getByRole('searchbox').fill('химия');
  await expect(page.locator('#noteList')).toHaveCount(0);
  await page.getByRole('searchbox').fill('');

  // the saved /notes page opens the note offline (the note id rides in the query)
  await expect.poll(() => page.evaluate(async () => !!(await caches.match(location.origin + '/notes'))), { timeout: 20_000 }).toBe(true);
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
  await page.locator('#noteList .row').first().click();
  await expect(page).toHaveURL(/\/notes\?id=n/);
  const url = page.url();
  await context.setOffline(true);
  await page.goto(url);
  await expect(page.getByRole('region', { name: t('Конспект мәтіні', 'Текст конспекта'), exact: true }).getByRole('textbox').nth(1)).toHaveValue('Медиана — реттелген қатардың ортасы');
  await expect(page.getByRole('button', { name: t('Қорытып, карточка жаса', 'Итог и карточки'), exact: true })).toBeDisabled();
  await expect(page.locator('#aiNoteWhy')).toContainText(t('интернет керек', 'нужен интернет'));
  await context.setOffline(false);
});
