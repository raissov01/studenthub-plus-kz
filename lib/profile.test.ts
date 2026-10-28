import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaults, demoProfile } from './defaults';
import { qualify, resumeText } from './domain';
import { i18n } from './format';
import { opportunity } from '../e2e/content';

test('fresh devices contain no invented identity or achievements; demo requires an explicit factory', () => {
  const s = defaults();
  assert.equal(s.lang, 'kk');
  assert.equal(s.onboarded, false);
  assert.equal(s.profile.name, '');
  assert.equal(s.profile.gpa, null);
  assert.equal(s.profile.demo, false);
  assert.deepEqual(s.profile.skills, []);
  assert.deepEqual(s.events, {});
  assert.deepEqual(s.gpaPlan, []);
  assert.equal(demoProfile('kk').name, 'Құралай');
  assert.equal(demoProfile('ru').name, 'Куралай');
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
