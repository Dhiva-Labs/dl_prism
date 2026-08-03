package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedCaller plays a fixed sequence of model turns and records what the
// agent asked for each time.
type scriptedCaller struct {
	mu    sync.Mutex
	turns []func(req CompleteRequest, onText func(string)) (*Turn, error)
	seen  []CompleteRequest
	next  int
}

func (c *scriptedCaller) Complete(_ context.Context, req CompleteRequest, onText func(string)) (*Turn, error) {
	c.mu.Lock()
	c.seen = append(c.seen, req)
	i := c.next
	c.next++
	if i >= len(c.turns) {
		c.mu.Unlock()
		return nil, errors.New("the model was called more times than the script allows")
	}
	fn := c.turns[i]
	c.mu.Unlock()
	return fn(req, onText)
}

func (c *scriptedCaller) requests() []CompleteRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]CompleteRequest(nil), c.seen...)
}

// says builds a turn that streams text and stops.
func says(text string, prompt, completion int) func(CompleteRequest, func(string)) (*Turn, error) {
	return func(_ CompleteRequest, onText func(string)) (*Turn, error) {
		for _, word := range strings.SplitAfter(text, " ") {
			if word != "" {
				onText(word)
			}
		}
		return &Turn{Text: text, PromptTokens: prompt, CompletionTokens: completion}, nil
	}
}

// wants builds a turn that asks for one tool call.
func wants(id, name, args string, prompt, completion int) func(CompleteRequest, func(string)) (*Turn, error) {
	return func(CompleteRequest, func(string)) (*Turn, error) {
		return &Turn{
			ToolCalls:        []ToolCall{{ID: id, Name: name, Arguments: args}},
			PromptTokens:     prompt,
			CompletionTokens: completion,
		}, nil
	}
}

// sink collects streamed events and lets a test wait for a particular one.
type sink struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newSink() *sink { return &sink{ch: make(chan Event, 128)} }

func (s *sink) emit(ev Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
	select {
	case s.ch <- ev:
	default:
	}
}

func (s *sink) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

func (s *sink) types() []string {
	var out []string
	for _, ev := range s.all() {
		out = append(out, ev.Type)
	}
	return out
}

// text concatenates every streamed message_delta.
func (s *sink) text() string {
	var b strings.Builder
	for _, ev := range s.all() {
		if ev.Type == EventMessageDelta {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

// waitFor blocks until an event of the given type is emitted.
func (s *sink) waitFor(t *testing.T, typ string) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.ch:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %s event; got %v", typ, s.types())
		}
	}
}

// agentFixture wires a store, a project with the agent on, one chat, and a
// scripted model.
type agentFixture struct {
	store   *Store
	agent   *Agent
	project *Project
	chat    *Chat
	caller  *scriptedCaller
}

func newAgentFixture(t *testing.T, set Settings,
	turns ...func(CompleteRequest, func(string)) (*Turn, error)) *agentFixture {

	t.Helper()
	s := newStore(t)
	p, err := s.CreateProject("proj", set)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	c := mustChat(t, s, p.ID, "run")
	caller := &scriptedCaller{turns: turns}
	return &agentFixture{
		store:   s,
		agent:   &Agent{Store: s, Caller: caller},
		project: p,
		chat:    c,
		caller:  caller,
	}
}

func (f *agentFixture) run(t *testing.T, message string) (*sink, error) {
	t.Helper()
	out := newSink()
	err := f.agent.Run(context.Background(), RunRequest{ChatID: f.chat.ID, Message: message}, out.emit)
	return out, err
}

// assertOrder checks that the streamed types match exactly.
func assertOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event types = %v, want %v", got, want)
		}
	}
}

