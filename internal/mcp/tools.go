package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
)

// maxGatewayBody caps what we read back from the gateway; tool results are
// text for a model to read, not bulk data.
const maxGatewayBody = 8 << 20

// tool is one entry in tools/list.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolResult is the tools/call payload. isError reports a failure the model
// can see and react to, as opposed to a protocol-level error.
type toolResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError"`
}

func textResult(text string, isErr bool) *toolResult {
	return &toolResult{
		Content: []map[string]any{{"type": "text", "text": text}},
		IsError: isErr,
	}
}

// invalidArgs marks an argument the caller must fix, which JSON-RPC reports as
// a protocol error rather than a tool failure.
type invalidArgs struct{ msg string }

func (e *invalidArgs) Error() string { return e.msg }

func toolDefinitions() []tool {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return []tool{
		{
			Name: "prism_chat",
			Description: "Ask a companion model through the DL Prism gateway and return its reply. " +
				"Use it for second opinions, cheap bulk work, or a locally-hosted model. " +
				"The model is chosen by Prism's routing rules unless you name one.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": str("The question or instruction to send to the model."),
					"model": str("Model id or alias from prism_models, or \"auto\" (default) to let " +
						"Prism's policy rules pick."),
					"system": str("Optional system prompt sent ahead of the user prompt."),
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
		{
			Name: "prism_explain",
			Description: "Show which provider and model a prompt would route to, and why, " +
				"without calling any upstream. Returns the decision, the classifier profile, " +
				"and the failover chain.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": str("The prompt whose routing decision you want explained."),
					"model":  str("Model id or alias to resolve; \"auto\" (default) runs the policy rules."),
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
		{
			Name: "prism_models",
			Description: "List every model the gateway can route to: id, provider, whether it runs " +
				"locally, and its context window.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			Name: "prism_usage",
			Description: "Report this month's spend through the gateway: total, budget, and a " +
				"per-model breakdown of requests, tokens and cost.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}

	var (
		text string
		err  error
	)
	switch p.Name {
	case "prism_chat":
		text, err = s.toolChat(p.Arguments)
	case "prism_explain":
		text, err = s.toolExplain(p.Arguments)
	case "prism_models":
		text, err = s.toolModels()
	case "prism_usage":
		text, err = s.toolUsage()
	default:
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("unknown tool %q", p.Name)}
	}

	var bad *invalidArgs
	if errors.As(err, &bad) {
		return nil, &rpcError{Code: codeInvalidParams, Message: bad.msg}
	}
	if err != nil {
		// The gateway is down or unhappy: the model should see that as a tool
		// failure it can report or retry, not as a broken protocol.
		s.logf("mcp: tool %s failed: %v", p.Name, err)
		return textResult(err.Error(), true), nil
	}
	return textResult(text, false), nil
}

// argString reads an optional string argument, tolerating the numbers and
// booleans a model occasionally emits for a string field.
func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func (s *Server) toolChat(args map[string]any) (string, error) {
	prompt := argString(args, "prompt")
	if prompt == "" {
		return "", &invalidArgs{"prism_chat requires a non-empty \"prompt\""}
	}
	model := argString(args, "model")
	if model == "" {
		model = "auto"
	}
	msgs := []map[string]string{}
	if system := argString(args, "system"); system != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": system})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": prompt})

	res, err := s.gatewayCall(http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   false,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return "", fmt.Errorf("the gateway returned a body that is not a chat completion: %v", err)
	}
	reply := ""
	if len(out.Choices) > 0 {
		reply = out.Choices[0].Message.Content
	}
	if strings.TrimSpace(reply) == "" {
		reply = "(the model returned an empty reply)"
	}
	return reply + "\n\n" + routeNote(res.Header), nil
}

// routeNote turns the X-Prism-* transparency headers into the one-line
// footer that tells the caller what actually answered.
func routeNote(h http.Header) string {
	provider := or(h.Get("X-Prism-Provider"), "unknown")
	model := or(h.Get("X-Prism-Model"), "unknown")
	mode := or(h.Get("X-Prism-Mode"), "unknown")
	reason := or(h.Get("X-Prism-Reason"), "no reason reported")
	return fmt.Sprintf("— routed %s/%s (%s: %s)", provider, model, mode, reason)
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (s *Server) toolExplain(args map[string]any) (string, error) {
	prompt := argString(args, "prompt")
	if prompt == "" {
		return "", &invalidArgs{"prism_explain requires a non-empty \"prompt\""}
	}
	model := argString(args, "model")
	if model == "" {
		model = "auto"
	}
	res, err := s.gatewayCall(http.MethodPost, "/v1/route/explain", map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	return prettyJSON(res.Body), nil
}

func prettyJSON(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}

func (s *Server) toolModels() (string, error) {
	res, err := s.gatewayCall(http.MethodGet, "/v1/models", nil)
	if err != nil {
		return "", err
	}
	var list struct {
		Data []struct {
			ID    string `json:"id"`
			Owner string `json:"owned_by"`
			Prism struct {
				Local         bool `json:"local"`
				ContextWindow int  `json:"context_window"`
			} `json:"prism"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body, &list); err != nil {
		return "", fmt.Errorf("the gateway returned a body that is not a model list: %v", err)
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tPROVIDER\tLOCAL\tCONTEXT")
	for _, m := range list.Data {
		local, context := "no", fmt.Sprint(m.Prism.ContextWindow)
		if m.Prism.Local {
			local = "yes"
		}
		if m.ID == "auto" {
			local, context = "-", "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.ID, m.Owner, local, context)
	}
	tw.Flush()
	return strings.TrimRight(b.String(), "\n"), nil
}

func (s *Server) toolUsage() (string, error) {
	res, err := s.gatewayCall(http.MethodGet, "/admin/usage", nil)
	if err != nil {
		return "", err
	}
	var usage struct {
		MonthTotal float64 `json:"month_total_usd"`
		Budget     float64 `json:"budget_usd"`
		Exceeded   bool    `json:"budget_exceeded"`
		Models     []struct {
			Model            string  `json:"model"`
			Provider         string  `json:"provider"`
			Requests         int     `json:"requests"`
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			CostUSD          float64 `json:"cost_usd"`
		} `json:"models"`
	}
	if err := json.Unmarshal(res.Body, &usage); err != nil {
		return "", fmt.Errorf("the gateway returned a body that is not a usage summary: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "This month: $%.4f", usage.MonthTotal)
	if usage.Budget > 0 {
		fmt.Fprintf(&b, " of $%.2f budget", usage.Budget)
		if usage.Exceeded {
			b.WriteString(" (budget exceeded)")
		}
	} else {
		b.WriteString(" (no budget configured)")
	}
	b.WriteString("\n")
	if len(usage.Models) == 0 {
		b.WriteString("No usage recorded yet.")
		return b.String(), nil
	}
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tPROVIDER\tREQUESTS\tTOK IN\tTOK OUT\tCOST USD")
	for _, m := range usage.Models {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%.4f\n",
			m.Model, m.Provider, m.Requests, m.PromptTokens, m.CompletionTokens, m.CostUSD)
	}
	tw.Flush()
	return strings.TrimRight(b.String(), "\n"), nil
}

// gatewayResult is a successful reply from the gateway.
type gatewayResult struct {
	Header http.Header
	Body   []byte
}

// gatewayCall performs one HTTP call against the gateway. Every failure —
// unreachable, non-2xx, unreadable — comes back as an error whose text is
// meant to be read by whoever called the tool.
func (s *Server) gatewayCall(method, path string, body any) (*gatewayResult, error) {
	base := strings.TrimRight(s.Gateway, "/")
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("bad gateway URL %q: %v", s.Gateway, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
	}

	resp, err := s.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the Prism gateway at %s: %v\n"+
			"Start it with `prism serve`, or point this MCP server elsewhere with "+
			"--gateway http://host:port.", base, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayBody))
	if err != nil {
		return nil, fmt.Errorf("reading the gateway response: %v", err)
	}
	if resp.StatusCode/100 != 2 {
		hint := ""
		if resp.StatusCode == http.StatusUnauthorized {
			hint = "\nThe gateway requires an API key: pass --api-key or set PRISM_API_KEY."
		}
		return nil, fmt.Errorf("the gateway returned %d for %s: %s%s",
			resp.StatusCode, path, gatewayErrorMessage(b), hint)
	}
	return &gatewayResult{Header: resp.Header, Body: b}, nil
}

// gatewayErrorMessage digs the message out of an error envelope, falling back
// to the raw body.
func gatewayErrorMessage(b []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &env) == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	if s == "" {
		s = "(empty response)"
	}
	return s
}
