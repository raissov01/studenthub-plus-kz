/* Run against the explicitly started dev app + mock. PLAYWRIGHT_MODULE may point to an
   existing Playwright installation; no test or mock code is imported by the application. */
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.QUIZ_TEST_URL || 'http://127.0.0.1:18198';
const mock = process.env.QUIZ_MOCK_URL || 'http://127.0.0.1:18197';
const fixtures = process.env.QUIZ_FIXTURES || '/tmp/studenthub-quiz-fixtures';
const artifacts = process.env.QUIZ_ARTIFACTS || '/tmp/studenthub-quiz-artifacts';
await mkdir(artifacts, { recursive: true });
const browser = await chromium.launch({ headless: true });
let checks = 0;
const errors = [];
const watch = (p) => {
  p.on('pageerror', (e) => errors.push(e.message));
  p.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text());
  });
  p.on('dialog', (d) => d.accept());
};
async function fit(page, name) {
  await page.evaluate(() => document.fonts.ready);
  const bad = await page.evaluate(() => {
    const width = document.documentElement.clientWidth;
    const result = [];
    if (document.documentElement.scrollWidth > width + 1) result.push('document overflow');
    for (const el of document.querySelectorAll(
      '.quiz button,.quiz label,.quiz h1,.quiz h2,.quiz h3,.quiz p,.quiz a,.quiz legend,.quiz span',
    )) {
      const b = el.getBoundingClientRect();
      if (!b.width || !b.height) continue;
      if (b.right > width + 1 || b.left < -1)
        result.push(`${el.tagName}: outside viewport: ${el.textContent.slice(0, 70)}`);
      const s = getComputedStyle(el);
      if ((s.overflowX === 'hidden' || s.textOverflow === 'ellipsis') && el.scrollWidth > el.clientWidth + 1)
        result.push(`clipped: ${el.textContent.slice(0, 70)}`);
    }
    return result;
  });
  assert.deepEqual(bad, [], name);
  checks++;
}
async function visible(locator) {
  await locator.waitFor({ state: 'visible', timeout: 30000 });
}
try {
  for (const lang of ['kk', 'ru'])
    for (const width of [320, 360, 390]) {
      if (process.env.QUIZ_CASE && process.env.QUIZ_CASE !== `${lang}-${width}`) continue;
      const t = (kk, ru) => (lang === 'kk' ? kk : ru);
      const context = await browser.newContext({
        viewport: { width, height: 850 },
        permissions: ['clipboard-read', 'clipboard-write'],
      });
      await context.addInitScript(
        (l) => localStorage.setItem('studenthub.v1', JSON.stringify({ lang: l, onboarded: true })),
        lang,
      );
      const page = await context.newPage();
      watch(page);
      const goto = async (path) => {
        await page.goto(base + path);
        await visible(page.locator('.quiz h1').first());
      };
      await goto('/quiz');
      await visible(page.getByRole('heading', { name: 'Математика / Математика', exact: true }));
      await fit(page, `${lang}/${width} discover`);
      await goto('/quiz/new');
      await page.getByLabel(t('Атауы', 'Название'), { exact: true }).fill(`Manual ${lang} ${width}`);
      await page.getByLabel(t('Сұрақ', 'Вопрос'), { exact: true }).fill('2 + 2 = ?');
      for (let i = 1; i <= 3; i++)
        await page.getByLabel(`${t('Нұсқа', 'Вариант')} ${i}`, { exact: true }).fill(String(i + 3));
      await page.getByLabel(t('Кім көре алады', 'Кто может видеть')).selectOption('unlisted');
      await fit(page, `${lang}/${width} manual editor`);
      await page.getByRole('button', { name: t('Сақтау', 'Сохранить'), exact: true }).click();
      await visible(page.getByRole('link', { name: t('Жаттығуды бастау', 'Начать тренировку') }));
      const manual = new URL(page.url()).pathname;
      await page.getByRole('button', { name: t('Бөлісу', 'Поделиться'), exact: true }).click();
      await visible(page.getByRole('dialog'));
      await fit(page, `${lang}/${width} share`);
      await page.getByRole('button', { name: t('Сілтемені көшіру', 'Копировать ссылку') }).click();
      await visible(page.getByRole('button', { name: t('Көшірілді', 'Скопировано') }));
      assert.match(await page.evaluate(() => navigator.clipboard.readText()), /\/quiz\/s\/share/);
      checks++;
      await page.keyboard.press('Escape');
      assert.equal(await page.getByRole('dialog').count(), 0);
      await page.getByRole('link', { name: t('Жаттығуды бастау', 'Начать тренировку') }).click();
      await visible(page.getByRole('radio', { name: '5', exact: true }));
      await page.getByRole('radio', { name: '5', exact: true }).check();
      await fit(page, `${lang}/${width} play`);
      await page.getByRole('button', { name: t('Аяқтау', 'Завершить'), exact: true }).click();
      await visible(page.getByRole('heading', { name: t('Нәтиже', 'Результат'), exact: true }));
      await fit(page, `${lang}/${width} results`);
      assert.equal(await page.locator('.quiz-score').textContent(), '0%');
      await page.getByRole('button', { name: t('Қате жауаптарды қайталау', 'Повторить ошибки') }).click();
      await page.getByRole('radio', { name: '4', exact: true }).check();
      await page.getByRole('button', { name: t('Аяқтау', 'Завершить'), exact: true }).click();
      await visible(page.locator('.quiz-score'));
      assert.equal(await page.locator('.quiz-score').textContent(), '100%');
      checks++;
      await goto('/quiz?tab=history');
      await visible(page.locator(`a[href^="${manual}/play?attempt="]`).first());
      await fit(page, `${lang}/${width} history`);
      await goto('/quiz/new');
      await page.getByRole('button', { name: t('Мәтінді қою', 'Вставить текст'), exact: true }).click();
      await page
        .getByLabel(t('Дәріс мәтіні', 'Текст лекции'), { exact: true })
        .fill('Two plus two equals four. Three is greater than two. '.repeat(8));
      await page.getByRole('button', { name: t('Квиз құрастыру', 'Составить квиз') }).click();
      await visible(page.getByRole('heading', { name: t('Жобаны тексеру', 'Проверка черновика') }));
      assert.equal(await page.locator('.quiz-quote').count(), 5);
      await page.getByLabel(t('Сұрақ', 'Вопрос'), { exact: true }).first().fill('Edited source question');
      await page
        .getByRole('button', { name: t('Сұрақты жою', 'Удалить вопрос'), exact: true })
        .last()
        .click();
      assert.equal(await page.locator('.quiz-quote').count(), 4);
      await fit(page, `${lang}/${width} draft review`);
      await page.screenshot({ path: `${artifacts}/draft-${lang}-${width}.png`, fullPage: true });
      await page.getByRole('button', { name: t('Сақтау', 'Сохранить'), exact: true }).click();
      await visible(page.getByRole('link', { name: t('Жаттығуды бастау', 'Начать тренировку') }));
      for (const ext of ['pdf', 'docx']) {
        await goto('/quiz/new?notes=1');
        assert.equal(await page.locator('input[capture="environment"]').count(), 1);
        await page
          .getByLabel(t('Конспект файлдары', 'Файлы конспекта'), { exact: true })
          .setInputFiles(`${fixtures}/lecture.${ext}`);
        await fit(page, `${lang}/${width} upload ${ext}`);
        await page.getByRole('button', { name: t('Квиз құрастыру', 'Составить квиз') }).click();
        await visible(page.getByRole('heading', { name: t('Жобаны тексеру', 'Проверка черновика') }));
        assert.equal(await page.locator('.quiz-quote').count(), 5);
        checks++;
      }
      await goto('/quiz/new');
      await page.getByRole('button', { name: t('Excel импорт', 'Импорт Excel'), exact: true }).click();
      await page
        .getByLabel(t('Excel файлы (.xlsx)', 'Файл Excel (.xlsx)'))
        .setInputFiles(`${fixtures}/questions.xlsx`);
      await visible(page.getByRole('heading', { name: t('Жобаны тексеру', 'Проверка черновика') }));
      await fit(page, `${lang}/${width} Excel`);
      await goto('/quiz/2/edit');
      await visible(page.getByLabel(t('Сұрақ түрі', 'Тип вопроса')).nth(11));
      await fit(page, `${lang}/${width} all type editors`);
      await goto('/quiz/2/play');
      for (let i = 0; i < 12; i++) {
        await fit(page, `${lang}/${width} type ${i}`);
        await page
          .getByRole('button', {
            name: i === 11 ? t('Аяқтау', 'Завершить') : t('Келесі', 'Далее'),
            exact: true,
          })
          .click();
      }
      await visible(page.getByRole('heading', { name: t('Нәтиже', 'Результат'), exact: true }));
      await fit(page, `${lang}/${width} all type review`);
      await goto('/quiz/1/host');
      await page.getByRole('button', { name: t('Ойын бөлмесін ашу', 'Открыть игровую комнату') }).click();
      await visible(page.locator('.quiz-live-code'));
      const code = await page.locator('.quiz-live-code').textContent();
      const players = [];
      for (let i = 0; i < 2; i++) {
        const p = await context.newPage();
        watch(p);
        players.push(p);
        await p.goto(`${base}/quiz/join?code=${code}`);
        await visible(p.getByLabel(t('Лақап ат', 'Никнейм')));
        await fit(p, `${lang}/${width} join`);
        await p.getByLabel(t('Лақап ат', 'Никнейм')).fill(`Player ${i + 1}`);
        await p.getByRole('button', { name: t('Қосылу', 'Присоединиться'), exact: true }).click();
        await visible(
          p.getByText(t('Жүргізуші ойынды бастағанша күтіңіз.', 'Дождитесь, пока ведущий начнёт игру.')),
        );
      }
      await visible(page.locator('.quiz-players li').nth(1));
      await fit(page, `${lang}/${width} host lobby`);
      await page.getByRole('button', { name: t('Бастау', 'Начать'), exact: true }).click();
      await visible(players[0].getByRole('radio', { name: '4', exact: true }));
      await players[0].getByRole('radio', { name: '4', exact: true }).check();
      await players[0].getByRole('button', { name: t('Жауап беру', 'Ответить'), exact: true }).click();
      await visible(
        players[0].getByText(t('Жауап қабылданды. Нәтижені күтіңіз.', 'Ответ принят. Дождитесь результата.')),
      );
      await fetch(`${mock}/__mock`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ disconnect: true }),
      });
      await visible(players[0].getByText(t('Байланыс орнатылды', 'Соединение установлено')));
      await visible(
        players[0].getByText(t('Жауап қабылданды. Нәтижені күтіңіз.', 'Ответ принят. Дождитесь результата.')),
      );
      await players[1].getByRole('radio', { name: '5', exact: true }).check();
      await players[1].getByRole('button', { name: t('Жауап беру', 'Ответить'), exact: true }).click();
      await visible(page.getByRole('heading', { name: t('Көшбасшылар', 'Лидеры') }));
      await fit(page, `${lang}/${width} leaderboard`);
      await page.getByRole('button', { name: t('Келесі сұрақ', 'Следующий вопрос'), exact: true }).click();
      for (const p of players) {
        await visible(p.getByRole('radio', { name: t('Дұрыс', 'Верно'), exact: true }));
        await fit(p, `${lang}/${width} live player`);
        await p.getByRole('radio', { name: t('Дұрыс', 'Верно'), exact: true }).check();
        await p.getByRole('button', { name: t('Жауап беру', 'Ответить'), exact: true }).click();
      }
      await page.getByRole('button', { name: t('Қорытынды', 'Итоги'), exact: true }).click();
      for (const p of [page, ...players]) {
        await visible(p.getByRole('heading', { name: t('Қорытынды орындар', 'Итоговый пьедестал') }));
        await fit(p, `${lang}/${width} podium`);
      }
      await page.screenshot({ path: `${artifacts}/podium-${lang}-${width}.png`, fullPage: true });
      await context.close();
      console.log(`PASS ${lang} ${width}px`);
    }
  // Mini App: private media and multipart upload must carry the in-memory Bearer token.
  {
    const context = await browser.newContext({ viewport: { width: 320, height: 850 } });
    await context.addInitScript(() => {
      localStorage.setItem('studenthub.v1', JSON.stringify({ onboarded: true, lang: 'kk' }));
      sessionStorage.setItem('studenthub.inTelegram', '1');
      const noop = () => {};
      window.Telegram = {
        WebApp: {
          initData: 'local-fixture',
          initDataUnsafe: {},
          colorScheme: 'light',
          themeParams: {},
          ready: noop,
          expand: noop,
          onEvent: noop,
          BackButton: { show: noop, hide: noop, onClick: noop, offClick: noop },
        },
      };
    });
    await context.route('**/api/auth/telegram/webapp', (route) =>
      route.fulfill({
        json: {
          user: { id: 1, lang: 'kk', name: 'Test', role: 'student', consentAt: '2026-10-29' },
          needsConsent: false,
          token: 'test-bearer-only',
        },
      }),
    );
    const mediaHeaders = [];
    await context.route('**/api/quiz/media*', (route) => {
      mediaHeaders.push(route.request().headers().authorization);
      return route.continue();
    });
    await context.route('**/api/quiz/media/**', (route) => {
      mediaHeaders.push(route.request().headers().authorization);
      return route.continue();
    });
    const page = await context.newPage();
    watch(page);
    await page.goto(`${base}/quiz/2/edit`);
    await visible(page.locator('.quiz-image img').first());
    await page
      .getByLabel('Сурет жүктеу (JPEG, PNG, WebP)', { exact: true })
      .first()
      .setInputFiles(`${fixtures}/photo.png`);
    await visible(page.locator('section.card').first().locator('.quiz-image img'));
    assert.ok(mediaHeaders.length >= 3);
    assert.ok(mediaHeaders.every((h) => h === 'Bearer test-bearer-only'));
    checks++;
    await fit(page, 'Mini App private image upload');
    // Exercise a real large camera-like PNG through the browser's JPEG compression path.
    const largePNG = await page.evaluate(() => {
      const canvas = document.createElement('canvas');
      canvas.width = 1000;
      canvas.height = 1000;
      const ctx = canvas.getContext('2d');
      const img = ctx.createImageData(1000, 1000);
      let seed = 17;
      for (let i = 0; i < img.data.length; i += 4) {
        for (let c = 0; c < 3; c++) {
          seed = (seed * 1664525 + 1013904223) >>> 0;
          img.data[i + c] = seed >>> 24;
        }
        img.data[i + 3] = 255;
      }
      ctx.putImageData(img, 0, 0);
      return canvas.toDataURL('image/png').split(',')[1];
    });
    const uploadResponse = page.waitForResponse(
      (r) => r.url().endsWith('/api/quiz/media') && r.request().method() === 'POST',
    );
    await page
      .getByLabel('Сурет жүктеу (JPEG, PNG, WebP)', { exact: true })
      .first()
      .setInputFiles({ name: 'camera.png', mimeType: 'image/png', buffer: Buffer.from(largePNG, 'base64') });
    const uploaded = await uploadResponse;
    assert.equal(uploaded.status(), 201);
    assert.ok(uploaded.request().postDataBuffer().length < 900 * 1024);
    assert.match(uploaded.request().postDataBuffer().toString('latin1').slice(0, 250), /camera\.jpg/);
    checks++;
    await page.goto(`${base}/quiz/new?notes=1`);
    await page
      .getByLabel('Конспект файлдары', { exact: true })
      .setInputFiles({
        name: 'large.docx',
        mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
        buffer: Buffer.alloc(5 * 1024 * 1024),
      });
    await page.getByRole('button', { name: 'Квиз құрастыру', exact: true }).click();
    await visible(
      page.getByRole('alert').filter({ hasText: 'Файлдардың жалпы көлемі 4 МБ-тан аспауы керек.' }),
    );
    await fit(page, 'oversized document error');
    checks++;

    await context.close();
  }
  // Guests can practise and retry locally; editing still requires an account.
  {
    const context = await browser.newContext({ viewport: { width: 320, height: 850 } });
    await context.addCookies([{ name: 'quizGuest', value: '1', url: base }]);
    await context.addInitScript(() =>
      localStorage.setItem('studenthub.v1', JSON.stringify({ onboarded: true, lang: 'kk' })),
    );
    const page = await context.newPage();
    watch(page);
    await page.goto(`${base}/quiz/new`);
    await visible(page.getByRole('button', { name: 'Кіру', exact: true }));
    assert.equal(await page.getByLabel('Атауы', { exact: true }).count(), 0);
    checks++;
    await page.goto(`${base}/quiz/1/play`);
    await page.getByRole('radio', { name: '5', exact: true }).check();
    await page.getByRole('button', { name: 'Келесі', exact: true }).click();
    await page.getByRole('radio', { name: 'Дұрыс', exact: true }).check();
    await page.getByRole('button', { name: 'Аяқтау', exact: true }).click();
    await visible(page.getByText('Қонақ нәтижесі сақталмайды. Қателерді осы бетте қайталауға болады.'));
    await page.getByRole('button', { name: 'Қате жауаптарды қайталау' }).click();
    await page.getByRole('radio', { name: '4', exact: true }).check();
    await page.getByRole('button', { name: 'Аяқтау', exact: true }).click();
    await visible(page.locator('.quiz-score'));
    assert.equal(await page.locator('.quiz-score').textContent(), '100%');
    checks++;
    await context.route('**/api/me', (route) =>
      route.fulfill({ json: { user: { id: 1 }, needsConsent: true } }),
    );
    await page.goto(`${base}/quiz/new`);
    await visible(page.getByRole('button', { name: 'Келісім беру', exact: true }));
    checks++;
    await context.close();
  }
  const context = await browser.newContext();
  await context.addInitScript(() =>
    localStorage.setItem('studenthub.v1', JSON.stringify({ onboarded: true })),
  );
  const page = await context.newPage();
  watch(page);
  const metadata = await (await fetch(`${base}/quiz/1`, { headers: { 'user-agent': 'Twitterbot' } })).text();
  assert.match(metadata, /property="og:title" content="Математика/);
  checks++;
  await fetch(`${mock}/__mock`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{"enabled":false}',
  });
  await page.goto(`${base}/quiz/new`);
  await visible(page.getByText('Мұндай бет жоқ.', { exact: true }));
  assert.equal(await page.locator('.quiz').count(), 0);
  checks++;
  await page.goto(base);
  await page.waitForTimeout(500);
  assert.equal(await page.getByRole('button', { name: 'Квиздер', exact: true }).count(), 0);
  await fetch(`${mock}/__mock`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{"enabled":true}',
  });
  await context.close();
  // The explicit disabled-route 404 is expected; every running flow must be console-clean.
  const unexpected = errors.filter((e) => !e.includes('404 (Not Found)'));
  assert.deepEqual(unexpected, []);
  console.log(`PASS ${checks} checks; console/page errors: ${unexpected.length}; screenshots: ${artifacts}`);
} catch (e) {
  console.log('BROWSER ERRORS', errors);
  for (const c of browser.contexts())
    for (const [i, p] of c.pages().entries()) {
      console.log(
        'PAGE',
        i,
        p.url(),
        await p
          .locator('main')
          .innerText()
          .catch(() => ''),
      );
      await p.screenshot({ path: `${artifacts}/failure-${i}.png`, fullPage: true }).catch(() => {});
    }
  throw e;
} finally {
  await browser.close();
}
