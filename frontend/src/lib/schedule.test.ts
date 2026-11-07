import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addDays, lanes, mm, mondayOf, wallYmd } from './schedule';

test('mondayOf: any day of the week maps to its Monday', () => {
  assert.equal(wallYmd(mondayOf(new Date(2026, 10, 8))), '2026-11-02'); // Sunday
  assert.equal(wallYmd(mondayOf(new Date(2026, 10, 2, 23, 30))), '2026-11-02');
  assert.equal(wallYmd(addDays(new Date(2026, 11, 30), 3)), '2027-01-02');
  assert.equal(mm(17 * 60 + 5), '17:05');
});

test('lanes: overlapping events share the width, a later one takes the free lane back', () => {
  const e = (s: string, en: string) => ({ starts_at: `2026-11-02T${s}:00Z`, ends_at: `2026-11-02T${en}:00Z` });
  const out = lanes([e('10:00', '12:00'), e('11:00', '13:00'), e('12:00', '12:30'), e('14:00', '15:00')]);
  const by = (s: string) => out.find(x => x.e.starts_at.includes(`T${s}`))!;
  assert.deepEqual([by('10:00').lane, by('11:00').lane, by('12:00').lane], [0, 1, 0]);
  assert.equal(by('10:00').lanes, 2);
  assert.deepEqual([by('14:00').lane, by('14:00').lanes], [0, 1]);
});
