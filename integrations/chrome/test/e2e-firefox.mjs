// End-to-end test: the DL Prism Companion Firefox build, in headless Firefox
// (WebDriver BiDi), against a real `prism serve` gateway in front of a mock
// OpenAI-compatible upstream. No network, no real model.
//
//   node tool/build.mjs
//   (cd test && npm install)            # puppeteer-core only; harness-only dep
//   node test/e2e-firefox.mjs [--shots dir]
//
// Env: FIREFOX (default /snap/firefox/current/usr/lib/firefox/firefox; the
// snap's /usr/bin/firefox is a launcher WebDriver can't drive), GO (default
// ~/sdk/go/bin/go), PRISM_BIN (skip the build and use this binary).
//
// Ports: mock upstream 127.0.0.1:8199, gateway 127.0.0.1:8130 (see
// prism.mock.yaml). The extension is the build in dist/firefox, with a pinned
// UUID. Host access goes through the real "Grant access" banner;
// extensions.webextOptionalPermissionPrompts=false grants permissions.request
// without a doorhanger. Context-menu clicks can't be made headless, so the
// selection flow stores the pending selection the way background.js does and
// opens popup.html?from=context-menu.

import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { existsSync, mkdirSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import puppeteer from 'puppeteer-core';
import { startMock, MOCK_PREFIX } from './mock-upstream.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..');
const extPath = join(root, 'dist', 'firefox');
const shotsAt = process.argv.indexOf('--shots');
const shots = shotsAt > 0 ? process.argv[shotsAt + 1] : null;
if (shots) await mkdir(shots, { recursive: true });

const ID = 'dl-prism@dhivalabs.dev';
const UUID = '7b2e5c19-3a4d-4f6b-8e10-9c5d2a1f0b47';
const EXT = `moz-extension://${UUID}`;
const GATEWAY = 'http://127.0.0.1:8130';
const API_KEY = 'mock-gateway-key'; // from prism.mock.yaml
const t0 = Date.now();

// --- build the extension and the gateway ---------------------------------
execFileSync(process.execPath, [join(root, 'tool', 'build.mjs')], { stdio: 'ignore' });
let prismBin = process.env.PRISM_BIN;
if (!prismBin) {
  const cache = join(homedir(), '.cache', 'prism-e2e');
  mkdirSync(cache, { recursive: true });
  prismBin = join(cache, 'prism');
  const go = process.env.GO || join(homedir(), 'sdk', 'go', 'bin', 'go');
  const tmp = join(homedir(), '.cache', 'go-tmp');
  mkdirSync(tmp, { recursive: true });
  execFileSync(go, ['build', '-o', prismBin, './cmd/prism'], {
    cwd: join(root, '..', '..'),
    env: { ...process.env, TMPDIR: process.env.TMPDIR || tmp, GOTMPDIR: process.env.GOTMPDIR || tmp },
    stdio: 'inherit',
  });
}

// --- mock upstream + gateway ----------------------------------------------
const upstreamBodies = [];
const mock = await startMock(8199, {
  log: (s) => {
    const i = s.indexOf('{');
    if (i > 0) upstreamBodies.push(JSON.parse(s.slice(i)));
  },
});
const gwHome = await mkdtemp(join(tmpdir(), 'prism-e2e-gw-'));
const gwLog = [];
const gateway = spawn(prismBin, ['serve', '-c', join(here, 'prism.mock.yaml')], {
  // Isolated state dir so a developer's own prism data is never touched.
  env: { PATH: process.env.PATH, HOME: gwHome, XDG_CONFIG_HOME: gwHome, XDG_DATA_HOME: gwHome, XDG_STATE_HOME: gwHome },
  stdio: ['ignore', 'pipe', 'pipe'],
});
gateway.stdout.on('data', (d) => gwLog.push(String(d)));
gateway.stderr.on('data', (d) => gwLog.push(String(d)));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
for (let i = 0; ; i++) {
  try {
    const r = await fetch(`${GATEWAY}/v1/models`, { headers: { Authorization: `Bearer ${API_KEY}` } });
    if (r.ok) break;
  } catch {}
  if (i > 100 || gateway.exitCode !== null) throw new Error(`gateway did not start:\n${gwLog.join('')}`);
  await sleep(100);
}

