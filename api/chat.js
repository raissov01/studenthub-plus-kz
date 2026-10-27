// STUDYGO AI chat proxy: keeps the OpenAI key on the server.
const MAX_MSGS = 16;
const MAX_LEN = 8000;

export default async function handler(req, res) {
  if (req.method !== 'POST') {
    res.setHeader('Allow', 'POST');
    return res.status(405).json({ error: { message: 'Method not allowed' } });
  }
  const key = process.env.OPENAI_API_KEY;
  if (!key) return res.status(503).json({ error: { message: 'AI is not configured' } });

  const body = typeof req.body === 'string' ? safeJson(req.body) : req.body || {};
  const system = typeof body.system === 'string' ? body.system.slice(0, 4000) : '';
  const raw = Array.isArray(body.messages) ? body.messages.slice(-MAX_MSGS) : [];
  const messages = raw
    .filter(m => m && (m.role === 'user' || m.role === 'assistant') && typeof m.content === 'string' && m.content.trim())
    .map(m => ({ role: m.role, content: m.content.slice(0, MAX_LEN) }));
  if (!messages.length || messages[messages.length - 1].role !== 'user') {
    return res.status(400).json({ error: { message: 'Bad request' } });
  }

  try {
    const r = await fetch((process.env.OPENAI_BASE_URL || 'https://api.openai.com/v1') + '/chat/completions', {
      method: 'POST',
      headers: { 'content-type': 'application/json', authorization: 'Bearer ' + key },
      body: JSON.stringify({
        model: process.env.OPENAI_MODEL || 'gpt-4.1-mini',
        max_tokens: Number(process.env.OPENAI_MAX_TOKENS) || 1200,
        messages: [...(system ? [{ role: 'system', content: system }] : []), ...messages]
      })
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) {
      return res.status(r.status === 429 ? 429 : 502).json({ error: { message: 'Upstream error ' + r.status } });
    }
    return res.status(200).json({ text: data.choices?.[0]?.message?.content || '' });
  } catch (e) {
    return res.status(502).json({ error: { message: 'Upstream unreachable' } });
  }
}

function safeJson(s) { try { return JSON.parse(s); } catch { return {}; } }
