/* Local contract fixture only. Never imported by the app or included in production builds.
   Run: QUIZ_MOCK=1 NODE_ENV=development node tools/quiz/mock-server.mjs */
import http from 'node:http';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { WebSocketServer } = require('next/dist/compiled/ws');
if (process.env.QUIZ_MOCK !== '1' || process.env.NODE_ENV !== 'development')
  throw new Error('Quiz mock requires QUIZ_MOCK=1 and NODE_ENV=development');
const port = Number(process.env.QUIZ_MOCK_PORT || 8097);
let enabled = true,
  nextID = 10,
  nextAttempt = 1,
  nextRoom = 123456;
const quizzes = new Map(),
  attempts = new Map(),
  rooms = new Map(),
  tokens = new Map();
const uploads = [];
const question = (id, lang = 'kk', i = 0) => ({
  id,
  type: 'mcq',
  prompt: lang === 'ru' ? `Сколько будет 2 + 2? (${i + 1})` : `2 + 2 қанша болады? (${i + 1})`,
  options: ['4', '5', '6'],
  correctIndex: 0,
  explanation: lang === 'ru' ? 'Два плюс два равно четырём.' : 'Екі қосу екі төртке тең.',
  sourceQuote: lang === 'ru' ? 'Сумма двух и двух равна четырём.' : 'Екі мен екінің қосындысы төртке тең.',
});
function create(input, source = 'manual', id = nextID++) {
  const old = quizzes.get(id);
  const quiz = {
    id,
    title: input.title || 'Математика / Математика',
    description: input.description || '',
    subject: input.subject || '',
    visibility: input.visibility || 'private',
    isDraft: input.isDraft || false,
    shareCode: `share${id}code`,
    timePerQuestion: input.timePerQuestion || 60,
    shuffle: input.shuffle ?? true,
    source,
    questionCount: input.questions.length,
    plays: 0,
    version: (old?.quiz.version || 0) + 1,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    isMine: true,
  };
  const qs = input.questions.map((q, i) => ({ ...q, id: q.id || id * 1000 + i + 1 }));
  const record = { quiz, questions: qs };
  quizzes.set(id, record);
  return record;
}
create(
  {
    title: 'Математика / Математика',
    visibility: 'public',
    questions: [
      question(101),
      { id: 102, type: 'true_false', prompt: '3 > 2', correctBool: true, explanation: '3 > 2' },
    ],
  },
  'manual',
  1,
);
const allTypes = [
  question(201),
  { id: 202, type: 'mcq_multi', prompt: '2 + 2 = ?', options: ['4', '2 × 2', '5'], correctIndexes: [0, 1] },
  { id: 203, type: 'true_false', prompt: '3 > 2', correctBool: true },
  { id: 204, type: 'fill_blank', prompt: '2 + 2 = …', answers: ['4'] },
  { id: 205, type: 'reorder', prompt: '1 → 2 → 3', items: ['1', '2', '3'] },
  {
    id: 206,
    type: 'matching',
    prompt: 'A → 1; B → 2',
    pairs: [
      { left: 'A', right: '1' },
      { left: 'B', right: '2' },
    ],
  },
  {
    id: 207,
    type: 'categorization',
    prompt: 'A → X; B → Y',
    pairs: [
      { left: 'A', right: 'X' },
      { left: 'B', right: 'Y' },
    ],
  },
  { id: 208, type: 'dropdown', prompt: '2 + 2 = ?', options: ['4', '5'], correctIndex: 0 },
  { id: 209, type: 'poll', prompt: 'A / B', options: ['A', 'B'] },
  {
    id: 210,
    type: 'hotspot',
    prompt: '1',
    image: '00000000000000000000000000000000.jpg',
    zones: [{ id: 'z1', x: 10, y: 10, w: 30, h: 30 }],
    correctZone: 'z1',
  },
  {
    id: 211,
    type: 'labeling',
    prompt: '1 → A; 2 → B',
    image: '00000000000000000000000000000000.jpg',
    zones: [
      { id: 'z1', x: 10, y: 10, w: 30, h: 30, label: 'A' },
      { id: 'z2', x: 60, y: 60, w: 30, h: 30, label: 'B' },
    ],
  },
  {
    id: 212,
    type: 'comprehension',
    prompt: '2 + 2 = ?',
    passage: '2 + 2 = 4.',
    subQuestions: [{ id: 's1', type: 'fill_blank', prompt: '2 + 2 = …', answers: ['4'] }],
  },
];
create({ title: '12 түр / 12 типов', visibility: 'public', questions: allTypes }, 'manual', 2);
function key(q) {
  if (q.type === 'true_false') return q.correctBool;
  if (q.type === 'mcq_multi') return q.correctIndexes;
  if (q.type === 'fill_blank') return q.answers;
  if (q.type === 'reorder') return q.items;
  if (q.type === 'matching' || q.type === 'categorization')
    return Object.fromEntries(q.pairs.map((p) => [p.left, p.right]));
  if (q.type === 'hotspot') return q.correctZone;
  if (q.type === 'labeling') return Object.fromEntries(q.zones.map((z) => [z.id, z.label]));
  if (q.type === 'comprehension') return Object.fromEntries(q.subQuestions.map((s) => [s.id, key(s)]));
  return q.correctIndex ?? null;
}
function playQuestion(q, i) {
  const v = { id: q.id, position: i + 1, type: q.type, prompt: q.prompt, timeLimit: q.timeLimit || 60 };
  for (const k of ['options', 'passage']) if (q[k]) v[k] = q[k];
  if (q.items) v.items = [...q.items].reverse();
  if (q.pairs) {
    if (q.type === 'matching') {
      v.lefts = q.pairs.map((p) => p.left);
      v.rights = q.pairs.map((p) => p.right).reverse();
    } else {
      v.items = q.pairs.map((p) => p.left);
      v.categories = [...new Set(q.pairs.map((p) => p.right))];
    }
  }
  if (q.zones) {
    v.zones = q.zones.map((z) => ({ ...z, label: undefined }));
    v.labels = q.zones
      .map((z) => z.label)
      .filter(Boolean)
      .reverse();
  }
  if (q.image) v.imageUrl = `/api/quiz/media/${q.image}`;
  if (q.subQuestions)
    v.subQuestions = q.subQuestions.map((s) => ({
      id: s.id,
      type: s.type,
      prompt: s.prompt,
      options: s.options,
    }));
  return v;
}
const play = (record) => ({ quiz: record.quiz, questions: record.questions.map(playQuestion) });
function correct(q, answer) {
  if (q.type === 'fill_blank')
    return q.answers.some((a) => a.toLowerCase() === String(answer).trim().toLowerCase());
  if (q.type === 'comprehension') return q.subQuestions.every((s) => correct(s, answer?.[s.id]));
  return q.type !== 'poll' && JSON.stringify(key(q)) === JSON.stringify(answer);
}
const send = (res, status, data) => {
  res.writeHead(status, { 'content-type': 'application/json' });
  res.end(JSON.stringify(data));
};
const fail = (res, status = 404, code = 'quiz_not_found') =>
  send(res, status, { error: { code, message: 'Табылмады / Не найдено' } });
