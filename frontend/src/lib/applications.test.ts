import assert from 'node:assert/strict';
import test from 'node:test';
import { byNextAction, canMove, cleanApplications, editApp, markApplied, mergeApps, sectionOf, type TrackedApp } from './applications';
import type { MyApplication } from './b2b';

const job = { ref: 'job:j1', title: { kk: 'Бариста', ru: 'Бариста' }, company: 'Кофе' };

test('old S.applied marks migrate to «sent» with their timestamp, without loss', () => {
  const list = cleanApplications(undefined, { j1: 1700000000000, j2: 1700000001000 });
  assert.deepEqual(list.map(a => [a.ref, a.stage, a.appliedAt]), [['job:j1', 'sent', 1700000000000], ['job:j2', 'sent', 1700000001000]]);
  // a stored row wins over the old mark of the same vacancy; junk is dropped
  const kept = cleanApplications([{ ref: 'job:j1', stage: 'interview', appliedAt: 5, note: 'x'.repeat(900), followUp: '2026-13-40' }, { ref: 'bad' }, null], { j1: 1 });
  assert.equal(kept.length, 1);
  assert.equal(kept[0].stage, 'interview');
  assert.equal(kept[0].note.length, 500);
  assert.equal(kept[0].followUp, '');
});

test('marking applied adds one row with today and keeps an existing one', () => {
  const a = markApplied([], job, 42);
  assert.deepEqual(a, [{ ...job, stage: 'sent', appliedAt: 42, followUp: '', note: '' }]);
  assert.equal(markApplied(a, job, 99), a);
});

test('an in-app application: the server owns the stage, the note stays local', () => {
  const server: MyApplication[] = [{ id: 7, jobId: 'job-12', title: 'Курьер', titleRu: 'Курьер', company: 'ТОО', status: 'viewed', inviteMessage: null, sharedFields: {}, createdAt: '2026-11-01T05:00:00Z', updatedAt: '2026-11-02T05:00:00Z' }];
  const local: TrackedApp[] = [{ ref: 'job:job-12', title: { kk: 'Курьер', ru: 'Курьер' }, company: 'ТОО', stage: 'interview', appliedAt: 1, followUp: '2026-11-12', note: 'HR', server: 7 }];
  const rows = mergeApps(local, server);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].serverStatus, 'viewed');
  assert.equal(rows[0].stage, 'sent');
  assert.equal(rows[0].note, 'HR');
  assert.equal(canMove(rows[0]), false);
  const moved = editApp(local, rows[0], { stage: 'offer', note: 'HR 2' });
  assert.equal(moved[0].stage, 'interview', 'local stage not moved');
  assert.equal(moved[0].note, 'HR 2');
  // a withdrawn in-app application is closed even when the local copy said interview
  assert.equal(sectionOf(mergeApps(local, [{ ...server[0], status: 'withdrawn' }])[0].stage), 'closed');
});

test('follow-ups first, soonest first; then the newest', () => {
  const rows = mergeApps([
    { ...job, ref: 'job:a', stage: 'sent', appliedAt: 1, followUp: '', note: '' },
    { ...job, ref: 'job:b', stage: 'sent', appliedAt: 2, followUp: '2026-11-20', note: '' },
    { ...job, ref: 'job:c', stage: 'sent', appliedAt: 3, followUp: '2026-11-12', note: '' },
    { ...job, ref: 'job:d', stage: 'sent', appliedAt: 4, followUp: '', note: '' },
  ], null);
  assert.deepEqual(byNextAction(rows).map(r => r.ref), ['job:c', 'job:b', 'job:d', 'job:a']);
});

test('editing an external row: stage, follow-up (validated) and note', () => {
  const list = markApplied([], job, 1);
  const row = mergeApps(list, null)[0];
  const next = editApp(list, row, { stage: 'interview', followUp: '2026-11-12', note: 'Айгүл' });
  assert.deepEqual([next[0].stage, next[0].followUp, next[0].note], ['interview', '2026-11-12', 'Айгүл']);
  assert.equal(editApp(next, row, { followUp: 'tomorrow' })[0].followUp, '');
});
