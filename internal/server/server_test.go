package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/cost"
	"github.com/dhivadhiva/dl_prism/internal/registry"
	"github.com/dhivadhiva/dl_prism/internal/router"
)

func TestMain(m *testing.M) {
	// handleChat logs every upstream failure; the failover tests provoke
	// several on purpose.
	log.SetOutput(io.Discard)
	m.Run()
}

// upstream is a controllable fake provider endpoint.
type upstream struct {
	*httptest.Server
	mu       sync.Mutex
	hits     int
	lastBody []byte
	handler  http.HandlerFunc
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.hits++
		u.lastBody = body
		h := u.handler
		u.mu.Unlock()
		if h == nil {
			t.Errorf("upstream %s received an unexpected request to %s", u.URL, r.URL.Path)
			http.Error(w, `{"error":"no handler configured"}`, http.StatusNotImplemented)
			return
		}
		h(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) serve(h http.HandlerFunc) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.handler = h
}

// respond replies with a fixed status and body.
func (u *upstream) respond(status int, body string) {
	u.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	})
}

func (u *upstream) Hits() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits
}

func (u *upstream) LastBody() []byte {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastBody
}

// cfgTemplate wires three real upstreams into a real config: primary/gpt-test
// falls back to backup/backup-test, plus an anthropic-type provider so the
// translating adapter is exercised through the full server path too.
const cfgTemplate = `
server:
  listen: 127.0.0.1:8130
providers:
  - name: primary
    type: openai
    base_url: %s/v1
    models:
      - id: gpt-test
        aliases: [fast]
        context_window: 128000
        capabilities: [code, vision]
        speed: fast
        cost:
          input_per_mtok: 1.0
          output_per_mtok: 2.0
  - name: backup
    type: openai
    base_url: %s/v1
    models:
      - id: backup-test
        aliases: [slow]
        context_window: 200000
        capabilities: [code]
        speed: medium
        cost:
          input_per_mtok: 0.5
          output_per_mtok: 1.0
  - name: anthro
    type: anthropic
    base_url: %s
    api_key: sk-anthropic-test
    models:
      - id: claude-test
        aliases: [claude]
        context_window: 200000
        capabilities: [code, vision]
        speed: medium
        cost:
          input_per_mtok: 3.0
          output_per_mtok: 15.0
routing:
  default: backup-test
  rules:
    - if:
        task: code
      then: gpt-test
  fallbacks:
    gpt-test:
      - backup-test
`

type fixture struct {
	t       *testing.T
	cfg     *config.Config
	reg     *registry.Registry
	engine  *router.Engine
	ledger  *cost.Ledger
	srv     *Server
	handler http.Handler
	primary *upstream
	backup  *upstream
	anthro  *upstream
}

func newFixture(t *testing.T, tweak ...func(*config.Config)) *fixture {
	t.Helper()
	f := &fixture{t: t, primary: newUpstream(t), backup: newUpstream(t), anthro: newUpstream(t)}

	cfg, err := config.Parse([]byte(fmt.Sprintf(cfgTemplate, f.primary.URL, f.backup.URL, f.anthro.URL)))
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}
	for _, fn := range tweak {
		fn(cfg)
	}
	reg, err := registry.New(cfg)
	if err != nil {
		t.Fatalf("registry.New: %v", err)
	}
	ledger, err := cost.Open(filepath.Join(t.TempDir(), "usage.jsonl"))
	if err != nil {
		t.Fatalf("cost.Open: %v", err)
	}
	engine := &router.Engine{Cfg: cfg, Reg: reg, Breaker: router.NewBreaker()}
	if err := engine.Validate(); err != nil {
		t.Fatalf("engine.Validate: %v", err)
	}

	f.cfg, f.reg, f.engine, f.ledger = cfg, reg, engine, ledger
	f.srv = New(cfg, reg, engine, ledger)
	f.handler = f.srv.Handler()
	return f
}

