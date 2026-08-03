package provider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// capture records what an upstream received, so tests can assert on the
// forwarded request rather than only on the response.
type capture struct {
	path   string
	header http.Header
	body   []byte
}

// newUpstream starts a test upstream and returns it alongside the capture it
// fills in on each request.
func newUpstream(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.header = r.Header.Clone()
		got.body, _ = io.ReadAll(r.Body)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func openaiProvider(baseURL string) *config.Provider {
	return &config.Provider{Name: "test", Type: "openai", BaseURL: baseURL}
}

func chatRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
}

func TestOpenAIRewritesModelAndPreservesOtherFields(t *testing.T) {
	srv, got := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"cmpl-1","choices":[]}`))
	})

	clientBody := `{"model":"fast","messages":[{"role":"user","content":"hi"}],
	                "temperature":0.3,"seed":42,"stream_options":{"include_usage":true},
	                "response_format":{"type":"json_object"}}`
	req := mustParseRequest(t, clientBody)

	w := httptest.NewRecorder()
	if _, err := (&OpenAI{}).ChatCompletion(openaiProvider(srv.URL+"/v1"), "gpt-4o-mini", req, w, chatRequest(t)); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if got.path != "/v1/chat/completions" {
		t.Errorf("upstream path = %q, want /v1/chat/completions", got.path)
	}

	var forwarded map[string]any
	if err := json.Unmarshal(got.body, &forwarded); err != nil {
		t.Fatalf("forwarded body is not JSON: %v (%s)", err, got.body)
	}
	if forwarded["model"] != "gpt-4o-mini" {
		t.Errorf("forwarded model = %v, want the registry model id gpt-4o-mini", forwarded["model"])
	}

	// Every other field the client sent must survive the rewrite untouched.
	var want map[string]any
	if err := json.Unmarshal([]byte(clientBody), &want); err != nil {
		t.Fatal(err)
	}
	want["model"] = "gpt-4o-mini"
	if !reflect.DeepEqual(want, forwarded) {
		t.Errorf("forwarded body mismatch\n got: %v\nwant: %v", forwarded, want)
	}
}

func TestOpenAIAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		provider func(url string) *config.Provider
		wantAuth string
		wantHdr  map[string]string
	}{
		{
			name: "api_key_env is read from the environment",
			env:  map[string]string{"PRISM_TEST_KEY": "sk-from-env"},
			provider: func(url string) *config.Provider {
				p := openaiProvider(url)
				p.APIKeyEnv = "PRISM_TEST_KEY"
				return p
			},
			wantAuth: "Bearer sk-from-env",
		},
		{
			name: "api_key expands ${VAR} references",
			env:  map[string]string{"PRISM_TEST_KEY": "sk-expanded"},
			provider: func(url string) *config.Provider {
				p := openaiProvider(url)
				p.APIKey = "${PRISM_TEST_KEY}"
				return p
			},
			wantAuth: "Bearer sk-expanded",
		},
		{
			name: "literal api_key is used as-is",
			provider: func(url string) *config.Provider {
				p := openaiProvider(url)
				p.APIKey = "sk-literal"
				return p
			},
			wantAuth: "Bearer sk-literal",
		},
		{
			name:     "keyless local provider sends no Authorization header",
			provider: openaiProvider,
			wantAuth: "",
		},
		{
			name: "configured headers are applied and may override Authorization",
			provider: func(url string) *config.Provider {
				p := openaiProvider(url)
				p.APIKey = "sk-literal"
				p.Headers = map[string]string{
					"Authorization": "Bearer sk-override",
					"X-Title":       "prism",
				}
				return p
			},
			wantAuth: "Bearer sk-override",
			wantHdr:  map[string]string{"X-Title": "prism"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			srv, got := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{}`))
			})
			req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
			w := httptest.NewRecorder()
			if _, err := (&OpenAI{}).ChatCompletion(tc.provider(srv.URL+"/v1"), "m", req, w, chatRequest(t)); err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			if auth := got.header.Get("Authorization"); auth != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", auth, tc.wantAuth)
			}
			if ct := got.header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			for k, v := range tc.wantHdr {
				if h := got.header.Get(k); h != v {
					t.Errorf("header %s = %q, want %q", k, h, v)
				}
			}
		})
	}
}

func TestOpenAIBaseURLTrailingSlash(t *testing.T) {
	srv, got := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	w := httptest.NewRecorder()
	if _, err := (&OpenAI{}).ChatCompletion(openaiProvider(srv.URL+"/v1/"), "m", req, w, chatRequest(t)); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("upstream path = %q, want /v1/chat/completions (no double slash)", got.path)
	}
}