const token = (claims) => {
  const t = crypto.randomUUID();
  tokens.set(t, claims);
  return t;
};
const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url, `http://127.0.0.1:${port}`),
      path = url.pathname;
    const chunks = [];
    for await (const c of req) chunks.push(c);
    const buffer = Buffer.concat(chunks),
      raw = buffer.toString();
    const body = req.headers['content-type']?.includes('application/json') && raw ? JSON.parse(raw) : {};
    const guest = req.headers.cookie?.includes('quizGuest=1');
    if (path === '/__mock') {
      if (body.enabled !== undefined) enabled = body.enabled;
      if (body.disconnect) for (const c of wss.clients) c.close(1012);
      return send(res, 200, { uploads, enabled, rooms: rooms.size });
    }
    if (path === '/api/config') return send(res, 200, { features: { quiz: enabled } });
    if (path === '/api/me')
      return send(res, 200, {
        user: guest
          ? null
          : {
              id: 1,
              name: 'Quiz tester',
              lang: 'kk',
              consentAt: new Date().toISOString(),
              role: 'student',
              isAdult: true,
            },
        needsConsent: false,
      });
    if (path === '/api/content')
      return send(res, 200, { jobs: [], opportunities: [], courses: [], deals: [] });
    if (!path.startsWith('/api/quiz/')) return send(res, 200, {});
    if (!enabled) return fail(res);
    if (path.startsWith('/api/quiz/media/') && req.method === 'GET') {
      res.writeHead(200, { 'content-type': 'image/svg+xml' });
      return res.end(
        '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="300"><rect width="400" height="300" fill="#eef2fb"/><path d="M0 150H400M200 0V300" stroke="#2f57e8"/></svg>',
      );
    }
    if (path === '/api/quiz/media')
      return send(res, 201, {
        name: '00000000000000000000000000000000.jpg',
        url: '/api/quiz/media/00000000000000000000000000000000.jpg',
      });
    if (path === '/api/quiz/quizzes' && req.method === 'GET')
      return send(res, 200, {
        quizzes: [...quizzes.values()]
          .map((x) => x.quiz)
          .filter(
            (q) =>
              !q.isDraft && q.visibility === 'public' && q.title.includes(url.searchParams.get('q') || ''),
          ),
        hasMore: false,
      });
    if (path === '/api/quiz/mine')
      return send(res, 200, { quizzes: [...quizzes.values()].map((x) => x.quiz) });
    if (path === '/api/quiz/quizzes' && req.method === 'POST') return send(res, 201, create(body));
    if (path.startsWith('/api/quiz/generate/')) {
      const field = (name) => raw.match(new RegExp(`name="${name}"\\r\\n\\r\\n([^\\r]+)`))?.[1];
      const files = [...raw.matchAll(/filename="([^"]+)"/g)].map((m) => m[1]);
      uploads.push({ files, bytes: buffer.length });
      const lang = body.lang || field('lang') || 'kk',
        count = body.count || Number(field('count') || 5);
      const record = create(
        {
          title: body.title || field('title'),
          isDraft: true,
          questions: Array.from({ length: count }, (_, i) => question(undefined, lang, i)),
        },
        path.endsWith('from-text') ? 'ai_text' : 'ai_files',
      );
      await new Promise((resolve) => setTimeout(resolve, 150));
      return send(res, 201, {
        id: record.quiz.id,
        quiz: record.quiz,
        generated: count,
        dropped: 0,
        truncated: false,
      });
    }
    if (path === '/api/quiz/import/xlsx')
      return send(res, 200, { questions: [question(undefined)], warnings: [], imported: 1, skipped: 0 });
    if (path === '/api/quiz/attempts')
      return send(res, 200, { attempts: [...attempts.values()].map((r) => ({ ...r, id: r.attemptId })) });
    const attemptMatch = path.match(/^\/api\/quiz\/attempts\/(\d+)(\/retry)?$/);
    if (attemptMatch) {
      const r = attempts.get(Number(attemptMatch[1]));
      if (!r) return fail(res);
      if (!attemptMatch[2]) return send(res, 200, r);
      const record = quizzes.get(r.quizId),
        wrong = r.answers.filter((a) => a.graded && !a.isCorrect).map((a) => a.questionId);
      return send(res, 200, {
        ...play({ ...record, questions: record.questions.filter((q) => wrong.includes(q.id)) }),
        retryOf: r.attemptId,
      });
    }
    const share = path.match(/^\/api\/quiz\/s\/([^/]+)(\/(meta|clone))?$/);
    const match = path.match(/^\/api\/quiz\/quizzes\/(\d+)(\/(play|clone|attempts))?$/);
    if (share || match) {
      const record = share
        ? [...quizzes.values()].find((x) => x.quiz.shareCode === share[1])
        : quizzes.get(Number(match[1]));
      if (!record) return fail(res);
      const action = share ? share[3] || 'play' : match[3];
      if (action === 'meta') return send(res, 200, record.quiz);
      if (action === 'play') return send(res, 200, play(record));
      if (action === 'clone') {
        const r = create(
          {
            ...record.quiz,
            isDraft: true,
            questions: record.questions.map((q) => ({ ...q, id: undefined })),
          },
          'clone',
        );
        return send(res, 201, { id: r.quiz.id, quiz: r.quiz });
      }
      if (action === 'attempts') {
        const qs = body.questionIds?.length
          ? record.questions.filter((q) => body.questionIds.includes(q.id))
          : record.questions;
        const answers = qs.map((q, i) => {
          const a = body.answers.find((a) => a.questionId === q.id);
          return {
            position: i + 1,
            questionId: q.id,
            type: q.type,
            prompt: q.prompt,
            view: playQuestion(q, i),
            given: a?.answer ?? null,
            correct: key(q),
            isCorrect: correct(q, a?.answer),
            graded: q.type !== 'poll',
            credit: correct(q, a?.answer) ? 1 : 0,
            explanation: q.explanation,
            sourceQuote: q.sourceQuote,
            timeSpent: a?.timeSpent || 0,
          };
        });
        const score = answers.filter((a) => a.isCorrect).length,
          maxScore = answers.filter((a) => a.graded).length;
        const result = {
          attemptId: guest ? null : nextAttempt++,
          quizId: record.quiz.id,
          quizTitle: record.quiz.title,
          mode: body.retryOf ? 'retry' : 'practice',
          score,
          maxScore,
          percentage: maxScore ? Math.floor((score / maxScore) * 100) : 0,
          correctCount: score,
          total: qs.length,
          wrongCount: maxScore - score,
          timeSpent: 0,
          createdAt: new Date().toISOString(),
          answers,
        };
        if (result.attemptId) attempts.set(result.attemptId, result);
        return send(res, 201, result);
      }
      if (req.method === 'PUT') {
        if (body.version !== record.quiz.version) return fail(res, 409, 'version_conflict');
        return send(res, 200, create(body, record.quiz.source, record.quiz.id));
      }
      return send(res, 200, record);
    }
    if (path === '/api/quiz/live/sessions' && req.method === 'POST') {
      const record = quizzes.get(body.quizId);
      if (!record) return fail(res);
      const code = String(nextRoom++),
        room = { code, record, players: [], status: 'lobby', index: 0, host: null };
      rooms.set(code, room);
      return send(res, 201, {
        code,
        hostToken: token({ code, role: 'host' }),
        expiresAt: Date.now() + 120000,
        title: record.quiz.title,
      });
    }
    const hostMatch = path.match(/^\/api\/quiz\/live\/sessions\/(\d+)\/host-token$/);
    if (hostMatch) {
      if (!rooms.has(hostMatch[1]) || rooms.get(hostMatch[1]).status === 'ended') return fail(res);
      return send(res, 200, {
        hostToken: token({ code: hostMatch[1], role: 'host' }),
        expiresAt: Date.now() + 120000,
      });
    }
    if (path === '/api/quiz/live/join') {
      const room = rooms.get(body.code);
      if (!room || room.status === 'ended') return fail(res);
      let p = room.players.find((x) => x.rejoinKey === body.rejoinKey);
      if (!p) {
        if (room.players.some((x) => x.nickname === body.nickname)) return fail(res, 409, 'nickname_taken');
        p = {
          id: crypto.randomUUID(),
          rejoinKey: crypto.randomUUID(),
          nickname: body.nickname,
          score: 0,
          correct: 0,
          online: false,
        };
        room.players.push(p);
      }
      broadcast(room, 'lobby', lobby(room));
      return send(res, 200, {
        playerId: p.id,
        nickname: p.nickname,
        rejoinKey: p.rejoinKey,
        token: token({ code: body.code, role: 'player', pid: p.id }),
        expiresAt: Date.now() + 120000,
        title: room.record.quiz.title,
      });
    }
    return fail(res);
  } catch (e) {
    console.error(e);
    send(res, 500, { error: { message: String(e) } });
  }
});
const wss = new WebSocketServer({ server, path: '/api/quiz/live/ws' });
const emit = (ws, type, data) => {
  if (ws?.readyState === 1) ws.send(JSON.stringify({ type, data }));
};
const broadcast = (r, type, data) => {
  emit(r.host, type, data);
  r.players.forEach((p) => emit(p.socket, type, data));
};
const lobby = (r) => ({
  players: r.players.map((p) => ({ id: p.id, nickname: p.nickname, online: p.online })),
  count: r.players.length,
  max: 50,
});
const leaderboard = (r) =>
  [...r.players]
    .sort((a, b) => b.score - a.score)
    .map((p, i) => ({ id: p.id, nickname: p.nickname, score: p.score, correct: p.correct, rank: i + 1 }));
