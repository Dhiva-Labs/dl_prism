// options.js — settings form: base URL, API key, default model, plus a
// "Test connection" button that hits /v1/models with whatever is currently
// typed in the form (not necessarily saved yet).

import { testConnection } from '../common/api.js';
import { getSettings, saveSettings } from '../common/storage.js';
import { initPermissionGate } from '../common/permission-ui.js';

const form = document.getElementById('settings-form');
const baseUrlInput = document.getElementById('base-url');
const apiKeyInput = document.getElementById('api-key');
const defaultModelInput = document.getElementById('default-model');
const testBtn = document.getElementById('test-connection');
const saveStatus = document.getElementById('save-status');
const testStatus = document.getElementById('test-status');
const permissionBanner = document.getElementById('permission-banner');
const grantAccessBtn = document.getElementById('grant-access-btn');

// Matches the extension's host_permissions in manifest.json — anything else
// would silently fail at fetch time, so we reject it here with a clear reason.
const ALLOWED_HOST_PATTERN = /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?(\/.*)?$/i;

init();

async function init() {
  const settings = await getSettings();
  baseUrlInput.value = settings.baseUrl;
  apiKeyInput.value = settings.apiKey;
  defaultModelInput.value = settings.defaultModel;

  // No-op on Chrome (host_permissions are install-time granted there); on
  // Firefox this surfaces the one-time "Grant access…" prompt. See
  // common/permission-ui.js.
  await initPermissionGate({ bannerEl: permissionBanner, buttonEl: grantAccessBtn });
}

function showStatus(el, message, ok) {
  el.textContent = message;
  el.classList.remove('hidden', 'ok', 'error');
  el.classList.add(ok ? 'ok' : 'error');
}

function currentConfig() {
  return {
    baseUrl: baseUrlInput.value.trim().replace(/\/+$/, '') || 'http://127.0.0.1:8130',
    apiKey: apiKeyInput.value.trim(),
  };
}

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const cfg = currentConfig();

  if (!ALLOWED_HOST_PATTERN.test(cfg.baseUrl)) {
    showStatus(
      saveStatus,
      'Base URL must be http://127.0.0.1 or http://localhost (matching this extension\'s host permissions).',
      false,
    );
    return;
  }

  await saveSettings({
    baseUrl: cfg.baseUrl,
    apiKey: cfg.apiKey,
    defaultModel: defaultModelInput.value.trim() || 'auto',
  });
  showStatus(saveStatus, 'Saved.', true);
});

testBtn.addEventListener('click', async () => {
  const cfg = currentConfig();
  if (!ALLOWED_HOST_PATTERN.test(cfg.baseUrl)) {
    showStatus(testStatus, 'Fix the base URL before testing (see above).', false);
    return;
  }

  testStatus.classList.remove('hidden', 'ok', 'error');
  testStatus.textContent = 'Testing…';

  const result = await testConnection(cfg);
  if (result.ok) {
    showStatus(testStatus, `Connected — ${result.count} model(s) available.`, true);
  } else {
    showStatus(testStatus, `Failed: ${result.error}`, false);
  }
});
