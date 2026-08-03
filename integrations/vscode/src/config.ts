import * as vscode from "vscode";

export interface PrismConfig {
  baseUrl: string;
  apiKey: string;
  model: string;
}

const SECTION = "dlPrism";

export function readConfig(): PrismConfig {
  const cfg = vscode.workspace.getConfiguration(SECTION);
  const baseUrl = (cfg.get<string>("baseUrl") || "http://127.0.0.1:8130").trim().replace(/\/+$/, "");
  const apiKey = cfg.get<string>("apiKey") || "";
  const model = cfg.get<string>("model") || "auto";
  return { baseUrl, apiKey, model };
}

export async function setModel(model: string): Promise<void> {
  const cfg = vscode.workspace.getConfiguration(SECTION);
  await cfg.update("model", model, vscode.ConfigurationTarget.Global);
}

export function onConfigChanged(listener: () => void): vscode.Disposable {
  return vscode.workspace.onDidChangeConfiguration((e) => {
    if (e.affectsConfiguration(SECTION)) {
      listener();
    }
  });
}