func (f *fixture) post(path, body string, headers ...[2]string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

func (f *fixture) get(path string, headers ...[2]string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

// errorMessage pulls the message out of an OpenAI-style error envelope.
func errorMessage(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("response is not an error envelope: %v (%s)", err, body)
	}
	return env.Error.Message
}

const okCompletion = `{"id":"cmpl-ok","object":"chat.completion","model":"gpt-test",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`

// (a) manual route happy path.
func TestChatManualRouteHappyPath(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if w.Body.String() != okCompletion {
		t.Errorf("body = %s\nwant the upstream body verbatim", w.Body.String())
	}
	if f.backup.Hits() != 0 {
		t.Errorf("backup was called %d times, want 0", f.backup.Hits())
	}

	want := map[string]string{
		"X-Prism-Mode":     "manual",
		"X-Prism-Provider": "primary",
		"X-Prism-Model":    "gpt-test",
	}
	for k, v := range want {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if reason := w.Header().Get("X-Prism-Reason"); !strings.Contains(reason, `client requested "gpt-test"`) {
		t.Errorf("X-Prism-Reason = %q, want it to name the client's request", reason)
	}
	if reason := w.Header().Get("X-Prism-Reason"); strings.Contains(reason, "failed over") {
		t.Errorf("X-Prism-Reason = %q, want no failover note on a first-try success", reason)
	}

	// The upstream must see the resolved registry model id, not the alias.
	var forwarded map[string]any
	if err := json.Unmarshal(f.primary.LastBody(), &forwarded); err != nil {
		t.Fatalf("forwarded body is not JSON: %v", err)
	}
	if forwarded["model"] != "gpt-test" {
		t.Errorf("forwarded model = %v, want gpt-test", forwarded["model"])
	}

	// Real usage from the upstream must land in the ledger unestimated.
	usage, err := f.ledger.Summary()
	if err != nil {
		t.Fatalf("ledger.Summary: %v", err)
	}
	if len(usage) != 1 {
		t.Fatalf("ledger has %d rows, want 1", len(usage))
	}
	if usage[0].Provider != "primary" || usage[0].Model != "gpt-test" {
		t.Errorf("ledger row = %s/%s, want primary/gpt-test", usage[0].Provider, usage[0].Model)
	}
	if usage[0].PromptTokens != 12 || usage[0].CompletionTokens != 5 {
		t.Errorf("ledger tokens = %d/%d, want 12/5", usage[0].PromptTokens, usage[0].CompletionTokens)
	}
	if want := 12.0/1e6*1.0 + 5.0/1e6*2.0; usage[0].CostUSD != want {
		t.Errorf("ledger cost = %v, want %v", usage[0].CostUSD, want)
	}
}

// A model alias must resolve to its registry entry.
func TestChatResolvesAlias(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/chat/completions",
		`{"model":"fast","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Model"); got != "gpt-test" {
		t.Errorf("X-Prism-Model = %q, want gpt-test", got)
	}
	var forwarded map[string]any
	json.Unmarshal(f.primary.LastBody(), &forwarded)
	if forwarded["model"] != "gpt-test" {
		t.Errorf("forwarded model = %v, want the alias resolved to gpt-test", forwarded["model"])
	}
}

// Automatic mode runs the classifier and the policy rules.
func TestChatAutoRouteUsesPolicyRules(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/chat/completions",
		`{"model":"auto","messages":[{"role":"user","content":"implement a unit test for this"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Mode"); got != "auto" {
		t.Errorf("X-Prism-Mode = %q, want auto", got)
	}
	if got := w.Header().Get("X-Prism-Model"); got != "gpt-test" {
		t.Errorf("X-Prism-Model = %q, want gpt-test (the task=code rule)", got)
	}
	if reason := w.Header().Get("X-Prism-Reason"); !strings.Contains(reason, "task=code") {
		t.Errorf("X-Prism-Reason = %q, want it to explain the matched rule", reason)
	}
}

// (b) failover: a 500 from the primary must be retried on the fallback.
func TestChatFailsOverOnRetryableUpstreamError(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusInternalServerError, `{"error":{"message":"primary exploded"}}`)
	f.backup.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from the fallback (body %s)", w.Code, w.Body.String())
	}
	if w.Body.String() != okCompletion {
		t.Errorf("body = %s, want the fallback's body", w.Body.String())
	}
	if f.primary.Hits() != 1 {
		t.Errorf("primary hits = %d, want 1", f.primary.Hits())
	}
	if f.backup.Hits() != 1 {
		t.Errorf("backup hits = %d, want 1", f.backup.Hits())
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "backup" {
		t.Errorf("X-Prism-Provider = %q, want backup", got)
	}
	if got := w.Header().Get("X-Prism-Model"); got != "backup-test" {
		t.Errorf("X-Prism-Model = %q, want backup-test", got)
	}
	reason := w.Header().Get("X-Prism-Reason")
	for _, want := range []string{"failed over after", "primary/gpt-test", "500"} {
		if !strings.Contains(reason, want) {
			t.Errorf("X-Prism-Reason = %q, want it to contain %q", reason, want)
		}
	}
	// The fallback ran with its own model id.
	var forwarded map[string]any
	json.Unmarshal(f.backup.LastBody(), &forwarded)
	if forwarded["model"] != "backup-test" {
		t.Errorf("fallback received model %v, want backup-test", forwarded["model"])
	}
	// Only the successful attempt is billed.
	usage, _ := f.ledger.Summary()
	if len(usage) != 1 || usage[0].Provider != "backup" {
		t.Errorf("ledger = %+v, want a single backup row", usage)
	}
}

// An unreachable upstream is a retryable failure too.
func TestChatFailsOverOnUnreachableUpstream(t *testing.T) {
	f := newFixture(t)
	f.backup.respond(http.StatusOK, okCompletion)
	f.primary.Close() // nothing listening on the primary's port any more

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from the fallback (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "backup" {
		t.Errorf("X-Prism-Provider = %q, want backup", got)
	}
}

// When every candidate fails retryably the client gets one 502 listing them.
func TestChatAllCandidatesFail(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusInternalServerError, `{"error":{"message":"primary exploded"}}`)
	f.backup.respond(http.StatusServiceUnavailable, `{"error":{"message":"backup busy"}}`)

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", w.Code, w.Body.String())
	}
	msg := errorMessage(t, w.Body.String())
	for _, want := range []string{"all candidates failed", "primary/gpt-test", "backup/backup-test"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q, want it to contain %q", msg, want)
		}
	}
	if f.primary.Hits() != 1 || f.backup.Hits() != 1 {
		t.Errorf("hits = %d/%d, want each candidate tried exactly once", f.primary.Hits(), f.backup.Hits())
	}
}

// (c) a non-retryable 4xx must not burn the fallback.
func TestChatDoesNotFailOverOnNonRetryableError(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"bad request", http.StatusBadRequest},
		{"unauthorized", http.StatusUnauthorized},
		{"not found", http.StatusNotFound},
		{"unprocessable", http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.primary.respond(tc.status, `{"error":{"message":"unknown parameter: seed"}}`)

			w := f.post("/v1/chat/completions",
				`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"seed":1}`)

			if f.primary.Hits() != 1 {
				t.Errorf("primary hits = %d, want 1", f.primary.Hits())
			}
			if f.backup.Hits() != 0 {
				t.Errorf("backup hits = %d, want 0: a %d must not fail over", f.backup.Hits(), tc.status)
			}
			if w.Code == http.StatusOK {
				t.Fatalf("status = 200, want an error status")
			}
			msg := errorMessage(t, w.Body.String())
			if !strings.Contains(msg, "unknown parameter: seed") {
				t.Errorf("error message %q, want it to relay the upstream error", msg)
			}
			if strings.Contains(msg, "all candidates failed") {
				t.Errorf("error message %q, want the single-attempt error, not the exhausted-chain one", msg)
			}
			// A non-retryable failure must not move the circuit breaker.
			if !f.engine.Breaker.Healthy("primary") {
				t.Error("breaker opened on a non-retryable client error")
			}
		})
	}
}