// A full run: the model asks for a tool, the tool runs for real inside the
// project's sandbox, and the model answers with the result in hand.
func TestRunExecutesAToolAndAnswers(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalNever},
		wants("call_1", ToolWriteFile, `{"path":"out.txt","content":"hello"}`, 100, 10),
		says("Done: the file is written.", 140, 8),
	)

	out, err := f.run(t, "write hello into out.txt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertOrder(t, out.types(),
		EventToolCall, EventToolResult,
		EventMessageDelta, EventMessageDelta, EventMessageDelta, EventMessageDelta, EventMessageDelta,
		EventDone)

	events := out.all()
	if events[0].Name != ToolWriteFile || events[0].ID != "call_1" {
		t.Errorf("tool_call = %+v, want the write_file call", events[0])
	}
	if string(events[0].Args) != `{"path":"out.txt","content":"hello"}` {
		t.Errorf("tool_call args = %s, want the model's arguments as JSON", events[0].Args)
	}
	if !strings.Contains(events[1].Output, "wrote 5 bytes") {
		t.Errorf("tool_result = %q, want the write confirmation", events[1].Output)
	}
	if events[1].ID != "call_1" {
		t.Errorf("tool_result id = %q, want it to match the call", events[1].ID)
	}
	if events[1].Approved != nil {
		t.Errorf("approved = %v on an ungated result, want the field absent", *events[1].Approved)
	}
	if out.text() != "Done: the file is written." {
		t.Errorf("streamed text = %q", out.text())
	}

	done := events[len(events)-1]
	if done.Usage == nil {
		t.Fatal("the done event carries no usage")
	}
	if done.Usage.PromptTokens != 240 || done.Usage.CompletionTokens != 18 {
		t.Errorf("usage = %+v, want every model call in the run summed (240/18)", done.Usage)
	}

	// The tool really ran, in the project's workspace.
	data, err := os.ReadFile(filepath.Join(f.store.WorkspaceDir(f.project.ID), "out.txt"))
	if err != nil {
		t.Fatalf("the tool did not write the file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q, want hello", data)
	}

	// The second model call has to carry the exchange the protocol requires.
	reqs := f.caller.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d model calls, want 2", len(reqs))
	}
	second := reqs[1].Messages
	last := second[len(second)-1]
	if last["role"] != "tool" || last["tool_call_id"] != "call_1" {
		t.Errorf("last message = %+v, want the tool result keyed to call_1", last)
	}
	if !strings.Contains(last["content"].(string), "wrote 5 bytes") {
		t.Errorf("tool message content = %v, want the tool output", last["content"])
	}
	assistant := second[len(second)-2]
	if assistant["role"] != "assistant" {
		t.Fatalf("second-to-last message = %+v, want the assistant's tool-calling turn", assistant)
	}
	calls, ok := assistant["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 1 || calls[0]["id"] != "call_1" {
		t.Errorf("assistant tool_calls = %+v, want the original call echoed back", assistant["tool_calls"])
	}

	// And the timeline was persisted in the same order.
	_, stored, err := f.store.Chat(f.chat.ID)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var types []string
	for _, ev := range stored {
		types = append(types, ev.Type)
	}
	assertOrder(t, types, "message", EventToolCall, EventToolResult, "message")
	if stored[0].Role != "user" || stored[0].Content != "write hello into out.txt" {
		t.Errorf("first stored event = %+v, want the user's message", stored[0])
	}
	if stored[3].Role != "assistant" || stored[3].Content != "Done: the file is written." {
		t.Errorf("last stored event = %+v, want the assistant's answer", stored[3])
	}
	// message_delta is a streaming detail; the timeline keeps whole messages.
	for _, ev := range stored {
		if ev.Type == EventMessageDelta || ev.Type == EventDone || ev.Type == EventAwaitApprove {
			t.Errorf("streaming-only event %q was persisted", ev.Type)
		}
	}
}

