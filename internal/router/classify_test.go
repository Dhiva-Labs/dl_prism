package router

import (
	"encoding/json"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/provider"
)

func textMessage(t *testing.T, role, text string) provider.Message {
	t.Helper()
	b, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal text: %v", err)
	}
	return provider.Message{Role: role, Content: b}
}

func partsMessage(t *testing.T, role string, parts []provider.Part) provider.Message {
	t.Helper()
	b, err := json.Marshal(parts)
	if err != nil {
		t.Fatalf("marshal parts: %v", err)
	}
	return provider.Message{Role: role, Content: b}
}

func TestClassifyPythonFence(t *testing.T) {
	text := "```python\ndef foo():\n    return 1\n```\n"
	req := &provider.ChatRequest{Messages: []provider.Message{textMessage(t, "user", text)}}
	p := Classify(req)
	if p.Task != "code" {
		t.Errorf("Task = %q, want %q", p.Task, "code")
	}
	if p.Language != "python" {
		t.Errorf("Language = %q, want %q", p.Language, "python")
	}
}

func TestClassifyProseLanguageMention(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"Write a python function that deduplicates a list", "python"},
		{"implement this in rust please", "rust"},
		{"a golang script for backups", "go"},
		{"can you go over my essay", ""}, // bare "go" must not match
	}
	for _, tt := range tests {
		req := &provider.ChatRequest{Messages: []provider.Message{textMessage(t, "user", tt.text)}}
		p := Classify(req)
		if p.Language != tt.want {
			t.Errorf("Classify(%q).Language = %q, want %q", tt.text, p.Language, tt.want)
		}
		if tt.want != "" && p.Task != "code" {
			t.Errorf("Classify(%q).Task = %q, want code", tt.text, p.Task)
		}
	}
}

func TestClassifyFenceAliasNormalization(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"py alias", "```py\nprint('hi')\n```", "python"},
		{"golang alias", "```golang\npackage main\n```", "go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &provider.ChatRequest{Messages: []provider.Message{textMessage(t, "user", tt.text)}}
			p := Classify(req)
			if p.Language != tt.want {
				t.Errorf("Language = %q, want %q", p.Language, tt.want)
			}
		})
	}
}

func TestClassifyKeywordDetectionNeedsTwoHints(t *testing.T) {
	// Two hints ("def " and "import ") should identify python.
	req := &provider.ChatRequest{Messages: []provider.Message{textMessage(t, "user", "def foo():\nimport os")}}
	p := Classify(req)
	if p.Language != "python" {
		t.Errorf("Language = %q, want %q for two keyword hints", p.Language, "python")
	}
	if p.Task != "code" {
		t.Errorf("Task = %q, want %q", p.Task, "code")
	}

	// A single hint must not be enough to identify a language.
	req = &provider.ChatRequest{Messages: []provider.Message{textMessage(t, "user", "def foo():")}}
	p = Classify(req)
	if p.Language != "" {
		t.Errorf("Language = %q, want empty for a single keyword hint", p.Language)
	}
}

func TestClassifySecurityTask(t *testing.T) {
	req := &provider.ChatRequest{Messages: []provider.Message{
		textMessage(t, "user", "Please perform a security audit and list vulnerabilities in this code."),
	}}
	p := Classify(req)
	if p.Task != "security" {
		t.Errorf("Task = %q, want %q", p.Task, "security")
	}
}

func TestClassifyImageContentIsVision(t *testing.T) {
	parts := []provider.Part{
		{Type: "text", Text: "what is in this image?"},
		{Type: "image_url", ImageURL: &provider.ImageURL{URL: "https://example.com/img.png"}},
	}
	req := &provider.ChatRequest{Messages: []provider.Message{partsMessage(t, "user", parts)}}
	p := Classify(req)
	if !p.Vision {
		t.Error("Vision = false, want true")
	}
	if p.Task != "vision" {
		t.Errorf("Task = %q, want %q", p.Task, "vision")
	}
}

func TestClassifyPlainQuestionIsGeneral(t *testing.T) {
	req := &provider.ChatRequest{Messages: []provider.Message{
		textMessage(t, "user", "What is the capital of France?"),
	}}
	p := Classify(req)
	if p.Task != "general" {
		t.Errorf("Task = %q, want %q", p.Task, "general")
	}
	if p.Language != "" {
		t.Errorf("Language = %q, want empty", p.Language)
	}
}

func TestClassifyEstTokensApproximatesQuarterOfLength(t *testing.T) {
	texts := []string{"hello world", "a much longer message used to compute token estimates"}
	var messages []provider.Message
	var totalLen int
	for _, s := range texts {
		messages = append(messages, textMessage(t, "user", s))
		totalLen += len(s) + 1 // Classify appends "\n" after each message's text.
	}
	req := &provider.ChatRequest{Messages: messages}
	p := Classify(req)
	want := totalLen / 4
	if p.EstTokens != want {
		t.Errorf("EstTokens = %d, want %d", p.EstTokens, want)
	}
}
