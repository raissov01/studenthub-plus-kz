import assert from 'node:assert/strict';
import test from 'node:test';
import {
  addMood, addToken, answersPayload, canContinue, detectFlag, EMERGENCY_LINES, flowPct, HELP_LINES, isAnswered, pct, readMood, readTokens,
  removeToken, SELF_HELP, telHref, templates, toggle, type Question,
} from './voice';

const mem = () => { const m = new Map<string, string>(); return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => { m.set(k, v); } }; };
const kk = (a: string) => a;

const qs: Question[] = [
  { id: 'q1', type: 'single', text: 'One', required: true, options: ['a', 'b'] },
  { id: 'q2', type: 'matrix', text: 'Grid', required: true, max: 5, rows: ['r1', 'r2'] },
  { id: 'q3', type: 'text', text: 'Text', required: false },
  { id: 'q4', type: 'multiple', text: 'Many', required: false, options: ['x', 'y', 'z'] },
];

test('answers: required questions block, optional ones are dropped when empty', () => {
  assert.equal(canContinue(qs[0], undefined), false);
  assert.equal(canContinue(qs[0], 1), true);
  assert.equal(isAnswered(qs[1], [3, 0]), false); // required matrix needs every row
  assert.equal(isAnswered(qs[1], [3, 4]), true);
  assert.equal(canContinue(qs[2], ''), true);
  assert.deepEqual(answersPayload(qs, { q1: 0, q2: [1, 5], q3: '  hi ', q4: [] }), { q1: 0, q2: [1, 5], q3: 'hi' });
  assert.deepEqual(toggle(toggle(undefined, 2), 0), [0, 2]);
  assert.deepEqual(toggle([0, 2], 2), [0]);
  assert.equal(flowPct(2, 4), 50);
  assert.equal(pct(1, 3), 33);
});

test('templates are structure only and every question is valid', () => {
  const ts = templates(kk);
  assert.deepEqual(ts.map(t => t.key), ['teacher', 'canteen', 'dorm']);
  for (const t of ts) {
    const ids = new Set(t.questions.map(q => q.id));
    assert.equal(ids.size, t.questions.length);
    for (const q of t.questions) {
      if (q.type === 'single' || q.type === 'multiple') assert.ok((q.options?.length ?? 0) >= 2);
      if (q.type === 'scale' || q.type === 'matrix') assert.ok(q.max === 5 || q.max === 10);
      if (q.type === 'matrix') assert.ok((q.rows?.length ?? 0) >= 2);
    }
  }
});

test('flag detection mirrors the server', () => {
  assert.equal(detectFlag('Жатақханада ыстық су жоқ'), '');
  assert.equal(detectFlag('Я убью его если ещё раз'), 'threat');
  assert.equal(detectFlag('не хочу жить, всё плохо'), 'threat');
  assert.equal(detectFlag('Өмір сүргім келмейді'), 'threat');
  assert.equal(detectFlag('Мен оны өлтіремін'), 'threat');
  assert.equal(detectFlag('Это блядство какое-то'), 'profanity');
  assert.equal(detectFlag('Ректорат пен деканат жауап бермейді'), '');
});

test('ticket tokens stay on the device, deduplicated and validated', () => {
  const s = mem();
  const tok = 'A'.repeat(43), tok2 = 'b'.repeat(43);
  addToken(s, { token: tok, id: 1, createdAt: '2026-11-05' });
  addToken(s, { token: tok2, id: 2, createdAt: '2026-11-05' });
  addToken(s, { token: tok, id: 1, createdAt: '2026-11-05' });
  assert.deepEqual(readTokens(s).map(t => t.id), [1, 2]);
  s.setItem('studenthub.feedback.v1', JSON.stringify([{ token: 'short', id: 3 }, { token: tok2, id: 2 }]));
  assert.deepEqual(readTokens(s).map(t => t.id), [2]);
  assert.deepEqual(removeToken(s, tok2), []);
  s.setItem('studenthub.feedback.v1', '{broken');
  assert.deepEqual(readTokens(s), []);
});

test('help lines: emergency first, every number has an official source', () => {
  assert.deepEqual(EMERGENCY_LINES.map(h => h.number), ['112', '103', '102', '101']);
  for (const h of HELP_LINES) {
    assert.ok(h.sources.length > 0 && h.sources.every(u => u.startsWith('https://')));
    assert.ok(h.sources.some(u => u.startsWith('https://www.gov.kz/')), h.number);
  }
  assert.ok(SELF_HELP.every(c => c.source.url.startsWith('https://')));
  assert.equal(telHref('+7 708 106 08 10'), 'tel:+77081060810');
});

test('mood check-in: one per day, newest 30, device only', () => {
  const s = mem();
  addMood(s, '2026-11-04', 2);
  addMood(s, '2026-11-05', 3);
  addMood(s, '2026-11-05', 4);
  assert.deepEqual(readMood(s), [{ d: '2026-11-05', m: 4 }, { d: '2026-11-04', m: 2 }]);
  for (let i = 1; i <= 40; i++) addMood(s, `2026-01-${String(i).padStart(2, '0')}`.slice(0, 10), 3);
  assert.ok(readMood(s).length <= 30);
});