// --- Firefox ------------------------------------------------------------------
const profile = await mkdtemp(join(tmpdir(), 'prism-ff-e2e-'));
const browser = await puppeteer.launch({
  browser: 'firefox',
  executablePath: process.env.FIREFOX || '/snap/firefox/current/usr/lib/firefox/firefox',
  headless: true,
  args: ['--remote-allow-system-access'], // lets WebDriver open moz-extension:// pages
  env: { HOME: profile, TMPDIR: profile, PATH: process.env.PATH, LANG: 'en_US.UTF-8' },
  extraPrefsFirefox: {
    'extensions.webextensions.uuids': JSON.stringify({ [ID]: UUID }),
    'extensions.webextOptionalPermissionPrompts': false,
    'devtools.console.stdout.content': true, // extension console to stdout
  },
});

const errors = [];
const warnings = [];
const watch = (stream) =>
  stream.on('data', (d) => {
    for (const line of String(d).split('\n')) {
      if (!line.includes(UUID) && !line.startsWith('console.')) continue;
      if (/error/i.test(line) || line.startsWith('console.error:')) errors.push(`[firefox] ${line.trim()}`);
      else if (/warn/i.test(line)) warnings.push(`[firefox] ${line.trim()}`);
    }
  });
watch(browser.process().stdout);
watch(browser.process().stderr);

const results = [];
const notes = [];
async function step(name, fn) {
  const s = Date.now();
  try {
    await fn();
    results.push(true);
    console.log(`  ok   ${name} (${Date.now() - s} ms)`);
  } catch (err) {
    results.push(false);
    console.log(`  FAIL ${name}\n       ${String(err?.stack || err).split('\n').slice(0, 6).join('\n       ')}`);
    if (errors.length) console.log(`       console errors: ${errors.join(' | ')}`);
    throw err;
  }
}

const track = (p, tag) => {
  p.on('console', (m) => {
    if (m.type() === 'error') errors.push(`[${tag}] ${m.text()}`);
    else if (m.type() === 'warning') warnings.push(`[${tag}] ${m.text()}`);
  });
  p.on('pageerror', (e) => errors.push(`[${tag}] ${e.message}`));
  return p;
};
const where = (p) => p.evaluate(() => `${location.href} ${document.readyState}`).catch(() => '');
// Puppeteer doesn't see navigations inside extension pages, so ask the page.
async function openExt(path, tag, size = { width: 360, height: 600 }) {
  const p = track(await browser.newPage(), tag);
  await p.setViewport(size);
  p.goto(EXT + path, { timeout: 0 }).catch(() => {});
  for (let i = 0; (await where(p)) !== `${EXT + path} complete`; i++) {
    if (i > 100) throw new Error(`${path} did not load (at ${await where(p)})`);
    await sleep(100);
  }
  return p;
}
const text = (p) => p.evaluate(() => document.body.innerText);
async function waitFor(p, fn, what, ms = 20000, arg) {
  const end = Date.now() + ms;
  for (;;) {
    if (await p.evaluate(fn, arg).catch(() => false)) return;
    if (Date.now() > end) throw new Error(`timed out waiting for ${what}; page shows: ${(await text(p)).replace(/\n+/g, ' | ')}`);
    await sleep(150);
  }
}
const hidden = (id) => (sel) => document.getElementById(sel).classList.contains('hidden');
const setValue = async (p, sel, value) => {
  await p.$eval(sel, (el) => { el.value = ''; });
  if (value) {
    await p.click(sel);
    await p.type(sel, value);
  }
};

