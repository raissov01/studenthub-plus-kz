import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classesOn, cleanTimetable, makeEntry, nextClass, sortTimetable, weekdayOf } from './timetable';

const E = (id: string, wd: number, t: string, end: string, name = 'Статистика') => ({ id, name, wd, t, end, room: '4-108', kind: 'Дәріс' });

test('the form makes a valid entry or names the problem', () => {
  assert.equal(makeEntry({ name: '  ', wd: 0, t: '09:00', end: '', room: '', kind: '' }, 'a'), 'name');
  assert.equal(makeEntry({ name: 'X', wd: 0, t: '', end: '', room: '', kind: '' }, 'a'), 'time');
  assert.equal(makeEntry({ name: 'X', wd: 0, t: '10:00', end: '09:00', room: '', kind: '' }, 'a'), 'time');
  assert.equal(makeEntry({ name: 'X', wd: 7, t: '10:00', end: '', room: '', kind: '' }, 'a'), 'time');
  assert.deepEqual(makeEntry({ name: ' Физика ', wd: 4, t: '10:00', end: '', room: ' 2-301 ', kind: 'Практика' }, 'a'),
    { id: 'a', name: 'Физика', wd: 4, t: '10:00', end: '10:50', room: '2-301', kind: 'Практика' });
});

test('stored data is cleaned: bad rows dropped, duplicates removed, sorted by day and time', () => {
  const list = cleanTimetable([E('b', 2, '13:00', '13:50'), E('a', 0, '09:00', '08:00'), E('b', 1, '09:00', '09:50'), { id: 'x', wd: 1, t: '09:00' }, 'junk']);
  assert.deepEqual(list.map(e => [e.id, e.wd, e.t, e.end]), [['a', 0, '09:00', '09:50'], ['b', 2, '13:00', '13:50']]);
  assert.deepEqual(cleanTimetable(undefined), []);
  assert.deepEqual(sortTimetable([E('2', 1, '10:00', '10:50'), E('1', 1, '08:00', '08:50')]).map(e => e.id), ['1', '2']);
});

test('classes of a weekday and the next class of today', () => {
  const list = [E('a', 2, '13:00', '13:50', 'Б'), E('b', 2, '09:00', '09:50', 'А'), E('c', 3, '09:00', '09:50')];
  const wed = new Date(2026, 10, 4, 10, 0); // Wednesday
  assert.equal(weekdayOf(wed), 2);
  assert.deepEqual(classesOn(list, 2).map(c => c.name), ['А', 'Б']);
  assert.equal(nextClass(list, wed)?.name, 'Б');
  assert.equal(nextClass(list, new Date(2026, 10, 4, 9, 30))?.name, 'А'); // in progress
  assert.equal(nextClass(list, new Date(2026, 10, 4, 14, 0)), null); // the day is over
  assert.equal(nextClass([], wed), null);
});
