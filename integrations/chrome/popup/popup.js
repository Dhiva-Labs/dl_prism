// popup.js — chat UI logic. Runs both as the toolbar popup and as the
// standalone window opened by the "Ask DL Prism about ..." context menu
// (background.js opens this same page with ?from=context-menu).

import { chatCompletion, listModels, getUsage, testConnection, PrismUnreachableError } from '../common/api.js';
import { getSettings, getApiConfig, takePendingSelection } from '../common/storage.js';
import { initPermissionGate } from '../common/permission-ui.js';

const statusDot = document.getElementById('status-dot');
const modelSelect = document.getElementById('model-select');
const openOptionsBtn = document.getElementById('open-options');
const permissionBanner = document.getElementById('permission-banner');
const grantAccessBtn = document.getElementById('grant-access-btn');
const offlineBanner = document.getElementById('offline-banner');
const offlineMessage = document.getElementById('offline-message');
const retryBtn = document.getElementById('retry-connection');
const bannerOpenOptionsBtn = document.getElementById('banner-open-options');
const chatLog = document.getElementById('chat-log');
const composer = document.getElementById('composer');
const messageInput = document.getElementById('message-input');
const sendBtn = document.getElementById('send-btn');
const spendLine = document.getElementById('spend-line');
const dashboardLink = document.getElementById('dashboard-link');

/** @type {{role: 'user'|'assistant', content: string}[]} */
const history = [];
let cfg = { baseUrl: 'http://127.0.0.1:8130', apiKey: '' };
let sending = false;

init();

async function init() {
  const settings = await getSettings();
  cfg = { baseUrl: settings.baseUrl, apiKey: settings.apiKey };
  dashboardLink.href = settings.baseUrl;

  renderEmptyState();

  // On Chrome this resolves granted:true immediately (host_permissions are
  // install-time granted) and the banner never shows. On Firefox it's the
  // one-time "Grant access to your local gateway" prompt — see
  // common/permission-ui.js.
  const granted = await initPermissionGate({
    bannerEl: permissionBanner,
    buttonEl: grantAccessBtn,
    onChange: (isGranted) => {
      if (isGranted) refreshAll();
    },
  });

  if (granted) {
    await refreshAll();
  }
  await applyPendingSelectionIfAny();

  messageInput.focus();
}

async function refreshAll() {
  const settings = await getSettings();
  cfg = { baseUrl: settings.baseUrl, apiKey: settings.apiKey };
  dashboardLink.href = settings.baseUrl;
  await Promise.all([loadModels(settings.defaultModel), refreshUsage(), checkConnection()]);
}

function renderEmptyState() {
  chatLog.innerHTML = '';
  const empty = document.createElement('div');
  empty.className = 'empty-state';
  empty.id = 'empty-state';
  empty.textContent = 'Ask anything — DL Prism will route it to the best model.';
  chatLog.appendChild(empty);
}

function clearEmptyState() {
  const empty = document.getElementById('empty-state');
  if (empty) empty.remove();
}

async function loadModels(preferredModel) {
  try {
    const models = await listModels(cfg);
    const ids = models.map((m) => m.id);
    // "auto" always leads the list, regardless of the order the gateway sent.
    ids.sort((a, b) => (a === 'auto' ? -1 : b === 'auto' ? 1 : 0));

    modelSelect.innerHTML = '';
    for (const id of ids) {
      const opt = document.createElement('option');
      opt.value = id;
      opt.textContent = id;
      modelSelect.appendChild(opt);
    }
    if (ids.length === 0) {
      const opt = document.createElement('option');
      opt.value = 'auto';
      opt.textContent = 'auto';
      modelSelect.appendChild(opt);
    }

    modelSelect.value = ids.includes(preferredModel) ? preferredModel : ids.includes('auto') ? 'auto' : ids[0] || 'auto';
    modelSelect.disabled = false;
  } catch (_err) {
    // Model list failed — leave a lone "auto" option so the chat still works
    // once the gateway comes back, and let checkConnection() surface the error.
    modelSelect.innerHTML = '<option value="auto">auto</option>';
  }
}

async function refreshUsage() {
  try {
    const usage = await getUsage(cfg);
    const spent = Number(usage.month_total_usd || 0).toFixed(2);
    const budget = usage.budget_usd ? ` / $${Number(usage.budget_usd).toFixed(2)}` : '';
    const exceeded = usage.budget_exceeded ? ' (budget exceeded)' : '';
    spendLine.textContent = `Spend this month: $${spent}${budget}${exceeded}`;
  } catch (_err) {
    spendLine.textContent = 'Spend this month: —';
  }
}

