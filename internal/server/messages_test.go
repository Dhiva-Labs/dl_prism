package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// forwardedBody decodes the OpenAI-shaped request the upstream received.
func forwardedBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("forwarded body is not JSON: %v (%s)", err, raw)
	}
	return body
}

// forwardedMessages pulls the messages array out of a forwarded request.
func forwardedMessages(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	msgs, ok := forwardedBody(t, raw)["messages"].([]any)
	if !ok {
		t.Fatalf("forwarded body has no messages array: %s", raw)
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.(map[string]any))
	}
	return out
}

// anthropicError pulls the message out of an Anthropic error envelope.
func anthropicError(t *testing.T, body string) (kind, msg string) {
	t.Helper()
	var env struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("response is not an error envelope: %v (%s)", err, body)
	}
	if env.Type != "error" {
		t.Errorf("envelope type = %q, want error (%s)", env.Type, body)
	}
	return env.Error.Type, env.Error.Message
}

// A system string becomes a leading system message; max_tokens carries over.
func TestMessagesTranslatesSystemString(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages", `{"model":"gpt-test","max_tokens":64,"temperature":0.2,
	  "stop_sequences":["END"],"system":"be terse",
	  "messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	body := forwardedBody(t, f.primary.LastBody())
	if body["model"] != "gpt-test" {
		t.Errorf("forwarded model = %v, want gpt-test", body["model"])
	}
	if body["max_tokens"] != float64(64) {
		t.Errorf("max_tokens = %v, want 64", body["max_tokens"])
	}
	if body["temperature"] != 0.2 {
		t.Errorf("temperature = %v, want 0.2", body["temperature"])
	}
	stop, _ := body["stop"].([]any)
	if len(stop) != 1 || stop[0] != "END" {
		t.Errorf("stop = %v, want [END]: stop_sequences must map onto stop", body["stop"])
	}
	msgs := forwardedMessages(t, f.primary.LastBody())
	if len(msgs) != 2 {
		t.Fatalf("forwarded %d messages, want the system message plus the user turn: %v", len(msgs), msgs)
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "be terse" {
		t.Errorf("first message = %v, want the lifted system message", msgs[0])
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "hi" {
		t.Errorf("second message = %v, want the user turn", msgs[1])
	}
}

// A system given as content blocks is flattened into the same system message.
func TestMessagesTranslatesSystemBlocks(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages", `{"model":"gpt-test","max_tokens":16,
	  "system":[{"type":"text","text":"first"},{"type":"text","text":"second"}],
	  "messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	msgs := forwardedMessages(t, f.primary.LastBody())
	if msgs[0]["content"] != "first\n\nsecond" {
		t.Errorf("system content = %q, want the blocks joined", msgs[0]["content"])
	}
	// Block content on a user turn becomes OpenAI content parts.
	parts, ok := msgs[1]["content"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("user content = %v, want one content part", msgs[1]["content"])
	}
	part := parts[0].(map[string]any)
	if part["type"] != "text" || part["text"] != "hi" {
		t.Errorf("content part = %v, want a text part", part)
	}
}

// A base64 image source becomes an OpenAI image_url data URL.
func TestMessagesTranslatesBase64Image(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages", `{"model":"gpt-test","max_tokens":16,"messages":[
	  {"role":"user","content":[
	    {"type":"text","text":"what is this"},
	    {"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	msgs := forwardedMessages(t, f.primary.LastBody())
	parts := msgs[0]["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content parts = %v, want text plus image", parts)
	}
	img := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("second part = %v, want an image_url part", img)
	}
	url := img["image_url"].(map[string]any)["url"]
	if url != "data:image/png;base64,aGVsbG8=" {
		t.Errorf("image url = %v, want the base64 source as a data URL", url)
	}
}

// A URL image source passes through as-is.
func TestMessagesTranslatesURLImage(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	f.post("/v1/messages", `{"model":"gpt-test","max_tokens":16,"messages":[
	  {"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://x.test/a.png"}}]}]}`)

	msgs := forwardedMessages(t, f.primary.LastBody())
	img := msgs[0]["content"].([]any)[0].(map[string]any)
	if url := img["image_url"].(map[string]any)["url"]; url != "https://x.test/a.png" {
		t.Errorf("image url = %v, want the source url verbatim", url)
	}
}

// The non-streamed response comes back in the Anthropic Messages shape.
func TestMessagesNonStreamResponseShape(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages",
		`{"model":"gpt-test","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var out struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not Anthropic JSON: %v (%s)", err, w.Body.String())
	}
	if out.Type != "message" || out.Role != "assistant" {
		t.Errorf("type/role = %q/%q, want message/assistant", out.Type, out.Role)
	}
	if !strings.HasPrefix(out.ID, "msg_") {
		t.Errorf("id = %q, want an msg_ prefixed id", out.ID)
	}
	if out.Model != "gpt-test" {
		t.Errorf("model = %q, want the routed model id", out.Model)
	}
	if len(out.Content) != 1 || out.Content[0].Type != "text" || out.Content[0].Text != "hello" {
		t.Fatalf("content = %+v, want a single text block saying hello", out.Content)
	}
	if out.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", out.StopReason)
	}
	if out.Usage.InputTokens != 12 || out.Usage.OutputTokens != 5 {
		t.Errorf("usage = %d/%d, want the upstream's 12/5", out.Usage.InputTokens, out.Usage.OutputTokens)
	}
	// The routing headers are still there, and the ledger still sees the call.
	if got := w.Header().Get("X-Prism-Provider"); got != "primary" {
		t.Errorf("X-Prism-Provider = %q, want primary", got)
	}
	if usage, _ := f.ledger.Summary(); len(usage) != 1 || usage[0].PromptTokens != 12 {
		t.Errorf("ledger = %+v, want the usual usage row", usage)
	}
}

// finish_reason=length maps onto Anthropic's max_tokens stop reason.
func TestMessagesMapsLengthStopReason(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, `{"id":"cmpl-l","object":"chat.completion","model":"gpt-test",
	  "choices":[{"index":0,"message":{"role":"assistant","content":"tru"},"finish_reason":"length"}],
	  "usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)

	w := f.post("/v1/messages",
		`{"model":"gpt-test","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)

	var out struct {
		StopReason string `json:"stop_reason"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.StopReason != "max_tokens" {
		t.Errorf("stop_reason = %q, want max_tokens", out.StopReason)
	}
}

// sseEvent is one parsed "event:/data:" pair.
type sseEvent struct {
	Name string
	Data map[string]any
}

// parseAnthropicSSE splits an Anthropic event stream into ordered events.
func parseAnthropicSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, block := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			if name, ok := strings.CutPrefix(line, "event: "); ok {
				ev.Name = name
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				if err := json.Unmarshal([]byte(data), &ev.Data); err != nil {
					t.Fatalf("event data is not JSON: %v (%s)", err, data)
				}
			}
		}
		if ev.Name == "" {
			t.Fatalf("SSE block without an event name: %q", block)
		}
		events = append(events, ev)
	}
	return events
}

// The streamed answer is re-emitted as the Anthropic event sequence.
func TestMessagesStreamingEventSequence(t *testing.T) {
	f := newFixture(t)
	f.primary.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range strings.SplitAfter(sseBody, "\n\n") {
			if event == "" {
				continue
			}
			w.Write([]byte(event))
			w.(http.Flusher).Flush()
		}
	})

	w := f.post("/v1/messages",
		`{"model":"gpt-test","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if !w.Flushed {
		t.Error("the translated stream was never flushed to the client")
	}
	// The upstream really was asked to stream.
	if forwardedBody(t, f.primary.LastBody())["stream"] != true {
		t.Error("the upstream request did not carry stream:true")
	}

	events := parseAnthropicSSE(t, w.Body.String())
	var names []string
	for _, e := range events {
		names = append(names, e.Name)
	}
	want := []string{
		"message_start", "content_block_start",
		"content_block_delta", "content_block_delta",
		"content_block_stop", "message_delta", "message_stop",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("event sequence = %v\nwant %v", names, want)
	}

	start := events[0].Data["message"].(map[string]any)
	if start["role"] != "assistant" || start["type"] != "message" {
		t.Errorf("message_start message = %v, want an assistant message", start)
	}
	if start["model"] != "gpt-test" {
		t.Errorf("message_start model = %v, want the routed model", start["model"])
	}
	if _, ok := start["usage"].(map[string]any)["input_tokens"]; !ok {
		t.Errorf("message_start usage = %v, want input_tokens", start["usage"])
	}
	block := events[1].Data["content_block"].(map[string]any)
	if block["type"] != "text" || events[1].Data["index"] != float64(0) {
		t.Errorf("content_block_start = %v, want a text block at index 0", events[1].Data)
	}

	var text strings.Builder
	for _, e := range events[2:4] {
		delta := e.Data["delta"].(map[string]any)
		if delta["type"] != "text_delta" {
			t.Errorf("delta type = %v, want text_delta", delta["type"])
		}
		text.WriteString(delta["text"].(string))
	}
	if text.String() != "hello" {
		t.Errorf("streamed text = %q, want hello", text.String())
	}

	delta := events[5].Data["delta"].(map[string]any)
	if delta["stop_reason"] != "end_turn" {
		t.Errorf("message_delta stop_reason = %v, want end_turn", delta["stop_reason"])
	}
	if out := events[5].Data["usage"].(map[string]any)["output_tokens"]; out != float64(2) {
		t.Errorf("message_delta output_tokens = %v, want the upstream's 2", out)
	}
	// Streaming usage still reaches the ledger.
	if usage, _ := f.ledger.Summary(); len(usage) != 1 || usage[0].CompletionTokens != 2 {
		t.Errorf("ledger = %+v, want the stream's 4/2 usage", usage)
	}
}

// An anthropic-type provider round-trips anthropic → openai → anthropic.
func TestMessagesThroughAnthropicProvider(t *testing.T) {
	f := newFixture(t)
	f.anthro.serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_x","model":"claude-test","stop_reason":"end_turn",
		  "content":[{"type":"text","text":"round tripped"}],
		  "usage":{"input_tokens":8,"output_tokens":3}}`))
	})

	w := f.post("/v1/messages",
		`{"model":"claude","max_tokens":32,"system":"be terse","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	// The upstream saw the system message lifted back into Anthropic's field.
	if sent := forwardedBody(t, f.anthro.LastBody()); sent["system"] != "be terse" {
		t.Errorf("upstream system = %v, want the system text to survive both hops", sent["system"])
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not Anthropic JSON: %v (%s)", err, w.Body.String())
	}
	if len(out.Content) != 1 || out.Content[0].Text != "round tripped" {
		t.Fatalf("content = %+v, want the upstream text", out.Content)
	}
	if out.Usage.InputTokens != 8 {
		t.Errorf("input_tokens = %d, want the upstream's 8", out.Usage.InputTokens)
	}
}

// "auto" and aliases route exactly as they do on the OpenAI endpoint.
func TestMessagesAutoRouting(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages",
		`{"model":"auto","max_tokens":32,"messages":[{"role":"user","content":"implement a unit test for this"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Mode"); got != "auto" {
		t.Errorf("X-Prism-Mode = %q, want auto", got)
	}
	if got := w.Header().Get("X-Prism-Model"); got != "gpt-test" {
		t.Errorf("X-Prism-Model = %q, want gpt-test (the task=code rule)", got)
	}
}

// Tool definitions are rejected with a clear 400, mirroring the outbound
// adapter's limitation — and without contacting any upstream.
func TestMessagesRejectsTools(t *testing.T) {
	f := newFixture(t)

	w := f.post("/v1/messages", `{"model":"gpt-test","max_tokens":32,
	  "tools":[{"name":"get_weather","input_schema":{"type":"object"}}],
	  "messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	kind, msg := anthropicError(t, w.Body.String())
	if kind != "invalid_request_error" {
		t.Errorf("error type = %q, want invalid_request_error", kind)
	}
	if !strings.Contains(msg, "tool") {
		t.Errorf("error message = %q, want it to explain the tool limitation", msg)
	}
	if f.primary.Hits()+f.backup.Hits()+f.anthro.Hits() != 0 {
		t.Error("no upstream should be contacted for a rejected request")
	}
}

// An empty tools array is not a tool request.
func TestMessagesAcceptsEmptyTools(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages",
		`{"model":"gpt-test","max_tokens":32,"tools":[],"messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

func TestMessagesRejectsMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"invalid json", `{`, "invalid request body"},
		{"no messages", `{"model":"gpt-test","max_tokens":8,"messages":[]}`, "messages must not be empty"},
		{"bad role", `{"model":"gpt-test","max_tokens":8,"messages":[{"role":"tool","content":"x"}]}`, "role"},
		{"unknown block", `{"model":"gpt-test","max_tokens":8,"messages":[
		   {"role":"user","content":[{"type":"tool_use","id":"t1"}]}]}`, "tool_use"},
		{"image without source", `{"model":"gpt-test","max_tokens":8,"messages":[
		   {"role":"user","content":[{"type":"image"}]}]}`, "source"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			w := f.post("/v1/messages", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
			}
			if _, msg := anthropicError(t, w.Body.String()); !strings.Contains(msg, tc.want) {
				t.Errorf("error message = %q, want it to mention %q", msg, tc.want)
			}
			if f.primary.Hits()+f.backup.Hits() != 0 {
				t.Error("no upstream should be contacted for a malformed request")
			}
		})
	}
}

// An unknown model is a 404 in the Anthropic envelope.
func TestMessagesUnknownModel(t *testing.T) {
	f := newFixture(t)
	w := f.post("/v1/messages",
		`{"model":"nope-9","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	kind, msg := anthropicError(t, w.Body.String())
	if kind != "not_found_error" {
		t.Errorf("error type = %q, want not_found_error", kind)
	}
	if !strings.Contains(msg, `unknown model "nope-9"`) {
		t.Errorf("error message = %q, want it to name the model", msg)
	}
}

// An upstream failure surfaces as an Anthropic error, not a half-translated
// body.
func TestMessagesUpstreamFailure(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	f.backup.respond(http.StatusServiceUnavailable, `{"error":{"message":"busy"}}`)

	w := f.post("/v1/messages",
		`{"model":"gpt-test","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", w.Code, w.Body.String())
	}
	kind, msg := anthropicError(t, w.Body.String())
	if kind != "overloaded_error" {
		t.Errorf("error type = %q, want overloaded_error", kind)
	}
	if !strings.Contains(msg, "all candidates failed") {
		t.Errorf("error message = %q, want the exhausted-chain error", msg)
	}
}

// A retryable failure still fails over: /v1/messages runs the same loop.
func TestMessagesFailsOver(t *testing.T) {
	f := newFixture(t)
	f.primary.respond(http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	f.backup.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/messages",
		`{"model":"gpt-test","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from the fallback (body %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Prism-Provider"); got != "backup" {
		t.Errorf("X-Prism-Provider = %q, want backup", got)
	}
	var out struct {
		Model   string `json:"model"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Model != "backup-test" {
		t.Errorf("model = %q, want the model that actually answered", out.Model)
	}
	if len(out.Content) != 1 || out.Content[0].Text != "hello" {
		t.Errorf("content = %+v, want the fallback's answer", out.Content)
	}
}

func TestMessagesCountTokens(t *testing.T) {
	f := newFixture(t)
	prompt := strings.Repeat("a", 40)
	w := f.post("/v1/messages/count_tokens",
		`{"model":"gpt-test","messages":[{"role":"user","content":"`+prompt+`"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding count_tokens: %v (%s)", err, w.Body.String())
	}
	if out.InputTokens != (len(prompt)+1)/4 {
		t.Errorf("input_tokens = %d, want the chars/4 estimate %d", out.InputTokens, (len(prompt)+1)/4)
	}
	if f.primary.Hits()+f.backup.Hits() != 0 {
		t.Error("count_tokens must not contact any upstream")
	}
}

// The preflight must answer even for content the translator cannot map, so
// tools and tool blocks fall back to a whole-body estimate instead of a 400.
func TestMessagesCountTokensToleratesTools(t *testing.T) {
	f := newFixture(t)
	w := f.post("/v1/messages/count_tokens", `{"model":"gpt-test",
	  "tools":[{"name":"get_weather","input_schema":{"type":"object"}}],
	  "messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.InputTokens <= 0 {
		t.Errorf("input_tokens = %d, want a positive estimate", out.InputTokens)
	}
}

func TestMessagesRequireAuth(t *testing.T) {
	withKeys := func(cfg *config.Config) { cfg.Server.APIKeys = []string{"sk-prism-test"} }
	body := `{"model":"gpt-test","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`

	for _, path := range []string{"/v1/messages", "/v1/messages/count_tokens"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t, withKeys)
			f.primary.respond(http.StatusOK, okCompletion)
			if w := f.post(path, body); w.Code != http.StatusUnauthorized {
				t.Errorf("unauthenticated status = %d, want 401", w.Code)
			}
			// Anthropic SDKs send the key as x-api-key, which the middleware
			// already accepts.
			w := f.post(path, body, [2]string{"x-api-key", "sk-prism-test"})
			if w.Code != http.StatusOK {
				t.Errorf("x-api-key status = %d, want 200 (body %s)", w.Code, w.Body.String())
			}
		})
	}
}