func TestOpenAINonStreamResponse(t *testing.T) {
	tests := []struct {
		name           string
		upstreamBody   string
		wantKnown      bool
		wantPrompt     int
		wantCompletion int
	}{
		{
			name:           "usage is parsed",
			upstreamBody:   `{"id":"cmpl-1","object":"chat.completion","usage":{"prompt_tokens":17,"completion_tokens":23,"total_tokens":40}}`,
			wantKnown:      true,
			wantPrompt:     17,
			wantCompletion: 23,
		},
		{
			name:         "missing usage leaves the result unknown",
			upstreamBody: `{"id":"cmpl-2","object":"chat.completion"}`,
		},
		{
			name:         "null usage leaves the result unknown",
			upstreamBody: `{"id":"cmpl-3","usage":null}`,
		},
		{
			name:         "non-JSON body still passes through",
			upstreamBody: `not json at all`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.upstreamBody))
			})
			req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
			w := httptest.NewRecorder()
			res, err := (&OpenAI{}).ChatCompletion(openaiProvider(srv.URL+"/v1"), "m", req, w, chatRequest(t))
			if err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", w.Code)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if w.Body.String() != tc.upstreamBody {
				t.Errorf("body = %q, want the upstream body verbatim %q", w.Body.String(), tc.upstreamBody)
			}
			if res.UsageKnown != tc.wantKnown {
				t.Errorf("UsageKnown = %t, want %t", res.UsageKnown, tc.wantKnown)
			}
			if res.PromptTokens != tc.wantPrompt || res.CompletionTokens != tc.wantCompletion {
				t.Errorf("tokens = %d/%d, want %d/%d",
					res.PromptTokens, res.CompletionTokens, tc.wantPrompt, tc.wantCompletion)
			}
		})
	}
}

// openaiStream is a realistic OpenAI SSE stream ending with a usage-only chunk,
// as sent when the client passes stream_options.include_usage.
const openaiStream = "data: {\"id\":\"cmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n" +
	"\n" +
	"data: {\"id\":\"cmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n" +
	"\n" +
	"data: [DONE]\n" +
	"\n"

func TestOpenAISSEPassthrough(t *testing.T) {
	srv, got := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(openaiStream))
		w.(http.Flusher).Flush()
	})

	req := mustParseRequest(t, `{"model":"m","stream":true,"stream_options":{"include_usage":true},
	                             "messages":[{"role":"user","content":"hi"}]}`)
	w := httptest.NewRecorder()
	res, err := (&OpenAI{}).ChatCompletion(openaiProvider(srv.URL+"/v1"), "m", req, w, chatRequest(t))
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if forwarded := string(got.body); !strings.Contains(forwarded, `"stream":true`) {
		t.Errorf("forwarded body lost the stream flag: %s", forwarded)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != openaiStream {
		t.Errorf("SSE bytes were not preserved\n got: %q\nwant: %q", w.Body.String(), openaiStream)
	}
	if !res.UsageKnown {
		t.Error("UsageKnown = false, want true: the final chunk carried usage")
	}
	if res.PromptTokens != 9 || res.CompletionTokens != 4 {
		t.Errorf("tokens = %d/%d, want 9/4", res.PromptTokens, res.CompletionTokens)
	}
}

func TestOpenAISSEWithoutUsage(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	srv, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Write([]byte(stream))
	})
	req := mustParseRequest(t, `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	w := httptest.NewRecorder()
	res, err := (&OpenAI{}).ChatCompletion(openaiProvider(srv.URL+"/v1"), "m", req, w, chatRequest(t))
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if w.Body.String() != stream {
		t.Errorf("SSE bytes were not preserved\n got: %q\nwant: %q", w.Body.String(), stream)
	}
	if res.UsageKnown {
		t.Error("UsageKnown = true, want false: no chunk carried usage")
	}
}

func TestOpenAIUpstreamErrorLeavesResponseUntouched(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantRetryable bool
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, true},
		{"server error", http.StatusInternalServerError, `{"error":{"message":"boom"}}`, true},
		{"gateway timeout", http.StatusGatewayTimeout, `upstream timeout`, true},
		{"bad request", http.StatusBadRequest, `{"error":{"message":"unknown parameter"}}`, false},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, false},
		{"not found", http.StatusNotFound, `{"error":{"message":"no such model"}}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
			w := httptest.NewRecorder()
			_, err := (&OpenAI{}).ChatCompletion(openaiProvider(srv.URL+"/v1"), "m", req, w, chatRequest(t))
			if err == nil {
				t.Fatalf("want an error for upstream status %d", tc.status)
			}
			var ue *UpstreamError
			if !errors.As(err, &ue) {
				t.Fatalf("error %v is not an *UpstreamError", err)
			}
			if ue.Status != tc.status {
				t.Errorf("UpstreamError.Status = %d, want %d", ue.Status, tc.status)
			}
			if ue.Body != tc.body {
				t.Errorf("UpstreamError.Body = %q, want %q", ue.Body, tc.body)
			}
			if Retryable(err) != tc.wantRetryable {
				t.Errorf("Retryable = %t, want %t", Retryable(err), tc.wantRetryable)
			}
			// Nothing may reach the client: the server still needs to fail over.
			if w.Body.Len() != 0 {
				t.Errorf("adapter wrote %q to the client before failing", w.Body.String())
			}
			if len(w.Header()) != 0 {
				t.Errorf("adapter set response headers before failing: %v", w.Header())
			}
		})
	}
}