function questionMsg(r) {
  return {
    index: r.index,
    total: r.record.questions.length,
    question: playQuestion(r.record.questions[r.index], r.index),
    deadline: r.deadline,
    serverTime: Date.now(),
    timeLimitMs: 60000,
  };
}
function show(r) {
  r.status = 'question';
  r.players.forEach((p) => {
    p.answered = false;
  });
  r.deadline = Date.now() + 60000;
  broadcast(r, 'question', questionMsg(r));
}
function reveal(r) {
  r.status = 'reveal';
  r.reveal = {
    index: r.index,
    correct: key(r.record.questions[r.index]),
    explanation: r.record.questions[r.index].explanation || '',
    leaderboard: leaderboard(r),
    last: r.index === r.record.questions.length - 1,
  };
  broadcast(r, 'reveal', r.reveal);
}
function end(r) {
  r.status = 'ended';
  broadcast(r, 'ended', { reason: 'finished', leaderboard: leaderboard(r) });
}
wss.on('connection', (ws, req) => {
  const claims = tokens.get(new URL(req.url, 'http://localhost').searchParams.get('token'));
  if (!claims) return ws.close(4003);
  const r = rooms.get(claims.code);
  if (!r) return ws.close(4004);
  const p = r.players.find((p) => p.id === claims.pid);
  if (claims.role === 'host') r.host = ws;
  else {
    p.socket = ws;
    p.online = true;
  }
  emit(ws, 'hello', {
    code: r.code,
    title: r.record.quiz.title,
    status: r.status,
    total: r.record.questions.length,
    index: r.index,
    you: p ? { answered: !!p.answered, score: p.score, rank: 1 } : undefined,
  });
  broadcast(r, 'lobby', lobby(r));
  if (r.status === 'question') emit(ws, 'question', questionMsg(r));
  if (r.status === 'reveal') emit(ws, 'reveal', r.reveal);
  ws.on('message', (raw) => {
    const m = JSON.parse(String(raw));
    if (claims.role === 'host') {
      if (m.type === 'start' && r.players.length) show(r);
      if (m.type === 'end') end(r);
      if (m.type === 'next') {
        if (r.status === 'question') reveal(r);
        else if (r.index === r.record.questions.length - 1) end(r);
        else {
          r.index++;
          show(r);
        }
      }
    } else if (m.type === 'answer') {
      if (p.answered) return emit(ws, 'answer_rejected', { index: m.index, reason: 'duplicate' });
      p.answered = true;
      if (correct(r.record.questions[r.index], m.answer)) {
        p.score += 1000;
        p.correct++;
      }
      emit(ws, 'answer_ack', { index: r.index });
      emit(r.host, 'progress', {
        index: r.index,
        answered: r.players.filter((p) => p.answered).length,
        total: r.players.length,
      });
      if (r.players.every((p) => p.answered)) reveal(r);
    }
  });
  ws.on('close', () => {
    if (p) p.online = false;
    broadcast(r, 'lobby', lobby(r));
  });
});
server.listen(port, '127.0.0.1', () => console.log(`Quiz mock: http://127.0.0.1:${port}`));
