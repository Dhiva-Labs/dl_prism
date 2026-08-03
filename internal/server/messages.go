package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/provider"
)

// This file makes Prism speak the Anthropic Messages API on the way *in*, so
// Claude Code and the Anthropic SDKs can point ANTHROPIC_BASE_URL at the
// gateway. The request is translated onto the internal OpenAI-shaped request
// that the router and the adapters already speak, run through the normal
// routing + failover path, and translated back on the way out — the mirror of
// the outbound anthropic adapter. When the routed provider is anthropic-type
// this means anthropic → openai → anthropic; accepted for v1, since the
// alternative is a second execution path that can drift from the first.

// defaultMessagesMaxTokens fills in for clients that omit max_tokens (the
// Anthropic API requires it; being lenient costs nothing).
const defaultMessagesMaxTokens = 4096

// messagesRequest is the inbound Anthropic Messages request shape.
type messagesRequest struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	System        json.RawMessage `json:"system"` // string or content blocks
	Messages      []messagesInput `json:"messages"`
	Stream        bool            `json:"stream"`
	Temperature   *float64        `json:"temperature"`
	TopP          *float64        `json:"top_p"`
	StopSequences []string        `json:"stop_sequences"`
	Tools         json.RawMessage `json:"tools"`
}

type messagesInput struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or content blocks
}

// contentBlock is one Anthropic content block. Only text and image are
// translatable today, matching the outbound adapter's limits.
type contentBlock struct {
	Type   string       `json:"type"`
	Text   string       `json:"text"`
	Source *imageSource `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"` // base64 | url
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	URL       string `json:"url"`
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	in, req, err := parseMessages(r)
	if err != nil {
		writeAnthropicErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	if hasTools(in.Tools) {
		writeAnthropicErr(w, http.StatusBadRequest,
			"prism does not translate tool definitions on /v1/messages yet; "+
				"call /v1/chat/completions against an openai-type provider for tool use",
			"invalid_request_error")
		return
	}
	dec, err := s.engine.Route(req)
	if err != nil {
		writeAnthropicErr(w, routingErrorStatus(err), err.Error(), "prism_routing_error")
		return
	}

	aw := &anthropicWriter{ResponseWriter: w, model: in.Model, estInput: estimateInputTokens(req)}
	ok := s.executeChain(aw, r, req, dec, func(_ http.ResponseWriter, status int, msg, kind string) {
		// Errors go straight to the client: the wrapper only translates
		// successful upstream output.
		writeAnthropicErr(w, status, msg, kind)
	})
	aw.finish(ok)
}

// handleCountTokens answers the Anthropic SDKs' preflight with the same
// chars/4 estimate the classifier uses. It is deliberately permissive: a
// preflight must never fail on content the estimate can simply skip.
func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeAnthropicErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	var in messagesRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeAnthropicErr(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error")
		return
	}
	tokens := len(body) / 4 // fallback for content we cannot translate (tool blocks…)
	if req, err := toChatRequest(&in); err == nil {
		tokens = estimateInputTokens(req)
	}
	writeJSON(w, map[string]any{"input_tokens": tokens})
}

// parseMessages reads and translates the inbound Anthropic request.
func parseMessages(r *http.Request) (*messagesRequest, *provider.ChatRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return nil, nil, err
	}
	var in messagesRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, nil, fmt.Errorf("invalid request body: %w", err)
	}
	req, err := toChatRequest(&in)
	if err != nil {
		return nil, nil, err
	}
	return &in, req, nil
}

func hasTools(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "[]"
}

// toChatRequest translates an Anthropic Messages request into the internal
// OpenAI-shaped request. It round-trips through JSON so ChatRequest.Raw is
// populated, which is what the passthrough adapter forwards.
func toChatRequest(in *messagesRequest) (*provider.ChatRequest, error) {
	if len(in.Messages) == 0 {
		return nil, errors.New("messages must not be empty")
	}
	sys, err := blocksText(in.System)
	if err != nil {
		return nil, fmt.Errorf("system: %w", err)
	}
	msgs := make([]map[string]any, 0, len(in.Messages)+1)
	if sys != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": sys})
	}
	for i := range in.Messages {
		m := &in.Messages[i]
		if m.Role != "user" && m.Role != "assistant" {
			return nil, fmt.Errorf("unsupported message role %q", m.Role)
		}
		content, err := toChatContent(m.Content)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, map[string]any{"role": m.Role, "content": content})
	}

	maxTokens := in.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMessagesMaxTokens
	}
	body := map[string]any{
		"model":      in.Model,
		"messages":   msgs,
		"max_tokens": maxTokens,
	}
	if in.Stream {
		body["stream"] = true
	}
	if in.Temperature != nil {
		body["temperature"] = *in.Temperature
	}
	if in.TopP != nil {
		body["top_p"] = *in.TopP
	}
	if len(in.StopSequences) > 0 {
		body["stop"] = in.StopSequences
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return provider.ParseChatRequest(buf)
}

