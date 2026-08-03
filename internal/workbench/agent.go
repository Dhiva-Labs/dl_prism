package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Streamed SSE event types. The persisted subset (message, tool_call,
// tool_result, error) shares names with the timeline so the UI can render a
// reloaded chat and a live run with one code path.
const (
	EventMessageDelta = "message_delta"
	EventToolCall     = "tool_call"
	EventAwaitApprove = "tool_await_approval"
	EventToolResult   = "tool_result"
	EventDone         = "done"
	EventError        = "error"
)

const (
	// defaultMaxSteps caps model round-trips in one run: enough for a real
	// multi-tool task, low enough that a looping model stops costing money.
	defaultMaxSteps = 12
	// defaultApprovalTimeout is how long a gated tool call waits for the user
	// before the run gives up.
	defaultApprovalTimeout = 10 * time.Minute
)

var (
	// ErrRunActive rejects a second concurrent run on one chat.
	ErrRunActive = errors.New("a run is already active for this chat")
	// ErrNoPendingApproval means nothing is waiting on that tool call id.
	ErrNoPendingApproval = errors.New("no tool call is awaiting approval")
	// ErrApprovalTimeout ends a run whose approval prompt was never answered.
	ErrApprovalTimeout = errors.New("timed out waiting for approval")
	// ErrAgentDisabled is returned when tools are requested on a project that
	// has not opted in.
	ErrAgentDisabled = errors.New("the agent is not enabled for this project")
)

// ToolCall is one function call the model asked for.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Turn is one completed model response.
type Turn struct {
	Text             string
	ToolCalls        []ToolCall
	PromptTokens     int
	CompletionTokens int
	// Route reports which candidate answered, when the caller knows.
	Route *Route
}

// CompleteRequest is one model round-trip's input.
type CompleteRequest struct {
	Model    string
	Messages []map[string]any
	Tools    []map[string]any
}

// Caller runs a chat completion through the gateway. The server implements
// it in-process against the same routing, failover, and ledger machinery
// that serves /v1/chat/completions, so an agent turn is billed and routed
// exactly like any other request. onText receives streamed text as it lands.
type Caller interface {
	Complete(ctx context.Context, req CompleteRequest, onText func(string)) (*Turn, error)
}

// Agent drives the tool-calling loop over a project's chats.
type Agent struct {
	Store  *Store
	Caller Caller

	// MaxSteps overrides the model round-trip cap; zero uses the default.
	MaxSteps int
	// ApprovalTimeout overrides how long a gated call waits; zero uses the
	// default. Tests shorten it.
	ApprovalTimeout time.Duration
	// NewTools builds the tool executor for a project; nil uses the real one.
	NewTools func(workspace, files string) *Tools

	mu   sync.Mutex
	runs map[string]*runState
}

// runState tracks the approvals one active run is parked on.
type runState struct {
	mu      sync.Mutex
	pending map[string]chan bool
}

// RunRequest is one /v1/agent/run call.
type RunRequest struct {
	ChatID  string `json:"chat_id"`
	Message string `json:"message"`
	Model   string `json:"model"`
}

// Active reports whether a run is in flight for a chat.
func (a *Agent) Active(chatID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.runs[chatID]
	return ok
}

// begin claims a chat for one run, refusing a second concurrent one.
func (a *Agent) begin(chatID string) (*runState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runs == nil {
		a.runs = map[string]*runState{}
	}
	if _, ok := a.runs[chatID]; ok {
		return nil, ErrRunActive
	}
	rs := &runState{pending: map[string]chan bool{}}
	a.runs[chatID] = rs
	return rs, nil
}

func (a *Agent) end(chatID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.runs, chatID)
}

// Approve resolves a parked tool call. It never blocks: the run owns the
// buffered channel and reads it exactly once.
func (a *Agent) Approve(chatID, toolCallID string, approve bool) error {
	a.mu.Lock()
	rs, ok := a.runs[chatID]
	a.mu.Unlock()
	if !ok {
		return ErrNoPendingApproval
	}
	rs.mu.Lock()
	ch, ok := rs.pending[toolCallID]
	if ok {
		delete(rs.pending, toolCallID)
	}
	rs.mu.Unlock()
	if !ok {
		return ErrNoPendingApproval
	}
	ch <- approve
	return nil
}