async function checkConnection() {
  const result = await testConnection(cfg);
  if (result.ok) {
    statusDot.classList.remove('bad');
    statusDot.classList.add('ok');
    statusDot.title = 'Connected to DL Prism';
    hideOfflineBanner();
  } else {
    statusDot.classList.remove('ok');
    statusDot.classList.add('bad');
    statusDot.title = result.error;
    showOfflineBanner(result.error);
  }
  return result.ok;
}

function showOfflineBanner(message) {
  offlineMessage.textContent = `Can't reach the DL Prism gateway at ${cfg.baseUrl}. ${message || ''}`.trim();
  offlineBanner.classList.remove('hidden');
}

function hideOfflineBanner() {
  offlineBanner.classList.add('hidden');
}

async function applyPendingSelectionIfAny() {
  const params = new URLSearchParams(location.search);
  if (params.get('from') !== 'context-menu') return;

  const pending = await takePendingSelection();
  if (!pending || !pending.text) return;

  const quoted = pending.text
    .split('\n')
    .map((line) => `> ${line}`)
    .join('\n');
  const sourceNote = pending.pageTitle ? `\n\n(from "${pending.pageTitle}")` : '';
  messageInput.value = `${quoted}${sourceNote}\n\n`;
  messageInput.focus();
  messageInput.setSelectionRange(messageInput.value.length, messageInput.value.length);
}

function appendMessage(role, text) {
  clearEmptyState();
  const wrapper = document.createElement('div');
  wrapper.className = `message ${role}`;

  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.textContent = text;
  wrapper.appendChild(bubble);

  chatLog.appendChild(wrapper);
  chatLog.scrollTop = chatLog.scrollHeight;
  return { wrapper, bubble };
}

function appendRouteLine(wrapper, routeInfo) {
  if (!routeInfo || (!routeInfo.provider && !routeInfo.model)) return;
  const line = document.createElement('div');
  line.className = 'route-line';
  const parts = [routeInfo.mode, routeInfo.provider, routeInfo.model].filter(Boolean).join(' · ');
  line.textContent = routeInfo.reason ? `${parts} — ${routeInfo.reason}` : parts;
  wrapper.appendChild(line);
}

composer.addEventListener('submit', (e) => {
  e.preventDefault();
  sendMessage();
});

messageInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    sendMessage();
  }
});

async function sendMessage() {
  if (sending) return;
  const text = messageInput.value.trim();
  if (!text) return;

  sending = true;
  sendBtn.disabled = true;
  messageInput.value = '';

  appendMessage('user', text);
  history.push({ role: 'user', content: text });

  const { wrapper, bubble } = appendMessage('assistant', '');
  bubble.classList.add('pending');

  const model = modelSelect.value || 'auto';
  let assistantText = '';

  try {
    const { routeInfo } = await chatCompletion(
      cfg,
      { model, messages: history, stream: true },
      {
        onDelta: (delta) => {
          bubble.classList.remove('pending');
          assistantText += delta;
          bubble.textContent = assistantText;
          chatLog.scrollTop = chatLog.scrollHeight;
        },
      },
    );
    bubble.classList.remove('pending');
    if (!assistantText) bubble.textContent = '(empty reply)';
    history.push({ role: 'assistant', content: assistantText });
    appendRouteLine(wrapper, routeInfo);
    hideOfflineBanner();
    refreshUsage();
  } catch (err) {
    bubble.classList.remove('pending');
    bubble.classList.add('error');
    if (err instanceof PrismUnreachableError) {
      bubble.textContent = `Can't reach the DL Prism gateway at ${cfg.baseUrl}.`;
      showOfflineBanner(err.message);
      statusDot.classList.remove('ok');
      statusDot.classList.add('bad');
    } else {
      bubble.textContent = `Error: ${err.message}`;
    }
    // Drop the failed turn from history so a retry doesn't resend a broken exchange.
    history.pop();
  } finally {
    sending = false;
    sendBtn.disabled = false;
    messageInput.focus();
  }
}

openOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());
bannerOpenOptionsBtn.addEventListener('click', () => chrome.runtime.openOptionsPage());
retryBtn.addEventListener('click', refreshAll);
