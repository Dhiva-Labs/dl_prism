package provider

import (
	"encoding/json"
	"net/http"

	"github.com/dhivadhiva/dl_prism/internal/config"
)

// CodexCLI answers from a paid ChatGPT plan through the locally installed
// `codex` CLI, run once per request in its non-interactive exec mode:
//
//	codex exec --json --skip-git-repo-check --model <model> \
//	           [--sandbox read-only] -- <prompt>
//
// --skip-git-repo-check is not optional: the child runs in an empty temp
// directory, and codex exec refuses to start outside a git repository.
//
// Codex has no partial-message mode, so streaming clients get the buffered
// fallback — one content chunk once the turn is done. The upside is that a
// streaming request keeps full failover, since nothing is written until the
// CLI has succeeded.
type CodexCLI struct{}

var defaultCodexCommand = []string{"codex"}

// codexOptionalFlags are probed for once per binary; see detectFlags.
//
//	--sandbox read-only   no writes and no network from model-run commands
var codexOptionalFlags = []string{"--sandbox"}

// codexScrubEnv is removed from the child's environment: OPENAI_BASE_URL
// pointed at Prism (the documented way to put Codex behind the gateway)
// would make this adapter call a CLI that calls the gateway, forever, and
// the dummy key that setup asks for would fail the CLI's own auth. Use
// `type: openai` against api.openai.com for API-key billing.
var codexScrubEnv = []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"}

func (c *CodexCLI) ChatCompletion(p *config.Provider, model string, req *ChatRequest, w http.ResponseWriter, r *http.Request) (*Result, error) {
	prompt, err := flattenPrompt(req, config.TypeCodexCLI)
	if err != nil {
		return nil, err
	}

	base := cliCommand(p, defaultCodexCommand)
	flags := detectFlags(append(append([]string{}, base...), "exec"), []string{"--help"}, codexOptionalFlags)

	argv := append([]string{}, base...)
	argv = append(argv, "exec", "--json", "--skip-git-repo-check", "--model", model)
	if flags["--sandbox"] {
		argv = append(argv, "--sandbox", "read-only")
	}
	argv = append(argv, "--", prompt)

	chat := &cliChat{
		argv:   argv,
		scrub:  codexScrubEnv,
		parse:  parseCodexEvent,
		stream: req.Stream,
		model:  model,
		label:  config.TypeCodexCLI,
	}
	return chat.run(r, w)
}

// codexLine covers every field this adapter reads from one JSONL line.
type codexLine struct {
	Type string `json:"type"`
	Item struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message json.RawMessage `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

// parseCodexEvent normalizes one line of `codex exec --json`.
//
// The vocabulary (codex-cli 0.14x):
//
//	{"type":"thread.started",...}                                 ignored
//	{"type":"turn.started"}                                       ignored
//	{"type":"item.completed","item":{"type":"agent_message",...}}  the answer
//	{"type":"item.completed","item":{"type":"error",...}}          a warning
//	{"type":"turn.completed","usage":{...}}                        final usage
//	{"type":"turn.failed","error":{"message":"..."}}               the turn failed
//	{"type":"error","message":"..."}                               a fatal error
//
// An `item` of type error is deliberately *not* fatal: codex uses it for
// advisory notes ("model metadata not found, using fallback") that do not
// stop the turn. Real failures arrive as turn.failed, a top-level error, or a
// non-zero exit — all of which the driver already treats as failures.
//
// Codex reports prompt tokens inclusive of the cached portion, so
// input_tokens maps straight onto prompt_tokens. A turn that ends without a
// turn.completed leaves UsageKnown false and the ledger estimates instead.
func parseCodexEvent(line []byte) (cliEvent, bool) {
	var l codexLine
	if err := json.Unmarshal(line, &l); err != nil {
		return cliEvent{}, false
	}
	switch l.Type {
	case "item.completed":
		if l.Item.Type != "agent_message" || l.Item.Text == "" {
			return cliEvent{}, false
		}
		return cliEvent{Text: l.Item.Text}, true

	case "turn.completed":
		return cliEvent{Usage: &Result{
			PromptTokens:     l.Usage.InputTokens,
			CompletionTokens: l.Usage.OutputTokens,
			UsageKnown:       true,
		}}, true

	case "turn.failed":
		return cliEvent{Err: firstNonEmpty(l.Error.Message, "the CLI reported a failed turn")}, true

	case "error":
		return cliEvent{Err: firstNonEmpty(l.Error.Message, rawText(l.Message), string(line))}, true
	}
	return cliEvent{}, false
}