// The breaker demotes a provider that keeps failing, so later requests try the
// fallback first.
func TestChatBreakerDemotesFailingProvider(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusInternalServerError, `{"error":{"message":"down"}}`)
	f.backup.respond(http.StatusOK, okCompletion)

	body := `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 3; i++ {
		if w := f.post("/v1/chat/completions", body); w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, w.Code)
		}
	}
	if f.engine.Breaker.Healthy("primary") {
		t.Fatal("breaker should be open after 3 consecutive failures")
	}

	before := f.primary.Hits()
	w := f.post("/v1/chat/completions", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "backup" {
		t.Errorf("X-Prism-Provider = %q, want backup: the open circuit demotes primary", got)
	}
	if f.primary.Hits() != before {
		t.Errorf("primary was retried %d more times, want 0 while demoted", f.primary.Hits()-before)
	}
}

// (d) an unknown model is a 404 that lists what is available.
func TestChatUnknownModelReturns404(t *testing.T) {
	f := newFixture(t)

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-9-turbo","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	msg := errorMessage(t, w.Body.String())
	if !strings.Contains(msg, `unknown model "gpt-9-turbo"`) {
		t.Errorf("error message %q, want it to name the requested model", msg)
	}
	for _, want := range []string{"gpt-test", "backup-test", "fast", "slow"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q, want it to list the available model %q", msg, want)
		}
	}
	if f.primary.Hits()+f.backup.Hits() != 0 {
		t.Error("no upstream should be contacted for an unknown model")
	}
}

// The anthropic adapter runs end to end through the server: OpenAI in,
// Anthropic Messages upstream, OpenAI out.
func TestChatAnthropicTranslationEndToEnd(t *testing.T) {
	f := newFixture(t)
	f.anthro.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("upstream path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-anthropic-test" {
			t.Errorf("x-api-key = %q, want the configured key", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header not set")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_x","model":"claude-test","stop_reason":"end_turn",
		    "content":[{"type":"text","text":"translated"}],
		    "usage":{"input_tokens":8,"output_tokens":3}}`)
	})

	w := f.post("/v1/chat/completions",
		`{"model":"claude","messages":[{"role":"system","content":"be terse"},{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "anthro" {
		t.Errorf("X-Prism-Provider = %q, want anthro", got)
	}

	// The upstream saw an Anthropic-shaped body.
	var sent map[string]any
	if err := json.Unmarshal(f.anthro.LastBody(), &sent); err != nil {
		t.Fatalf("forwarded body is not JSON: %v", err)
	}
	if sent["system"] != "be terse" {
		t.Errorf("system = %v, want the lifted system message", sent["system"])
	}
	if sent["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v, want the 4096 default", sent["max_tokens"])
	}
	if sent["model"] != "claude-test" {
		t.Errorf("model = %v, want the resolved id claude-test", sent["model"])
	}

	// The client saw an OpenAI-shaped body.
	var out struct {
		Object  string `json:"object"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not OpenAI JSON: %v (%s)", err, w.Body.String())
	}
	if out.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", out.Object)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content != "translated" {
		t.Fatalf("choices = %+v, want the translated text", out.Choices)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", out.Choices[0].FinishReason)
	}

	usage, _ := f.ledger.Summary()
	if len(usage) != 1 || usage[0].PromptTokens != 8 || usage[0].CompletionTokens != 3 {
		t.Errorf("ledger = %+v, want anthropic usage 8/3", usage)
	}
}

