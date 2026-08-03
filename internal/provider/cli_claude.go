package provider

import (
	"encoding/json"
	"net/http"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// ClaudeCode answers from a paid Claude subscription through the locally
// installed `claude` CLI, run once per request in headless print mode:
//
//	claude [--tools ""] --print --output-format stream-json --verbose \
//	       --model <model> [--safe-mode] [--max-turns 1] \
//	       [--include-partial-messages] -- <prompt>
//
// Model ids are whatever the CLI accepts — the `sonnet`/`opus`/`haiku`
// aliases or a full model name. Everything after `--` is the flattened
// prompt, so a prompt starting with a dash cannot be read as a flag.
type ClaudeCode struct{}

var defaultClaudeCommand = []string{"claude"}

// claudeOptionalFlags are probed for once per binary. They are all hardening
// or quality-of-life, never load-bearing, so an older CLI simply gets the
// baseline command line:
//
//	--tools ""                  no filesystem, shell or web access: chat only
//	--safe-mode                 ignore the operator's CLAUDE.md, skills,
//	                            plugins, hooks and MCP servers, so a gateway
//	                            request is not shaped by the host's setup
//	--max-turns 1               one turn, belt-and-braces alongside --tools ""
//	--include-partial-messages  real token-by-token deltas for streaming
var claudeOptionalFlags = []string{
	"--tools", "--safe-mode", "--max-turns", "--include-partial-messages",
}

// claudeScrubEnv is removed from the child's environment.
//
// ANTHROPIC_BASE_URL is the important one: the documented way to put Claude
// Code behind Prism is to point it at the gateway, and inheriting that here
// would make the gateway call a CLI that calls the gateway, forever. The key
// variables go with it — this provider exists to spend a subscription, and
// the `ANTHROPIC_API_KEY=dummy` that the base-URL setup asks for would
// otherwise turn every call into a 401. Use `type: anthropic` for API-key
// billing. The CLAUDECODE markers just stop the child mistaking itself for a
// nested session.
var claudeScrubEnv = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_MODEL",
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
}

func (c *ClaudeCode) ChatCompletion(p *config.Provider, model string, req *ChatRequest, w http.ResponseWriter, r *http.Request) (*Result, error) {
	prompt, err := flattenPrompt(req, config.TypeClaudeCode)
	if err != nil {
		return nil, err
	}

	base := cliCommand(p, defaultClaudeCommand)
	flags := detectFlags(base, []string{"--help"}, claudeOptionalFlags)

	argv := append([]string{}, base...)
	if flags["--tools"] {
		// Variadic: it swallows following words, so it goes first where the
		// next token is guaranteed to be a flag.
		argv = append(argv, "--tools", "")
	}
	argv = append(argv, "--print", "--output-format", "stream-json", "--verbose", "--model", model)
	if flags["--safe-mode"] {
		argv = append(argv, "--safe-mode")
	}
	if flags["--max-turns"] {
		argv = append(argv, "--max-turns", "1")
	}
	partial := req.Stream && flags["--include-partial-messages"]
	if partial {
		argv = append(argv, "--include-partial-messages")
	}
	argv = append(argv, "--", prompt)

	chat := &cliChat{
		argv:   argv,
		scrub:  claudeScrubEnv,
		parse:  func(line []byte) (cliEvent, bool) { return parseClaudeEvent(line, partial) },
		deltas: partial,
		stream: req.Stream,
		model:  model,
		label:  config.TypeClaudeCode,
	}
	return chat.run(r, w)
}

// claudeLine covers every field this adapter reads from one JSONL line.
type claudeLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	// result events. Result and Error are raw because the CLI has shipped
	// both as plain strings and as structured objects; a typed string field
	// would make one shape fail to decode and lose the whole line.
	IsError    bool            `json:"is_error"`
	Result     json.RawMessage `json:"result"`
	StopReason string          `json:"stop_reason"`
	Usage      claudeCLIUsage  `json:"usage"`
	Error      json.RawMessage `json:"error"`

	// assistant events
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`

	// stream_event events wrap a raw Anthropic Messages SSE event.
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	} `json:"event"`
}

type claudeCLIUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// parseClaudeEvent normalizes one line of `claude --output-format stream-json`.
//
// The vocabulary (Claude Code 2.x):
//
//	{"type":"system","subtype":"init",...}       session banner — ignored
//	{"type":"rate_limit_event",...}              plan headroom — ignored
//	{"type":"assistant","message":{...}}         one whole assistant message
//	{"type":"stream_event","event":{...}}        a raw Anthropic SSE event,
//	                                             only with --include-partial-messages
//	{"type":"result","subtype":"success",...}    final status, text and usage
//
// With partials on, both stream_event deltas and the assistant summary carry
// the same words, so exactly one source is read: the deltas when partial is
// set, the assistant messages otherwise.
//
// Prompt tokens sum input_tokens with both cache figures — Anthropic reports
// cached context separately, and the total is what actually went through the
// model. Cost is zero on a subscription, so these numbers are telemetry.
func parseClaudeEvent(line []byte, partial bool) (cliEvent, bool) {
	var l claudeLine
	if err := json.Unmarshal(line, &l); err != nil {
		return cliEvent{}, false
	}
	switch l.Type {
	case "assistant":
		if partial {
			return cliEvent{}, false
		}
		var text string
		for _, c := range l.Message.Content {
			if c.Type == "text" {
				text += c.Text
			}
		}
		if text == "" {
			return cliEvent{}, false
		}
		return cliEvent{Text: text}, true

	case "stream_event":
		if !partial {
			return cliEvent{}, false
		}
		switch l.Event.Type {
		case "content_block_delta":
			if l.Event.Delta.Type != "text_delta" || l.Event.Delta.Text == "" {
				return cliEvent{}, false
			}
			return cliEvent{Text: l.Event.Delta.Text}, true
		case "message_delta":
			if l.Event.Delta.StopReason == "" {
				return cliEvent{}, false
			}
			return cliEvent{Finish: mapStopReason(l.Event.Delta.StopReason)}, true
		}
		return cliEvent{}, false

	case "result":
		ev := cliEvent{
			Usage: &Result{
				PromptTokens: l.Usage.InputTokens +
					l.Usage.CacheCreationInputTokens + l.Usage.CacheReadInputTokens,
				CompletionTokens: l.Usage.OutputTokens,
				UsageKnown:       true,
			},
		}
		if l.StopReason != "" {
			ev.Finish = mapStopReason(l.StopReason)
		}
		if l.IsError || (l.Subtype != "" && l.Subtype != "success") {
			ev.Err = firstNonEmpty(rawText(l.Result), rawText(l.Error), l.Subtype,
				"the CLI reported a failed turn")
		}
		return ev, true

	case "error":
		return cliEvent{Err: firstNonEmpty(rawText(l.Error), rawText(l.Result), string(line))}, true
	}
	return cliEvent{}, false
}

// rawText renders a JSON value as a message: unquoted when it is a string,
// verbatim JSON when it is anything else.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