func TestRunStreamsAPlainAnswerWithoutTools(t *testing.T) {
	f := newAgentFixture(t, Settings{}, says("just talking", 5, 2))

	out, err := f.run(t, "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertOrder(t, out.types(), EventMessageDelta, EventMessageDelta, EventDone)
	if out.text() != "just talking" {
		t.Errorf("text = %q", out.text())
	}
	// A project with the agent off must not be offered tools at all.
	reqs := f.caller.requests()
	if len(reqs[0].Tools) != 0 {
		t.Errorf("%d tools were sent to a project with the agent disabled", len(reqs[0].Tools))
	}
}

func TestRunSendsToolDefinitionsWhenEnabled(t *testing.T) {
	f := newAgentFixture(t, Settings{AgentEnabled: true}, says("ok", 1, 1))
	if _, err := f.run(t, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	tools := f.caller.requests()[0].Tools
	if len(tools) != len(Definitions()) {
		t.Fatalf("%d tools sent, want all %d", len(tools), len(Definitions()))
	}
}

func TestRunReplaysHistoryAndKnowledge(t *testing.T) {
	f := newAgentFixture(t, Settings{
		AgentEnabled: true, ApprovalMode: ApprovalNever, SystemPrompt: "Answer in haiku.",
	}, says("first", 1, 1), says("second", 1, 1))

	if _, err := f.store.PutFile(f.project.ID, "brief.md", []byte("ship by friday")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(t, "one"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := f.run(t, "two"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := f.caller.requests()
	first := reqs[0].Messages
	if first[0]["role"] != "system" {
		t.Fatalf("first message = %+v, want the system prompt", first[0])
	}
	sys := first[0]["content"].(string)
	for _, want := range []string{"Answer in haiku.", "ship by friday", "Project knowledge:", "workspace"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt is missing %q:\n%s", want, sys)
		}
	}
	if len(first) != 2 {
		t.Fatalf("first call sent %d messages, want system + user", len(first))
	}

	second := reqs[1].Messages
	if len(second) != 4 {
		t.Fatalf("second call sent %d messages, want system + 2 replayed turns + the new one", len(second))
	}
	if second[1]["content"] != "one" || second[2]["content"] != "first" || second[3]["content"] != "two" {
		t.Errorf("replayed history = %+v", second)
	}
	if second[2]["role"] != "assistant" {
		t.Errorf("replayed assistant turn has role %v", second[2]["role"])
	}
}

// --- approvals -------------------------------------------------------

func TestRunParksOnApprovalAndResumes(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalMutations},
		wants("call_1", ToolShell, `{"command":"echo approved"}`, 10, 1),
		says("it printed approved", 20, 3),
	)

	out := newSink()
	errc := make(chan error, 1)
	go func() {
		errc <- f.agent.Run(context.Background(),
			RunRequest{ChatID: f.chat.ID, Message: "run echo"}, out.emit)
	}()

	ev := out.waitFor(t, EventAwaitApprove)
	if ev.ID != "call_1" || ev.Name != ToolShell {
		t.Errorf("tool_await_approval = %+v, want the shell call", ev)
	}
	if !f.agent.Active(f.chat.ID) {
		t.Error("the chat is not reported as having an active run while parked")
	}
	if err := f.agent.Approve(f.chat.ID, "call_1", true); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertOrder(t, out.types(),
		EventToolCall, EventAwaitApprove, EventToolResult,
		EventMessageDelta, EventMessageDelta, EventMessageDelta, EventDone)
	events := out.all()
	if !strings.Contains(events[2].Output, "approved") {
		t.Errorf("tool_result = %q, want the command's output", events[2].Output)
	}
	// A result that actually ran carries no approved field at all: its
	// presence is what marks a denial.
	if events[2].Approved != nil {
		t.Errorf("approved = %v on a result that ran, want the field absent", *events[2].Approved)
	}
	if b, _ := json.Marshal(events[2]); strings.Contains(string(b), `"approved":`) {
		t.Errorf("approved tool_result = %s, want no approved field", b)
	}
	if f.agent.Active(f.chat.ID) {
		t.Error("the run is still registered after finishing")
	}
	// The approval prompt itself is not part of the stored timeline; the call
	// and its result are.
	_, stored, _ := f.store.Chat(f.chat.ID)
	var types []string
	for _, ev := range stored {
		types = append(types, ev.Type)
	}
	assertOrder(t, types, "message", EventToolCall, EventToolResult, "message")
}

func TestRunDeniedToolTellsTheModel(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalAlways},
		wants("call_1", ToolReadFile, `{"path":"secrets.txt"}`, 10, 1),
		says("understood, I will not read it", 20, 3),
	)

	out := newSink()
	errc := make(chan error, 1)
	go func() {
		errc <- f.agent.Run(context.Background(),
			RunRequest{ChatID: f.chat.ID, Message: "read secrets.txt"}, out.emit)
	}()

	out.waitFor(t, EventAwaitApprove)
	if err := f.agent.Approve(f.chat.ID, "call_1", false); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Run: %v", err)
	}

	var result Event
	for _, ev := range out.all() {
		if ev.Type == EventToolResult {
			result = ev
		}
	}
	if result.Output != "denied by user" {
		t.Errorf("tool_result = %q, want the denial", result.Output)
	}
	// A replayed transcript has to tell a denial from a success.
	if result.Approved == nil || *result.Approved {
		t.Errorf("tool_result approved = %v, want an explicit false", result.Approved)
	}
	if b, _ := json.Marshal(result); !strings.Contains(string(b), `"approved":false`) {
		t.Errorf("denied tool_result = %s, want approved:false on the wire", b)
	}
	// And the stored event carries it too, not just the live stream.
	_, stored, err := f.store.Chat(f.chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	var storedResult Event
	for _, ev := range stored {
		if ev.Type == EventToolResult {
			storedResult = ev
		}
	}
	if storedResult.Approved == nil || *storedResult.Approved {
		t.Errorf("stored tool_result approved = %v, want false", storedResult.Approved)
	}
	reqs := f.caller.requests()
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last["content"] != "denied by user" {
		t.Errorf("the model was told %q, want the denial", last["content"])
	}
	// Even under "always", a read the user refused must not have happened.
	if _, err := os.Stat(filepath.Join(f.store.WorkspaceDir(f.project.ID), "secrets.txt")); !os.IsNotExist(err) {
		t.Error("the denied tool touched the workspace")
	}
}

