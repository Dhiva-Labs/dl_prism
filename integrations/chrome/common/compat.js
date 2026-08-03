// compat.js — small cross-browser MV3 helpers. Zero dependencies (no
// webextension-polyfill).
//
// Both Chrome and Firefox expose the extension APIs under the chrome.*
// namespace in Manifest V3 — Firefox added that alongside its native
// promise-based browser.* API specifically so cross-browser code can stay on
// chrome.* everywhere. We do exactly that. The one wrinkle: a few chrome.*
// methods (contextMenus.create/removeAll in particular) are documented as
// callback-only even under MV3 and do not resolve a promise when the
// callback is omitted. Rather than special-case per method or per browser,
// every call here always passes a callback *and* chains off a returned
// promise if one comes back — whichever calling convention the running
// browser actually implements, this resolves correctly.

function callWithCallback(fn, thisArg, args) {
  return new Promise((resolve, reject) => {
    let settled = false;
    const settle = (ok, value) => {
      if (settled) return;
      settled = true;
      if (ok) resolve(value);
      else reject(value);
    };
    const done = (result) => {
      const err = chrome.runtime && chrome.runtime.lastError;
      if (err) settle(false, new Error(err.message));
      else settle(true, result);
    };
    const maybeReturn = fn.apply(thisArg, [...args, done]);
    if (maybeReturn && typeof maybeReturn.then === 'function') {
      maybeReturn.then(
        (result) => settle(true, result),
        (err) => settle(false, err),
      );
    }
  });
}

/**
 * Host origins the "grant access" flow in popup/options checks and
 * requests. Must match manifest.json's host_permissions in both dist
 * targets — see tool/build.mjs.
 */
export const HOST_PERMISSIONS = ['http://127.0.0.1/*', 'http://localhost/*'];

export function contextMenusCreate(props) {
  return callWithCallback(chrome.contextMenus.create, chrome.contextMenus, [props]);
}

export function contextMenusRemoveAll() {
  return callWithCallback(chrome.contextMenus.removeAll, chrome.contextMenus, []);
}

export function windowsCreate(createData) {
  return callWithCallback(chrome.windows.create, chrome.windows, [createData]);
}

/**
 * True if every one of `origins` is already granted. Never throws — falls
 * back to `true` (i.e. don't gate the UI on it) if chrome.permissions isn't
 * available for some reason, since that API needs no manifest declaration
 * and both target browsers always expose it.
 */
export async function permissionsContains(origins) {
  if (!chrome.permissions || !chrome.permissions.contains) return true;
  try {
    return await callWithCallback(chrome.permissions.contains, chrome.permissions, [{ origins }]);
  } catch (_err) {
    return true;
  }
}

/**
 * Prompts the user to grant `origins`; resolves true if granted, false if
 * declined. Must be invoked synchronously from a user-gesture handler (e.g.
 * directly inside a click listener) — browsers require transient user
 * activation for permission prompts.
 */
export async function permissionsRequest(origins) {
  if (!chrome.permissions || !chrome.permissions.request) return true;
  return callWithCallback(chrome.permissions.request, chrome.permissions, [{ origins }]);
}
