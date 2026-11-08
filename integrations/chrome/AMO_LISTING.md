# addons.mozilla.org listing

Copy for the Firefox Add-ons developer hub.

## Files

| Upload | Make it with |
|---|---|
| Add-on package | `node tool/package.mjs` → `dist/dl-prism-companion-firefox-<version>.zip` |
| Source code | Not needed: the package is the repository's own JavaScript, copied unchanged (no bundler, minifier or generator). Answer "No" on the source step. |

Distribution: **On this site** (listed). Compatibility: Firefox for desktop.
Leave Firefox for Android unticked until it has been tried there.

## Describe add-on

**Name:** DL Prism Companion

**Add-on URL:** `dl-prism-companion`

**Summary** (250 characters at most):

> Chat with your local DL Prism AI gateway, pick models, and see routing decisions right from the browser toolbar.

**Description:**

```
DL Prism Companion is a toolbar client for DL Prism, an open-source AI gateway you run on your own computer. It needs DL Prism running (prism serve): https://github.com/Dhiva-Labs/dl_prism

**What it does**

- Chat from the toolbar popup, with replies streamed from your gateway.
- Pick a model, or "auto" to let your gateway's routing rules choose.
- See which provider and model answered, and why, under each reply.
- Select text on any page and choose "Ask DL Prism about …" from the right-click menu.
- See this month's spend from your gateway.

**What it sends, and where**

Only to your gateway, at 127.0.0.1 or localhost: the messages you type, and for "Ask DL Prism about …", the selected text and the page title. Your gateway then sends requests to the AI providers you set up in it. Because of the selection command, Firefox asks you at install to allow sending website content.

The add-on has no server, account, analytics or tracking.

**Settings**

The gateway address (default http://127.0.0.1:8130), the gateway's API key if you set one, and a default model.

DL Prism and this add-on are open source under the Apache License 2.0.
```

**Categories:** Web Development

**Tags:** none (AMO's fixed tag list has nothing for AI or gateways)

**Support website:** https://github.com/Dhiva-Labs/dl_prism/issues

**Homepage:** https://github.com/Dhiva-Labs/dl_prism

**License:** Apache License 2.0

**Privacy policy:** tick "This add-on has a privacy policy" and paste
[PRIVACY.md](PRIVACY.md).

**Version release notes (1.0.0):**

> First Firefox release: toolbar chat with streaming replies, model picker with "auto", the route behind each reply, "Ask DL Prism about …" for selected text, and this month's spend.

## Notes to reviewer

```
The add-on is a client for DL Prism, a gateway the user runs locally. You can test it without any AI provider account using the mock kit in the repository (integrations/chrome/test/):

1. Get the gateway: the prism-0.2.0-linux-amd64 binary from https://github.com/Dhiva-Labs/dl_prism/releases/tag/v0.2.0, or `go build ./cmd/prism` in the repository.
2. node integrations/chrome/test/mock-upstream.mjs   (an OpenAI-compatible stand-in; Node.js only)
3. prism serve -c integrations/chrome/test/prism.mock.yaml   (gateway on 127.0.0.1:8130)
4. Install the add-on. Open the popup: if Firefox hasn't granted access to 127.0.0.1/localhost, click "Grant access to your local gateway".
5. Options: enter the API key from prism.mock.yaml and press "Test connection". Then chat from the popup, or select text on a page and use "Ask DL Prism about …".

Data collection: websiteContent is declared as required because "Ask DL Prism about …" sends the selected text and page title to the user's own gateway on localhost. Nothing goes anywhere else.

Source: the package is the repository's integrations/chrome files, unchanged; tool/build.mjs only writes the Firefox manifest (event page instead of a service worker, plus browser_specific_settings).
```