// WebDriver can't screenshot extension pages (privileged scope), so the
// snapshot is the browser's own tab capture, taken in the parent process.
async function shot(p, name, size) {
  if (!shots) return;
  const href = (await where(p)).replace(/ \w+$/, '');
  const { contexts } = (await browser.connection.send('browsingContext.getTree', { 'moz:scope': 'chrome' })).result;
  const { result } = await browser.connection.send('script.evaluate', {
    expression: `(async () => {
      const win = Services.wm.getMostRecentWindow('navigator:browser');
      const tab = win.gBrowser.tabs.find((t) => t.linkedBrowser.currentURI.spec === ${JSON.stringify(href)});
      win.gBrowser.selectedTab = tab;
      await new Promise((r) => win.setTimeout(r, 300));
      const wg = win.gBrowser.selectedBrowser.browsingContext.currentWindowGlobal;
      const bitmap = await wg.drawSnapshot(new DOMRect(0, 0, ${size.width}, ${size.height}), 2, 'white');
      const canvas = win.document.createElementNS('http://www.w3.org/1999/xhtml', 'canvas');
      canvas.width = bitmap.width;
      canvas.height = bitmap.height;
      canvas.getContext('2d').drawImage(bitmap, 0, 0);
      return canvas.toDataURL('image/png');
    })()`,
    target: { context: contexts[0].context },
    awaitPromise: true,
  });
  const url = result.result?.value;
  assert.ok(url, `snapshot failed: ${JSON.stringify(result)}`);
  await writeFile(join(shots, `${name}.png`), Buffer.from(url.split(',')[1], 'base64'));
  console.log(`       screenshot ${join(shots, name + '.png')}`);
}

async function sendMessage(p, msg) {
  await p.click('#message-input');
  await p.type('#message-input', msg);
  await p.keyboard.press('Enter');
}
const lastReply = (p) =>
  p.evaluate(() => {
    const m = [...document.querySelectorAll('.message.assistant')].pop();
    return m ? { text: m.querySelector('.bubble').textContent, route: m.querySelector('.route-line')?.textContent || '', pending: !!m.querySelector('.bubble.pending'), error: !!m.querySelector('.bubble.error') } : null;
  });

