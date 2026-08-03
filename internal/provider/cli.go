package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// Subscription-CLI providers.
//
// Paid Claude (Pro/Max) and Codex (ChatGPT) plans come with a locally
// installed, already-logged-in CLI that has an official headless mode. These
// adapters shell out to that CLI once per request and translate its JSONL
// event stream into OpenAI chat completions, so a flat-rate subscription can
// sit in the routing table next to per-token API providers — typically as the
// overflow valve a `budget_exceeded` rule points at.
//
// This is a chat bridge, not an API replacement. The CLI answers as its
// agentic self (its own system prompt is in the context, and its wording may
// carry that framing), and the bridge deliberately refuses anything the
// command line cannot carry faithfully: images and tool definitions are
// rejected with a 400, the same way the outbound anthropic adapter rejects
// tools today.
const (
	// cliScanMax bounds one JSONL line. A single line can hold a whole
	// assistant message, so the default 64 KiB scanner limit is far too small.
	cliScanMax = 16 * 1024 * 1024
	// cliWaitDelay is how long Wait keeps trying to drain the child's pipes
	// after the request context is cancelled before forcing them shut. Without
	// it a grandchild holding stdout would keep the gateway goroutine alive
	// after the client has hung up.
	cliWaitDelay = 5 * time.Second
	// cliHelpTimeout bounds the one-off `--help` probe used for feature
	// detection. It is not the request timeout: CLI turns have no deadline
	// beyond the client's context.
	cliHelpTimeout = 15 * time.Second
)

// cliCommand resolves the executable (and any leading arguments) for a CLI
// provider: the config override when present, the vendor default otherwise.
func cliCommand(p *config.Provider, def []string) []string {
	if len(p.Command) > 0 {
		return p.Command
	}
	return def
}

// cliEvent is one line of a CLI's JSONL output, normalized across vendors.
type cliEvent struct {
	// Text is assistant output: an increment when the CLI was asked to
	// stream partial messages, a whole message otherwise.
	Text string
	// Usage is set by the CLI's final accounting event.
	Usage *Result
	// Finish is the OpenAI finish_reason the CLI reported.
	Finish string
	// Err, when non-empty, is a failure the CLI reported in-band. The child
	// usually also exits non-zero, but not always, so this is authoritative.
	Err string
}

// cliChat drives one CLI invocation to an OpenAI-shaped answer.
type cliChat struct {
	// argv is the full command line, prompt included.
	argv []string
	// scrub names environment variables removed from the child's environment.
	scrub []string
	// parse turns one JSONL line into a normalized event. The second result
	// is false for lines that carry nothing (banners, progress, noise).
	parse func(line []byte) (cliEvent, bool)
	// deltas is true when the CLI was asked for incremental text, so events
	// can be forwarded to the client as they arrive.
	deltas bool
	// stream is the client's stream flag; model names the answer's model.
	stream bool
	model  string
	// label is the provider type, used in error messages.
	label string
}

