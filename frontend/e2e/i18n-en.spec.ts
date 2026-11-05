/* English UI (src/lib/i18n): switch to English, then the main screens must show no Kazakh UI strings. Kazakh-specific
   letters are searched in the DOM text and the user-facing attributes, outside elements marked with another language
   (the «Қаз» button) and outside content from the API (fixture strings: English users see its Russian text, or the
   Kazakh one where the item has no Russian). Runs once (kk-390 project): the language is chosen inside the test. */
import type { Page } from '@playwright/test';
import { test, expect, deviceState } from './fixtures';
import { content } from './content';
import { EN } from '../src/lib/i18n/en';

const KAZAKH = /[ӘәҒғҚқҢңӨөҰұҮүҺһІі]/;
const en = (kk: string) => {
  const v = EN[kk];
  if (!v) throw new Error(`no English for ${kk}`);
  return v;
};

/** every string in the API fixtures: the only Kazakh an English screen may show */
function contentStrings(v: unknown, out = new Set<string>()): Set<string> {
  if (typeof v === 'string') out.add(v);
  else if (Array.isArray(v)) v.forEach(x => contentStrings(x, out));
  else if (v && typeof v === 'object') Object.values(v).forEach(x => contentStrings(x, out));
  return out;
}
const CONTENT = [...contentStrings(content)].filter(s => KAZAKH.test(s));

async function kazakhLeft(page: Page): Promise<string[]> {
  const found = await page.evaluate((src: string) => {
    const re = new RegExp(src);
    const out: string[] = [];
    const foreign = (el: Element | null) => !!el?.closest('[lang]:not([lang="en"]):not(html)');
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const el = n.parentElement;
      if (!el || el.closest('script,style,noscript,[hidden],[aria-hidden="true"]') || foreign(el)) continue;
      if (!el.checkVisibility?.()) continue;
      const text = (n.textContent || '').trim();
      if (re.test(text)) out.push(text);
    }
    for (const el of document.body.querySelectorAll('[placeholder],[aria-label],[title],[alt]')) {
      if (foreign(el) || !(el as HTMLElement).checkVisibility?.()) continue;
      for (const a of ['placeholder', 'aria-label', 'title', 'alt']) {
        const v = el.getAttribute(a);
        if (v && re.test(v)) out.push(`${a}=${v}`);
      }
    }
    if (re.test(document.title)) out.push(`title=${document.title}`);
    return out;
  }, KAZAKH.source);
  return found.filter(t => !CONTENT.some(c => t.includes(c) || c.includes(t.replace(/^\w+=/, ''))));
}

test.beforeEach(({}, info) => { test.skip(info.project.name !== 'kk-390', 'English is chosen inside the test; one viewport is enough'); });

test('switch to English: home, search, calendar and profile have no Kazakh UI strings', async ({ page }) => {
  await page.goto('/');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('tab', { name: 'English', exact: true }).click();
  await expect(dialog.getByRole('heading', { name: en('Қош келдіңіз!') })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await dialog.locator('#fName').fill('Aruzhan Test');
  await dialog.locator('#fSpec').selectOption('econ');
  await dialog.locator('#fCourse').selectOption('2');
  await dialog.getByRole('button', { name: en('Бастау'), exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(await deviceState(page)).toMatchObject({ onboarded: true, lang: 'en' });

  for (const path of ['/', '/search', '/calendar', '/profile']) {
    await page.goto(path);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    // the dictionary is a separate chunk: wait until the tab bar speaks English
    await expect(page.getByText(en('Іздеу'), { exact: true }).first()).toBeVisible();
    await expect.poll(() => kazakhLeft(page), { message: `Kazakh UI strings on ${path}` }).toEqual([]);
  }

  // dates and money follow the English locale; content without English shows its Russian text
  await page.goto('/search?k=jobs');
  await expect(page.getByText('Помощник библиотеки').first()).toBeVisible();
  await expect(page.getByText(/1,500 ₸/).first()).toBeVisible();

  // and back to Kazakh from the settings
  await page.goto('/settings');
  await page.getByRole('tab', { name: 'Қазақша', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'kk');
  await expect(page.getByRole('heading', { name: 'Тіл', exact: true })).toBeVisible();
});