func TestRunApprovalTimesOut(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalAlways},
		wants("call_1", ToolShell, `{"command":"echo hi"}`, 10, 1),
	)
	f.agent.ApprovalTimeout = 150 * time.Millisecond

	start := time.Now()
	out, err := f.run(t, "run something")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the run waited %v, want it to give up near the 150ms window", elapsed)
	}

	assertOrder(t, out.types(), EventToolCall, EventAwaitApprove, EventError)
	last := out.all()[2]
	if !strings.Contains(last.Message, "approval") {
		t.Errorf("error = %q, want it to blame the missing approval", last.Message)
	}
	// The failure is on the record.
	_, stored, _ := f.store.Chat(f.chat.ID)
	if stored[len(stored)-1].Type != EventError {
		t.Errorf("timeline ends with %q, want an error event", stored[len(stored)-1].Type)
	}
	if f.agent.Active(f.chat.ID) {
		t.Error("the run stayed registered after timing out")
	}
}

func TestApproveWithoutAParkedCall(t *testing.T) {
	f := newAgentFixture(t, Settings{AgentEnabled: true})
	if err := f.agent.Approve(f.chat.ID, "call_1", true); !errors.Is(err, ErrNoPendingApproval) {
		t.Errorf("Approve on an idle chat = %v, want ErrNoPendingApproval", err)
	}

	// And with a run parked, an unknown id is still rejected.
	f2 := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalAlways},
		wants("call_1", ToolShell, `{"command":"echo hi"}`, 1, 1),
	)
	f2.agent.ApprovalTimeout = 2 * time.Second
	out := newSink()
	errc := make(chan error, 1)
	go func() {
		errc <- f2.agent.Run(context.Background(),
			RunRequest{ChatID: f2.chat.ID, Message: "go"}, out.emit)
	}()
	out.waitFor(t, EventAwaitApprove)
	if err := f2.agent.Approve(f2.chat.ID, "call_other", true); !errors.Is(err, ErrNoPendingApproval) {
		t.Errorf("Approve with a stale id = %v, want ErrNoPendingApproval", err)
	}
	f2.agent.Approve(f2.chat.ID, "call_1", false)
	<-errc
}

