import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaults, dropDemo } from './defaults';
import { allDecks, certificates, notifs, qualify, resumeText } from './domain';
import type { AppState } from './types';
import { i18n } from './format';
import { opportunity } from '../../e2e/content';

test('fresh devices contain no invented identity, timetable, grades, decks or notices', () => {
  const s = defaults();
  assert.equal(s.lang, 'kk');
  assert.equal(s.onboarded, false);
  assert.equal(s.profile.name, '');
  assert.equal(s.profile.gpa, null);
  assert.equal('demo' in s.profile, false);
  assert.deepEqual(s.profile.skills, []);
  assert.deepEqual(s.gpaPlan, []);
  assert.deepEqual(s.timetable, []);
  assert.deepEqual(allDecks(s), []);
  assert.deepEqual(certificates(s, i18n('kk')), []);
  const empty = { jobs: [], opportunities: [], discounts: [], events: [], places: [], source: 'test', updated_at: '' };
  assert.deepEqual(notifs(s, i18n('kk'), empty), []);
});

test('old device state with the removed sample content migrates without crashing', () => {
  const old = {
    ...defaults('ru'), onboarded: true,
    profile: { ...defaults('ru').profile, name: 'Куралай', spec: 'econ', course: 2, gpa: 3.13, demo: true },
    events: { e1: { code: 'EV-E1-1234' }, p1: { code: 'EV-P1-1' } }, hideDemoDeck: true,
    decks: [{ id: 'deck-demo', name: 'x', cards: [] }, { id: 'dk1', name: 'Мой набор', cards: [{ q: 'a', b: 'b' }] }],
    calendarPins: [
      { ref: 'event:e1', kind: 'event', title: { kk: 'Мансап жәрмеңкесі', ru: 'Ярмарка карьеры' }, date: '2026-11-07', time: '11:00', prio: 'mid', done: 0 },
      { ref: 'job:j1', kind: 'job', title: { kk: 'Промоутер', ru: 'Промоутер' }, date: '2026-11-07', time: '', prio: 'mid', done: 0 },
      { ref: 'event:42', kind: 'event', title: { kk: 'Нақты', ru: 'Реальное' }, date: '2026-11-08', time: '10:00', prio: 'mid', done: 0 },
      { ref: 'opp:adm-3', kind: 'opp', title: { kk: 'Грант', ru: 'Грант' }, date: '2026-11-09', time: '', prio: 'high', done: 0 },
    ],
    timetable: [{ id: 't1', name: ' Статистика ', wd: 2, t: '09:00', end: '09:50', room: '4-108', kind: 'Семинар' }, { id: 'bad', name: 'x', wd: 9, t: '25:00' }, null],
    gpaPlan: [{ n: 'Custom', cr: 3, g: 'A' }],
    done: { c3: Date.UTC(2026, 9, 1), c8: Date.UTC(2026, 9, 2) },
  } as unknown as AppState;
  const s = dropDemo(old) as AppState & Record<string, unknown>;
  assert.equal(s.onboarded, false, 'the sample profile was never the student\'s own');
  assert.equal(s.profile.name, '');
  assert.equal(s.profile.gpa, null);
  assert.equal('demo' in s.profile, false);
  assert.equal('events' in s, false);
  assert.equal('hideDemoDeck' in s, false);
  assert.deepEqual(s.decks.map(d => d.id), ['dk1']);
  assert.deepEqual(s.calendarPins.map(p => p.ref), ['event:42', 'opp:adm-3']);
  assert.deepEqual(s.timetable, [{ id: 't1', name: 'Статистика', wd: 2, t: '09:00', end: '09:50', room: '4-108', kind: 'Семинар' }]);
  assert.deepEqual(s.gpaPlan, [{ n: 'Custom', cr: 3, g: 'A' }]);
  // a removed course the student finished becomes their own certificate; a kept one stays a course
  assert.deepEqual(s.done, { c8: Date.UTC(2026, 9, 2) });
  assert.deepEqual(s.certs, [{ title: 'Финансовое моделирование', org: 'Coursera', date: '2026-10-01T00:00:00.000Z' }]);
  assert.doesNotThrow(() => resumeText('ru', s, i18n('ru')));
  // an own profile is kept as it is
  const own = dropDemo({ ...defaults('kk'), onboarded: true, profile: { ...defaults('kk').profile, name: 'Аружан', demo: false } } as AppState);
  assert.equal(own.onboarded, true);
  assert.equal(own.profile.name, 'Аружан');
});

test('missing GPA is unknown, never a fabricated zero or a successful match', () => {
  for (const lang of ['kk', 'ru'] as const) {
    const s = defaults(lang);
    s.profile = { ...s.profile, name: 'Student', course: 2, spec: 'econ' };
    const q = qualify(opportunity, s.profile, i18n(lang));
    const gpa = q.res.find(r => r.t.startsWith('GPA'))!;
    assert.equal(gpa.ok, false);
    assert.equal(gpa.mid, true);
    assert.match(gpa.you, /көрсетілмеген|не указан/);
    assert.equal(q.ok, false);
    assert.doesNotMatch(resumeText(lang, s, i18n(lang)), /GPA:/);
    s.profile.gpa = 0;
    assert.equal(qualify(opportunity, s.profile, i18n(lang)).res.find(r => r.t.startsWith('GPA'))?.mid, false);
  }
});
