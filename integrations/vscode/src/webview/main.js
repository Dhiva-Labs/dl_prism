// DL Prism chat webview client.
//
// Plain vanilla JS on purpose (no bundler, no framework, no runtime deps):
// this file is loaded as-is by the webview via a <script> tag. It talks to
// the extension host exclusively through postMessage — it never calls the
// gateway directly (see the extension's chatViewProvider.ts / apiClient.ts
// for why: the extension host is a plain Node fetch context with no CORS
// restrictions, which is what makes SSE streaming reliable here; a webview
// fetch() is subject to the page's CSP/origin sandboxing and the gateway
// sends no CORS headers, so it cannot reliably fetch cross-origin itself).
(function () {
  const vscode = acquireVsCodeApi();

  /** @type {{ id:string, role:'user'|'assistant', content:string, streaming?:boolean, error?:string, route?:any }[]} */
  let messages = [];
  let models = [];
  let selectedModel = "auto";
  let activeRequestId = null;
  let reqCounter = 0;

  const els = {
    root: document.getElementById("root"),
    banner: document.getElementById("offline-banner"),
    modelSelect: document.getElementById("model-select"),
    clearBtn: document.getElementById("clear-btn"),
    messages: document.getElementById("messages"),
    composer: document.getElementById("composer"),
    input: document.getElementById("input"),
    sendBtn: document.getElementById("send-btn"),
  };

  // ---- persisted state (survives the webview being hidden/reshown) ----
  const prior = vscode.getState();
  if (prior && Array.isArray(prior.messages)) {
    messages = prior.messages;
    selectedModel = prior.selectedModel || "auto";
  }

  function saveState() {
    vscode.setState({ messages, selectedModel });
  }

  function showBanner(text) {
    els.banner.textContent = text;
    els.banner.classList.remove("hidden");
  }
  function hideBanner() {
    els.banner.classList.add("hidden");
    els.banner.textContent = "";
  }

  function formatRoute(route) {
    if (!route) return "";
    const bits = [];
    if (route.provider || route.model) {
      bits.push(`via ${route.provider ?? "?"} / ${route.model ?? "?"}`);
    }
    if (route.mode) bits.push(`mode: ${route.mode}`);
    const reason = route.reason ? ` — ${route.reason}` : "";
    return bits.join(" · ") + reason;
  }

  function render() {
    els.messages.innerHTML = "";
    for (const m of messages) {
      const wrap = document.createElement("div");
      wrap.className = `msg msg-${m.role}` + (m.error ? " msg-error" : "");

      const bubble = document.createElement("div");
      bubble.className = "bubble";
      bubble.textContent = m.content || (m.streaming ? "" : "");
      if (m.streaming && !m.content) {
        bubble.innerHTML = `<span class="typing">●●●</span>`;
      }
      wrap.appendChild(bubble);

      if (m.error) {
        const err = document.createElement("div");
        err.className = "route-bar route-bar-error";
        err.textContent = m.error;
        wrap.appendChild(err);
      } else if (m.role === "assistant" && m.route) {
        const route = document.createElement("div");
        route.className = "route-bar";
        route.textContent = formatRoute(m.route);
        wrap.appendChild(route);
      }

      els.messages.appendChild(wrap);
    }
    els.messages.scrollTop = els.messages.scrollHeight;
    saveState();
  }

  function renderModels() {
    els.modelSelect.innerHTML = "";
    const list = models.length ? models : [{ id: selectedModel }];
    for (const m of list) {
      const opt = document.createElement("option");
      opt.value = m.id;
      const owner = m.prism && m.prism.local === false ? "cloud" : m.prism && m.prism.local ? "local" : "";
      opt.textContent = owner && m.id !== "auto" ? `${m.id} (${owner})` : m.id;
      if (m.id === selectedModel) opt.selected = true;
      els.modelSelect.appendChild(opt);
    }
  }

  function setBusy(busy) {
    els.sendBtn.textContent = busy ? "Stop" : "Send";
    els.input.disabled = busy;
  }

  function appendUserMessage(text) {
    messages.push({ id: `u${Date.now()}`, role: "user", content: text });
  }

  function startAssistantReply(model) {
    const id = `req${++reqCounter}-${Date.now()}`;
    messages.push({ id, role: "assistant", content: "", streaming: true });
    render();
    return id;
  }

  function findMessage(requestId) {
    return messages.find((m) => m.id === requestId);
  }

  function send(text, opts) {
    opts = opts || {};
    if (!text.trim() || activeRequestId) return;
    hideBanner();
    appendUserMessage(text);
    const requestId = startAssistantReply(selectedModel);
    activeRequestId = requestId;
    setBusy(true);

    const history = messages
      .filter((m) => m.id !== requestId)
      .map((m) => ({ role: m.role, content: m.content }));

    vscode.postMessage({
      type: "send",
      requestId,
      model: selectedModel,
      messages: history,
    });
  }

  // ---- composer ----
  els.composer.addEventListener("submit", (e) => {
    e.preventDefault();
    if (activeRequestId) {
      vscode.postMessage({ type: "cancel", requestId: activeRequestId });
      return;
    }
    const text = els.input.value;
    els.input.value = "";
    send(text);
  });
  els.input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      els.composer.requestSubmit();
    }
  });

  els.modelSelect.addEventListener("change", () => {
    selectedModel = els.modelSelect.value;
    saveState();
  });

  els.clearBtn.addEventListener("click", () => {
    if (activeRequestId) {
      vscode.postMessage({ type: "cancel", requestId: activeRequestId });
    }
    activeRequestId = null;
    setBusy(false);
    messages = [];
    render();
  });

  // ---- inbound messages from the extension host ----
  window.addEventListener("message", (event) => {
    const msg = event.data;
    switch (msg.type) {
      case "init": {
        if (!prior) selectedModel = msg.model || "auto";
        renderModels();
        break;
      }
      case "models": {
        models = msg.models || [];
        if (!models.find((m) => m.id === selectedModel)) {
          selectedModel = msg.selected || (models[0] && models[0].id) || "auto";
        }
        renderModels();
        break;
      }
      case "modelsError": {
        showBanner(msg.message);
        renderModels();
        break;
      }
      case "configChanged": {
        // Base URL changed; model default may also have shifted — only
        // adopt it if the user hasn't already picked something manually
        // in this session (models list will still validate it below).
        vscode.postMessage({ type: "refreshModels" });
        break;
      }
      case "streamEvent": {
        const target = findMessage(msg.requestId);
        if (!target) break;
        const ev = msg.event;
        if (ev.type === "route") {
          target.route = ev.route;
        } else if (ev.type === "delta") {
          target.content += ev.content || "";
          hideBanner();
        } else if (ev.type === "error") {
          target.streaming = false;
          target.error = ev.message || "Unknown error.";
          if (/unreachable|ECONNREFUSED|failed to fetch|network/i.test(target.error)) {
            showBanner("DL Prism gateway is offline — " + target.error);
          }
          if (msg.requestId === activeRequestId) {
            activeRequestId = null;
            setBusy(false);
          }
        } else if (ev.type === "done") {
          target.streaming = false;
          if (msg.requestId === activeRequestId) {
            activeRequestId = null;
            setBusy(false);
          }
        }
        render();
        break;
      }
      case "clear": {
        if (activeRequestId) vscode.postMessage({ type: "cancel", requestId: activeRequestId });
        activeRequestId = null;
        setBusy(false);
        messages = [];
        render();
        break;
      }
      case "prefill": {
        const fence = msg.language ? "```" + msg.language : "```";
        const composed = `${msg.question}\n\n${fence}\n${msg.code}\n\`\`\``;
        if (msg.autoSend) {
          send(composed);
        } else {
          els.input.value = composed;
          els.input.focus();
        }
        break;
      }
    }
  });

  render();
  renderModels();
  vscode.postMessage({ type: "ready" });
})();