// park registers a pending approval and returns the channel to wait on plus
// the release to run when the wait is over. Registration happens before the
// prompt is streamed, so a user who answers instantly cannot miss it.
func (a *Agent) park(rs *runState, toolCallID string) (<-chan bool, func()) {
	ch := make(chan bool, 1)
	rs.mu.Lock()
	rs.pending[toolCallID] = ch
	rs.mu.Unlock()
	return ch, func() {
		rs.mu.Lock()
		delete(rs.pending, toolCallID)
		rs.mu.Unlock()
	}
}

// await blocks until the user answers, the run's context ends, or the
// approval window closes.
func (a *Agent) await(ctx context.Context, ch <-chan bool) (bool, error) {
	timeout := a.ApprovalTimeout
	if timeout <= 0 {
		timeout = defaultApprovalTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ok := <-ch:
		return ok, nil
	case <-timer.C:
		return false, ErrApprovalTimeout
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Run executes one agent turn on a chat: it persists the user's message,
// drives the model/tool loop, streams every step to emit, and appends the
// resulting timeline to the chat's JSONL.
//
// A returned error means the run never started (unknown chat, empty message,
// another run already active) and the caller may still answer with an HTTP
// error. Once the loop is running, every failure is reported as an error
// event on the stream and Run returns nil.
//
// emit is called from Run's goroutine only, so the transport does not need
// its own lock.
func (a *Agent) Run(ctx context.Context, req RunRequest, emit func(Event)) error {
	if strings.TrimSpace(req.Message) == "" {
		return errors.New("message is required")
	}
	projectID, err := a.Store.Owner(req.ChatID)
	if err != nil {
		return err
	}
	project, err := a.Store.Project(projectID)
	if err != nil {
		return err
	}

	rs, err := a.begin(req.ChatID)
	if err != nil {
		return err
	}
	defer a.end(req.ChatID)

	history, err := a.Store.History(req.ChatID)
	if err != nil {
		return err
	}
	if err := a.Store.Append(req.ChatID, Event{Type: "message", Role: "user", Content: req.Message}); err != nil {
		return err
	}

	// A run that fails after this point has already reported an error event to
	// the client, so the handler must not also write an error envelope.
	fail := func(err error) error {
		msg := err.Error()
		a.emitAndStore(req.ChatID, emit, Event{Type: EventError, Message: msg})
		return nil
	}

	messages := a.messages(project, history, req.Message)
	var tools []map[string]any
	if project.Settings.AgentEnabled {
		tools = Definitions()
	}
	runner := a.tools(project)

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = project.Settings.Model
	}

	var usage Usage
	var route *Route
	steps := a.MaxSteps
	if steps <= 0 {
		steps = defaultMaxSteps
	}

	for step := 0; step < steps; step++ {
		turn, err := a.Caller.Complete(ctx, CompleteRequest{
			Model:    model,
			Messages: messages,
			Tools:    tools,
		}, func(text string) {
			if text != "" {
				emit(Event{Type: EventMessageDelta, Text: text, TS: time.Now().UTC()})
			}
		})
		if err != nil {
			return fail(err)
		}
		usage.PromptTokens += turn.PromptTokens
		usage.CompletionTokens += turn.CompletionTokens
		if turn.Route != nil {
			route = turn.Route
		}

		if turn.Text != "" {
			a.store(req.ChatID, Event{Type: "message", Role: "assistant", Content: turn.Text})
		}
		if len(turn.ToolCalls) == 0 {
			emit(Event{Type: EventDone, TS: time.Now().UTC(), Usage: &usage, Route: route})
			return nil
		}
		if !project.Settings.AgentEnabled {
			// The model invented a tool call for a project with tools off.
			return fail(ErrAgentDisabled)
		}

		messages = append(messages, assistantMessage(turn))
		for _, call := range turn.ToolCalls {
			result, err := a.runTool(ctx, rs, project, runner, req.ChatID, call, emit)
			if err != nil {
				return fail(err)
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": call.ID,
				"content":      result,
			})
		}
	}

	return fail(fmt.Errorf("stopped after %d tool-calling steps without a final answer", steps))
}

