#!/usr/bin/env node
// mock-upstream.mjs: a tiny OpenAI-compatible server for testing DL Prism
// without any real model. Node built-ins only.
//
// AMO reviewer test kit (run in two terminals, from integrations/chrome/):
//   node test/mock-upstream.mjs            # listens on 127.0.0.1:8199
//   prism serve -c test/prism.mock.yaml    # gateway on 127.0.0.1:8130
// Then, in the extension's settings, set the API key to "mock-gateway-key"
// and press "Test connection". Chat with model "mock-chat" (or "auto").
// Every reply is "Mock reply to: <first 40 chars of your last message>".
//
// Usage: node test/mock-upstream.mjs [port]   (env MOCK_PORT also works)

import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';

export const MOCK_PREFIX = 'Mock reply to: ';

export function replyFor(messages) {
  const last = [...(messages || [])].reverse().find((m) => m.role === 'user');
  let content = last?.content ?? '';
  if (Array.isArray(content)) content = content.map((p) => p.text || '').join('');
  return MOCK_PREFIX + String(content).slice(0, 40);
}

export function startMock(port = 0, { log = (s) => console.log(s) } = {}) {
  const server = createServer((req, res) => {
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8');
      const path = req.url.split('?')[0];
      const json = (code, obj) => {
        res.writeHead(code, { 'content-type': 'application/json' });
        res.end(JSON.stringify(obj));
      };
      if (req.method === 'GET' && path === '/v1/models') {
        return json(200, { object: 'list', data: [{ id: 'mock-chat', object: 'model', owned_by: 'mock' }] });
      }
      if (req.method === 'POST' && path === '/v1/chat/completions') {
        log(`[mock] ${req.method} ${path} ${raw}`);
        let body;
        try { body = JSON.parse(raw); } catch { return json(400, { error: { message: 'bad json' } }); }
        const text = replyFor(body.messages);
        const base = { id: 'chatcmpl-mock', object: 'chat.completion.chunk', created: Math.floor(Date.now() / 1000), model: 'mock-chat' };
        if (body.stream) {
          res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' });
          const parts = text.match(/.{1,8}/gs) || [''];
          const send = (delta, finish = null, extra = {}) =>
            res.write(`data: ${JSON.stringify({ ...base, choices: [{ index: 0, delta, finish_reason: finish }], ...extra })}\n\n`);
          send({ role: 'assistant', content: '' });
          let i = 0;
          const tick = setInterval(() => {
            if (i < parts.length) return send({ content: parts[i++] });
            clearInterval(tick);
            send({}, 'stop');
            res.write('data: [DONE]\n\n');
            res.end();
          }, 15);
          return;
        }
        return json(200, {
          ...base, object: 'chat.completion',
          choices: [{ index: 0, message: { role: 'assistant', content: text }, finish_reason: 'stop' }],
          usage: { prompt_tokens: 5, completion_tokens: 8, total_tokens: 13 },
        });
      }
      json(404, { error: { message: `not found: ${path}` } });
    });
  });
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const port = Number(process.argv[2] || process.env.MOCK_PORT || 8199);
  await startMock(port);
  console.log(`[mock] OpenAI-compatible mock on http://127.0.0.1:${port}/v1`);
}
