import assert from 'node:assert/strict';
import test from 'node:test';
import { categoryLabel, CERT_LIMITS, checkCertFile, cleanCertMeta, downloadName, fitSide, fmtBytes, isCategory, problemText, sniffCert, type CertMeta } from './certfiles';

const bytes = (s: string) => Uint8Array.from(s, c => c.charCodeAt(0));
const tt = (kk: string) => kk;

test('the real type comes from the bytes, not the name', () => {
  assert.equal(sniffCert(bytes('%PDF-1.7\n%âãÏÓ')), 'pdf');
  assert.equal(sniffCert(bytes('%PDF-2.0\n')), 'pdf');
  assert.equal(sniffCert(bytes('%PDF-x.y\n')), '');
  assert.equal(sniffCert(bytes('MZ\x90\x00\x03\x00\x00\x00\x04\x00')), ''); // a Windows .exe renamed to .pdf
  assert.equal(sniffCert(bytes('<!DOCTYPE html><html>')), '');
  assert.equal(sniffCert(bytes('<svg xmlns="http://www.w3.org/2000/svg">')), '');
  assert.equal(sniffCert(bytes('\xff\xd8\xff\xe0\x00\x10JFIF')), 'jpeg');
  assert.equal(sniffCert(bytes('\x89PNG\r\n\x1a\n\x00\x00')), 'png');
  assert.equal(sniffCert(bytes('RIFF\x00\x00\x00\x00WEBPVP8 ')), 'webp');
  assert.equal(sniffCert(bytes('GIF89a')), '');
  assert.equal(sniffCert(new Uint8Array()), '');
});

test('size limits: PDF 10 MB, photo 8 MB', () => {
  const pdf = bytes('%PDF-1.4\n'), jpg = bytes('\xff\xd8\xff\xe0');
  assert.deepEqual(checkCertFile(CERT_LIMITS.pdf, pdf), { kind: 'pdf', problem: '' });
  assert.deepEqual(checkCertFile(CERT_LIMITS.pdf + 1, pdf), { kind: 'pdf', problem: 'too_large' });
  assert.deepEqual(checkCertFile(CERT_LIMITS.image, jpg), { kind: 'image', problem: '' });
  assert.deepEqual(checkCertFile(CERT_LIMITS.image + 1, jpg), { kind: 'image', problem: 'too_large' });
  assert.deepEqual(checkCertFile(500, bytes('MZ\x90\x00')), { kind: null, problem: 'type' });
  assert.deepEqual(checkCertFile(0, pdf), { kind: null, problem: 'empty' });
  assert.match(problemText('type', tt), /PDF, JPG/);
  assert.equal(problemText('', tt), '');
});

test('sizes and image fitting', () => {
  assert.equal(fmtBytes(512), '512 Б');
  // edges: empty usage, a bad number, rounding across a unit
  assert.equal(fmtBytes(0), '0 Б');
  assert.equal(fmtBytes(-5), '0 Б');
  assert.equal(fmtBytes(NaN), '0 Б');
  assert.equal(fmtBytes(0.4), '1 Б');
  assert.equal(fmtBytes(1023.7), '1 КБ');
  assert.equal(fmtBytes(1024 * 1023.8), '1,0 МБ');
  assert.equal(fmtBytes(200 * 1024), '200 КБ');
  assert.equal(fmtBytes(1.5 * 1024 * 1024), '1,5 МБ');
  assert.equal(fmtBytes(100 * 1024 * 1024), '100 МБ');
  assert.deepEqual(fitSide(4000, 3000), { w: 1600, h: 1200 });
  assert.deepEqual(fitSide(900, 3200), { w: 450, h: 1600 });
  assert.deepEqual(fitSide(800, 600), { w: 800, h: 600 });
});

test('metadata is cleaned the way the server checks it', () => {
  const base: CertMeta = { title: '  IELTS Academic  ', issuer: 'British Council', issuedOn: '2026-05-12', credentialId: ' A1 ', credentialUrl: '', category: 'language' };
  const today = new Date('2026-11-05T00:00:00Z');
  assert.deepEqual(cleanCertMeta(base, today), { meta: { ...base, title: 'IELTS Academic', credentialId: 'A1' }, problem: '' });
  assert.equal(cleanCertMeta({ ...base, title: '   ' }, today).problem, 'title');
  assert.equal(cleanCertMeta({ ...base, title: 'x'.repeat(121) }, today).problem, 'title');
  assert.equal(cleanCertMeta({ ...base, credentialUrl: 'javascript:alert(1)' }, today).problem, 'credentialUrl');
  assert.equal(cleanCertMeta({ ...base, credentialUrl: 'http://example.org' }, today).problem, 'credentialUrl');
  assert.equal(cleanCertMeta({ ...base, credentialUrl: 'example.org/verify/1' }, today).meta.credentialUrl, 'https://example.org/verify/1');
  assert.equal(cleanCertMeta({ ...base, credentialUrl: 'https://x.org/a b' }, today).problem, 'credentialUrl');
  assert.equal(cleanCertMeta({ ...base, issuedOn: '2027-01-01' }, today).problem, 'issuedOn');
  assert.equal(cleanCertMeta({ ...base, issuedOn: '1900-01-01' }, today).problem, 'issuedOn');
  assert.equal(cleanCertMeta({ ...base, issuedOn: '' }, today).meta.issuedOn, null);
  assert.equal(cleanCertMeta({ ...base, category: 'money' as CertMeta['category'] }, today).meta.category, 'other');
});

test('categories and download names', () => {
  assert.ok(isCategory('olympiad'));
  assert.ok(!isCategory('verified'));
  assert.equal(categoryLabel('language').ru, 'Языковой тест (IELTS и др.)');
  assert.equal(categoryLabel('nope').kk, 'Басқа');
  assert.equal(downloadName('../../etc/passwd', 'pdf'), 'etc passwd.pdf');
  assert.equal(downloadName('Олимпиада: 1-орын', 'image', 'image/webp'), 'Олимпиада 1-орын.webp');
  assert.equal(downloadName('   ', 'image'), 'certificate.jpg');
});