// An adapter that rejects the request before reaching the network is a client
// error, not a bad gateway, and must not burn a fallback.
func TestChatAdapterPreflightRejectionIsClientError(t *testing.T) {
	f := newFixture(t)

	w := f.post("/v1/chat/completions",
		`{"model":"claude-test","tools":[{"type":"function","function":{"name":"f"}}],
		  "messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(errorMessage(t, w.Body.String()), "tool calls") {
		t.Errorf("error message %q, want it to explain the tool limitation", errorMessage(t, w.Body.String()))
	}
	if f.anthro.Hits() != 0 {
		t.Errorf("anthro hits = %d, want 0: the adapter rejected the request pre-flight", f.anthro.Hits())
	}
	if !f.engine.Breaker.Healthy("anthro") {
		t.Error("a pre-flight rejection must not open the circuit breaker")
	}
	usage, _ := f.ledger.Summary()
	if len(usage) != 0 {
		t.Errorf("ledger = %+v, want nothing billed for a rejected request", usage)
	}
}

func TestChatRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid json", `{`},
		{"no messages", `{"model":"gpt-test","messages":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			w := f.post("/v1/chat/completions", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
			}
			if f.primary.Hits()+f.backup.Hits() != 0 {
				t.Error("no upstream should be contacted for a malformed request")
			}
		})
	}
}

