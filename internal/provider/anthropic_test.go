package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func mustParseRequest(t *testing.T, body string) *ChatRequest {
	t.Helper()
	req, err := ParseChatRequest([]byte(body))
	if err != nil {
		t.Fatalf("ParseChatRequest(%s): %v", body, err)
	}
	return req
}

// normalize round-trips a value through JSON so translated bodies (map[string]any
// holding ints, *float64 derefs, []map[string]any, ...) compare against literal
// expected JSON without type mismatches.
func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return out
}

func normalizeJSON(t *testing.T, s string) any {
	t.Helper()
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("unmarshal %s: %v", s, err)
	}
	return out
}

func TestToAnthropicRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "plain message defaults max_tokens to 4096",
			body: `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "system message is lifted out of the message list",
			body: `{"model":"auto","messages":[
			         {"role":"system","content":"be terse"},
			         {"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"system":"be terse",
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "developer role is treated as system",
			body: `{"model":"auto","messages":[
			         {"role":"developer","content":"follow the style guide"},
			         {"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"system":"follow the style guide",
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "multiple system/developer messages join with a blank line",
			body: `{"model":"auto","messages":[
			         {"role":"system","content":"be terse"},
			         {"role":"developer","content":"prefer go"},
			         {"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"system":"be terse\n\nprefer go",
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "structured system content is flattened to text",
			body: `{"model":"auto","messages":[
			         {"role":"system","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]},
			         {"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"system":"a\nb",
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "explicit max_tokens wins over the default",
			body: `{"model":"auto","max_tokens":128,"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":128,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "max_completion_tokens is used when max_tokens is absent",
			body: `{"model":"auto","max_completion_tokens":256,"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":256,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "max_tokens takes precedence over max_completion_tokens",
			body: `{"model":"auto","max_tokens":10,"max_completion_tokens":256,
			        "messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":10,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "temperature and top_p are forwarded",
			body: `{"model":"auto","temperature":0.25,"top_p":0.9,
			        "messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"temperature":0.25,"top_p":0.9,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "zero temperature is forwarded, not dropped",
			body: `{"model":"auto","temperature":0,"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"temperature":0,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "string stop becomes a single stop_sequence",
			body: `{"model":"auto","stop":"END","messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"stop_sequences":["END"],
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "array stop becomes stop_sequences",
			body: `{"model":"auto","stop":["\n\n","END"],"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"stop_sequences":["\n\n","END"],
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "null stop is omitted",
			body: `{"model":"auto","stop":null,"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "empty stop array is omitted",
			body: `{"model":"auto","stop":[],"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "stream flag is translated",
			body: `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"stream":true,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "assistant turns are preserved in order",
			body: `{"model":"auto","messages":[
			         {"role":"user","content":"1"},
			         {"role":"assistant","content":"2"},
			         {"role":"user","content":"3"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"messages":[
			         {"role":"user","content":"1"},
			         {"role":"assistant","content":"2"},
			         {"role":"user","content":"3"}]}`,
		},
		{
			name: "null tools does not trip the tool rejection",
			body: `{"model":"auto","tools":null,"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "empty tools array does not trip the tool rejection",
			body: `{"model":"auto","tools":[],"messages":[{"role":"user","content":"hi"}]}`,
			want: `{"model":"claude-test","max_tokens":4096,
			        "messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "image data URL becomes a base64 source block",
			body: `{"model":"auto","messages":[{"role":"user","content":[
			         {"type":"text","text":"what is this?"},
			         {"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"messages":[{"role":"user","content":[
			         {"type":"text","text":"what is this?"},
			         {"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}]}`,
		},
		{
			name: "http image URL becomes a url source block",
			body: `{"model":"auto","messages":[{"role":"user","content":[
			         {"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}]}]}`,
			want: `{"model":"claude-test","max_tokens":4096,"messages":[{"role":"user","content":[
			         {"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}}]}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toAnthropicRequest("claude-test", mustParseRequest(t, tc.body))
			if err != nil {
				t.Fatalf("toAnthropicRequest: unexpected error: %v", err)
			}
			if want, have := normalizeJSON(t, tc.want), normalize(t, got); !reflect.DeepEqual(want, have) {
				gotJSON, _ := json.Marshal(got)
				t.Errorf("translated body mismatch\n got: %s\nwant: %s", gotJSON, tc.want)
			}
		})
	}
}

func TestToAnthropicRequestErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantSub string
	}{
		{
			name:    "tools are rejected",
			body:    `{"model":"auto","tools":[{"type":"function","function":{"name":"f"}}],"messages":[{"role":"user","content":"hi"}]}`,
			wantSub: "does not translate tool calls",
		},
		{
			name:    "tool result messages are rejected",
			body:    `{"model":"auto","messages":[{"role":"tool","content":"result"}]}`,
			wantSub: `cannot translate role "tool"`,
		},
		{
			name:    "non-base64 data URL is rejected",
			body:    `{"model":"auto","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png,rawbytes"}}]}]}`,
			wantSub: "unsupported data URL",
		},
		{
			name:    "image_url part without a url object is rejected",
			body:    `{"model":"auto","messages":[{"role":"user","content":[{"type":"image_url"}]}]}`,
			wantSub: "image_url part without url",
		},
		{
			name:    "unknown content part type is rejected",
			body:    `{"model":"auto","messages":[{"role":"user","content":[{"type":"input_audio"}]}]}`,
			wantSub: `cannot translate content part type "input_audio"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toAnthropicRequest("claude-test", mustParseRequest(t, tc.body))
			if err == nil {
				t.Fatalf("toAnthropicRequest: want error containing %q, got body %v", tc.wantSub, got)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantSub)
			}
		})
	}
}

func TestParseStops(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"absent", "", nil},
		{"null", `null`, nil},
		{"string", `"END"`, []string{"END"}},
		{"array", `["a","b"]`, []string{"a", "b"}},
		{"empty array", `[]`, nil},
		{"unsupported shape", `{"a":1}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStops(json.RawMessage(tc.raw))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseStops(%s) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestFromAnthropicResponse(t *testing.T) {
	tests := []struct {
		name             string
		upstream         string
		wantContent      string
		wantFinish       string
		wantPrompt       int
		wantCompletion   int
		wantModel, wanID string
	}{
		{
			name: "text blocks concatenate and end_turn maps to stop",
			upstream: `{"id":"msg_01","model":"claude-sonnet-4-5","stop_reason":"end_turn",
			            "content":[{"type":"text","text":"Hello"},{"type":"text","text":", world"}],
			            "usage":{"input_tokens":11,"output_tokens":7}}`,
			wantContent:    "Hello, world",
			wantFinish:     "stop",
			wantPrompt:     11,
			wantCompletion: 7,
			wantModel:      "claude-sonnet-4-5",
			wanID:          "msg_01",
		},
		{
			name: "non-text blocks are skipped",
			upstream: `{"id":"msg_02","model":"m","stop_reason":"end_turn",
			            "content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"answer"}],
			            "usage":{"input_tokens":1,"output_tokens":2}}`,
			wantContent:    "answer",
			wantFinish:     "stop",
			wantPrompt:     1,
			wantCompletion: 2,
			wantModel:      "m",
			wanID:          "msg_02",
		},
		{
			name: "max_tokens maps to length",
			upstream: `{"id":"msg_03","model":"m","stop_reason":"max_tokens",
			            "content":[{"type":"text","text":"trunc"}],
			            "usage":{"input_tokens":3,"output_tokens":4}}`,
			wantContent:    "trunc",
			wantFinish:     "length",
			wantPrompt:     3,
			wantCompletion: 4,
			wantModel:      "m",
			wanID:          "msg_03",
		},
		{
			name: "stop_sequence maps to stop",
			upstream: `{"id":"msg_04","model":"m","stop_reason":"stop_sequence",
			            "content":[{"type":"text","text":"x"}],"usage":{"input_tokens":0,"output_tokens":0}}`,
			wantContent: "x",
			wantFinish:  "stop",
			wantModel:   "m",
			wanID:       "msg_04",
		},
		{
			name:        "empty content yields an empty assistant message",
			upstream:    `{"id":"msg_05","model":"m","stop_reason":"end_turn","content":[],"usage":{}}`,
			wantContent: "",
			wantFinish:  "stop",
			wantModel:   "m",
			wanID:       "msg_05",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, res, err := fromAnthropicResponse([]byte(tc.upstream))
			if err != nil {
				t.Fatalf("fromAnthropicResponse: %v", err)
			}
			var got struct {
				ID      string `json:"id"`
				Object  string `json:"object"`
				Created int64  `json:"created"`
				Model   string `json:"model"`
				Choices []struct {
					Index        int    `json:"index"`
					FinishReason string `json:"finish_reason"`
					Message      struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("translated response is not valid OpenAI JSON: %v (%s)", err, out)
			}
			if got.ID != tc.wanID {
				t.Errorf("id = %q, want %q", got.ID, tc.wanID)
			}
			if got.Object != "chat.completion" {
				t.Errorf("object = %q, want chat.completion", got.Object)
			}
			if got.Created == 0 {
				t.Error("created timestamp not set")
			}
			if got.Model != tc.wantModel {
				t.Errorf("model = %q, want %q", got.Model, tc.wantModel)
			}
			if len(got.Choices) != 1 {
				t.Fatalf("choices = %d, want 1", len(got.Choices))
			}
			c := got.Choices[0]
			if c.Message.Role != "assistant" {
				t.Errorf("role = %q, want assistant", c.Message.Role)
			}
			if c.Message.Content != tc.wantContent {
				t.Errorf("content = %q, want %q", c.Message.Content, tc.wantContent)
			}
			if c.FinishReason != tc.wantFinish {
				t.Errorf("finish_reason = %q, want %q", c.FinishReason, tc.wantFinish)
			}
			if got.Usage.PromptTokens != tc.wantPrompt || got.Usage.CompletionTokens != tc.wantCompletion {
				t.Errorf("usage = %d/%d, want %d/%d",
					got.Usage.PromptTokens, got.Usage.CompletionTokens, tc.wantPrompt, tc.wantCompletion)
			}
			if want := tc.wantPrompt + tc.wantCompletion; got.Usage.TotalTokens != want {
				t.Errorf("total_tokens = %d, want %d", got.Usage.TotalTokens, want)
			}
			if res == nil || !res.UsageKnown {
				t.Fatalf("Result = %#v, want usage known", res)
			}
			if res.PromptTokens != tc.wantPrompt || res.CompletionTokens != tc.wantCompletion {
				t.Errorf("Result tokens = %d/%d, want %d/%d",
					res.PromptTokens, res.CompletionTokens, tc.wantPrompt, tc.wantCompletion)
			}
		})
	}
}

func TestFromAnthropicResponseInvalidJSON(t *testing.T) {
	if _, _, err := fromAnthropicResponse([]byte("not json")); err == nil {
		t.Fatal("want error decoding a non-JSON upstream response")
	}
}

// sseEvents splits an SSE body into its `data:` payloads.
func sseEvents(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			out = append(out, data)
		}
	}
	return out
}

type openAIChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// anthropicStream is a realistic Anthropic Messages SSE stream.
const anthropicStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_01STREAM","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"usage":{"input_tokens":25,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: ping
data: {"type":"ping"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":15}}

event: message_stop
data: {"type":"message_stop"}

`

func TestTranslateStream(t *testing.T) {
	w := httptest.NewRecorder()
	res, err := translateStream(w, strings.NewReader(anthropicStream), "claude-test")
	if err != nil {
		t.Fatalf("translateStream: %v", err)
	}

	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	events := sseEvents(t, w.Body.String())
	if len(events) != 6 {
		t.Fatalf("got %d SSE events, want 6:\n%s", len(events), w.Body.String())
	}
	if last := events[len(events)-1]; last != "[DONE]" {
		t.Errorf("final event = %q, want [DONE]", last)
	}

	chunks := make([]openAIChunk, 0, 5)
	for _, e := range events[:len(events)-1] {
		var c openAIChunk
		if err := json.Unmarshal([]byte(e), &c); err != nil {
			t.Fatalf("chunk %q is not valid JSON: %v", e, err)
		}
		if c.Object != "chat.completion.chunk" {
			t.Errorf("object = %q, want chat.completion.chunk", c.Object)
		}
		if c.Model != "claude-test" {
			t.Errorf("model = %q, want the prism-side model claude-test", c.Model)
		}
		if c.ID != "msg_01STREAM" {
			t.Errorf("id = %q, want the upstream message id", c.ID)
		}
		if len(c.Choices) != 1 {
			t.Fatalf("chunk %q has %d choices, want 1", e, len(c.Choices))
		}
		chunks = append(chunks, c)
	}

	// First chunk opens the message with the assistant role and no content.
	if r := chunks[0].Choices[0].Delta.Role; r != "assistant" {
		t.Errorf("first chunk role = %q, want assistant", r)
	}
	if c := chunks[0].Choices[0].Delta.Content; c != "" {
		t.Errorf("first chunk content = %q, want empty", c)
	}
	if f := chunks[0].Choices[0].FinishReason; f != nil {
		t.Errorf("first chunk finish_reason = %v, want null", *f)
	}

	// Middle chunks carry the text deltas, in order, with no finish_reason.
	var text strings.Builder
	for _, c := range chunks[1:4] {
		if f := c.Choices[0].FinishReason; f != nil {
			t.Errorf("delta chunk finish_reason = %q, want null", *f)
		}
		if c.Choices[0].Delta.Role != "" {
			t.Errorf("delta chunk repeated the role: %q", c.Choices[0].Delta.Role)
		}
		text.WriteString(c.Choices[0].Delta.Content)
	}
	if text.String() != "Hello, world!" {
		t.Errorf("streamed text = %q, want %q", text.String(), "Hello, world!")
	}

	// Final chunk carries only the finish reason.
	final := chunks[4].Choices[0]
	if final.FinishReason == nil || *final.FinishReason != "stop" {
		t.Errorf("final chunk finish_reason = %v, want stop", final.FinishReason)
	}
	if final.Delta.Content != "" || final.Delta.Role != "" {
		t.Errorf("final chunk delta = %+v, want empty", final.Delta)
	}

	if !res.UsageKnown {
		t.Error("Result.UsageKnown = false, want true")
	}
	if res.PromptTokens != 25 {
		t.Errorf("Result.PromptTokens = %d, want 25 (from message_start)", res.PromptTokens)
	}
	if res.CompletionTokens != 15 {
		t.Errorf("Result.CompletionTokens = %d, want 15 (from message_delta)", res.CompletionTokens)
	}
}

func TestTranslateStreamStopReasons(t *testing.T) {
	tests := []struct {
		name       string
		stopReason string
		want       string
	}{
		{"end_turn", "end_turn", "stop"},
		{"max_tokens", "max_tokens", "length"},
		{"stop_sequence", "stop_sequence", "stop"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := `data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":2}}}

data: {"type":"message_delta","delta":{"stop_reason":"` + tc.stopReason + `"},"usage":{"output_tokens":9}}

data: {"type":"message_stop"}

`
			w := httptest.NewRecorder()
			if _, err := translateStream(w, strings.NewReader(stream), "m"); err != nil {
				t.Fatalf("translateStream: %v", err)
			}
			events := sseEvents(t, w.Body.String())
			var c openAIChunk
			if err := json.Unmarshal([]byte(events[len(events)-2]), &c); err != nil {
				t.Fatalf("decoding final chunk: %v", err)
			}
			got := c.Choices[0].FinishReason
			if got == nil || *got != tc.want {
				t.Errorf("finish_reason = %v, want %q", got, tc.want)
			}
		})
	}
}

func TestTranslateStreamIgnoresUnparseableLines(t *testing.T) {
	stream := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":4}}}` + "\n\n" +
		": a comment line\n\n" +
		"data: {not json}\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}` + "\n\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	w := httptest.NewRecorder()
	res, err := translateStream(w, strings.NewReader(stream), "m")
	if err != nil {
		t.Fatalf("translateStream: %v", err)
	}
	events := sseEvents(t, w.Body.String())
	// role chunk, one text delta, [DONE] — the malformed line, the comment and
	// the non-text delta are all dropped.
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3:\n%s", len(events), w.Body.String())
	}
	var c openAIChunk
	if err := json.Unmarshal([]byte(events[1]), &c); err != nil {
		t.Fatalf("decoding text chunk: %v", err)
	}
	if c.Choices[0].Delta.Content != "ok" {
		t.Errorf("content = %q, want %q", c.Choices[0].Delta.Content, "ok")
	}
	if res.PromptTokens != 4 {
		t.Errorf("PromptTokens = %d, want 4", res.PromptTokens)
	}
	if res.UsageKnown {
		t.Error("UsageKnown = true, want false: the stream carried no message_delta usage")
	}
}

// A mid-stream error event must surface as an error: the caller can no longer
// fail over, but it must not record a failed request as a success.
func TestTranslateStreamReportsUpstreamErrorEvents(t *testing.T) {
	stream := `data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":30}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}

event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}

`
	w := httptest.NewRecorder()
	res, err := translateStream(w, strings.NewReader(stream), "m")
	if err == nil {
		t.Fatal("want an error when the stream carries an error event")
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Errorf("error = %q, want it to relay the upstream error payload", err)
	}
	if res == nil {
		t.Fatal("want a non-nil Result so the caller keeps the tokens already spent")
	}
	// Whatever was already emitted stays emitted; the stream simply ends.
	if !strings.Contains(w.Body.String(), "partial") {
		t.Errorf("body = %q, want the deltas emitted before the failure", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "[DONE]") {
		t.Errorf("body = %q, want no [DONE] terminator on a failed stream", w.Body.String())
	}
}

func TestTranslateStreamPropagatesWriteErrors(t *testing.T) {
	res, err := translateStream(&failingWriter{}, strings.NewReader(anthropicStream), "m")
	if err == nil {
		t.Fatal("want the write error to propagate to the caller")
	}
	if res == nil {
		t.Fatal("want a non-nil Result even on write failure, so the caller can still record usage")
	}
}

// failingWriter accepts headers but fails every body write, standing in for a
// client that hung up mid-stream.
type failingWriter struct {
	header http.Header
}

func (f *failingWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (f *failingWriter) WriteHeader(int)           {}
