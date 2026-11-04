import { readFileSync } from 'node:fs';
import path from 'node:path';
import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

const PDF = path.join(__dirname, 'files', 'certificate.pdf');
const pdfBytes = readFileSync(PDF);
const EMPTY_TEXT = (lang: Lang) => label(lang, 'Сертификатыңызды PDF не сурет ретінде қосыңыз', 'Добавьте сертификат как PDF или фото');
const FILE_NAME = 'a'.repeat(32) + '.pdf';

const portfolioMe = {
  settings: {
    enabled: true, slug: 'k3m9x2p7qa', url: 'http://127.0.0.1/p/k3m9x2p7qa', displayName: null, headline: null, bio: null,
    showUniversity: true, showCourse: true, showEvents: true, showCerts: true, showExperience: true, showSkills: true, showBadges: true,
    showContact: false, syncConsentAt: null, slugRotatedAt: null, updatedAt: '2026-11-01T00:00:00Z',
  },
  items: [],
  preview: {
    noindex: true, profile: { name: 'Аружан Тест', headline: null, bio: null, university: null, course: null },
    verified: { events: [], certificates: [], badges: [] },
    selfReported: { certificates: [], files: [], experience: [], skills: [], badges: [] },
    contact: null, og: { title: 'x', description: null, image: 'http://127.0.0.1/og.png', url: 'http://127.0.0.1/p/k3m9x2p7qa' }, site: 'http://127.0.0.1',
  },
};