// (e) /v1/models advertises "auto" plus every registry entry.
func TestModelsEndpoint(t *testing.T) {
	f := newFixture(t)
	w := f.get("/v1/models")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var list struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
			Prism   struct {
				Aliases      []string `json:"aliases"`
				Local        bool     `json:"local"`
				ContextWin   int      `json:"context_window"`
				Capabilities []string `json:"capabilities"`
			} `json:"prism"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding /v1/models: %v (%s)", err, w.Body.String())
	}
	if list.Object != "list" {
		t.Errorf("object = %q, want list", list.Object)
	}
	byID := map[string]int{}
	for i, m := range list.Data {
		byID[m.ID] = i
		if m.Object != "model" {
			t.Errorf("model %s: object = %q, want model", m.ID, m.Object)
		}
	}
	for _, want := range []string{"auto", "gpt-test", "backup-test", "claude-test"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("/v1/models is missing %q (got %v)", want, byID)
		}
	}
	if list.Data[0].ID != "auto" {
		t.Errorf("first model = %q, want auto listed first", list.Data[0].ID)
	}
	if list.Data[0].OwnedBy != "prism" {
		t.Errorf("auto owned_by = %q, want prism", list.Data[0].OwnedBy)
	}
	gpt := list.Data[byID["gpt-test"]]
	if gpt.OwnedBy != "primary" {
		t.Errorf("gpt-test owned_by = %q, want primary", gpt.OwnedBy)
	}
	if gpt.Prism.ContextWin != 128000 {
		t.Errorf("gpt-test context_window = %d, want 128000", gpt.Prism.ContextWin)
	}
	if len(gpt.Prism.Aliases) != 1 || gpt.Prism.Aliases[0] != "fast" {
		t.Errorf("gpt-test aliases = %v, want [fast]", gpt.Prism.Aliases)
	}
}

const sseBody = "data: {\"id\":\"cmpl-s\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-s\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"},\"finish_reason\":null}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-s\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":null}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-s\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}\n" +
	"\n" +
	"data: [DONE]\n" +
	"\n"

// (f) streaming passthrough, end to end.
func TestChatStreamingPassthrough(t *testing.T) {
	f := newFixture(t)
	f.primary.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range strings.SplitAfter(sseBody, "\n\n") {
			if event == "" {
				continue
			}
			io.WriteString(w, event)
			w.(http.Flusher).Flush()
		}
	})

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","stream":true,"stream_options":{"include_usage":true},
		  "messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if w.Body.String() != sseBody {
		t.Errorf("streamed bytes were altered\n got: %q\nwant: %q", w.Body.String(), sseBody)
	}
	if !w.Flushed {
		t.Error("the stream was never flushed to the client")
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "primary" {
		t.Errorf("X-Prism-Provider = %q, want primary", got)
	}
	if f.backup.Hits() != 0 {
		t.Errorf("backup hits = %d, want 0", f.backup.Hits())
	}
	// Usage came from the final chunk, so the ledger entry is not an estimate.
	usage, _ := f.ledger.Summary()
	if len(usage) != 1 {
		t.Fatalf("ledger has %d rows, want 1", len(usage))
	}
	if usage[0].PromptTokens != 4 || usage[0].CompletionTokens != 2 {
		t.Errorf("ledger tokens = %d/%d, want 4/2 from the usage chunk",
			usage[0].PromptTokens, usage[0].CompletionTokens)
	}
}

// Once the first byte is on the wire, a mid-stream failure must not fail over:
// the client would otherwise receive two spliced responses.
func TestChatNoFailoverAfterFirstByte(t *testing.T) {
	f := newFixture(t)
	partial := "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"
	f.primary.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, partial)
		w.(http.Flusher).Flush()
		// Hang up mid-stream, as a crashing upstream would.
		panic(http.ErrAbortHandler)
	})
	f.backup.serve(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backup must not be tried once bytes have reached the client")
		w.Write([]byte(okCompletion))
	})

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want the 200 that was already committed", w.Code)
	}
	if f.backup.Hits() != 0 {
		t.Errorf("backup hits = %d, want 0", f.backup.Hits())
	}
	if got := w.Body.String(); !strings.Contains(got, "partial") {
		t.Errorf("body = %q, want the bytes streamed before the failure", got)
	}
	// The truncated stream must not be followed by an error envelope.
	if strings.Contains(w.Body.String(), `"error"`) {
		t.Errorf("an error envelope was appended to a committed stream: %q", w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "primary" {
		t.Errorf("X-Prism-Provider = %q, want primary", got)
	}
}

// Streams without usage fall back to the classifier's token estimate.
func TestChatEstimatesUsageWhenUpstreamReportsNone(t *testing.T) {
	f := newFixture(t)
	f.primary.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	})

	w := f.post("/v1/chat/completions",
		`{"model":"gpt-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	usage, _ := f.ledger.Summary()
	if len(usage) != 1 {
		t.Fatalf("ledger has %d rows, want 1", len(usage))
	}
	if usage[0].Requests != 1 {
		t.Errorf("ledger requests = %d, want 1", usage[0].Requests)
	}
}

// Anthropic SSE is translated into OpenAI chunks on the way out.
func TestChatAnthropicStreamTranslation(t *testing.T) {
	f := newFixture(t)
	f.anthro.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "event: message_start\n"+
			`data: {"type":"message_start","message":{"id":"msg_s","usage":{"input_tokens":20}}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi there"}}`+"\n\n"+
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":6}}`+"\n\n"+
			`data: {"type":"message_stop"}`+"\n\n")
		w.(http.Flusher).Flush()
	})

	w := f.post("/v1/chat/completions",
		`{"model":"claude-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Errorf("body = %q, want OpenAI chunk objects", body)
	}
	if !strings.Contains(body, "hi there") {
		t.Errorf("body = %q, want the translated text delta", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Errorf("body = %q, want it to end with the [DONE] terminator", body)
	}
	usage, _ := f.ledger.Summary()
	if len(usage) != 1 || usage[0].PromptTokens != 20 || usage[0].CompletionTokens != 6 {
		t.Errorf("ledger = %+v, want stream usage 20/6", usage)
	}
}

// A stream that dies on an upstream error event must not be billed or counted
// as a healthy request, even though its bytes are already committed.
func TestChatAnthropicStreamErrorIsNotRecordedAsSuccess(t *testing.T) {
	f := newFixture(t)
	f.anthro.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_e","usage":{"input_tokens":20}}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n"+
			"event: error\n"+
			`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`+"\n\n")
		w.(http.Flusher).Flush()
	})

	w := f.post("/v1/chat/completions",
		`{"model":"claude-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want the 200 already committed to the client", w.Code)
	}
	if !strings.Contains(w.Body.String(), "partial") {
		t.Errorf("body = %q, want the deltas sent before the failure", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "[DONE]") {
		t.Errorf("body = %q, want no [DONE] on a stream that failed", w.Body.String())
	}
	if usage, _ := f.ledger.Summary(); len(usage) != 0 {
		t.Errorf("ledger = %+v, want nothing billed for a failed stream", usage)
	}
}

