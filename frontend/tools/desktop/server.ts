// The screenshot tool's server-side API (SSR fetches run in Node, outside page.route()): the public content fixture
// plus /api/config with the quiz switched on, so the /quiz pages (gated in app/quiz/layout.tsx) render. Not used by e2e.
import { createServer } from 'node:http';
import { config, content } from '../../e2e/content';
import { quizMeta, quizPlay } from './quiz-fixture';

createServer((req, res) => {
  res.setHeader('Content-Type', 'application/json');
  const url = (req.url || '').replace(/\?.*$/, '');
  if (url === '/api/content') res.end(JSON.stringify(content));
  else if (url === '/api/config') res.end(JSON.stringify({ ...config, features: { ...config.features, quiz: true } }));
  else if (url === '/api/quiz/quizzes/1/play') res.end(JSON.stringify(quizPlay));
  else if (url === '/api/quiz/quizzes/1') res.end(JSON.stringify({ quiz: quizMeta, questions: [] }));
  else { res.statusCode = 404; res.end(JSON.stringify({ error: { code: 'fixture_not_found' } })); }
}).listen(Number(process.env.E2E_API_PORT || 4571), '127.0.0.1');
