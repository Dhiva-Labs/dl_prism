//go:build unix

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// writeCLI drops an executable shell script into a fresh temp directory and
// returns the provider.command that runs it. Every script gets its own path,
// which also keeps the --help feature-detection cache from leaking answers
// between tests.
func writeCLI(t *testing.T, body string) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-cli")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("writing fake CLI: %v", err)
	}
	return []string{path}
}

// helpShim answers any invocation carrying --help with the given text and
// exits, so a fake CLI can advertise exactly the flags a test wants probed.
func helpShim(help string) string {
	return fmt.Sprintf(`for a in "$@"; do
  if [ "$a" = "--help" ]; then cat <<'PRISM_HELP'
%s
PRISM_HELP
  exit 0; fi
done
`, help)
}

// emit builds a script body that prints lines on stdout, text on stderr and
// exits with code.
func emit(stdout, stderr string, code int) string {
	body := ""
	if stdout != "" {
		body += fmt.Sprintf("cat <<'PRISM_OUT'\n%s\nPRISM_OUT\n", stdout)
	}
	if stderr != "" {
		body += fmt.Sprintf("cat >&2 <<'PRISM_ERR'\n%s\nPRISM_ERR\n", stderr)
	}
	return body + fmt.Sprintf("exit %d\n", code)
}

// trackedWriter mirrors the server's failover recorder: it remembers whether
// anything reached the client, which is the line past which failover is over.
type trackedWriter struct {
	*httptest.ResponseRecorder
	wrote bool
}

func newTracked() *trackedWriter {
	return &trackedWriter{ResponseRecorder: httptest.NewRecorder()}
}

func (t *trackedWriter) WriteHeader(code int) {
	t.wrote = true
	t.ResponseRecorder.WriteHeader(code)
}

func (t *trackedWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseRecorder.Write(b)
}

func (t *trackedWriter) Flush() {
	t.wrote = true
	t.ResponseRecorder.Flush()
}

func callCLI(t *testing.T, ctx context.Context, a Adapter, typ string, cmd []string, model, body string) (*trackedWriter, *Result, error) {
	t.Helper()
	p := &config.Provider{Name: "cli", Type: typ, Command: cmd}
	w := newTracked()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
	res, err := a.ChatCompletion(p, model, mustParseRequest(t, body), w, r)
	return w, res, err
}

// Canned output from the real CLIs, trimmed to the fields the adapters read.

const claudeJSONL = `{"type":"system","subtype":"init","cwd":"/tmp/x","tools":[],"model":"claude-sonnet-5"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}
{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_01","role":"assistant","content":[{"type":"text","text":"Hello, world"}]}}
{"type":"system","subtype":"post_turn_summary","status_detail":"said hello"}
{"is_error":false,"num_turns":1,"stop_reason":"end_turn","total_cost_usd":0.011,"usage":{"input_tokens":2,"cache_creation_input_tokens":1694,"cache_read_input_tokens":3289,"output_tokens":7},"subtype":"success","result":"Hello, world","type":"result"}`

const claudePartialJSONL = `{"type":"system","subtype":"init","tools":[]}
{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_01","role":"assistant","content":[]}}}
{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}}
{"type":"assistant","message":{"id":"msg_01","role":"assistant","content":[{"type":"text","text":"Hello, world"}]}}
{"type":"stream_event","event":{"type":"content_block_stop","index":0}}
{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"is_error":false,"stop_reason":"end_turn","usage":{"input_tokens":2,"cache_creation_input_tokens":1694,"cache_read_input_tokens":3289,"output_tokens":7},"subtype":"success","result":"Hello, world","type":"result"}`

const codexJSONL = `{"type":"thread.started","thread_id":"019f"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Model metadata not found. Defaulting to fallback metadata."}}
{"type":"item.completed","item":{"id":"item_1","type":"reasoning","text":"thinking about it"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Hello, world"}}
{"type":"turn.completed","usage":{"input_tokens":13539,"cached_input_tokens":11008,"output_tokens":5}}`