func TestAuth(t *testing.T) {
	withKeys := func(cfg *config.Config) { cfg.Server.APIKeys = []string{"sk-prism-test"} }

	tests := []struct {
		name       string
		headers    [][2]string
		wantStatus int
	}{
		{"no credentials", nil, http.StatusUnauthorized},
		{"wrong bearer", [][2]string{{"Authorization", "Bearer nope"}}, http.StatusUnauthorized},
		{"correct bearer", [][2]string{{"Authorization", "Bearer sk-prism-test"}}, http.StatusOK},
		{"correct x-api-key", [][2]string{{"x-api-key", "sk-prism-test"}}, http.StatusOK},
		{"wrong x-api-key", [][2]string{{"x-api-key", "nope"}}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, withKeys)
			w := f.get("/v1/models", tc.headers...)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}

	t.Run("healthz stays open", func(t *testing.T) {
		f := newFixture(t, withKeys)
		if w := f.get("/healthz"); w.Code != http.StatusOK {
			t.Errorf("/healthz status = %d, want 200", w.Code)
		}
	})

	// subtle.ConstantTimeCompare reports a match for two empty strings, so a
	// blank api_keys entry must never stand in for "no auth required".
	t.Run("blank configured key does not authenticate an empty token", func(t *testing.T) {
		blank := func(cfg *config.Config) { cfg.Server.APIKeys = []string{""} }
		f := newFixture(t, blank)
		if w := f.get("/v1/models"); w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401: a blank key must not open the gateway", w.Code)
		}
	})

	t.Run("blank entry alongside a real key still rejects empty credentials", func(t *testing.T) {
		mixed := func(cfg *config.Config) { cfg.Server.APIKeys = []string{"", "sk-real"} }
		f := newFixture(t, mixed)
		if w := f.get("/v1/models"); w.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated status = %d, want 401", w.Code)
		}
		if w := f.get("/v1/models", [2]string{"Authorization", "Bearer sk-real"}); w.Code != http.StatusOK {
			t.Errorf("authenticated status = %d, want 200", w.Code)
		}
	})

	t.Run("chat endpoint is protected too", func(t *testing.T) {
		f := newFixture(t, withKeys)
		w := f.post("/v1/chat/completions",
			`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
		if f.primary.Hits()+f.backup.Hits() != 0 {
			t.Error("an unauthenticated request reached an upstream")
		}
	})
}

func TestExplainEndpoint(t *testing.T) {
	f := newFixture(t)
	w := f.post("/v1/route/explain",
		`{"model":"auto","messages":[{"role":"user","content":"implement a unit test"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var out struct {
		Mode    string `json:"mode"`
		Reason  string `json:"reason"`
		Profile *struct {
			Task      string `json:"task"`
			EstTokens int    `json:"est_tokens"`
		} `json:"profile"`
		Chain []struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"chain"`
		EstInputCost *float64 `json:"est_input_cost_usd"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding explain: %v (%s)", err, w.Body.String())
	}
	if out.Mode != "auto" {
		t.Errorf("mode = %q, want auto", out.Mode)
	}
	if out.Profile == nil || out.Profile.Task != "code" {
		t.Errorf("profile = %+v, want task=code", out.Profile)
	}
	if len(out.Chain) != 2 {
		t.Fatalf("chain = %+v, want primary then backup", out.Chain)
	}
	if out.Chain[0].Provider != "primary" || out.Chain[1].Provider != "backup" {
		t.Errorf("chain = %+v, want [primary backup]", out.Chain)
	}
	if out.EstInputCost == nil {
		t.Error("est_input_cost_usd missing from an auto-mode explanation")
	}
	if f.primary.Hits()+f.backup.Hits() != 0 {
		t.Error("explain must not contact any upstream")
	}
}

func TestUsageEndpoint(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)
	f.post("/v1/chat/completions", `{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)

	w := f.get("/admin/usage")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var out struct {
		MonthTotal float64 `json:"month_total_usd"`
		Models     []struct {
			Provider string `json:"provider"`
			Requests int    `json:"requests"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding usage: %v (%s)", err, w.Body.String())
	}
	if out.MonthTotal <= 0 {
		t.Errorf("month_total_usd = %v, want a positive spend", out.MonthTotal)
	}
	if len(out.Models) != 1 || out.Models[0].Provider != "primary" {
		t.Errorf("models = %+v, want one primary row", out.Models)
	}
}

// A Server built without a ledger is legal (record() guards for it), so the
// usage endpoint must not panic.
func TestUsageEndpointWithoutLedger(t *testing.T) {
	f := newFixture(t)
	srv := New(f.cfg, f.reg, f.engine, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/usage", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

func TestChatWithoutLedgerStillServes(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)
	srv := New(f.cfg, f.reg, f.engine, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	f := newFixture(t)
	w := f.get("/healthz")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "ok\n" {
		t.Errorf("body = %q, want %q", w.Body.String(), "ok\n")
	}
}

func TestRecorderTracksFirstWrite(t *testing.T) {
	t.Run("WriteHeader marks the response committed", func(t *testing.T) {
		rec := &recorder{ResponseWriter: httptest.NewRecorder()}
		if rec.wrote {
			t.Fatal("a fresh recorder must report nothing written")
		}
		rec.WriteHeader(http.StatusOK)
		if !rec.wrote {
			t.Error("WriteHeader did not mark the response committed")
		}
	})
	t.Run("Write marks the response committed", func(t *testing.T) {
		rec := &recorder{ResponseWriter: httptest.NewRecorder()}
		rec.Write([]byte("x"))
		if !rec.wrote {
			t.Error("Write did not mark the response committed")
		}
	})
	t.Run("Flush marks the response committed", func(t *testing.T) {
		inner := httptest.NewRecorder()
		rec := &recorder{ResponseWriter: inner}
		rec.Flush()
		if !inner.Flushed {
			t.Error("Flush did not reach the underlying writer")
		}
		if !rec.wrote {
			t.Error("Flush commits the response to the client, so failover is no longer possible")
		}
	})
}