// toChatContent maps Anthropic content (string or blocks) to OpenAI content.
func toChatContent(raw json.RawMessage) (any, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, errors.New("message content must be a string or a list of content blocks")
	}
	parts := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": b.Text})
		case "image":
			url, err := imageURL(b.Source)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": url},
			})
		default:
			return nil, fmt.Errorf("prism cannot translate content block type %q on /v1/messages yet", b.Type)
		}
	}
	return parts, nil
}

// imageURL turns an Anthropic image source into the data or https URL the
// OpenAI content-part shape wants.
func imageURL(src *imageSource) (string, error) {
	if src == nil {
		return "", errors.New("image block without a source")
	}
	switch src.Type {
	case "base64":
		if src.MediaType == "" || src.Data == "" {
			return "", errors.New("base64 image source needs media_type and data")
		}
		return "data:" + src.MediaType + ";base64," + src.Data, nil
	case "url":
		if src.URL == "" {
			return "", errors.New("url image source needs a url")
		}
		return src.URL, nil
	}
	return "", fmt.Errorf("unsupported image source type %q", src.Type)
}

// blocksText flattens a string-or-blocks field (system) into plain text.
func blocksText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", errors.New("must be a string or a list of content blocks")
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// estimateInputTokens is the classifier's chars/4 estimate, computed without
// re-running classification.
func estimateInputTokens(req *provider.ChatRequest) int {
	n := 0
	for i := range req.Messages {
		n += len(req.Messages[i].Text()) + 1 // the classifier joins with newlines
	}
	return n / 4
}

// anthropicStopReason maps an OpenAI finish_reason onto Anthropic's vocabulary.
func anthropicStopReason(finish string) string {
	switch finish {
	case "length":
		return "max_tokens"
	case "":
		return ""
	default:
		return "end_turn"
	}
}

// anthropicWriter wraps the client's ResponseWriter and rewrites whatever the
// provider adapter produces — a JSON completion or an OpenAI SSE stream — into
// the Anthropic Messages shape. Streams are translated event by event as they
// arrive; only single non-streamed bodies are buffered.
type anthropicWriter struct {
	http.ResponseWriter
	model    string // model the client asked for; the routed id wins when known
	estInput int    // chars/4 estimate, used until the upstream reports usage

	sse    bool
	status int
	begun  bool // message_start + content_block_start already emitted
	id     string

	pending bytes.Buffer // partial SSE line
	body    bytes.Buffer // non-streamed response body
	outText int          // characters streamed, for the output-token estimate

	stopReason string
	inTokens   int
	outTokens  int
	usageKnown bool
}

// WriteHeader decides the translation mode from the Content-Type the adapter
// set. Streams commit immediately (their headers are the client's cue to start
// reading); a single body is held back so a translation failure can still be
// reported as an error status.
func (aw *anthropicWriter) WriteHeader(code int) {
	if aw.status != 0 {
		return
	}
	aw.status = code
	aw.sse = strings.HasPrefix(aw.Header().Get("Content-Type"), "text/event-stream")
	if aw.sse {
		aw.ResponseWriter.WriteHeader(code)
	}
}

func (aw *anthropicWriter) Write(b []byte) (int, error) {
	if aw.status == 0 {
		aw.WriteHeader(http.StatusOK)
	}
	if !aw.sse {
		return aw.body.Write(b)
	}
	aw.pending.Write(b)
	for {
		buf := aw.pending.Bytes()
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(buf[:i]), "\r")
		aw.pending.Next(i + 1)
		aw.streamLine(line)
	}
	return len(b), nil
}

func (aw *anthropicWriter) Flush() {
	if f, ok := aw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// openAIChunk is the part of an OpenAI chunk (or completion) we translate.
type openAIChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// streamLine translates one line of the adapter's OpenAI SSE output.
func (aw *anthropicWriter) streamLine(line string) {
	data, ok := strings.CutPrefix(line, "data: ")
	if !ok || data == "[DONE]" {
		return // blank separators, "event:" lines and the terminator carry nothing
	}
	var chunk openAIChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}
	if !aw.begun {
		aw.begin(chunk.ID)
	}
	if chunk.Usage != nil {
		aw.inTokens, aw.outTokens = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
		aw.usageKnown = true
	}
	for _, c := range chunk.Choices {
		if c.Delta.Content != "" {
			aw.outText += len(c.Delta.Content)
			aw.emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": c.Delta.Content},
			})
		}
		if c.FinishReason != "" {
			aw.stopReason = anthropicStopReason(c.FinishReason)
		}
	}
}

