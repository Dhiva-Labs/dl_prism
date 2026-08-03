package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

const (
	anthropicVersion   = "2023-06-01"
	defaultMaxTokens   = 4096
	anthropicChunkSize = 4 * 1024 * 1024
)

// Anthropic translates OpenAI chat-completion requests to the Anthropic
// Messages API and translates responses (including SSE streams) back.
// Tool calling is not translated yet; requests with tools are rejected.
type Anthropic struct{}

func (a *Anthropic) ChatCompletion(p *config.Provider, model string, req *ChatRequest, w http.ResponseWriter, r *http.Request) (*Result, error) {
	body, err := toAnthropicRequest(model, req)
	if err != nil {
		return nil, err
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	up.Header.Set("Content-Type", "application/json")
	up.Header.Set("anthropic-version", anthropicVersion)
	if k := p.Key(); k != "" {
		up.Header.Set("x-api-key", k)
	}
	for k, v := range p.Headers {
		up.Header.Set(k, v)
	}

	resp, err := httpClient.Do(up)
	if err != nil {
		return nil, &UpstreamError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &UpstreamError{Status: resp.StatusCode, Body: string(b)}
	}

	if req.Stream {
		return translateStream(w, resp.Body, model)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &UpstreamError{Err: err}
	}
	out, res, err := fromAnthropicResponse(b)
	if err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(out)
	return res, err
}

// toAnthropicRequest maps the OpenAI request shape onto Anthropic Messages.
func toAnthropicRequest(model string, req *ChatRequest) (map[string]any, error) {
	if len(req.Tools) > 0 && string(req.Tools) != "null" && string(req.Tools) != "[]" {
		return nil, fmt.Errorf("anthropic adapter does not translate tool calls yet; route tool requests to an openai-type provider")
	}
	var system []string
	msgs := []map[string]any{}
	for i := range req.Messages {
		m := &req.Messages[i]
		switch m.Role {
		case "system", "developer":
			system = append(system, m.Text())
		case "user", "assistant":
			content, err := toAnthropicContent(m)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, map[string]any{"role": m.Role, "content": content})
		default:
			return nil, fmt.Errorf("anthropic adapter cannot translate role %q", m.Role)
		}
	}

	maxTokens := defaultMaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	} else if req.MaxComplete != nil {
		maxTokens = *req.MaxComplete
	}
	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTokens,
	}
	if req.Stream {
		body["stream"] = true
	}
	if len(system) > 0 {
		body["system"] = strings.Join(system, "\n\n")
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if stops := parseStops(req.Stop); len(stops) > 0 {
		body["stop_sequences"] = stops
	}
	return body, nil
}

func parseStops(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func toAnthropicContent(m *Message) (any, error) {
	parts := m.Parts()
	if parts == nil {
		return m.Text(), nil
	}
	var blocks []map[string]any
	for _, p := range parts {
		switch p.Type {
		case "text":
			blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
		case "image_url":
			if p.ImageURL == nil {
				return nil, fmt.Errorf("image_url part without url")
			}
			block, err := toAnthropicImage(p.ImageURL.URL)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		default:
			return nil, fmt.Errorf("anthropic adapter cannot translate content part type %q", p.Type)
		}
	}
	return blocks, nil
}

func toAnthropicImage(url string) (map[string]any, error) {
	if data, ok := strings.CutPrefix(url, "data:"); ok {
		media, b64, ok := strings.Cut(data, ";base64,")
		if !ok {
			return nil, fmt.Errorf("unsupported data URL (expected base64)")
		}
		return map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": media, "data": b64},
		}, nil
	}
	return map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": url},
	}, nil
}

type anthropicResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage anthropicUsage `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func mapStopReason(s string) string {
	if s == "max_tokens" {
		return "length"
	}
	return "stop"
}

func fromAnthropicResponse(b []byte) ([]byte, *Result, error) {
	var ar anthropicResponse
	if err := json.Unmarshal(b, &ar); err != nil {
		return nil, nil, fmt.Errorf("decoding anthropic response: %w", err)
	}
	var text strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	out, err := json.Marshal(map[string]any{
		"id":      ar.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   ar.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": text.String()},
			"finish_reason": mapStopReason(ar.StopReason),
		}},
		"usage": map[string]any{
			"prompt_tokens":     ar.Usage.InputTokens,
			"completion_tokens": ar.Usage.OutputTokens,
			"total_tokens":      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return out, &Result{
		PromptTokens:     ar.Usage.InputTokens,
		CompletionTokens: ar.Usage.OutputTokens,
		UsageKnown:       true,
	}, nil
}

// translateStream converts Anthropic SSE events into OpenAI chat chunks.
func translateStream(w http.ResponseWriter, body io.Reader, model string) (*Result, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	res := &Result{}
	id := "chatcmpl-prism"
	created := time.Now().Unix()
	emit := func(payload map[string]any) error {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), anthropicChunkSize)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				ID    string         `json:"id"`
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
			Delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage anthropicUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			if ev.Message.ID != "" {
				id = ev.Message.ID
			}
			res.PromptTokens = ev.Message.Usage.InputTokens
			if err := emit(chunk(map[string]any{"role": "assistant", "content": ""}, nil)); err != nil {
				return res, err
			}
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				if err := emit(chunk(map[string]any{"content": ev.Delta.Text}, nil)); err != nil {
					return res, err
				}
			}
		case "message_delta":
			res.CompletionTokens = ev.Usage.OutputTokens
			res.UsageKnown = true
			if err := emit(chunk(map[string]any{}, mapStopReason(ev.Delta.StopReason))); err != nil {
				return res, err
			}
		case "message_stop":
			if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
				return res, err
			}
			if flusher != nil {
				flusher.Flush()
			}
		case "error":
			// The upstream failed part-way through. Bytes are already on the
			// wire so the caller cannot fail over, but it must not treat this
			// as a completed request: that would credit the circuit breaker
			// with a success and bill the ledger for a broken answer.
			return res, fmt.Errorf("anthropic stream error: %s", data)
		}
	}
	return res, sc.Err()
}
