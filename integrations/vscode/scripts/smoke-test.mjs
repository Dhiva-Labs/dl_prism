#!/usr/bin/env node
// Smoke-tests the extension's ApiClient module against a live DL Prism
// gateway, without needing VS Code itself. Exercises exactly what the
// extension does at runtime: list models, stream a chat completion, and
// read usage. Run `npm run smoke` (builds dist/apiClient.mjs first).
//
// Env overrides: PRISM_BASE_URL (default http://127.0.0.1:8130), PRISM_API_KEY.

import { ApiClient } from "../dist/apiClient.mjs";

const baseUrl = process.env.PRISM_BASE_URL || "http://127.0.0.1:8130";
const apiKey = process.env.PRISM_API_KEY || undefined;

const results = [];

function record(name, ok, detail) {
  results.push({ name, ok, detail });
  const mark = ok ? "PASS" : "FAIL";
  console.log(`[${mark}] ${name}${detail ? " — " + detail : ""}`);
}

async function main() {
  console.log(`DL Prism ApiClient smoke test against ${baseUrl}\n`);
  const client = new ApiClient({ baseUrl, apiKey, timeoutMs: 10000 });

  // 1. GET /v1/models
  let models = [];
  try {
    models = await client.listModels();
    const ids = models.map((m) => m.id);
    record(
      "listModels()",
      Array.isArray(models) && models.length > 0 && ids.includes("auto"),
      `${models.length} models: ${ids.join(", ")}`
    );
  } catch (err) {
    record("listModels()", false, err.message);
  }

  // 2. POST /v1/chat/completions with stream:true — verify route headers +
  //    incremental deltas + a terminal [DONE].
  await new Promise((resolve) => {
    const chunks = [];
    let route = null;
    let sawDone = false;
    let errorMsg = null;

    client
      .chatStream(
        { model: "auto", messages: [{ role: "user", content: "Say hello in exactly three words." }] },
        (event) => {
          if (event.type === "route") route = event.route;
          else if (event.type === "delta") chunks.push(event.content ?? "");
          else if (event.type === "error") errorMsg = event.message;
          else if (event.type === "done") sawDone = true;
        }
      )
      .then(() => {
        const full = chunks.join("");
        record(
          "chatStream() route headers",
          !!(route && (route.provider || route.model)),
          route ? `provider=${route.provider} model=${route.model} mode=${route.mode}` : "no route info"
        );
        record(
          "chatStream() streamed deltas",
          !errorMsg && chunks.length > 0 && full.length > 0,
          errorMsg ? errorMsg : `${chunks.length} chunks, ${full.length} chars: "${full.slice(0, 80)}"`
        );
        record("chatStream() terminal done", sawDone, sawDone ? "" : "stream never signalled done");
        resolve();
      })
      .catch((err) => {
        record("chatStream()", false, err.message);
        resolve();
      });
  });

  // 3. GET /admin/usage
  try {
    const usage = await client.getUsage();
    record(
      "getUsage()",
      typeof usage.month_total_usd === "number" && typeof usage.budget_usd === "number",
      `month_total_usd=${usage.month_total_usd} budget_usd=${usage.budget_usd} budget_exceeded=${usage.budget_exceeded}`
    );
  } catch (err) {
    record("getUsage()", false, err.message);
  }

  // 4. Bonus: POST /v1/route/explain (also part of ApiClient, cheap to check).
  try {
    const explain = await client.explainRoute([{ role: "user", content: "hi" }], "auto");
    record(
      "explainRoute() [bonus]",
      typeof explain.mode === "string" && Array.isArray(explain.chain),
      `mode=${explain.mode} chain=${(explain.chain || []).map((c) => `${c.provider}/${c.model}`).join(",")}`
    );
  } catch (err) {
    record("explainRoute() [bonus]", false, err.message);
  }

  // 5. Graceful-degradation check: point at an unreachable port and confirm
  //    we get a clean PrismApiError (offline:true), never an uncaught throw.
  try {
    const deadClient = new ApiClient({ baseUrl: "http://127.0.0.1:1", timeoutMs: 1500 });
    await deadClient.listModels();
    record("offline handling", false, "expected listModels() to reject against an unreachable port");
  } catch (err) {
    record("offline handling", err && err.name === "PrismApiError" && err.offline === true, err.message);
  }

  console.log();
  const failed = results.filter((r) => !r.ok);
  if (failed.length > 0) {
    console.log(`${results.length - failed.length}/${results.length} checks passed. FAILURES:`);
    for (const f of failed) console.log(`  - ${f.name}: ${f.detail}`);
    process.exitCode = 1;
  } else {
    console.log(`All ${results.length} checks passed.`);
  }
}

main().catch((err) => {
  console.error("Smoke test crashed:", err);
  process.exitCode = 1;
});
