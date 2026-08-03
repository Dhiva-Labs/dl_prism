package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// OpenAI forwards to any OpenAI-compatible endpoint (OpenAI, DeepSeek, Qwen,
// Mistral, Groq, OpenRouter, Ollama, vLLM, LM Studio, llama.cpp server...).
// The body passes through with only the model field rewritten.
type OpenAI struct{}

func (o *OpenAI) ChatCompletion(p *config.Provider, model string, req *ChatRequest, w http.ResponseWriter, r *http.Request) (*Result, error) {
	body := make(map[string]any, len(req.Raw))
	for k, v := range req.Raw {
		body[k] = v
	}
	body["model"] = model
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	up.Header.Set("Content-Type", "application/json")
	if k := p.Key(); k != "" {
		up.Header.Set("Authorization", "Bearer "+k)
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

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return pipeSSE(w, resp.Body)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &UpstreamError{Err: err}
	}
	res := &Result{}
	var parsed struct {
		Usage *usage `json:"usage"`
	}
	if json.Unmarshal(b, &parsed) == nil && parsed.Usage != nil {
		res.PromptTokens = parsed.Usage.PromptTokens
		res.CompletionTokens = parsed.Usage.CompletionTokens
		res.UsageKnown = true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(b)
	return res, err
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// pipeSSE streams the upstream's SSE bytes through unchanged, watching the
// chunks for a usage object (sent when the client asked for
// stream_options.include_usage).
func pipeSSE(w http.ResponseWriter, body io.Reader) (*Result, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	res := &Result{}
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return res, err
		}
		if flusher != nil && line == "" {
			flusher.Flush()
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok && data != "[DONE]" {
			var chunk struct {
				Usage *usage `json:"usage"`
			}
			if json.Unmarshal([]byte(data), &chunk) == nil && chunk.Usage != nil {
				res.PromptTokens = chunk.Usage.PromptTokens
				res.CompletionTokens = chunk.Usage.CompletionTokens
				res.UsageKnown = true
			}
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	return res, sc.Err()
}
