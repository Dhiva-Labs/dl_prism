import * as vscode from "vscode";
import { ApiClient } from "./apiClient";
import { PrismConfig } from "./config";
import { ChatMessage, ChatStreamEvent, PrismModel } from "./types";

/** Messages sent from the webview to the extension host. */
type InboundMessage =
  | { type: "ready" }
  | { type: "send"; requestId: string; model: string; messages: ChatMessage[] }
  | { type: "cancel"; requestId: string }
  | { type: "refreshModels" };

/** Messages sent from the extension host to the webview. */
type OutboundMessage =
  | { type: "init"; baseUrl: string; model: string }
  | { type: "models"; models: PrismModel[]; selected: string }
  | { type: "modelsError"; message: string }
  | { type: "streamEvent"; requestId: string; event: ChatStreamEvent }
  | { type: "clear" }
  | { type: "prefill"; question: string; code: string; language: string; autoSend: boolean }
  | { type: "configChanged"; baseUrl: string; model: string };

export class ChatViewProvider implements vscode.WebviewViewProvider {
  static readonly viewId = "dlPrismChatView";

  private view: vscode.WebviewView | undefined;
  private readonly pending = new Map<string, AbortController>();

  constructor(
    private readonly extensionUri: vscode.Uri,
    private readonly apiClient: ApiClient,
    private readonly getConfig: () => PrismConfig
  ) {}

  resolveWebviewView(webviewView: vscode.WebviewView): void {
    this.view = webviewView;
    webviewView.webview.options = {
      enableScripts: true,
      localResourceRoots: [vscode.Uri.joinPath(this.extensionUri, "dist", "webview")],
    };
    webviewView.webview.html = this.renderHtml(webviewView.webview);

    webviewView.webview.onDidReceiveMessage((msg: InboundMessage) => {
      void this.handleMessage(msg);
    });

    webviewView.onDidDispose(() => {
      for (const controller of this.pending.values()) controller.abort();
      this.pending.clear();
      this.view = undefined;
    });
  }

  /** Invoked by the "DL Prism: Clear Chat" command / view-title button. */
  clear(): void {
    this.post({ type: "clear" });
  }

  /** Invoked by the "DL Prism: Ask About Selection" command. */
  async revealAndAsk(question: string, code: string, language: string): Promise<void> {
    await vscode.commands.executeCommand(`${ChatViewProvider.viewId}.focus`);
    this.post({ type: "prefill", question, code, language, autoSend: true });
  }

  /** Invoked when dlPrism.* settings change, so the webview can re-sync (e.g. reload the model list). */
  notifyConfigChanged(baseUrl: string, model: string): void {
    this.post({ type: "configChanged", baseUrl, model });
  }

  private async handleMessage(msg: InboundMessage): Promise<void> {
    switch (msg.type) {
      case "ready": {
        const { baseUrl, model } = this.getConfig();
        this.post({ type: "init", baseUrl, model });
        await this.sendModels();
        return;
      }
      case "refreshModels":
        await this.sendModels();
        return;
      case "send":
        await this.handleSend(msg.requestId, msg.model, msg.messages);
        return;
      case "cancel": {
        const controller = this.pending.get(msg.requestId);
        controller?.abort();
        this.pending.delete(msg.requestId);
        return;
      }
    }
  }

  private async sendModels(): Promise<void> {
    try {
      const models = await this.apiClient.listModels();
      const { model } = this.getConfig();
      // "auto" first, as requested — the gateway already returns it first
      // in practice, but don't rely on that.
      const sorted = [...models].sort((a, b) => {
        if (a.id === "auto") return -1;
        if (b.id === "auto") return 1;
        return a.id.localeCompare(b.id);
      });
      this.post({ type: "models", models: sorted, selected: model });
    } catch (err) {
      this.post({
        type: "modelsError",
        message:
          err instanceof Error
            ? `Could not load models: ${err.message}`
            : "Could not load models from the gateway.",
      });
    }
  }

  private async handleSend(requestId: string, model: string, messages: ChatMessage[]): Promise<void> {
    const controller = new AbortController();
    this.pending.set(requestId, controller);
    try {
      await this.apiClient.chatStream(
        { model, messages, stream: true },
        (event) => this.post({ type: "streamEvent", requestId, event }),
        controller.signal
      );
    } catch (err) {
      // chatStream is designed not to throw, but degrade gracefully even if it does.
      this.post({
        type: "streamEvent",
        requestId,
        event: {
          type: "error",
          message: err instanceof Error ? err.message : "Unknown error contacting DL Prism.",
        },
      });
    } finally {
      this.pending.delete(requestId);
    }
  }

  private post(msg: OutboundMessage): void {
    void this.view?.webview.postMessage(msg);
  }

  private renderHtml(webview: vscode.Webview): string {
    const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(this.extensionUri, "dist", "webview", "main.js"));
    const styleUri = webview.asWebviewUri(vscode.Uri.joinPath(this.extensionUri, "dist", "webview", "style.css"));
    const nonce = getNonce();
    const csp = [
      `default-src 'none'`,
      `img-src ${webview.cspSource} data:`,
      `style-src ${webview.cspSource} 'unsafe-inline'`,
      `script-src 'nonce-${nonce}'`,
      `font-src ${webview.cspSource}`,
    ].join("; ");

    return /* html */ `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <link href="${styleUri}" rel="stylesheet" />
  <title>DL Prism Chat</title>
</head>
<body>
  <div id="root">
    <div id="offline-banner" class="banner hidden" role="status"></div>
    <div class="toolbar">
      <select id="model-select" aria-label="Model"></select>
      <button id="clear-btn" title="Clear conversation">Clear</button>
    </div>
    <div id="messages" aria-live="polite"></div>
    <form id="composer">
      <textarea id="input" rows="2" placeholder="Ask DL Prism…"></textarea>
      <button id="send-btn" type="submit">Send</button>
    </form>
  </div>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
  }
}

function getNonce(): string {
  let text = "";
  const possible = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  for (let i = 0; i < 32; i++) {
    text += possible.charAt(Math.floor(Math.random() * possible.length));
  }
  return text;
}