// run spawns the CLI, translates its output onto w and reports token usage.
//
// It honours the Adapter contract — nothing reaches w until the request has
// succeeded — with one deliberate relaxation for live streaming: the commit
// point is the first assistant *text*, not the first parsed event. The
// session banner and rate-limit events a CLI emits before it has said
// anything carry no answer, so failing over after them is still safe and
// still invisible to the client. Any failure before that first text (spawn
// error, non-zero exit, in-band error event) is a retryable UpstreamError, so
// the server's failover loop can try the next candidate. When the CLI cannot
// stream partial messages the whole answer is buffered, which means even
// streaming requests keep full failover.
func (c *cliChat) run(r *http.Request, w http.ResponseWriter) (*Result, error) {
	ctx := r.Context()

	// The child's working directory is an empty temp dir it owns: a CLI
	// launched in the gateway's cwd would happily read that project.
	dir, err := os.MkdirTemp("", "prism-cli-")
	if err != nil {
		return nil, &UpstreamError{Err: fmt.Errorf("%s: creating work dir: %w", c.label, err)}
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = nil // never share the gateway's stdin with a child process
	cmd.Env = scrubEnv(os.Environ(), c.scrub)
	cmd.WaitDelay = cliWaitDelay
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &UpstreamError{Err: fmt.Errorf("%s: %w", c.label, err)}
	}
	if err := cmd.Start(); err != nil {
		return nil, &UpstreamError{Err: fmt.Errorf("%s: starting %q: %w", c.label, c.argv[0], err)}
	}

	out := &sseEmitter{w: w, model: c.model}
	var (
		text     strings.Builder
		res      = &Result{}
		finish   = "stop"
		cliErr   string
		writeErr error
		events   int
	)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), cliScanMax)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue // CLIs print the odd human-readable banner alongside JSONL
		}
		ev, ok := c.parse(line)
		if !ok {
			continue
		}
		events++
		if ev.Err != "" && cliErr == "" {
			cliErr = ev.Err
		}
		if ev.Usage != nil {
			res = ev.Usage
		}
		if ev.Finish != "" {
			finish = ev.Finish
		}
		if ev.Text == "" {
			continue
		}
		text.WriteString(ev.Text)
		if c.deltas && c.stream {
			if err := out.delta(ev.Text); err != nil {
				writeErr = err
				break
			}
		}
	}
	scanErr := sc.Err()

	// Stop reading before the child is done (client hung up, oversized line)
	// and it would block forever on a full pipe, so kill it first.
	if writeErr != nil || scanErr != nil {
		_ = cmd.Process.Kill()
	}
	// Wait also waits for the stderr copier, so stderr is only safe to read
	// from here on.
	waitErr := cmd.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		// The client is gone (or timed out). Failing over would just run the
		// same CLI again for nobody, so this is deliberately not an
		// UpstreamError and therefore not retryable.
		return res, fmt.Errorf("%s: %w", c.label, ctxErr)
	}
	if writeErr != nil {
		return res, writeErr
	}
	if scanErr != nil {
		return res, c.fail(out, fmt.Sprintf("reading output: %v", scanErr), &stderr)
	}
	if cliErr != "" {
		return res, c.fail(out, cliErr, &stderr)
	}
	if waitErr != nil {
		return res, c.fail(out, waitErr.Error(), &stderr)
	}
	if events == 0 {
		// A clean exit having said nothing this adapter understands means the
		// command is not the CLI we think it is (wrong binary, a wrapper that
		// swallowed the flags, a release too old for --output-format).
		// Handing the client a silent empty answer would also credit the
		// circuit breaker with a success, so this counts as a failure.
		return res, c.fail(out, "the CLI exited cleanly with no recognizable events", &stderr)
	}

	if c.stream {
		return res, out.complete(text.String(), finish)
	}
	body, err := openAIChatCompletion(c.model, text.String(), finish, res)
	if err != nil {
		return res, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(body)
	return res, err
}

// fail renders a CLI failure. Before the commit point it is a retryable
// UpstreamError so the server can fail over; after it, a plain error, which
// stops the request being recorded as a success without pretending failover
// is still possible.
func (c *cliChat) fail(out *sseEmitter, msg string, stderr *bytes.Buffer) error {
	if s := strings.TrimSpace(stderr.String()); s != "" {
		msg = fmt.Sprintf("%s: %s", msg, truncate(s, 2048))
	}
	msg = fmt.Sprintf("%s: %s", c.label, msg)
	if out.started {
		return fmt.Errorf("%s (stream already started)", msg)
	}
	return &UpstreamError{Body: msg}
}