try {
  await browser.installExtension(extPath);
  await sleep(1000);
  console.log(`DL Prism Companion Firefox e2e on ${await browser.version()}`);

  let popup;
  let options;

  // a. popup loads; the grant banner (Firefox does not auto-grant host access) works
  await step('a. popup loads without errors; grant banner shows, click grants access, banner goes away', async () => {
    popup = await openExt('/popup/popup.html', 'popup');
    // Firefox grants host_permissions to a temporary add-on at install, so
    // revoke them (as a user can in about:addons) to exercise the banner.
    const granted0 = await popup.evaluate(() => chrome.permissions.contains({ origins: ['http://127.0.0.1/*', 'http://localhost/*'] }));
    console.log(`       host access at install: ${granted0}`);
    if (granted0) {
      const removed = await popup.evaluate(() => chrome.permissions.remove({ origins: ['http://127.0.0.1/*', 'http://localhost/*'] }));
      assert.equal(removed, true, 'revoked install-time host access');
      await popup.close();
      popup = await openExt('/popup/popup.html', 'popup');
    }
    await waitFor(popup, () => !document.getElementById('permission-banner').classList.contains('hidden'), 'permission banner (Firefox MV3 does not auto-grant host access)');
    const before = await popup.evaluate(() => chrome.permissions.contains({ origins: ['http://127.0.0.1/*', 'http://localhost/*'] }));
    assert.equal(before, false, 'host access not granted before the click');
    await popup.click('#grant-access-btn');
    await waitFor(popup, () => document.getElementById('permission-banner').classList.contains('hidden'), 'banner to hide after grant');
    const after = await popup.evaluate(() => chrome.permissions.contains({ origins: ['http://127.0.0.1/*', 'http://localhost/*'] }));
    assert.equal(after, true, 'host access granted after the click');
    assert.deepEqual(errors, []);
  });

  // b. options: wrong key -> auth failure, right key -> success
  await step('b. options: wrong API key reports an auth failure; right key connects and saves', async () => {
    options = await openExt('/options/options.html', 'options', { width: 640, height: 640 });
    assert.equal(await options.evaluate(() => document.getElementById('permission-banner').classList.contains('hidden')), true, 'no grant banner once granted');
    assert.equal(await options.$eval('#base-url', (el) => el.value), GATEWAY);
    await setValue(options, '#api-key', 'definitely-wrong-key');
    await options.click('#test-connection');
    await waitFor(options, () => /^Failed:/.test(document.getElementById('test-status').textContent), 'a Failed: status');
    const bad = await options.$eval('#test-status', (el) => ({ t: el.textContent, err: el.classList.contains('error') }));
    assert.ok(bad.err, 'status styled as an error');
    assert.match(bad.t, /key|auth|unauthori[sz]ed|401/i, `auth failure message, got: ${bad.t}`);
    console.log(`       wrong key -> "${bad.t}"`);

    await setValue(options, '#api-key', API_KEY);
    await options.click('#test-connection');
    await waitFor(options, () => /^Connected/.test(document.getElementById('test-status').textContent), 'Connected status');
    console.log(`       right key -> "${await options.$eval('#test-status', (el) => el.textContent)}"`);
    await options.click('button[type=submit]');
    await waitFor(options, () => document.getElementById('save-status').textContent === 'Saved.', 'Saved.');
    // Persisted
    const stored = await options.evaluate(async () => (await chrome.storage.local.get('prism.settings'))['prism.settings']);
    assert.equal(stored.apiKey, API_KEY);
    assert.deepEqual(errors, []);
    // Screenshot with the key field blanked in the DOM only (it is a password input anyway).
    await options.$eval('#api-key', (el) => { el.value = ''; });
    await shot(options, 'options', { width: 640, height: 640 });
    await options.$eval('#api-key', (el, v) => { el.value = v; }, API_KEY);
  });

  // c. popup chat
  await step('c. popup: model picker, streamed reply matches the mock echo, route line, spend line', async () => {
    await popup.close();
    popup = await openExt('/popup/popup.html', 'popup');
    await waitFor(popup, () => document.getElementById('model-select').options.length >= 2, 'model list');
    const opts = await popup.$$eval('#model-select option', (os) => os.map((o) => o.value));
    assert.equal(opts[0], 'auto', `auto first: ${opts}`);
    assert.ok(opts.includes('mock-chat'), `mock model listed: ${opts}`);
    assert.equal(await popup.evaluate(() => document.getElementById('status-dot').classList.contains('ok')), true, 'status dot is ok');

    upstreamBodies.length = 0;
    await sendMessage(popup, 'hello from firefox');
    await waitFor(popup, () => {
      const m = [...document.querySelectorAll('.message.assistant')].pop();
      return !!(m && m.querySelector('.route-line'));
    }, 'reply with a route line');
    const reply = await lastReply(popup);
    assert.equal(reply.error, false, `no error bubble: ${reply.text}`);
    assert.equal(reply.text, `${MOCK_PREFIX}hello from firefox`);
    assert.match(reply.route, /mock\s*·\s*mock-chat/, `route line shows provider/model: ${reply.route}`);
    console.log(`       reply: "${reply.text}"  route: "${reply.route}"`);
    assert.equal(upstreamBodies.length, 1);
    assert.equal(upstreamBodies[0].stream, true, 'sent as a streaming request');
    await waitFor(popup, () => /^Spend this month: \$\d+\.\d\d/.test(document.getElementById('spend-line').textContent), 'spend line');
    const spend = await popup.$eval('#spend-line', (el) => el.textContent);
    console.log(`       ${spend}`);
    assert.deepEqual(errors, []);
    await shot(popup, 'popup-chat', { width: 360, height: 600 });
  });

  // d. selection flow
  await step('d. selection flow: pending selection reaches the gateway with the page title; menu item registered', async () => {
    const selected = 'Prism routes every prompt to the best model';
    // What background.js does on a context-menu click: setPendingSelection(...)
    await options.evaluate(async (base, sel) => {
      const { setPendingSelection } = await import(`${base}/common/storage.js`);
      await setPendingSelection({ text: sel, pageUrl: 'https://example.org/article', pageTitle: 'Prism Test Page' });
    }, EXT, selected);
    const win = await openExt('/popup/popup.html?from=context-menu', 'popup-ctx');
    await waitFor(win, () => document.getElementById('message-input').value.includes('Prism Test Page'), 'prefilled prompt');
    const prefill = await win.$eval('#message-input', (el) => el.value);
    assert.ok(prefill.includes(`> ${selected}`) && prefill.includes('(from "Prism Test Page")'), `prefill: ${prefill}`);
    assert.equal(await win.evaluate(async () => (await chrome.storage.local.get('prism.pendingSelection'))['prism.pendingSelection']), undefined, 'pending selection consumed');
    upstreamBodies.length = 0;
    await win.click('#message-input');
    await win.keyboard.type('Summarise.');
    await win.keyboard.press('Enter');
    await waitFor(win, () => !!document.querySelector('.message.assistant .route-line'), 'reply');
    assert.equal(upstreamBodies.length, 1);
    const sent = upstreamBodies[0].messages.at(-1).content;
    assert.ok(sent.includes(selected), `prompt has the selected text: ${sent}`);
    assert.ok(sent.includes('Prism Test Page'), `prompt has the page title: ${sent}`);
    const reply = await lastReply(win);
    assert.ok(reply.text.startsWith(MOCK_PREFIX), `reply arrived: ${reply.text}`);
    await win.close();

    // Context menu item: ask Firefox to create the same id from an extension
    // page. Menu items are per-extension, so a duplicate id is refused when
    // background.js's registration worked; success would mean it was missing.
    const dup = await options.evaluate(
      () => new Promise((res) => {
        const r = chrome.contextMenus.create({ id: 'dl-prism-ask-selection', title: 'probe', contexts: ['selection'] }, () => res(chrome.runtime.lastError ? chrome.runtime.lastError.message : null));
        if (r === undefined) return;
      }),
    );
    if (dup === null) {
      await options.evaluate(() => chrome.contextMenus.remove('dl-prism-ask-selection'));
      assert.fail('context menu item dl-prism-ask-selection was NOT registered (probe created it)');
    }
    assert.match(dup, /already exists/i, `unexpected probe result: ${dup}`);
    console.log(`       context menu item registered (probe: "${dup}")`);
  });

  // e. background event page
  await step('e. background event page (module script) started with no errors', async () => {
    const info = await options.evaluate(async () => {
      const bg = await chrome.runtime.getBackgroundPage();
      return bg ? { href: bg.location.href, scripts: [...bg.document.scripts].map((s) => `${s.type}:${s.src.split('/').pop()}`) } : null;
    });
    assert.ok(info, 'getBackgroundPage returned the event page');
    assert.ok(info.scripts.some((s) => s.startsWith('module:')), `module script loaded: ${JSON.stringify(info)}`);
    console.log(`       ${info.href} scripts=${info.scripts.join(',')}`);
    assert.deepEqual(errors, []);
  });

  await step('no extension console errors over the whole run', async () => {
    assert.deepEqual(errors, []);
  });
} finally {
  await browser.close().catch(() => {});
  gateway.kill();
  mock.close();
  await rm(profile, { recursive: true, force: true });
  await rm(gwHome, { recursive: true, force: true });
}

if (warnings.length) console.log(`warnings:\n  ${[...new Set(warnings)].join('\n  ')}`);
const failed = results.filter((r) => !r).length;
console.log(failed ? `${failed} failed` : `all ${results.length} passed`, `in ${((Date.now() - t0) / 1000).toFixed(1)} s`);
process.exit(failed ? 1 : 0);