// begin emits message_start and the single text content_block_start. The
// input-token count is the estimate: an OpenAI upstream only reports usage at
// the end of the stream, long after this event has to go out.
func (aw *anthropicWriter) begin(upstreamID string) {
	aw.begun = true
	aw.id = messageID(upstreamID)
	aw.emit("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            aw.id,
			"type":          "message",
			"role":          "assistant",
			"model":         aw.modelName(),
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": aw.estInput, "output_tokens": 0},
		},
	})
	aw.emit("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
}

func (aw *anthropicWriter) emit(event string, payload map[string]any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(aw.ResponseWriter, "event: %s\ndata: %s\n\n", event, b)
	aw.Flush()
}

// finish writes the terminal events (stream) or the translated body
// (non-stream). ok is false when no candidate succeeded: an error envelope has
// already gone out, or a committed stream died mid-flight — in which case
// closing it cleanly would tell the client a truncated answer was complete.
func (aw *anthropicWriter) finish(ok bool) {
	if !ok {
		return
	}
	if aw.sse {
		if !aw.begun {
			aw.begin("")
		}
		aw.emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		aw.emit("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   aw.stopReasonOrDefault(),
				"stop_sequence": nil,
			},
			"usage": map[string]any{"output_tokens": aw.outputTokens()},
		})
		aw.emit("message_stop", map[string]any{"type": "message_stop"})
		return
	}

	var chunk openAIChunk
	if err := json.Unmarshal(aw.body.Bytes(), &chunk); err != nil {
		writeAnthropicErr(aw.ResponseWriter, http.StatusBadGateway,
			"upstream response was not a chat completion: "+err.Error(), "upstream_error")
		return
	}
	var text strings.Builder
	stop := ""
	for _, c := range chunk.Choices {
		text.WriteString(c.Message.Content)
		if c.FinishReason != "" {
			stop = c.FinishReason
		}
	}
	in, out := aw.estInput, len(text.String())/4
	if chunk.Usage != nil {
		in, out = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
	}
	body, err := json.Marshal(map[string]any{
		"id":            messageID(chunk.ID),
		"type":          "message",
		"role":          "assistant",
		"model":         aw.modelName(),
		"content":       []map[string]any{{"type": "text", "text": text.String()}},
		"stop_reason":   anthropicStopReason(stop),
		"stop_sequence": nil,
		"usage":         map[string]any{"input_tokens": in, "output_tokens": out},
	})
	if err != nil {
		writeAnthropicErr(aw.ResponseWriter, http.StatusInternalServerError, err.Error(), "prism_internal_error")
		return
	}
	aw.Header().Set("Content-Type", "application/json")
	aw.ResponseWriter.WriteHeader(aw.statusOrOK())
	aw.ResponseWriter.Write(body)
}

func (aw *anthropicWriter) statusOrOK() int {
	if aw.status == 0 {
		return http.StatusOK
	}
	return aw.status
}

func (aw *anthropicWriter) stopReasonOrDefault() string {
	if aw.stopReason == "" {
		return "end_turn"
	}
	return aw.stopReason
}

func (aw *anthropicWriter) outputTokens() int {
	if aw.usageKnown {
		return aw.outTokens
	}
	return aw.outText / 4
}

// modelName prefers the model actually routed to (set on the response headers
// by executeChain) over the alias the client asked for.
func (aw *anthropicWriter) modelName() string {
	if routed := aw.Header().Get("X-Prism-Model"); routed != "" {
		return routed
	}
	return aw.model
}

func messageID(upstream string) string {
	if upstream == "" {
		return "msg_prism"
	}
	if strings.HasPrefix(upstream, "msg_") {
		return upstream
	}
	return "msg_" + upstream
}

// writeAnthropicErr writes the Anthropic error envelope, whose error type is
// derived from the HTTP status the SDKs key off.
func writeAnthropicErr(w http.ResponseWriter, status int, msg, _ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"type":  "error",
		"error": map[string]any{"type": anthropicErrorType(status), "message": msg},
	})
}

func anthropicErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		return "overloaded_error"
	default:
		return "api_error"
	}
}