// truncate caps a stderr excerpt at n bytes, backing off to a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// scrubEnv drops the named variables from an environ slice.
func scrubEnv(env, names []string) []string {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && drop[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// sseEmitter writes an OpenAI chat.completion.chunk stream, holding back the
// response head until there is something to say.
type sseEmitter struct {
	w       http.ResponseWriter
	model   string
	started bool
	created int64
	flusher http.Flusher
}

func (e *sseEmitter) begin() {
	e.started = true
	e.created = time.Now().Unix()
	e.flusher, _ = e.w.(http.Flusher)
	e.w.Header().Set("Content-Type", "text/event-stream")
	e.w.Header().Set("Cache-Control", "no-cache")
	e.w.WriteHeader(http.StatusOK)
	_ = e.emit(map[string]any{"role": "assistant", "content": ""}, nil)
}

func (e *sseEmitter) emit(delta map[string]any, finish any) error {
	b, err := json.Marshal(map[string]any{
		"id":      "chatcmpl-prism",
		"object":  "chat.completion.chunk",
		"created": e.created,
		"model":   e.model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", b); err != nil {
		return err
	}
	if e.flusher != nil {
		e.flusher.Flush()
	}
	return nil
}

// delta forwards one increment of assistant text, opening the stream on the
// first call. This is the commit point: once it returns, bytes are on the
// wire and failover is over.
func (e *sseEmitter) delta(text string) error {
	if !e.started {
		e.begin()
	}
	if text == "" {
		return nil
	}
	return e.emit(map[string]any{"content": text}, nil)
}

// complete closes the stream, first emitting full as a single chunk if
// nothing has been sent yet (the no-partial-messages fallback).
func (e *sseEmitter) complete(full, finish string) error {
	if !e.started {
		if err := e.delta(full); err != nil {
			return err
		}
	}
	if err := e.emit(map[string]any{}, finish); err != nil {
		return err
	}
	_, err := fmt.Fprint(e.w, "data: [DONE]\n\n")
	if e.flusher != nil {
		e.flusher.Flush()
	}
	return err
}

// openAIChatCompletion renders a non-streaming chat.completion body.
func openAIChatCompletion(model, text, finish string, res *Result) ([]byte, error) {
	return json.Marshal(map[string]any{
		"id":      "chatcmpl-prism",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": text},
			"finish_reason": finish,
		}},
		"usage": map[string]any{
			"prompt_tokens":     res.PromptTokens,
			"completion_tokens": res.CompletionTokens,
			"total_tokens":      res.PromptTokens + res.CompletionTokens,
		},
	})
}

// flattenPrompt renders an OpenAI message list as the single prompt string a
// headless CLI takes on its command line:
//
//	System: <every system and developer message, joined by blank lines>
//
//	User: <text>
//
//	Assistant: <text>
//
//	User: <text>
//
// The one case that is *not* tagged is a lone user message with no system
// prompt: that is passed through verbatim, so an ordinary single-turn
// completion reaches the CLI exactly as the client wrote it.
//
// Content the command line cannot carry is refused rather than silently
// dropped: tool definitions, images, any other non-text content part, and any
// role beyond system/developer/user/assistant.
func flattenPrompt(req *ChatRequest, label string) (string, error) {
	if len(req.Tools) > 0 && string(req.Tools) != "null" && string(req.Tools) != "[]" {
		return "", fmt.Errorf("%s adapter cannot forward tool definitions; route tool requests to an openai-type provider", label)
	}

	var system, turns []string
	for i := range req.Messages {
		m := &req.Messages[i]
		for _, part := range m.Parts() {
			switch part.Type {
			case "text":
			case "image_url":
				return "", fmt.Errorf("%s adapter cannot forward images; route vision requests to an api-type provider", label)
			default:
				return "", fmt.Errorf("%s adapter cannot translate content part type %q", label, part.Type)
			}
		}
		switch m.Role {
		case "system", "developer":
			system = append(system, m.Text())
		case "user":
			turns = append(turns, "User: "+m.Text())
		case "assistant":
			turns = append(turns, "Assistant: "+m.Text())
		default:
			return "", fmt.Errorf("%s adapter cannot translate role %q", label, m.Role)
		}
	}

	if len(system) == 0 && len(req.Messages) == 1 && req.Messages[0].Role == "user" {
		return req.Messages[0].Text(), nil
	}
	var blocks []string
	if len(system) > 0 {
		blocks = append(blocks, "System: "+strings.Join(system, "\n\n"))
	}
	blocks = append(blocks, turns...)
	return strings.Join(blocks, "\n\n"), nil
}

// cliFlags is the set of long flags a CLI's help output advertises.
type cliFlags map[string]bool

type flagProbe struct {
	once  sync.Once
	flags cliFlags
}

var (
	probeMu sync.Mutex
	probes  = map[string]*flagProbe{}
)

// detectFlags reports which of want the CLI advertises, by running its help
// command once. The answer is cached for the process lifetime, keyed on the
// command and help arguments: help text does not change under a running
// gateway, and probing per request would add a process spawn to every call.
//
// Detection is deliberately one-directional. Flags the gateway always passes
// are never probed — an installation too old for them should fail loudly with
// the CLI's own message. Only genuinely optional flags go through here, and a
// help probe that fails is read as "not supported", which yields the
// conservative baseline command line.
func detectFlags(argv, help, want []string) cliFlags {
	key := strings.Join(append(append([]string{}, argv...), help...), "\x00")
	probeMu.Lock()
	p := probes[key]
	if p == nil {
		p = &flagProbe{}
		probes[key] = p
	}
	probeMu.Unlock()

	p.once.Do(func() {
		p.flags = cliFlags{}
		ctx, cancel := context.WithTimeout(context.Background(), cliHelpTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, argv[0], append(append([]string{}, argv[1:]...), help...)...)
		cmd.Stdin = nil
		cmd.WaitDelay = cliWaitDelay
		out, _ := cmd.CombinedOutput() // a non-zero exit still prints usage
		text := string(out)
		for _, f := range want {
			p.flags[f] = mentionsFlag(text, f)
		}
	})
	return p.flags
}

// mentionsFlag looks for flag as a whole word, so --tools is not matched by
// --allowed-tools and --model is not matched by --fallback-model.
func mentionsFlag(help, flag string) bool {
	for i := 0; ; {
		j := strings.Index(help[i:], flag)
		if j < 0 {
			return false
		}
		j += i
		i = j + len(flag)
		if j > 0 && (help[j-1] == '-' || isFlagChar(help[j-1])) {
			continue
		}
		if i < len(help) && (help[i] == '-' || isFlagChar(help[i])) {
			continue
		}
		return true
	}
}

func isFlagChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