// claudeFullHelp advertises every optional flag the adapter knows about.
const claudeFullHelp = `Usage: claude [options] [command] [prompt]
  --include-partial-messages  Include partial message chunks as they arrive
  --max-turns <n>             Maximum agentic turns
  --safe-mode                 Start with all customizations disabled
  --tools <tools...>          Specify the list of available tools
  --allowed-tools <tools...>  Comma-separated list of tool names to allow`

const codexFullHelp = `Usage: codex exec [OPTIONS] [PROMPT]
  -s, --sandbox <SANDBOX_MODE>  Select the sandbox policy
      --json                    Print events to stdout as JSONL`

func decodeCompletion(t *testing.T, body string) (content, finish string, prompt, completion int) {
	t.Helper()
	var got struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
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
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("response is not valid OpenAI JSON: %v (%s)", err, body)
	}
	if got.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", got.Object)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(got.Choices))
	}
	if got.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", got.Choices[0].Message.Role)
	}
	if want := got.Usage.PromptTokens + got.Usage.CompletionTokens; got.Usage.TotalTokens != want {
		t.Errorf("total_tokens = %d, want %d", got.Usage.TotalTokens, want)
	}
	return got.Choices[0].Message.Content, got.Choices[0].FinishReason,
		got.Usage.PromptTokens, got.Usage.CompletionTokens
}

func TestClaudeCodeNonStream(t *testing.T) {
	cmd := writeCLI(t, helpShim(claudeFullHelp)+emit(claudeJSONL, "", 0))
	w, res, err := callCLI(t, context.Background(), &ClaudeCode{}, config.TypeClaudeCode, cmd,
		"sonnet", `{"model":"sonnet","messages":[{"role":"user","content":"hi"}]}`)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	content, finish, prompt, completion := decodeCompletion(t, w.Body.String())
	if content != "Hello, world" {
		t.Errorf("content = %q, want %q", content, "Hello, world")
	}
	if finish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finish)
	}
	// 2 fresh + 1694 cache-creation + 3289 cache-read.
	if prompt != 4985 || completion != 7 {
		t.Errorf("usage = %d/%d, want 4985/7", prompt, completion)
	}
	if !res.UsageKnown || res.PromptTokens != 4985 || res.CompletionTokens != 7 {
		t.Errorf("Result = %+v, want 4985/7 known", res)
	}
}

