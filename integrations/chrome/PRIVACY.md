# DL Prism Companion privacy policy

Last updated: 2026-11-08

This policy covers the DL Prism Companion extension for Chrome and Firefox.

## Summary

DL Prism Companion is a client for DL Prism, an AI gateway you run on your
own computer. The extension talks only to that gateway. Dhiva Labs runs no
server for it and receives nothing from it: no account, no analytics, no
crash reports, no tracking.

## What the extension sends, and where

Everything goes to your gateway, at the address you set in the extension's
options (by default `http://127.0.0.1:8130`). The extension can only reach
`127.0.0.1` and `localhost`.

- **Messages you type** in the toolbar popup, and the conversation so far.
- **"Ask DL Prism about …"**: when you choose it from the right-click menu,
  the text you selected and the title of the page it came from. Nothing is
  read from a page unless you choose this.
- **Your gateway API key**, if you set one, as the `Authorization` header.
- **Requests for the model list and this month's usage**, to show the model
  picker and the spend line.

Your gateway then sends your requests on to the AI providers you configured
in it (for example a local model, or a cloud provider with your own key).
What those providers keep is set by their own policies and by your gateway's
configuration, not by this extension.

## What stays in your browser

- In the browser's extension storage: the gateway address, the gateway API
  key and your default model.
- Briefly, the selected text, page title and page address from "Ask DL
  Prism about …", until the popup opens and reads them. They are then
  deleted.
- The conversation lives in memory only while the popup is open. Closing it
  discards the conversation.

Uninstalling the extension deletes its storage.

## Permissions

- **Storage:** keeps the settings above.
- **Context menus:** adds "Ask DL Prism about …" for selected text.
- **Access to `127.0.0.1` and `localhost`:** to reach your gateway. On
  Firefox you grant this from the banner the extension shows.

## Contact

Questions: reachout@dhivalabs.com. Source code:
https://github.com/Dhiva-Labs/dl_prism

## Changes

- 2026-11-08: first version, for the Firefox Add-ons release.
