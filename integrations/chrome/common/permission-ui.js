// permission-ui.js — wires up the "grant access" banner + button shared by
// popup.js and options.js.
//
// On Chrome, host_permissions are granted at install time, so
// permissionsContains() resolves true immediately and the banner is simply
// never shown — a deliberate no-op, not a browser check. On Firefox (MV3),
// host_permissions are NOT auto-granted, so this banner is the one-time
// prompt that lets the user opt in via permissions.request(), which — per
// the WebExtensions permission model — must run synchronously inside a
// click handler so it carries the required user gesture.

import { permissionsContains, permissionsRequest, HOST_PERMISSIONS } from './compat.js';

/**
 * @param {object} opts
 * @param {HTMLElement} opts.bannerEl - container toggled via the "hidden" class
 * @param {HTMLElement} [opts.buttonEl] - the "Grant access…" button, if present on this page
 * @param {(granted: boolean) => void} [opts.onChange] - called after every check/request with the current state
 * @returns {Promise<boolean>} whether the permission is currently granted
 */
export async function initPermissionGate({ bannerEl, buttonEl, onChange }) {
  async function refresh() {
    const granted = await permissionsContains(HOST_PERMISSIONS);
    bannerEl.classList.toggle('hidden', granted);
    if (onChange) onChange(granted);
    return granted;
  }

  if (buttonEl) {
    buttonEl.addEventListener('click', async () => {
      buttonEl.disabled = true;
      try {
        await permissionsRequest(HOST_PERMISSIONS);
      } catch (_err) {
        // Request API unavailable, or the user dismissed the prompt — the
        // banner just stays visible so they can try again.
      } finally {
        buttonEl.disabled = false;
        await refresh();
      }
    });
  }

  return refresh();
}