// A parked run must not survive the client hanging up.
func TestRunStopsWhenTheClientDisconnects(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalAlways},
		wants("call_1", ToolShell, `{"command":"echo hi"}`, 1, 1),
	)
	ctx, cancel := context.WithCancel(context.Background())
	out := newSink()
	errc := make(chan error, 1)
	go func() {
		errc <- f.agent.Run(ctx, RunRequest{ChatID: f.chat.ID, Message: "go"}, out.emit)
	}()
	out.waitFor(t, EventAwaitApprove)
	cancel()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not stop when its context was cancelled")
	}
	types := out.types()
	if types[len(types)-1] != EventError {
		t.Errorf("event types = %v, want it to end in an error", types)
	}
	if f.agent.Active(f.chat.ID) {
		t.Error("the cancelled run stayed registered")
	}
}

// --- concurrency and limits ------------------------------------------

func TestSecondConcurrentRunIsRejected(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalAlways},
		wants("call_1", ToolShell, `{"command":"echo hi"}`, 1, 1),
		says("done", 1, 1),
	)
	f.agent.ApprovalTimeout = 3 * time.Second

	out := newSink()
	errc := make(chan error, 1)
	go func() {
		errc <- f.agent.Run(context.Background(),
			RunRequest{ChatID: f.chat.ID, Message: "first"}, out.emit)
	}()
	out.waitFor(t, EventAwaitApprove)

	second := newSink()
	err := f.agent.Run(context.Background(),
		RunRequest{ChatID: f.chat.ID, Message: "second"}, second.emit)
	if !errors.Is(err, ErrRunActive) {
		t.Fatalf("second run = %v, want ErrRunActive", err)
	}
	if len(second.all()) != 0 {
		t.Errorf("the rejected run emitted %v", second.types())
	}

	// A different chat in the same project is unaffected.
	other := mustChat(t, f.store, f.project.ID, "other")
	otherSink := newSink()
	if err := f.agent.Run(context.Background(),
		RunRequest{ChatID: other.ID, Message: "elsewhere"}, otherSink.emit); err != nil {
		t.Errorf("a run on another chat was blocked: %v", err)
	}

	f.agent.Approve(f.chat.ID, "call_1", false)
	<-errc

	// Once finished, the chat accepts a run again.
	if f.agent.Active(f.chat.ID) {
		t.Error("the finished run is still active")
	}
}

func TestRunStopsAtTheStepCap(t *testing.T) {
	var turns []func(CompleteRequest, func(string)) (*Turn, error)
	for i := 0; i < 10; i++ {
		turns = append(turns, wants("call_x", ToolListDir, `{"path":"."}`, 1, 1))
	}
	f := newAgentFixture(t, Settings{AgentEnabled: true, ApprovalMode: ApprovalNever}, turns...)
	f.agent.MaxSteps = 3

	out, err := f.run(t, "loop forever")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	types := out.types()
	if types[len(types)-1] != EventError {
		t.Fatalf("event types = %v, want the run to end in an error", types)
	}
	if !strings.Contains(out.all()[len(types)-1].Message, "3") {
		t.Errorf("error = %q, want it to name the step cap", out.all()[len(types)-1].Message)
	}
	if len(f.caller.requests()) != 3 {
		t.Errorf("%d model calls, want the cap to stop at 3", len(f.caller.requests()))
	}
}

