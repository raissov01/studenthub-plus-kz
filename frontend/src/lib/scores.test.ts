import assert from 'node:assert/strict';
import test from 'node:test';
import {
  absenceState, admission, admissionShortfall, cleanScores, emptyEntry, fillGpa, final, GRADING, letter, needFor,
  needTable, nextReachable, parseScore, scoreState, type ScoreEntry,
} from './scores';

const e = (p: Partial<ScoreEntry>): ScoreEntry => ({ ...emptyEntry('s1', 'Экология', 1), ...p });

test('RK1 78, RK2 85 → R 81.5; exam 85 → F 82.9 → B', () => {
  const x = e({ rk1: 78, rk2: 85 });
  assert.equal(admission(x), 81.5);
  const f = final({ ...x, exam: 85 })!;
  assert.ok(Math.abs(f - 82.9) < 1e-9);
  assert.equal(letter(f), 'B');
  assert.equal(scoreState({ ...x, exam: 85 }), 'passed');
});

test('letter bounds are inclusive at the lower end and compared unrounded', () => {
  assert.equal(letter(84.99), 'B');
  assert.equal(letter(85), 'B+');
  assert.equal(letter(94.999), 'A-');
  assert.equal(letter(49.99), 'F');
  assert.equal(letter(100), 'A');
});

test('needFor A with R 81.5 is out of reach and the next reachable letter is offered', () => {
  const x = e({ rk1: 78, rk2: 85 });
  assert.equal(needFor(x, 'A'), null);
  assert.equal(needFor(x, 'A-'), null);
  assert.equal(needFor(x, 'B+'), 90.3);
  assert.deepEqual(nextReachable(x), { letter: 'B+', need: 90.3 });
  // never above 100, never below the exam minimum
  for (const row of needTable(x)) assert.ok(row.need == null || (row.need >= GRADING.minExam && row.need <= 100));
  assert.equal(needFor(e({ rk1: 100, rk2: 100 }), 'D'), 50);
});

test('R 45 → not admitted, 5 points short; no exam advice', () => {
  const x = e({ rk1: 40, rk2: 50 });
  assert.equal(scoreState(x), 'not_admitted');
  assert.equal(admissionShortfall(x), 5);
  assert.equal(nextReachable(x), null);
});

test('one RK only is a partial rating; exam failed and retake', () => {
  const x = e({ rk1: 70 });
  assert.equal(scoreState(x), 'partial');
  assert.equal(admission(x), 70);
  assert.equal(needFor(x, 'B'), null);
  const y = e({ rk1: 70, rk2: 70, exam: 40 });
  assert.equal(scoreState(y), 'exam_failed');
  assert.equal(scoreState({ ...y, retake: 75 }), 'passed');
  assert.equal(final({ ...y, retake: 75 }), 72);
});

test('scores: input validation and stored data', () => {
  assert.equal(parseScore(''), null);
  assert.equal(parseScore('78,5'), 78.5);
  assert.equal(parseScore('100'), 100);
  assert.equal(parseScore('101'), 'invalid');
  assert.equal(parseScore('7.55'), 'invalid');
  assert.equal(parseScore('-1'), 'invalid');
  assert.deepEqual(cleanScores(undefined), []);
  assert.deepEqual(cleanScores({}), []);
  const c = cleanScores([{ subject: 'a', rk1: 150, rk2: 80, absences: -2, absenceLimit: 0 }, { subject: 'a' }, null, { name: 'no id' }]);
  assert.equal(c.length, 1);
  assert.deepEqual([c[0].rk1, c[0].rk2, c[0].absences, c[0].absenceLimit, c[0].source], [null, 80, 0, null, 'self']);
});

test('absences: no warning without a limit', () => {
  assert.equal(absenceState({ absences: 9, absenceLimit: null }), 'none');
  assert.equal(absenceState({ absences: 3, absenceLimit: 6 }), 'ok');
  assert.equal(absenceState({ absences: 5, absenceLimit: 6 }), 'near');
  assert.equal(absenceState({ absences: 6, absenceLimit: 6 }), 'limit');
});

test('GPA fill sets letters for subjects with finals and leaves the others alone', () => {
  const plan = [{ n: 'Экология', cr: 5, g: 'C' }, { n: 'Философия', cr: 3, g: 'A' }];
  const scores = [e({ rk1: 78, rk2: 85, exam: 85 }), e({ subject: 's2', name: 'Тарих', rk1: 90, rk2: 90, exam: 100 }), e({ subject: 's3', name: 'Философия', rk1: 60 })];
  const r = fillGpa(plan, scores, x => [x.name]);
  assert.deepEqual(r.plan, [{ n: 'Экология', cr: 5, g: 'B' }, { n: 'Философия', cr: 3, g: 'A' }, { n: 'Тарих', cr: 5, g: 'A-' }]);
  assert.equal(r.changed, 2);
  assert.deepEqual(plan[0], { n: 'Экология', cr: 5, g: 'C' }, 'input not mutated');
});
