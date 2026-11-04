import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  addDays, hasContact, hasLongNumber, hasPhoneOrHandle, lfProblem, normKey, parseQaRoute, parseTags, rmProblem, type LFDraft, type RMDraft,
} from './community';

test('contacts in free text are caught like the server does', () => {
  for (const s of ['жаз: +7 701 123 45 67', '@aru_kz', 't.me/aru', 'https://x.kz', 'мейл aru@mail.ru', '8(701)1234567', 'instagram.com/aru']) {
    assert.equal(hasContact(s), true, s);
  }
  for (const s of ['Сары түсті рюкзак, 2-корпус', '3 бөлмелі пәтер, 1-қабат', 'бюджет 60 000']) assert.equal(hasContact(s), false, s);
  assert.equal(hasPhoneOrHandle('see https://docs.python.org/3/'), false);
  assert.equal(hasPhoneOrHandle('жаз @aru_kz'), true);
  assert.equal(hasLongNumber('карта 4400 4301 2345 6789'), true);
  assert.equal(hasLongNumber('1-305 аудитория'), false);
});

test('subject board key matches the Go NormKey', () => {
  assert.equal(normKey('  Математика-1 '), 'математика 1');
  assert.equal(normKey('МАТЕМАТИКА 1'), 'математика 1');
  assert.equal(normKey('Ёлка'), 'елка');
});

test('tags and the /qa?id= routes', () => {
  assert.deepEqual(parseTags('#Туынды, емтихан емтихан'), ['туынды', 'емтихан']);
  assert.equal(parseTags('a'), null);
  assert.equal(parseTags('a1 b2 c3 d4 e5 f6'), null);
  assert.deepEqual(parseQaRoute('12'), { kind: 'question', id: '12' });
  assert.deepEqual(parseQaRoute('s:Физика'), { kind: 'board', subject: 'Физика' });
  assert.deepEqual(parseQaRoute('new:Физика'), { kind: 'new', subject: 'Физика' });
  assert.deepEqual(parseQaRoute(null), { kind: 'list' });
});

test('lost & found: documents and cards rules', () => {
  const today = '2026-11-05';
  const d: LFDraft = { kind: 'found', category: 'keys', title: 'Кілттер', desc: '', room: '1-305', photo: '', handedTo: '', happenedOn: today };
  assert.equal(lfProblem(d, today), '');
  assert.equal(lfProblem({ ...d, category: 'cards', photo: 'x.jpg', handedTo: 'dean' }, today), 'sensitive_photo');
  assert.equal(lfProblem({ ...d, category: 'documents' }, today), 'handed_required');
  assert.equal(lfProblem({ ...d, category: 'documents', handedTo: 'dean', desc: 'ЖСН 990101300123' }, today), 'has_number');
  assert.equal(lfProblem({ ...d, kind: 'lost', category: 'documents' }, today), '');
  assert.equal(lfProblem({ ...d, desc: 'жаз @me_kz' }, today), 'has_contact');
  assert.equal(lfProblem({ ...d, happenedOn: addDays(today, 1) }, today), 'bad_date');
  assert.equal(lfProblem({ ...d, happenedOn: addDays(today, -91) }, today), 'bad_date');
});

test('roommates: budget, move-in, contacts, gender', () => {
  const today = '2026-11-05';
  const d: RMDraft = { district: 'Сарыарқа', about: '', budgetMin: 40000, budgetMax: 80000, moveIn: today, habits: [], sameGender: false };
  assert.equal(rmProblem(d, null, today), '');
  assert.equal(rmProblem({ ...d, about: 'тел 87011234567' }, null, today), 'has_contact');
  assert.equal(rmProblem({ ...d, budgetMin: 90000 }, null, today), 'bad_budget');
  assert.equal(rmProblem({ ...d, moveIn: '2027-06-01' }, null, today), 'bad_move_in');
  assert.equal(rmProblem({ ...d, sameGender: true }, null, today), 'need_gender');
  assert.equal(rmProblem({ ...d, sameGender: true }, 'f', today), '');
});