func TestClaudeCodeStreamFallbackWhenPartialsUnsupported(t *testing.T) {
	// Help mentions no --include-partial-messages, so the whole answer is
	// buffered and emitted as one chunk.
	cmd := writeCLI(t, helpShim("Usage: claude\n  --print  headless")+emit(claudeJSONL, "", 0))
	w, res, err := callCLI(t, context.Background(), &ClaudeCode{}, config.TypeClaudeCode, cmd,
		"sonnet", `{"model":"sonnet","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	events := sseEvents(t, w.Body.String())
	if len(events) != 4 {
		t.Fatalf("got %d SSE events, want role+content+finish+[DONE]:\n%s", len(events), w.Body.String())
	}
	if events[3] != "[DONE]" {
		t.Errorf("final event = %q, want [DONE]", events[3])
	}
	var chunks []openAIChunk
	for _, e := range events[:3] {
		var c openAIChunk
		if err := json.Unmarshal([]byte(e), &c); err != nil {
			t.Fatalf("chunk %q is not valid JSON: %v", e, err)
		}
		if c.Object != "chat.completion.chunk" || c.Model != "sonnet" {
			t.Errorf("chunk = %+v, want a sonnet chat.completion.chunk", c)
		}
		chunks = append(chunks, c)
	}
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk role = %q, want assistant", chunks[0].Choices[0].Delta.Role)
	}
	if got := chunks[1].Choices[0].Delta.Content; got != "Hello, world" {
		t.Errorf("content chunk = %q, want the whole answer in one piece", got)
	}
	if f := chunks[2].Choices[0].FinishReason; f == nil || *f != "stop" {
		t.Errorf("finish_reason = %v, want stop", f)
	}
	if !res.UsageKnown || res.CompletionTokens != 7 {
		t.Errorf("Result = %+v, want usage from the result event", res)
	}
}

func TestClaudeCodeStreamPartials(t *testing.T) {
	cmd := writeCLI(t, helpShim(claudeFullHelp)+emit(claudePartialJSONL, "", 0))
	w, res, err := callCLI(t, context.Background(), &ClaudeCode{}, config.TypeClaudeCode, cmd,
		"sonnet", `{"model":"sonnet","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	events := sseEvents(t, w.Body.String())
	// role + "Hello" + ", world" + finish + [DONE]. The assistant summary
	// event must not be counted a second time.
	if len(events) != 5 {
		t.Fatalf("got %d SSE events, want 5 (deltas, not the buffered summary):\n%s",
			len(events), w.Body.String())
	}
	var text strings.Builder
	for _, e := range events[:len(events)-1] {
		var c openAIChunk
		if err := json.Unmarshal([]byte(e), &c); err != nil {
			t.Fatalf("chunk %q: %v", e, err)
		}
		text.WriteString(c.Choices[0].Delta.Content)
	}
	if text.String() != "Hello, world" {
		t.Errorf("streamed text = %q, want %q (no duplication)", text.String(), "Hello, world")
	}
	if !res.UsageKnown || res.PromptTokens != 4985 {
		t.Errorf("Result = %+v, want 4985 prompt tokens", res)
	}
}

func TestClaudeCodeArgv(t *testing.T) {
	tests := []struct {
		name   string
		help   string
		body   string
		want   []string
		prompt string
	}{
		{
			name: "every optional flag advertised, non-stream",
			help: claudeFullHelp,
			body: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			want: []string{"--tools", "", "--print", "--output-format", "stream-json",
				"--verbose", "--model", "sonnet", "--safe-mode", "--max-turns", "1", "--", "hi"},
		},
		{
			name: "streaming adds --include-partial-messages",
			help: claudeFullHelp,
			body: `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			want: []string{"--tools", "", "--print", "--output-format", "stream-json",
				"--verbose", "--model", "sonnet", "--safe-mode", "--max-turns", "1",
				"--include-partial-messages", "--", "hi"},
		},
		{
			name: "a CLI advertising nothing gets the baseline command line",
			help: "Usage: claude",
			body: `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			want: []string{"--print", "--output-format", "stream-json",
				"--verbose", "--model", "sonnet", "--", "hi"},
		},
		{
			name: "--allowed-tools must not be mistaken for --tools",
			help: "Usage: claude\n  --allowed-tools <tools...>  allow list\n  --disallowed-tools <t...>",
			body: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			want: []string{"--print", "--output-format", "stream-json",
				"--verbose", "--model", "sonnet", "--", "hi"},
		},
		{
			name: "the flattened prompt is one argument after --",
			help: "Usage: claude",
			body: `{"model":"m","messages":[
			         {"role":"system","content":"be terse"},
			         {"role":"user","content":"1"},
			         {"role":"assistant","content":"2"},
			         {"role":"user","content":"3"}]}`,
			want: []string{"--print", "--output-format", "stream-json", "--verbose",
				"--model", "sonnet", "--", "System: be terse\n\nUser: 1\n\nAssistant: 2\n\nUser: 3"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argvFile := filepath.Join(t.TempDir(), "argv")
			cmd := writeCLI(t, helpShim(tc.help)+
				fmt.Sprintf("printf '%%s\\n' \"$@\" > %q\n", argvFile)+
				emit(claudeJSONL, "", 0))
			if _, _, err := callCLI(t, context.Background(), &ClaudeCode{}, config.TypeClaudeCode,
				cmd, "sonnet", tc.body); err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			got := readArgv(t, argvFile, len(tc.want))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("argv =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// readArgv reads a newline-separated argv dump. The last argument is the
// prompt, which may itself contain newlines, so everything past want-1 is
// rejoined into one element.
func readArgv(t *testing.T, path string, want int) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading argv dump: %v", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if want > 0 && len(fields) > want {
		fields = append(fields[:want-1], strings.Join(fields[want-1:], "\n"))
	}
	return fields
}

func TestCodexCLINonStream(t *testing.T) {
	cmd := writeCLI(t, helpShim(codexFullHelp)+emit(codexJSONL, "", 0))
	w, res, err := callCLI(t, context.Background(), &CodexCLI{}, config.TypeCodexCLI, cmd,
		"gpt-5", `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	content, finish, prompt, completion := decodeCompletion(t, w.Body.String())
	if content != "Hello, world" {
		t.Errorf("content = %q, want %q (reasoning and advisory items skipped)", content, "Hello, world")
	}
	if finish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finish)
	}
	if prompt != 13539 || completion != 5 {
		t.Errorf("usage = %d/%d, want 13539/5 (cached tokens already included)", prompt, completion)
	}
	if !res.UsageKnown {
		t.Error("Result.UsageKnown = false, want true")
	}
}

func TestCodexCLIStreamIsBuffered(t *testing.T) {
	cmd := writeCLI(t, helpShim(codexFullHelp)+emit(codexJSONL, "", 0))
	w, _, err := callCLI(t, context.Background(), &CodexCLI{}, config.TypeCodexCLI, cmd,
		"gpt-5", `{"model":"gpt-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	events := sseEvents(t, w.Body.String())
	if len(events) != 4 || events[3] != "[DONE]" {
		t.Fatalf("got %d events, want role+content+finish+[DONE]:\n%s", len(events), w.Body.String())
	}
	var c openAIChunk
	if err := json.Unmarshal([]byte(events[1]), &c); err != nil {
		t.Fatalf("chunk: %v", err)
	}
	if c.Choices[0].Delta.Content != "Hello, world" {
		t.Errorf("content = %q, want the whole answer", c.Choices[0].Delta.Content)
	}
}

func TestCodexCLIUsageUnknownWithoutTurnCompleted(t *testing.T) {
	jsonl := `{"type":"thread.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}`
	cmd := writeCLI(t, helpShim(codexFullHelp)+emit(jsonl, "", 0))
	w, res, err := callCLI(t, context.Background(), &CodexCLI{}, config.TypeCodexCLI, cmd,
		"gpt-5", `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if res.UsageKnown {
		t.Error("Result.UsageKnown = true, want false so the ledger estimates")
	}
	if content, _, _, _ := decodeCompletion(t, w.Body.String()); content != "ok" {
		t.Errorf("content = %q, want ok", content)
	}
}

func TestCodexCLIArgv(t *testing.T) {
	tests := []struct {
		name string
		help string
		want []string
	}{
		{
			name: "sandbox advertised",
			help: codexFullHelp,
			want: []string{"exec", "--json", "--skip-git-repo-check", "--model", "gpt-5",
				"--sandbox", "read-only", "--", "hi"},
		},
		{
			name: "sandbox not advertised",
			help: "Usage: codex exec",
			want: []string{"exec", "--json", "--skip-git-repo-check", "--model", "gpt-5", "--", "hi"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argvFile := filepath.Join(t.TempDir(), "argv")
			cmd := writeCLI(t, helpShim(tc.help)+
				fmt.Sprintf("printf '%%s\\n' \"$@\" > %q\n", argvFile)+
				emit(codexJSONL, "", 0))
			if _, _, err := callCLI(t, context.Background(), &CodexCLI{}, config.TypeCodexCLI,
				cmd, "gpt-5", `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`); err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			if got := readArgv(t, argvFile, len(tc.want)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("argv =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// Failure modes. Every one of these must leave w untouched and come back
// retryable, so the server's failover loop can try the next candidate.
func TestCLIFailuresAreRetryableBeforeAnyOutput(t *testing.T) {
	tests := []struct {
		name    string
		adapter Adapter
		typ     string
		script  string
		wantSub string
	}{
		{
			name:    "claude exits non-zero",
			adapter: &ClaudeCode{},
			typ:     config.TypeClaudeCode,
			script:  emit("", "Invalid API key · Please run /login", 1),
			wantSub: "Please run /login",
		},
		{
			name:    "claude reports an error result event",
			adapter: &ClaudeCode{},
			typ:     config.TypeClaudeCode,
			script: emit(`{"type":"result","subtype":"error_during_execution","is_error":true,`+
				`"result":"Claude usage limit reached","usage":{"input_tokens":1,"output_tokens":0}}`, "", 0),
			wantSub: "Claude usage limit reached",
		},
		{
			name:    "claude prints garbage and exits non-zero",
			adapter: &ClaudeCode{},
			typ:     config.TypeClaudeCode,
			script:  emit("not json at all\n<html>404</html>", "command not found: node", 127),
			wantSub: "command not found: node",
		},
		{
			name:    "claude prints only garbage and exits clean",
			adapter: &ClaudeCode{},
			typ:     config.TypeClaudeCode,
			script:  emit("Welcome to something else entirely", "", 0),
			wantSub: "no recognizable events",
		},
		{
			name:    "codex turn fails",
			adapter: &CodexCLI{},
			typ:     config.TypeCodexCLI,
			script: emit(`{"type":"thread.started"}`+"\n"+
				`{"type":"turn.failed","error":{"message":"not supported with a ChatGPT account"}}`, "", 1),
			wantSub: "not supported with a ChatGPT account",
		},
		{
			name:    "codex emits a fatal error event",
			adapter: &CodexCLI{},
			typ:     config.TypeCodexCLI,
			script:  emit(`{"type":"error","message":"stream disconnected"}`, "", 0),
			wantSub: "stream disconnected",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := writeCLI(t, helpShim(claudeFullHelp)+tc.script)
			w, res, err := callCLI(t, context.Background(), tc.adapter, tc.typ, cmd,
				"m", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
			if err == nil {
				t.Fatalf("want an error, got body %q", w.Body.String())
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantSub)
			}
			if !Retryable(err) {
				t.Errorf("Retryable(%v) = false, want true so the chain fails over", err)
			}
			if w.wrote {
				t.Errorf("adapter wrote to the client before succeeding: %q", w.Body.String())
			}
			if res == nil {
				t.Error("want a non-nil Result even on failure")
			}
		})
	}
}

func TestCLIMissingBinaryIsRetryable(t *testing.T) {
	cmd := []string{filepath.Join(t.TempDir(), "definitely-not-installed")}
	w, _, err := callCLI(t, context.Background(), &ClaudeCode{}, config.TypeClaudeCode, cmd,
		"sonnet", `{"model":"sonnet","messages":[{"role":"user","content":"hi"}]}`)
	if err == nil {
		t.Fatal("want an error when the CLI is not installed")
	}
	if !Retryable(err) {
		t.Errorf("Retryable(%v) = false, want true", err)
	}
	if w.wrote {
		t.Error("adapter wrote to the client")
	}
}

// A client that hangs up must take the child process with it.
func TestCLIContextCancellationKillsTheChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	// exec keeps the PID, so killing the child kills the sleep itself.
	cmd := writeCLI(t, helpShim(claudeFullHelp)+
		fmt.Sprintf("printf '%%s' \"$$\" > %q\nexec sleep 60\n", pidFile))

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		_, _, err := callCLI(t, ctx, &ClaudeCode{}, config.TypeClaudeCode, cmd,
			"sonnet", `{"model":"sonnet","messages":[{"role":"user","content":"hi"}]}`)
		done <- outcome{err}
	}()

	pid := waitForPID(t, pidFile)
	cancel()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("want an error when the client's context is cancelled")
		}
		if !strings.Contains(got.err.Error(), "context canceled") {
			t.Errorf("error = %q, want it to name the cancellation", got.err)
		}
		if Retryable(got.err) {
			t.Error("Retryable = true; re-running the CLI for a departed client is pointless")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("ChatCompletion did not return after the context was cancelled")
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return // the child is gone
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived the cancelled request", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			var pid int
			if _, err := fmt.Sscanf(string(b), "%d", &pid); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fake CLI never reported its pid")
	return 0
}

// Requests the command line cannot carry are refused before anything is
// spawned, as a plain error the server turns into a 400.
func TestCLIRejectsUnsupportedRequests(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantSub string
	}{
		{
			name:    "tool definitions",
			body:    `{"model":"m","tools":[{"type":"function","function":{"name":"f"}}],"messages":[{"role":"user","content":"hi"}]}`,
			wantSub: "cannot forward tool definitions",
		},
		{
			name: "images",
			body: `{"model":"m","messages":[{"role":"user","content":[
			         {"type":"text","text":"what is this?"},
			         {"type":"image_url","image_url":{"url":"data:image/png;base64,iVBOR"}}]}]}`,
			wantSub: "cannot forward images",
		},
		{
			name:    "tool result messages",
			body:    `{"model":"m","messages":[{"role":"tool","content":"result"}]}`,
			wantSub: `cannot translate role "tool"`,
		},
		{
			name:    "content parts that are neither text nor image",
			body:    `{"model":"m","messages":[{"role":"user","content":[{"type":"input_audio"}]}]}`,
			wantSub: `cannot translate content part type "input_audio"`,
		},
	}
	for _, adapter := range []struct {
		name string
		a    Adapter
		typ  string
	}{
		{"claude-code", &ClaudeCode{}, config.TypeClaudeCode},
		{"codex-cli", &CodexCLI{}, config.TypeCodexCLI},
	} {
		for _, tc := range tests {
			t.Run(adapter.name+"/"+tc.name, func(t *testing.T) {
				// A command that would fail loudly if it were ever run.
				cmd := []string{filepath.Join(t.TempDir(), "never-run")}
				w, _, err := callCLI(t, context.Background(), adapter.a, adapter.typ, cmd,
					"m", tc.body)
				if err == nil {
					t.Fatal("want a rejection")
				}
				if !strings.Contains(err.Error(), tc.wantSub) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantSub)
				}
				if Retryable(err) {
					t.Error("Retryable = true; a malformed request must not fail over")
				}
				if w.wrote {
					t.Error("adapter wrote to the client")
				}
			})
		}
	}
}

// The child must not inherit anything that could point it back at the
// gateway, and must not be able to read the gateway's working directory.
func TestCLIChildIsIsolated(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8130")
	t.Setenv("ANTHROPIC_API_KEY", "dummy")
	t.Setenv("PRISM_TEST_KEEP_ME", "kept")

	dir := t.TempDir()
	report := filepath.Join(dir, "report")
	cmd := writeCLI(t, helpShim(claudeFullHelp)+fmt.Sprintf(`{
  printf 'cwd=%%s\n' "$(pwd)"
  printf 'entries=%%s\n' "$(ls -A . | wc -l)"
  printf 'base_url=[%%s]\n' "${ANTHROPIC_BASE_URL-unset}"
  printf 'api_key=[%%s]\n' "${ANTHROPIC_API_KEY-unset}"
  printf 'keep=[%%s]\n' "${PRISM_TEST_KEEP_ME-unset}"
  printf 'stdin=[%%s]\n' "$(cat)"
} > %q
`, report)+emit(claudeJSONL, "", 0))

	if _, _, err := callCLI(t, context.Background(), &ClaudeCode{}, config.TypeClaudeCode, cmd,
		"sonnet", `{"model":"sonnet","messages":[{"role":"user","content":"hi"}]}`); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	b, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("reading child report: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		"base_url=[unset]", // the loop back into the gateway is cut
		"api_key=[unset]",  // subscription auth, not the gateway's dummy key
		"keep=[kept]",      // unrelated environment still reaches the CLI
		"entries=0",        // an empty working directory
		"stdin=[]",         // no shared stdin
	} {
		if !strings.Contains(got, want) {
			t.Errorf("child report missing %q:\n%s", want, got)
		}
	}
	wd, _ := os.Getwd()
	if strings.Contains(got, "cwd="+wd+"\n") {
		t.Errorf("child ran in the gateway's own directory:\n%s", got)
	}
}

func TestFlattenPrompt(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a lone user message is passed through verbatim",
			body: `{"model":"m","messages":[{"role":"user","content":"summarise this"}]}`,
			want: "summarise this",
		},
		{
			name: "a system prompt tags every turn",
			body: `{"model":"m","messages":[
			         {"role":"system","content":"be terse"},
			         {"role":"user","content":"hi"}]}`,
			want: "System: be terse\n\nUser: hi",
		},
		{
			name: "system and developer messages join with a blank line",
			body: `{"model":"m","messages":[
			         {"role":"system","content":"be terse"},
			         {"role":"developer","content":"prefer go"},
			         {"role":"user","content":"hi"}]}`,
			want: "System: be terse\n\nprefer go\n\nUser: hi",
		},
		{
			name: "multi-turn history keeps its order",
			body: `{"model":"m","messages":[
			         {"role":"user","content":"1"},
			         {"role":"assistant","content":"2"},
			         {"role":"user","content":"3"}]}`,
			want: "User: 1\n\nAssistant: 2\n\nUser: 3",
		},
		{
			name: "structured text parts are flattened",
			body: `{"model":"m","messages":[{"role":"user","content":[
			         {"type":"text","text":"a"},{"type":"text","text":"b"}]}]}`,
			want: "a\nb",
		},
		{
			name: "null tools does not trip the tool rejection",
			body: `{"model":"m","tools":null,"messages":[{"role":"user","content":"hi"}]}`,
			want: "hi",
		},
		{
			name: "an empty tools array does not trip the tool rejection",
			body: `{"model":"m","tools":[],"messages":[{"role":"user","content":"hi"}]}`,
			want: "hi",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := flattenPrompt(mustParseRequest(t, tc.body), "claude-code")
			if err != nil {
				t.Fatalf("flattenPrompt: %v", err)
			}
			if got != tc.want {
				t.Errorf("prompt = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseClaudeEvent(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		partial bool
		want    cliEvent
		wantOK  bool
	}{
		{
			name:   "the session banner carries nothing",
			line:   `{"type":"system","subtype":"init","tools":[]}`,
			wantOK: false,
		},
		{
			name:   "an assistant message yields its text",
			line:   `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			want:   cliEvent{Text: "hi"},
			wantOK: true,
		},
		{
			name:   "assistant text blocks concatenate, others are skipped",
			line:   `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`,
			want:   cliEvent{Text: "ab"},
			wantOK: true,
		},
		{
			name:    "assistant messages are ignored while partials are on",
			line:    `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			partial: true,
			wantOK:  false,
		},
		{
			name:   "stream events are ignored while partials are off",
			line:   `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}`,
			wantOK: false,
		},
		{
			name:    "a text delta yields its increment",
			line:    `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}`,
			partial: true,
			want:    cliEvent{Text: "hi"},
			wantOK:  true,
		},
		{
			name:    "a non-text delta carries nothing",
			line:    `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{}"}}}`,
			partial: true,
			wantOK:  false,
		},
		{
			name:    "message_delta carries the finish reason",
			line:    `{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}}`,
			partial: true,
			want:    cliEvent{Finish: "length"},
			wantOK:  true,
		},
		{
			name: "a successful result carries usage and the finish reason",
			line: `{"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","result":"hi","usage":{"input_tokens":2,"cache_creation_input_tokens":10,"cache_read_input_tokens":100,"output_tokens":4}}`,
			want: cliEvent{
				Finish: "stop",
				Usage:  &Result{PromptTokens: 112, CompletionTokens: 4, UsageKnown: true},
			},
			wantOK: true,
		},
		{
			name: "is_error surfaces the result text as the failure",
			line: `{"type":"result","subtype":"success","is_error":true,"result":"usage limit reached","usage":{}}`,
			want: cliEvent{
				Err:   "usage limit reached",
				Usage: &Result{UsageKnown: true},
			},
			wantOK: true,
		},
		{
			name: "a non-success subtype is a failure even without is_error",
			line: `{"type":"result","subtype":"error_max_turns","usage":{}}`,
			want: cliEvent{
				Err:   "error_max_turns",
				Usage: &Result{UsageKnown: true},
			},
			wantOK: true,
		},
		{
			name:   "a structured error object is rendered, not dropped",
			line:   `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			want:   cliEvent{Err: `{"type":"overloaded_error","message":"Overloaded"}`},
			wantOK: true,
		},
		{
			name:   "unparseable lines are skipped",
			line:   `{not json`,
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseClaudeEvent([]byte(tc.line), tc.partial)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (event %+v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("event = %+v (usage %+v), want %+v (usage %+v)",
					got, got.Usage, tc.want, tc.want.Usage)
			}
		})
	}
}

func TestParseCodexEvent(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   cliEvent
		wantOK bool
	}{
		{
			name:   "thread.started carries nothing",
			line:   `{"type":"thread.started","thread_id":"019f"}`,
			wantOK: false,
		},
		{
			name:   "an agent message yields its text",
			line:   `{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}`,
			want:   cliEvent{Text: "hi"},
			wantOK: true,
		},
		{
			name:   "reasoning items are skipped",
			line:   `{"type":"item.completed","item":{"type":"reasoning","text":"hmm"}}`,
			wantOK: false,
		},
		{
			name:   "an advisory error item is not fatal",
			line:   `{"type":"item.completed","item":{"type":"error","message":"model metadata not found"}}`,
			wantOK: false,
		},
		{
			name: "turn.completed carries usage inclusive of cached tokens",
			line: `{"type":"turn.completed","usage":{"input_tokens":13539,"cached_input_tokens":11008,"output_tokens":5}}`,
			want: cliEvent{
				Usage: &Result{PromptTokens: 13539, CompletionTokens: 5, UsageKnown: true},
			},
			wantOK: true,
		},
		{
			name:   "turn.failed is fatal",
			line:   `{"type":"turn.failed","error":{"message":"boom"}}`,
			want:   cliEvent{Err: "boom"},
			wantOK: true,
		},
		{
			name:   "a top-level error is fatal",
			line:   `{"type":"error","message":"stream disconnected"}`,
			want:   cliEvent{Err: "stream disconnected"},
			wantOK: true,
		},
		{
			name:   "unparseable lines are skipped",
			line:   `not json`,
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseCodexEvent([]byte(tc.line))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (event %+v)", ok, tc.wantOK, got)
			}
			if ok && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("event = %+v (usage %+v), want %+v (usage %+v)",
					got, got.Usage, tc.want, tc.want.Usage)
			}
		})
	}
}

func TestMentionsFlag(t *testing.T) {
	tests := []struct {
		help, flag string
		want       bool
	}{
		{"  --tools <tools...>  the tool list", "--tools", true},
		{"  --allowed-tools <t...>", "--tools", false},
		{"  --disallowed-tools <t...>", "--tools", false},
		{"  --toolset x", "--tools", false},
		{"  --model <m>", "--model", true},
		{"  --fallback-model <m>", "--model", false},
		{"  --max-turns <n>", "--max-turns", true},
		{"  --max-turns-total <n>", "--max-turns", false},
		{"use --safe-mode.", "--safe-mode", true},
		{"", "--safe-mode", false},
		{"  -s, --sandbox <MODE>", "--sandbox", true},
	}
	for _, tc := range tests {
		if got := mentionsFlag(tc.help, tc.flag); got != tc.want {
			t.Errorf("mentionsFlag(%q, %q) = %v, want %v", tc.help, tc.flag, got, tc.want)
		}
	}
}

func TestScrubEnv(t *testing.T) {
	env := []string{"PATH=/bin", "ANTHROPIC_API_KEY=secret", "HOME=/home/x", "ANTHROPIC_BASE_URL=http://p"}
	got := scrubEnv(env, []string{"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "NOT_PRESENT"})
	want := []string{"PATH=/bin", "HOME=/home/x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scrubEnv = %q, want %q", got, want)
	}
}

func TestCLICommandOverride(t *testing.T) {
	def := []string{"claude"}
	if got := cliCommand(&config.Provider{}, def); !reflect.DeepEqual(got, def) {
		t.Errorf("cliCommand with no override = %q, want %q", got, def)
	}
	over := []string{"npx", "@anthropic-ai/claude-code"}
	if got := cliCommand(&config.Provider{Command: over}, def); !reflect.DeepEqual(got, over) {
		t.Errorf("cliCommand with override = %q, want %q", got, over)
	}
}

func TestForTypeCLIAdapters(t *testing.T) {
	if a, err := ForType(config.TypeClaudeCode); err != nil {
		t.Errorf("ForType(claude-code): %v", err)
	} else if _, ok := a.(*ClaudeCode); !ok {
		t.Errorf("ForType(claude-code) = %T, want *ClaudeCode", a)
	}
	if a, err := ForType(config.TypeCodexCLI); err != nil {
		t.Errorf("ForType(codex-cli): %v", err)
	} else if _, ok := a.(*CodexCLI); !ok {
		t.Errorf("ForType(codex-cli) = %T, want *CodexCLI", a)
	}
}
