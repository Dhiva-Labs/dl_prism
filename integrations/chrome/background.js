// background.js — MV3 service worker.
//
// Responsibilities:
//   1. Register the "Ask DL Prism about ..." selection context menu.
//   2. On click, stash the selection in chrome.storage.local and open the
//      popup page in a small standalone window (a context menu can't open
//      the toolbar popup directly, so this is the documented workaround).

import { setPendingSelection } from './common/storage.js';
import { contextMenusCreate, contextMenusRemoveAll, windowsCreate } from './common/compat.js';

const MENU_ID = 'dl-prism-ask-selection';
const POPUP_WINDOW_WIDTH = 420;
const POPUP_WINDOW_HEIGHT = 640;

async function registerContextMenu() {
  await contextMenusRemoveAll();
  await contextMenusCreate({
    id: MENU_ID,
    title: 'Ask DL Prism about "%s"',
    contexts: ['selection'],
  });
}

chrome.runtime.onInstalled.addListener(registerContextMenu);
chrome.runtime.onStartup.addListener(registerContextMenu);

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== MENU_ID || !info.selectionText) return;

  await setPendingSelection({
    text: info.selectionText,
    pageUrl: info.pageUrl || tab?.url || '',
    pageTitle: tab?.title || '',
  });

  // `type: 'popup'` + explicit width/height are supported by windows.create()
  // on both browsers — this is the documented workaround for a context menu
  // not being able to open the toolbar popup directly, on Chrome and Firefox
  // alike.
  await windowsCreate({
    url: chrome.runtime.getURL('popup/popup.html?from=context-menu'),
    type: 'popup',
    width: POPUP_WINDOW_WIDTH,
    height: POPUP_WINDOW_HEIGHT,
  });
});