func TestOpenAIUnreachableUpstreamIsRetryable(t *testing.T) {
	srv, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {})
	url := srv.URL + "/v1"
	srv.Close() // nothing is listening any more

	req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	w := httptest.NewRecorder()
	_, err := (&OpenAI{}).ChatCompletion(openaiProvider(url), "m", req, w, chatRequest(t))
	if err == nil {
		t.Fatal("want an error when the upstream is unreachable")
	}
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("error %v is not an *UpstreamError", err)
	}
	if ue.Status != 0 {
		t.Errorf("Status = %d, want 0 for a transport failure", ue.Status)
	}
	if !Retryable(err) {
		t.Error("a transport failure must be retryable")
	}
	if w.Body.Len() != 0 {
		t.Errorf("adapter wrote %q to the client before failing", w.Body.String())
	}
}

func TestRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"transport failure", &UpstreamError{Err: errors.New("dial tcp")}, true},
		{"408", &UpstreamError{Status: 408}, true},
		{"429", &UpstreamError{Status: 429}, true},
		{"500", &UpstreamError{Status: 500}, true},
		{"503", &UpstreamError{Status: 503}, true},
		{"400", &UpstreamError{Status: 400}, false},
		{"401", &UpstreamError{Status: 401}, false},
		{"404", &UpstreamError{Status: 404}, false},
		{"422", &UpstreamError{Status: 422}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Retryable(tc.err); got != tc.want {
				t.Errorf("Retryable(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestParseChatRequest(t *testing.T) {
	t.Run("keeps unmodelled fields in Raw", func(t *testing.T) {
		req, err := ParseChatRequest([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"logit_bias":{"1":2}}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := req.Raw["logit_bias"]; !ok {
			t.Errorf("Raw is missing logit_bias: %v", req.Raw)
		}
	})
	t.Run("rejects an empty message list", func(t *testing.T) {
		if _, err := ParseChatRequest([]byte(`{"model":"m","messages":[]}`)); err == nil {
			t.Error("want an error for a request with no messages")
		}
	})
	t.Run("rejects invalid JSON", func(t *testing.T) {
		if _, err := ParseChatRequest([]byte(`{`)); err == nil {
			t.Error("want an error for malformed JSON")
		}
	})
	t.Run("detects images in structured content", func(t *testing.T) {
		req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":[
			{"type":"text","text":"look"},
			{"type":"image_url","image_url":{"url":"https://x/y.png"}}]}]}`)
		if !req.HasImages() {
			t.Error("HasImages = false, want true")
		}
		if got := req.Messages[0].Text(); got != "look" {
			t.Errorf("Text() = %q, want %q", got, "look")
		}
	})
	t.Run("plain text messages have no images", func(t *testing.T) {
		req := mustParseRequest(t, `{"model":"m","messages":[{"role":"user","content":"just text"}]}`)
		if req.HasImages() {
			t.Error("HasImages = true, want false")
		}
	})
}

func TestForType(t *testing.T) {
	if a, err := ForType("openai"); err != nil {
		t.Errorf("ForType(openai): %v", err)
	} else if _, ok := a.(*OpenAI); !ok {
		t.Errorf("ForType(openai) = %T, want *OpenAI", a)
	}
	if a, err := ForType("anthropic"); err != nil {
		t.Errorf("ForType(anthropic): %v", err)
	} else if _, ok := a.(*Anthropic); !ok {
		t.Errorf("ForType(anthropic) = %T, want *Anthropic", a)
	}
	if _, err := ForType("bedrock"); err == nil {
		t.Error("want an error for an unknown provider type")
	}
}
