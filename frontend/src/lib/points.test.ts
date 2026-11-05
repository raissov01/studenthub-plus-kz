import assert from 'node:assert/strict';
import test from 'node:test';
import { almatyToday, awardText, levelPct, newKey, plural, milestoneToShow, REMINDER_TIMES } from './points';

const kk = (a: string) => a;
const ru = (_: string, b: string) => b;

test('award toast shows only what was earned', () => {
  assert.equal(awardText({ xp: 10, coins: 2, reason: 'quiz' }, kk), '+10 XP · +2 монета');
  assert.equal(awardText({ xp: 30, coins: 15, reason: 'event_checkin' }, ru), '+30 XP · +15 монет');
  assert.equal(awardText({ xp: 5, coins: 0, reason: 'roadmap_step' }, ru), '+5 XP');
  assert.equal(awardText({ xp: 0, coins: 0, duplicate: true, reason: 'quiz' }, kk), '');
  assert.equal(awardText(null, kk), '');
});

test('russian plural', () => {
  assert.deepEqual([1, 2, 5, 11, 21, 22, 25, 112].map(n => plural(n, 'монета', 'монеты', 'монет')),
    ['монета', 'монеты', 'монет', 'монет', 'монета', 'монеты', 'монет', 'монет']);
});

test('level progress stays within 0..100', () => {
  assert.equal(levelPct({ xp: 0, levelXp: 0, nextLevelXp: 100 }), 0);
  assert.equal(levelPct({ xp: 150, levelXp: 100, nextLevelXp: 300 }), 25);
  assert.equal(levelPct({ xp: 999, levelXp: 100, nextLevelXp: 300 }), 100);
});

test('idempotency keys match the server pattern and differ', () => {
  const a = newKey(), b = newKey();
  assert.match(a, /^[A-Za-z0-9_-]{8,64}$/);
  assert.notEqual(a, b);
});

test('Almaty day rolls over at 19:00 UTC', () => {
  assert.equal(almatyToday(new Date('2026-11-08T18:59:00Z')), '2026-11-08');
  assert.equal(almatyToday(new Date('2026-11-08T19:00:00Z')), '2026-11-09');
});

test('a milestone is shown once per device, only when newer than the last one shown', () => {
  assert.equal(milestoneToShow(7, 0), 7);
  assert.equal(milestoneToShow(7, 7), 0);
  assert.equal(milestoneToShow(undefined, 0), 0);
  assert.equal(milestoneToShow(14, 7), 14);
  assert.deepEqual(REMINDER_TIMES.filter(t => t < '19:00' || t > '21:00'), []);
});