func TestRunDefaultsToTwelveSteps(t *testing.T) {
	a := &Agent{}
	if defaultMaxSteps != 12 {
		t.Errorf("defaultMaxSteps = %d, want the contracted 12", defaultMaxSteps)
	}
	if a.MaxSteps != 0 {
		t.Errorf("MaxSteps = %d, want the zero value to mean the default", a.MaxSteps)
	}
	if defaultApprovalTimeout != 10*time.Minute {
		t.Errorf("defaultApprovalTimeout = %v, want 10m", defaultApprovalTimeout)
	}
}

// --- failure paths ---------------------------------------------------

func TestRunReportsAModelFailureAsAnErrorEvent(t *testing.T) {
	f := newAgentFixture(t, Settings{},
		func(CompleteRequest, func(string)) (*Turn, error) {
			return nil, errors.New("all candidates failed: primary/gpt-test (500)")
		})

	out, err := f.run(t, "hi")
	if err != nil {
		t.Fatalf("Run = %v, want the failure reported on the stream instead", err)
	}
	assertOrder(t, out.types(), EventError)
	if !strings.Contains(out.all()[0].Message, "all candidates failed") {
		t.Errorf("error = %q, want the upstream reason relayed", out.all()[0].Message)
	}
	_, stored, _ := f.store.Chat(f.chat.ID)
	if len(stored) != 2 || stored[1].Type != EventError {
		t.Errorf("timeline = %+v, want the user turn then the error", stored)
	}
}

