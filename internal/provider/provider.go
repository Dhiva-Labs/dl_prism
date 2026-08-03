// Package provider defines the upstream adapter interface and the two
// adapters that cover the market: generic OpenAI-compatible passthrough
// and an Anthropic Messages translator.
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// ChatRequest is the parsed OpenAI-format request. Raw retains every field
// from the client so passthrough adapters forward parameters we don't model.
type ChatRequest struct {
	Model       string          `json:"model"`
	Messages    []Message       `json:"messages"`
	Stream      bool            `json:"stream"`
	MaxTokens   *int            `json:"max_tokens"`
	MaxComplete *int            `json:"max_completion_tokens"`
	Temperature *float64        `json:"temperature"`
	TopP        *float64        `json:"top_p"`
	Stop        json.RawMessage `json:"stop"`
	Tools       json.RawMessage `json:"tools"`

	Raw map[string]any `json:"-"`
}

func ParseChatRequest(body []byte) (*ChatRequest, error) {
	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("request has no messages")
	}
	if err := json.Unmarshal(body, &req.Raw); err != nil {
		return nil, err
	}
	return &req, nil
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type Part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text"`
	ImageURL *ImageURL `json:"image_url"`
}

type ImageURL struct {
	URL string `json:"url"`
}

// Parts decodes structured content; nil when content is a plain string.
func (m *Message) Parts() []Part {
	var parts []Part
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return nil
	}
	return parts
}

// Text returns the message's textual content, flattening content parts.
func (m *Message) Text() string {
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	var b strings.Builder
	for _, p := range m.Parts() {
		if p.Type == "text" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func (r *ChatRequest) HasImages() bool {
	for i := range r.Messages {
		for _, p := range r.Messages[i].Parts() {
			if p.Type == "image_url" {
				return true
			}
		}
	}
	return false
}

// Result reports actual token usage when the upstream provided it.
type Result struct {
	PromptTokens     int
	CompletionTokens int
	UsageKnown       bool
}

// Adapter forwards a chat request to one upstream. Implementations must not
// write to w until the upstream has answered successfully, so the caller can
// fail over to the next candidate on error.
type Adapter interface {
	ChatCompletion(p *config.Provider, model string, req *ChatRequest, w http.ResponseWriter, r *http.Request) (*Result, error)
}

// ForType returns the adapter for a validated provider type.
func ForType(t string) (Adapter, error) {
	switch t {
	case "openai":
		return &OpenAI{}, nil
	case "anthropic":
		return &Anthropic{}, nil
	}
	return nil, fmt.Errorf("no adapter for provider type %q", t)
}

// UpstreamError is a failure from (or reaching) an upstream provider.
// Status 0 means the request never got an HTTP response.
type UpstreamError struct {
	Status int
	Body   string
	Err    error
}

func (e *UpstreamError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("upstream unreachable: %v", e.Err)
	}
	return fmt.Sprintf("upstream returned %d: %s", e.Status, e.Body)
}

// Retryable reports whether failing over to another candidate is safe.
func Retryable(err error) bool {
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		return false
	}
	return ue.Status == 0 || ue.Status == 408 || ue.Status == 429 || ue.Status >= 500
}

// httpClient allows long streams: no overall timeout, but bounded connect
// and first-byte waits (large local models can take minutes to load).
var httpClient = &http.Client{
	Transport: &http.Transport{
		ResponseHeaderTimeout: 10 * time.Minute,
		Proxy:                 http.ProxyFromEnvironment,
	},
}