// runTool emits the call, waits for approval when the project's mode demands
// it, executes, and emits the result. The returned string is what the model
// sees; a returned error ends the whole run.
func (a *Agent) runTool(ctx context.Context, rs *runState, project *Project, runner *Tools,
	chatID string, call ToolCall, emit func(Event)) (string, error) {

	a.emitAndStore(chatID, emit, Event{
		Type: EventToolCall, ID: call.ID, Name: call.Name, Args: rawArgs(call.Arguments),
	})

	if NeedsApproval(project.Settings.ApprovalMode, call.Name) {
		ch, release := a.park(rs, call.ID)
		emit(Event{Type: EventAwaitApprove, TS: time.Now().UTC(), ID: call.ID, Name: call.Name,
			Args: rawArgs(call.Arguments)})
		approved, err := a.await(ctx, ch)
		release()
		if err != nil {
			return "", err
		}
		if !approved {
			const denied = "denied by user"
			no := false
			a.emitAndStore(chatID, emit, Event{
				Type: EventToolResult, ID: call.ID, Name: call.Name, Output: denied, Approved: &no,
			})
			return denied, nil
		}
	}

	out, truncated, err := runner.Execute(ctx, call.Name, rawArgs(call.Arguments))
	if err != nil {
		// Tool failures are the model's problem to work around, not the
		// run's to die on.
		out = "error: " + err.Error()
	}
	a.emitAndStore(chatID, emit, Event{
		Type: EventToolResult, ID: call.ID, Name: call.Name, Output: out, Truncated: truncated,
	})
	return out, nil
}

// tools builds the executor for a project's sandbox.
func (a *Agent) tools(p *Project) *Tools {
	workspace, files := a.Store.WorkspaceDir(p.ID), a.Store.FilesDir(p.ID)
	if a.NewTools != nil {
		return a.NewTools(workspace, files)
	}
	return &Tools{Workspace: workspace, Files: files}
}

// emitAndStore streams an event and appends it to the chat timeline.
func (a *Agent) emitAndStore(chatID string, emit func(Event), ev Event) {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	emit(ev)
	a.store(chatID, ev)
}

// store appends to the timeline. A failed append must not kill a run that is
// otherwise working: the client already has the event, and the turn in
// progress still has its history in memory.
func (a *Agent) store(chatID string, ev Event) {
	_ = a.Store.Append(chatID, ev)
}

// messages assembles the model's input: system prompt, replayed history, and
// the new user turn.
func (a *Agent) messages(p *Project, history []HistoryMessage, message string) []map[string]any {
	out := make([]map[string]any, 0, len(history)+2)
	if sys := a.systemPrompt(p); sys != "" {
		out = append(out, map[string]any{"role": "system", "content": sys})
	}
	for _, h := range history {
		role := h.Role
		if role != "user" && role != "assistant" && role != "system" {
			role = "user"
		}
		out = append(out, map[string]any{"role": role, "content": h.Content})
	}
	return append(out, map[string]any{"role": "user", "content": message})
}

// systemPrompt combines the project's own instructions with the sandbox
// briefing and whatever attached knowledge is small enough to ride along.
func (a *Agent) systemPrompt(p *Project) string {
	var b strings.Builder
	if s := strings.TrimSpace(p.Settings.SystemPrompt); s != "" {
		b.WriteString(s)
	}
	if p.Settings.AgentEnabled {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "You are working inside the DL Prism Workbench on the project %q. "+
			"You have tools for the project workspace: list_dir, read_file, write_file, "+
			"shell, and web_fetch. Every path is relative to the workspace root and nothing "+
			"outside it is reachable; attached knowledge files are read-only under the "+
			"files/ prefix. Prefer reading before writing, and keep shell commands short "+
			"and non-interactive.", p.Name)
		if p.Settings.ApprovalMode != ApprovalNever {
			b.WriteString(" Some tool calls wait for the user's approval before they run.")
		}
	}
	if k, err := a.Store.Knowledge(p.ID); err == nil && k != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(k)
	}
	return b.String()
}

// assistantMessage reproduces the model's tool-calling turn in the shape the
// next request has to echo back.
func assistantMessage(turn *Turn) map[string]any {
	calls := make([]map[string]any, 0, len(turn.ToolCalls))
	for _, c := range turn.ToolCalls {
		calls = append(calls, map[string]any{
			"id":       c.ID,
			"type":     "function",
			"function": map[string]any{"name": c.Name, "arguments": c.Arguments},
		})
	}
	msg := map[string]any{"role": "assistant", "tool_calls": calls}
	// Some upstreams reject a null content alongside tool_calls.
	msg["content"] = turn.Text
	return msg
}

// rawArgs presents a model's argument blob as JSON: an object when it parses
// as one, a JSON string otherwise, so the UI never has to guess.
func rawArgs(args string) json.RawMessage {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	b, err := json.Marshal(trimmed)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}
