import * as vscode from "vscode";
import { ApiClient } from "./apiClient";
import { PrismConfig } from "./config";

const POLL_INTERVAL_MS = 60_000;

/**
 * Status bar item showing the configured default model and month-to-date
 * spend, refreshed every 60s from /admin/usage. Tolerates the gateway
 * being offline by falling back to a quiet "prism offline" label instead
 * of throwing or repeatedly popping up error notifications.
 */
export class PrismStatusBar implements vscode.Disposable {
  private readonly item: vscode.StatusBarItem;
  private timer: ReturnType<typeof setInterval> | undefined;

  constructor(
    private readonly apiClient: ApiClient,
    private readonly getConfig: () => PrismConfig
  ) {
    this.item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    this.item.name = "DL Prism";
    this.item.command = "dlPrism.switchModel";
    this.item.text = "$(zap) prism …";
    this.item.tooltip = "DL Prism — click to switch model";
    this.item.show();
  }

  start(): void {
    void this.refresh();
    this.timer = setInterval(() => void this.refresh(), POLL_INTERVAL_MS);
  }

  /** Re-render the model portion immediately (e.g. right after a settings change) without waiting on a network call. */
  onModelChanged(): void {
    const { model } = this.getConfig();
    const spendMatch = this.item.text.match(/\$([\d.,]+)$/);
    this.item.text = spendMatch ? `$(zap) prism ${model} · $${spendMatch[1]}` : `$(zap) prism ${model}`;
  }

  async refresh(): Promise<void> {
    const { model } = this.getConfig();
    try {
      const usage = await this.apiClient.getUsage();
      const spend = usage.month_total_usd.toFixed(2);
      this.item.text = `$(zap) prism ${model} · $${spend}`;
      const budgetNote = usage.budget_exceeded
        ? ` — budget of $${usage.budget_usd} exceeded`
        : ` (budget $${usage.budget_usd})`;
      this.item.tooltip = `DL Prism — model: ${model}, month spend: $${spend}${budgetNote}\nClick to switch model`;
      this.item.backgroundColor = usage.budget_exceeded
        ? new vscode.ThemeColor("statusBarItem.warningBackground")
        : undefined;
    } catch {
      // Gateway down/unreachable — degrade quietly, no repeated popups.
      this.item.text = "$(zap) prism offline";
      this.item.tooltip = "DL Prism gateway is unreachable. Click to switch model once it is back.";
      this.item.backgroundColor = undefined;
    }
  }

  dispose(): void {
    if (this.timer) clearInterval(this.timer);
    this.item.dispose();
  }
}
