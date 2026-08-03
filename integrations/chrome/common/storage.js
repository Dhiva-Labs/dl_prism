// storage.js — chrome.storage.local wrapper for extension settings and the
// transient "selection" handoff between the context-menu action and the
// popup window it opens. Browser-only (uses chrome.*), unlike api.js.

import { DEFAULTS } from './api.js';

const SETTINGS_KEY = 'prism.settings';
const PENDING_SELECTION_KEY = 'prism.pendingSelection';

const SETTINGS_DEFAULTS = Object.freeze({
  baseUrl: DEFAULTS.baseUrl,
  apiKey: '',
  defaultModel: 'auto',
});

/** Reads settings, filling in defaults for anything not yet stored. */
export async function getSettings() {
  const stored = await chrome.storage.local.get(SETTINGS_KEY);
  return { ...SETTINGS_DEFAULTS, ...(stored[SETTINGS_KEY] || {}) };
}

/** Merges `partial` into the stored settings and persists the result. */
export async function saveSettings(partial) {
  const current = await getSettings();
  const next = { ...current, ...partial };
  await chrome.storage.local.set({ [SETTINGS_KEY]: next });
  return next;
}

/** Convenience: settings shaped the way api.js wants ({baseUrl, apiKey}). */
export async function getApiConfig() {
  const { baseUrl, apiKey } = await getSettings();
  return { baseUrl, apiKey };
}

/**
 * Stashes the selected text (plus originating page title/url) so the popup
 * window opened by the context-menu handler can pick it up on load. Cleared
 * by takePendingSelection() so it's only ever consumed once.
 */
export async function setPendingSelection(selection) {
  await chrome.storage.local.set({
    [PENDING_SELECTION_KEY]: { ...selection, ts: Date.now() },
  });
}

/** Reads and clears the pending selection, if any. */
export async function takePendingSelection() {
  const stored = await chrome.storage.local.get(PENDING_SELECTION_KEY);
  const value = stored[PENDING_SELECTION_KEY] || null;
  if (value) {
    await chrome.storage.local.remove(PENDING_SELECTION_KEY);
  }
  return value;
}

export { SETTINGS_DEFAULTS };