/** a signed-in student with the portfolio switch on; the certificate API is a small in-memory server */
async function signedIn(page: Page, lang: Lang) {
  const calls: { method: string; path: string; body?: unknown; multipart?: string }[] = [];
  let certs: Record<string, unknown>[] = [];
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, portfolio: true } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 501, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'IT' }, needsConsent: false, inboxUnread: 0 } }));
  await page.route('**/api/portfolio/me', route => route.fulfill({ json: portfolioMe }));
  const usage = () => ({ files: certs.length, bytes: certs.reduce((n, c) => n + (c.bytes as number), 0), maxFiles: 30, maxBytes: 100 * 1024 * 1024 });
  await page.route('**/api/portfolio/certificates**', async route => {
    const req = route.request();
    const p = new URL(req.url()).pathname;
    const id = Number(p.split('/').pop());
    switch (req.method()) {
      case 'GET':
        calls.push({ method: 'GET', path: p });
        return route.fulfill({ json: { certificates: certs, usage: usage(), pageVisible: true } });
      case 'POST': {
        const body = req.postDataBuffer()?.toString('latin1') ?? '';
        calls.push({ method: 'POST', path: p, multipart: body });
        const field = (name: string) => body.match(new RegExp(`name="${name}"\\r\\n\\r\\n([^\\r]*)`))?.[1] ?? '';
        const cert = {
          id: 41, title: Buffer.from(field('title'), 'latin1').toString('utf8'), issuer: Buffer.from(field('issuer'), 'latin1').toString('utf8'),
          issuedOn: field('issuedOn') || null, credentialId: '', credentialUrl: field('credentialUrl'), category: field('category'),
          kind: 'pdf', mime: 'application/pdf', bytes: pdfBytes.length, width: null, height: null, public: false,
          url: `/api/portfolio/files/${FILE_NAME}`, createdAt: '2026-11-05T05:00:00Z',
        };
        certs = [cert, ...certs];
        return route.fulfill({ status: 201, json: { certificate: cert, usage: usage() } });
      }
      case 'PATCH': {
        const body = req.postDataJSON();
        calls.push({ method: 'PATCH', path: p, body });
        certs = certs.map(c => (c.id === id ? { ...c, ...body } : c));
        return route.fulfill({ json: { certificate: certs.find(c => c.id === id) } });
      }
      case 'DELETE':
        calls.push({ method: 'DELETE', path: p });
        certs = certs.filter(c => c.id !== id);
        return route.fulfill({ json: { deleted: true, usage: usage() } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_certs' } } });
  });
  await page.route('**/api/portfolio/files/**', route => {
    calls.push({ method: 'GET', path: new URL(route.request().url()).pathname });
    return route.fulfill({
      body: pdfBytes, contentType: 'application/pdf',
      headers: { 'x-content-type-options': 'nosniff', 'content-security-policy': "default-src 'none'; sandbox", 'content-disposition': 'inline; filename="certificate.pdf"' },
    });
  });
  return calls;
}

async function addPdf(page: Page, lang: Lang, title: string) {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.getByRole('button', { name: '+ ' + t('Қосу', 'Добавить'), exact: true }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByRole('heading', { name: t('Сертификат қосу', 'Добавить сертификат') })).toBeVisible();
  // a renamed executable is refused before anything is sent or stored
  await sheet.locator('#cfFile').setInputFiles({ name: 'certificate.pdf', mimeType: 'application/pdf', buffer: Buffer.from('MZ\x90\x00\x03\x00\x00\x00 not a pdf') });
  await expect(sheet.getByRole('alert')).toContainText(t('Тек PDF, JPG немесе PNG', 'Можно добавить только PDF, JPG или PNG'));
  // the file input is reachable from the keyboard and labelled
  await sheet.locator('#cfFile').focus();
  await expect(sheet.locator('#cfFile')).toBeFocused();
  await expect(sheet.getByLabel(t('Файл таңдау', 'Выбрать файл'))).toHaveCount(1);
  await sheet.locator('#cfFile').setInputFiles(PDF);
  await expect(sheet.getByText('certificate.pdf')).toBeVisible();
  await sheet.getByLabel(t('Атауы', 'Название')).fill(title);
  await sheet.getByLabel(t('Берген ұйым', 'Кем выдан')).fill('British Council');
  await sheet.getByLabel(t('Берілген күні', 'Дата выдачи')).fill('2026-05-12');
  await sheet.getByLabel(t('Санаты', 'Категория')).selectOption('language');
  await fits(page);
  await sheet.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(sheet).not.toBeVisible();
}

async function openAndCheckViewer(page: Page, lang: Lang, title: string) {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const card = page.locator('.cf-card', { hasText: title });
  await card.getByRole('button', { name: t('Ашу', 'Открыть'), exact: true }).click();
  const viewer = page.getByRole('dialog');
  await expect(viewer.getByRole('heading', { name: title })).toBeVisible();
  const frame = viewer.locator('iframe.cf-pdf');
  await expect(frame).toHaveAttribute('src', /^blob:/);
  await expect(frame).toHaveAttribute('title', `PDF: ${title}`);
  const download = viewer.getByRole('link', { name: t('Жүктеп алу', 'Скачать') });
  await expect(download).toHaveAttribute('download', `${title.replace(/,/g, '')}.pdf`);
  // «Жүктеп алу» hands over the very PDF bytes
  const [file] = await Promise.all([page.waitForEvent('download'), download.click()]);
  expect(readFileSync(await file.path())).toEqual(pdfBytes);
  await fits(page);
  await viewer.getByRole('button', { name: t('Жабу', 'Закрыть') }).click();
  await expect(viewer).not.toBeVisible();
}

test('certificates: a signed-in student adds a PDF, opens it, makes it public and deletes it', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const calls = await signedIn(page, lang);
  await onboard(page, lang);
  await page.goto('/portfolio');
  await expect(page.getByText(EMPTY_TEXT(lang))).toBeVisible();
  const title = 'IELTS Academic 7.5';
  await addPdf(page, lang, title);

  const post = calls.find(c => c.method === 'POST');
  expect(post?.multipart).toContain('name="file"; filename="IELTS Academic 7.5.pdf"');
  expect(post?.multipart).toContain('Content-Type: application/pdf');
  expect(post?.multipart).toContain('%PDF-1.4');
  expect(post?.multipart).toMatch(/name="category"\r\n\r\nlanguage/);
  expect(post?.multipart).not.toMatch(/name="public"/);

  const card = page.locator('.cf-card', { hasText: title });
  await expect(card).toBeVisible();
  await expect(card.locator('.cf-thumb-pdf')).toContainText('PDF');
  await expect(card).toContainText('British Council');
  await expect(card).toContainText(t('Жеке', 'Личный'));
  await expect(page.getByText(EMPTY_TEXT(lang))).toHaveCount(0);

  await openAndCheckViewer(page, lang, title);
  expect(calls.some(c => c.method === 'GET' && c.path === `/api/portfolio/files/${FILE_NAME}`)).toBe(true);

  const toggle = card.getByLabel(t('Жария портфолиода көрсету', 'Показывать в публичном портфолио'));
  await expect(toggle).not.toBeChecked();
  await toggle.check();
  await expect(card).toContainText(t('Жария', 'Публичный'));
  await expect(toggle).toBeChecked();
  expect(calls.find(c => c.method === 'PATCH')?.body).toEqual({ public: true });

  await card.getByRole('button', { name: t('Жою', 'Удалить'), exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: t('Иә, жою', 'Да, удалить') }).click();
  await expect(card).toHaveCount(0);
  expect(calls.filter(c => c.method === 'DELETE').map(c => c.path)).toEqual(['/api/portfolio/certificates/41']);
  await expect(page.getByText(EMPTY_TEXT(lang))).toBeVisible();
  await fits(page);
});

test('certificates: signed out, the file stays on the device and survives a reload', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, portfolio: true } } }));
  await onboard(page, lang);
  await page.goto('/portfolio');
  await expect(page.getByText(EMPTY_TEXT(lang))).toBeVisible();
  const note = page.getByRole('note');
  await expect(note).toContainText(t('тек осы құрылғыда', 'только на этом устройстве'));
  await expect(note).toContainText(t('бөлісе аласыз', 'делиться'));
  const title = t('Олимпиада, 1-орын', 'Олимпиада, 1 место');
  await addPdf(page, lang, title);
  const card = page.locator('.cf-card', { hasText: title });
  await expect(card).toContainText(t('Құрылғыда', 'На устройстве'));
  // no public toggle without an account
  await expect(card.getByRole('checkbox')).toHaveCount(0);

  await page.reload();
  await expect(card).toBeVisible();
  await openAndCheckViewer(page, lang, title);

  await card.getByRole('button', { name: t('Өңдеу', 'Изменить') }).click();
  const sheet = page.getByRole('dialog');
  await sheet.getByLabel(t('Атауы', 'Название')).fill(title + ' (2026)');
  await sheet.getByRole('button', { name: t('Сақтау', 'Сохранить'), exact: true }).click();
  const edited = page.locator('.cf-card', { hasText: title + ' (2026)' });
  await expect(edited).toBeVisible();

  await edited.getByRole('button', { name: t('Жою', 'Удалить'), exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: t('Иә, жою', 'Да, удалить') }).click();
  await expect(page.locator('.cf-card')).toHaveCount(0);
  await page.reload();
  await expect(page.getByText(EMPTY_TEXT(lang))).toBeVisible();
});
