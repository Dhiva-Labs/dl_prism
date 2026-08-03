#!/usr/bin/env node
// smoke.mjs — exercises common/api.js against a live DL Prism gateway.
//
// This is the "we can't launch Chrome here" compensation: api.js only uses
// globalThis.fetch and standard Fetch streaming, so the exact same client
// code the extension loads in the browser can be driven from plain Node.
//
// Usage: node smoke/smoke.mjs [baseUrl] [apiKey]
// Defaults to http://127.0.0.1:8130 with no API key.

import {
  listModels,
  getUsage,
  explainRoute,
  chatCompletion,
  testConnection,
  PrismApiError,
  PrismUnreachableError,
} from '../common/api.js';

const baseUrl = process.argv[2] || 'http://127.0.0.1:8130';
const apiKey = process.argv[3] || '';
const cfg = { baseUrl, apiKey };

let failures = 0;

function ok(label, detail) {
  console.log(`  [ok] ${label}${detail ? ` — ${detail}` : ''}`);
}

function fail(label, err) {
  failures++;
  console.error(`  [FAIL] ${label} — ${err && err.stack ? err.stack : err}`);
}

async function step(label, fn) {
  try {
    await fn();
  } catch (err) {
    fail(label, err);
  }
}

console.log(`DL Prism Chrome extension — api.js smoke test`);
console.log(`gateway: ${baseUrl}\n`);

await step('testConnection()', async () => {
  const result = await testConnection(cfg);
  if (!result.ok) throw new Error(`expected ok:true, got ${JSON.stringify(result)}`);
  ok('testConnection()', `${result.count} model(s)`);
});

let models = [];
await step('listModels()', async () => {
  models = await listModels(cfg);
  if (!Array.isArray(models) || models.length === 0) {
    throw new Error('expected a non-empty model list');
  }
  const ids = models.map((m) => m.id);
  if (!ids.includes('auto')) throw new Error(`expected "auto" model in list, got: ${ids.join(', ')}`);
  ok('listModels()', `${ids.length} models: ${ids.join(', ')}`);
});

await step('getUsage()', async () => {
  const usage = await getUsage(cfg);
  if (typeof usage.month_total_usd !== 'number') {
    throw new Error(`expected numeric month_total_usd, got ${JSON.stringify(usage)}`);
  }
  ok('getUsage()', `month_total_usd=${usage.month_total_usd}, budget_usd=${usage.budget_usd}`);
});

await step('explainRoute()', async () => {
  const explain = await explainRoute(cfg, { model: 'auto', messages: [{ role: 'user', content: 'hello' }] });
  if (!explain.mode) throw new Error(`expected a "mode" field, got ${JSON.stringify(explain)}`);
  ok('explainRoute()', `mode=${explain.mode}, reason=${explain.reason}`);
});

await step('chatCompletion() non-streaming', async () => {
  const { routeInfo, fullText } = await chatCompletion(
    cfg,
    { model: 'auto', messages: [{ role: 'user', content: 'say hi' }], stream: false },
  );
  if (!fullText) throw new Error('expected non-empty fullText');
  if (!routeInfo.model) throw new Error(`expected routeInfo.model to be set, got ${JSON.stringify(routeInfo)}`);
  ok('chatCompletion() non-streaming', `model=${routeInfo.model} reply="${fullText}"`);
});

await step('chatCompletion() streaming', async () => {
  let deltas = 0;
  let streamed = '';
  const { routeInfo, fullText } = await chatCompletion(
    cfg,
    { model: 'auto', messages: [{ role: 'user', content: 'say hi in a few words' }], stream: true },
    {
      onDelta: (delta) => {
        deltas++;
        streamed += delta;
      },
    },
  );
  if (deltas < 2) throw new Error(`expected multiple onDelta calls, got ${deltas}`);
  if (streamed !== fullText) throw new Error('streamed text did not match fullText accumulator');
  ok(
    'chatCompletion() streaming',
    `${deltas} chunks, provider=${routeInfo.provider} model=${routeInfo.model} mode=${routeInfo.mode}`,
  );
});

await step('chatCompletion() error handling (unknown model)', async () => {
  try {
    await chatCompletion(cfg, { model: 'definitely-not-a-real-model', messages: [{ role: 'user', content: 'hi' }] });
    throw new Error('expected chatCompletion to throw for an unknown model');
  } catch (err) {
    if (!(err instanceof PrismApiError)) throw err;
    ok('chatCompletion() error handling', `PrismApiError: ${err.message}`);
  }
});

await step('unreachable gateway handling', async () => {
  const deadCfg = { baseUrl: 'http://127.0.0.1:1', apiKey: '' };
  try {
    await listModels(deadCfg);
    throw new Error('expected listModels to throw for an unreachable gateway');
  } catch (err) {
    if (!(err instanceof PrismUnreachableError)) throw err;
    ok('unreachable gateway handling', `PrismUnreachableError: ${err.message}`);
  }
});

console.log();
if (failures > 0) {
  console.error(`${failures} check(s) failed.`);
  process.exit(1);
} else {
  console.log('All checks passed.');
}