// A failing tool is the model's problem to route around, not the run's to die
// on.
func TestRunSurvivesAFailingTool(t *testing.T) {
	f := newAgentFixture(t,
		Settings{AgentEnabled: true, ApprovalMode: ApprovalNever},
		wants("call_1", ToolReadFile, `{"path":"../../etc/passwd"}`, 1, 1),
		says("that path is not available", 1, 1),
	)

	out, err := f.run(t, "read /etc/passwd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var result Event
	for _, ev := range out.all() {
		if ev.Type == EventToolResult {
			result = ev
		}
	}
	if !strings.HasPrefix(result.Output, "error: ") {
		t.Errorf("tool_result = %q, want the failure handed to the model", result.Output)
	}
	if !strings.Contains(result.Output, "outside the project workspace") {
		t.Errorf("tool_result = %q, want the containment reason", result.Output)
	}
	if out.types()[len(out.types())-1] != EventDone {
		t.Errorf("event types = %v, want the run to finish normally", out.types())
	}
}

// A model that invents a tool call on a project with tools off gets stopped.
func TestRunRefusesToolCallsWhenTheAgentIsDisabled(t *testing.T) {
	f := newAgentFixture(t, Settings{AgentEnabled: false},
		wants("call_1", ToolShell, `{"command":"rm -rf /"}`, 1, 1))

	out, err := f.run(t, "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertOrder(t, out.types(), EventError)
	if !strings.Contains(out.all()[0].Message, "not enabled") {
		t.Errorf("error = %q, want it to name the disabled agent", out.all()[0].Message)
	}
}

func TestRunRejectsBadRequests(t *testing.T) {
	f := newAgentFixture(t, Settings{})

	if _, err := f.run(t, "   "); err == nil {
		t.Error("an empty message was accepted")
	}
	out := newSink()
	err := f.agent.Run(context.Background(),
		RunRequest{ChatID: "0123456789abcdef", Message: "hi"}, out.emit)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Run on an unknown chat = %v, want ErrNotFound", err)
	}
	if len(out.all()) != 0 {
		t.Errorf("a rejected run emitted %v", out.types())
	}
}

// The model's own choice of model id is passed through; otherwise the
// project's default is used.
func TestRunPicksTheModel(t *testing.T) {
	f := newAgentFixture(t, Settings{Model: "project-model"}, says("a", 1, 1), says("b", 1, 1))

	if _, err := f.run(t, "one"); err != nil {
		t.Fatal(err)
	}
	if got := f.caller.requests()[0].Model; got != "project-model" {
		t.Errorf("model = %q, want the project default", got)
	}

	out := newSink()
	if err := f.agent.Run(context.Background(),
		RunRequest{ChatID: f.chat.ID, Message: "two", Model: "override"}, out.emit); err != nil {
		t.Fatal(err)
	}
	if got := f.caller.requests()[1].Model; got != "override" {
		t.Errorf("model = %q, want the per-run override", got)
	}
}

// The stream carries no per-turn headers, so whichever candidate answered is
// reported in the done event.
func TestRunReportsTheRouteThatAnswered(t *testing.T) {
	f := newAgentFixture(t, Settings{},
		func(CompleteRequest, func(string)) (*Turn, error) {
			return &Turn{Text: "hi", Route: &Route{Provider: "backup", Model: "backup-test", Mode: "auto"}}, nil
		})

	out, err := f.run(t, "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	done := out.all()[len(out.all())-1]
	if done.Type != EventDone {
		t.Fatalf("last event = %s, want done", done.Type)
	}
	if done.Route == nil || done.Route.Provider != "backup" || done.Route.Model != "backup-test" {
		t.Errorf("route = %+v, want the answering candidate", done.Route)
	}
	b, _ := json.Marshal(done)
	if !strings.Contains(string(b), `"route":{"provider":"backup"`) {
		t.Errorf("done event = %s, want a route object", b)
	}
}

func TestRawArgs(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`{"path":"a.txt"}`, `{"path":"a.txt"}`},
		{"", "{}"},
		{"   ", "{}"},
		{"not json", `"not json"`},
		{`{"a":1`, `"{\"a\":1"`},
	}
	for _, tc := range tests {
		got := string(rawArgs(tc.in))
		if got != tc.want {
			t.Errorf("rawArgs(%q) = %s, want %s", tc.in, got, tc.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("rawArgs(%q) produced invalid JSON: %s", tc.in, got)
		}
	}
}

// Every streamed event must serialise to the shape the API contract names.
func TestEventJSONShape(t *testing.T) {
	tests := []struct {
		ev     Event
		want   []string
		absent []string
	}{
		{Event{Type: EventMessageDelta, Text: "hi"}, []string{`"type":"message_delta"`, `"text":"hi"`},
			[]string{"output", "usage"}},
		{Event{Type: EventToolCall, ID: "c1", Name: "shell", Args: json.RawMessage(`{"command":"ls"}`)},
			[]string{`"type":"tool_call"`, `"id":"c1"`, `"name":"shell"`, `"args":{"command":"ls"}`}, nil},
		{Event{Type: EventAwaitApprove, ID: "c1"}, []string{`"type":"tool_await_approval"`, `"id":"c1"`}, nil},
		{Event{Type: EventToolResult, ID: "c1", Output: "ok", Truncated: true},
			[]string{`"type":"tool_result"`, `"output":"ok"`, `"truncated":true`}, nil},
		{Event{Type: EventDone, Usage: &Usage{PromptTokens: 3, CompletionTokens: 4}},
			[]string{`"type":"done"`, `"prompt_tokens":3`, `"completion_tokens":4`}, nil},
		{Event{Type: EventError, Message: "boom"}, []string{`"type":"error"`, `"message":"boom"`}, nil},
	}
	for _, tc := range tests {
		b, err := json.Marshal(tc.ev)
		if err != nil {
			t.Fatalf("marshalling %s: %v", tc.ev.Type, err)
		}
		got := string(b)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s event = %s, want it to contain %s", tc.ev.Type, got, want)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(got, `"`+absent+`"`) {
				t.Errorf("%s event = %s, want no %q field", tc.ev.Type, got, absent)
			}
		}
	}
}
