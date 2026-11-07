/* English UI: every Kazakh/Russian pair in the code has an English entry (CI fails on a missing one), and tt()/L()
   answer in English with the documented fallbacks. Run: npx tsx --test src/lib/i18n/en.test.ts */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { extractAll, extractSource } from '../../../tools/i18n-extract.mjs';
import { EN } from './en';
import { english, fill, lt, setDictionary, tr } from './index';

type Pair = { kk: string; ru: string; files: string[] };
const holes = (s: string) => [...s.matchAll(/\{(\d+)(?:\|[^}]*)?\}/g)].map(m => m[1]).sort().join(',');

test('every extracted UI string has an English entry', () => {
  const pairs = extractAll() as Pair[];
  assert.ok(pairs.length > 1000, `only ${pairs.length} pairs extracted`);
  const missing = pairs.filter(p => !(p.kk in EN)).map(p => `${p.files[0]}: ${JSON.stringify(p.kk)}`);
  assert.deepEqual(missing, [], `${missing.length} strings without English — run node tools/i18n-translate.mjs (or add them to src/lib/i18n/en.ts by hand)`);
});

test('English entries keep the template holes and contain no Kazakh letters', () => {
  const bad: string[] = [];
  for (const [key, en] of Object.entries(EN)) {
    const kk = key.split('\u2016')[0];
    if (holes(kk.replace(/\{(\d+)\}/g, '{$1}')) !== [...new Set(holes(en).split(','))].filter(Boolean).sort().join(',')) bad.push(`holes: ${kk} -> ${en}`);
    if (/[ӘәҒғҚқҢңӨөҰұҮүҺһІі]/.test(en)) bad.push(`Kazakh letters: ${en}`);
    if (!en.trim() && kk.trim()) bad.push(`empty: ${kk}`);
  }
  assert.deepEqual(bad, []);
});

test('the extractor finds calls, pairs, LText objects and templates', () => {
  const found = extractSource(`
    const a = tt('Сақтау', 'Сохранить');
    const b = tr(lang, 'Іздеу', 'Поиск');
    const c: Pair = ['Жұмыс', 'Работа'];
    const d = { kk: 'Іс-шара', ru: 'Мероприятие' };
    const e = { kk: 'Skip', ru: 'Пропуск', en: 'Skip' };
    const f = tt(\`\${n} күн қалды\`, \`Осталось \${n}\`);
    const g = go('home', 'search');
  `) as { kk: string; ru: string }[];
  assert.deepEqual(found.map(p => p.kk), ['Сақтау', 'Іздеу', 'Жұмыс', 'Іс-шара', '{0} күн қалды']);
});

test('tt: dictionary, templates with plurals, Russian fallback', () => {
  setDictionary({ 'Сақтау': 'Save', '{0} күн қалды': '{0} {0|day|days} left', '{0}-курс, {1}': 'Year {0}, {1}' });
  try {
    assert.equal(tr('en', 'Сақтау', 'Сохранить'), 'Save');
    assert.equal(tr('kk', 'Сақтау', 'Сохранить'), 'Сақтау');
    assert.equal(tr('ru', 'Сақтау', 'Сохранить'), 'Сохранить');
    assert.equal(tr('en', '1 күн қалды', 'Остался 1 день'), '1 day left');
    assert.equal(tr('en', '12 күн қалды', 'Осталось 12 дней'), '12 days left');
    assert.equal(tr('en', '2-курс, экономика', '2 курс, экономика'), 'Year 2, экономика');
    assert.equal(tr('en', 'Жоқ кілт', 'Нет ключа'), 'Нет ключа', 'missing key -> Russian');
    assert.equal(tr('en', 'SIVA', 'Студент Хаб'), 'SIVA', 'no Cyrillic in kk -> kk as is');
    assert.equal(english('Басқа'), null);
    // L(): content from the API has no English -> Russian; our own LText goes through the dictionary
    assert.equal(lt('en', { kk: 'Сақтау', ru: 'Сохранить' }), 'Save');
    assert.equal(lt('en', { kk: 'Вакансия атауы', ru: 'Название вакансии' }), 'Название вакансии');
    assert.equal(lt('en', { kk: 'а', ru: 'б', en: 'own' }), 'own');
    assert.equal(lt('kk', { kk: 'а', ru: 'б' }), 'а');
  } finally { setDictionary(null); }
  assert.equal(tr('en', 'Сақтау', 'Сохранить'), 'Сохранить', 'dictionary not loaded yet -> Russian');
});

test('fill picks plural forms and keeps unknown holes empty', () => {
  assert.equal(fill('{0} {0|coin|coins}', { 0: '1' }), '1 coin');
  assert.equal(fill('{0} {0|coin|coins}', { 0: '1 000' }), '1 000 coins');
  assert.equal(fill('{1} of {0}', { 0: '3', 1: '2' }), '2 of 3');
});
