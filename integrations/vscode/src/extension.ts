import * as vscode from "vscode";
import { ApiClient } from "./apiClient";
import { ChatViewProvider } from "./chatViewProvider";
import { onConfigChanged, readConfig, setModel } from "./config";
import { PrismStatusBar } from "./statusBar";
import { PrismModel } from "./types";

let outputChannel: vscode.OutputChannel | undefined;

function getOutputChannel(): vscode.OutputChannel {
  if (!outputChannel) {
    outputChannel = vscode.window.createOutputChannel("DL Prism Routing");
  }
  return outputChannel;
}

function describeModel(m: PrismModel): string {
  const meta = m.prism;
  if (!meta) return m.owned_by ?? "";
  const bits: string[] = [];
  bits.push(meta.local === false ? "cloud" : meta.local ? "local" : "");
  if (meta.capabilities?.length) bits.push(meta.capabilities.join(","));
  if (meta.cost_per_mtok && (meta.cost_per_mtok.input || meta.cost_per_mtok.output)) {
    bits.push(`$${meta.cost_per_mtok.input ?? 0}/$${meta.cost_per_mtok.output ?? 0} per Mtok`);
  } else if (meta.local) {
    bits.push("free");
  }
  return bits.filter(Boolean).join(" · ");
}

export function activate(context: vscode.ExtensionContext): void {
  const initialConfig = readConfig();
  const apiClient = new ApiClient({ baseUrl: initialConfig.baseUrl, apiKey: initialConfig.apiKey });

  const chatProvider = new ChatViewProvider(context.extensionUri, apiClient, readConfig);
  const statusBar = new PrismStatusBar(apiClient, readConfig);

  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider(ChatViewProvider.viewId, chatProvider, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
    statusBar,
    getOutputChannel(),
    onConfigChanged(() => {
      const cfg = readConfig();
      apiClient.updateConfig({ baseUrl: cfg.baseUrl, apiKey: cfg.apiKey });
      statusBar.onModelChanged();
      void statusBar.refresh();
      chatProvider.notifyConfigChanged(cfg.baseUrl, cfg.model);
    })
  );

  statusBar.start();

  context.subscriptions.push(
    vscode.commands.registerCommand("dlPrism.focusChat", async () => {
      await vscode.commands.executeCommand(`${ChatViewProvider.viewId}.focus`);
    }),

    vscode.commands.registerCommand("dlPrism.clearChat", () => {
      chatProvider.clear();
    }),

    vscode.commands.registerCommand("dlPrism.openDashboard", async () => {
      const { baseUrl } = readConfig();
      try {
        await vscode.env.openExternal(vscode.Uri.parse(baseUrl));
      } catch {
        void vscode.window.showErrorMessage(`DL Prism: could not open ${baseUrl}.`);
      }
    }),

    vscode.commands.registerCommand("dlPrism.switchModel", async () => {
      await runSwitchModel(apiClient, statusBar);
    }),

    vscode.commands.registerCommand("dlPrism.explainRouting", async () => {
      await runExplainRouting(apiClient);
    }),

    vscode.commands.registerCommand("dlPrism.askAboutSelection", async () => {
      await runAskAboutSelection(chatProvider);
    })
  );
}

export function deactivate(): void {
  // Disposables registered via context.subscriptions handle cleanup.
}

async function runSwitchModel(apiClient: ApiClient, statusBar: PrismStatusBar): Promise<void> {
  const quickPick = vscode.window.createQuickPick<vscode.QuickPickItem & { modelId: string }>();
  quickPick.title = "DL Prism: Switch Model";
  quickPick.placeholder = "Loading models…";
  quickPick.busy = true;
  quickPick.show();

  try {
    const models = await apiClient.listModels();
    const sorted = [...models].sort((a, b) => {
      if (a.id === "auto") return -1;
      if (b.id === "auto") return 1;
      return a.id.localeCompare(b.id);
    });
    const current = readConfig().model;
    quickPick.items = sorted.map((m) => ({
      label: m.id === current ? `$(check) ${m.id}` : m.id,
      description: describeModel(m),
      modelId: m.id,
    }));
    quickPick.placeholder = "Select the default model for DL Prism";
    quickPick.busy = false;
  } catch (err) {
    quickPick.hide();
    quickPick.dispose();
    void vscode.window.showErrorMessage(
      `DL Prism: could not load models (${err instanceof Error ? err.message : "gateway unreachable"}).`
    );
    return;
  }

  quickPick.onDidAccept(() => {
    const picked = quickPick.selectedItems[0];
    quickPick.hide();
    if (picked) {
      void setModel(picked.modelId).then(() => statusBar.onModelChanged());
    }
  });
  quickPick.onDidHide(() => quickPick.dispose());
}

async function runExplainRouting(apiClient: ApiClient): Promise<void> {
  const prompt = await vscode.window.showInputBox({
    title: "DL Prism: Explain Routing",
    prompt: "Enter a prompt to see how DL Prism would route it",
    placeHolder: "e.g. Refactor this function to be async",
    ignoreFocusOut: true,
  });
  if (prompt === undefined || prompt.trim() === "") return;

  const { model } = readConfig();
  try {
    const result = await apiClient.explainRoute([{ role: "user", content: prompt }], model);
    const channel = getOutputChannel();
    channel.appendLine("─".repeat(60));
    channel.appendLine(`Prompt: ${prompt}`);
    channel.appendLine(`Requested model: ${model}`);
    channel.appendLine(`Mode: ${result.mode}`);
    channel.appendLine(`Reason: ${result.reason}`);
    if (typeof result.est_input_cost_usd === "number") {
      channel.appendLine(`Estimated input cost: $${result.est_input_cost_usd}`);
    }
    channel.appendLine("Chain:");
    for (const step of result.chain ?? []) {
      channel.appendLine(`  - ${step.provider}/${step.model} (${step.local ? "local" : "cloud"})`);
    }
    channel.show(true);

    const top = result.chain?.[0];
    const summary = top
      ? `DL Prism would route to ${top.provider}/${top.model} (${result.mode}). ${result.reason}`
      : `DL Prism routing: ${result.mode}. ${result.reason}`;
    void vscode.window.showInformationMessage(summary, "Show Details").then((choice) => {
      if (choice === "Show Details") channel.show(true);
    });
  } catch (err) {
    void vscode.window.showErrorMessage(
      `DL Prism: could not explain routing (${err instanceof Error ? err.message : "gateway unreachable"}).`
    );
  }
}

async function runAskAboutSelection(chatProvider: ChatViewProvider): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.selection.isEmpty) {
    void vscode.window.showInformationMessage("DL Prism: select some code first.");
    return;
  }
  const code = editor.document.getText(editor.selection);
  const language = editor.document.languageId;

  const question = await vscode.window.showInputBox({
    title: "DL Prism: Ask About Selection",
    prompt: "What do you want to ask about the selected code?",
    placeHolder: "e.g. What does this function do?",
    ignoreFocusOut: true,
  });
  if (question === undefined || question.trim() === "") return;

  try {
    await chatProvider.revealAndAsk(question, code, language);
  } catch (err) {
    void vscode.window.showErrorMessage(
      `DL Prism: could not open the chat view (${err instanceof Error ? err.message : "unknown error"}).`
    );
  }
}
