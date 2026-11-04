// SSR requests run in Node, outside Playwright route(). Serve only the public content fixture.
// Browser API calls are intercepted separately in fixtures.ts; there is no test login bypass.
import { createServer } from 'node:http';
import { content } from './content';
createServer((req, res) => {
  res.setHeader('Content-Type', 'application/json');
  if (req.url === '/api/content') res.end(JSON.stringify(content));
  else { res.statusCode = 404; res.end(JSON.stringify({ error: { code: 'fixture_not_found' } })); }
}).listen(Number(process.env.E2E_API_PORT || 4311), '127.0.0.1');
