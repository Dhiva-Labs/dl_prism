package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// session drives a Server over in-memory pipes, exactly as a host drives it
// over stdio.
type session struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan string
	done  chan error
}

func newSession(t *testing.T, s *Server) *session {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	sess := &session{t: t, in: inW, lines: make(chan string, 32), done: make(chan error, 1)}

	go func() {
		err := s.Serve(inR, outW)
		outW.Close()
		sess.done <- err
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 64*1024), 8<<20)
		for sc.Scan() {
			sess.lines <- sc.Text()
		}
		close(sess.lines)
	}()

	t.Cleanup(sess.close)
	return sess
}

// send writes one JSON-RPC message, as one line.
func (s *session) send(v any) {
	s.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		s.t.Fatalf("marshalling request: %v", err)
	}
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		s.t.Fatalf("writing to the server: %v", err)
	}
}

// sendRaw writes a line verbatim, for malformed-input tests.
func (s *session) sendRaw(line string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, line+"\n"); err != nil {
		s.t.Fatalf("writing to the server: %v", err)
	}
}

// recv reads one response line.
func (s *session) recv() map[string]any {
	s.t.Helper()
	select {
	case line, ok := <-s.lines:
		if !ok {
			s.t.Fatal("the server closed its output without answering")
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			s.t.Fatalf("response is not JSON: %v (%s)", err, line)
		}
		if v["jsonrpc"] != "2.0" {
			s.t.Errorf("jsonrpc = %v, want 2.0 (%s)", v["jsonrpc"], line)
		}
		return v
	case <-time.After(5 * time.Second):
		s.t.Fatal("timed out waiting for a response")
		return nil
	}
}

// result returns a successful response's result, failing on an error reply.
func (s *session) result(resp map[string]any) map[string]any {
	s.t.Helper()
	if e, ok := resp["error"]; ok {
		s.t.Fatalf("unexpected JSON-RPC error: %v", e)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		s.t.Fatalf("response has no result object: %v", resp)
	}
	return res
}

// rpcErr returns an error response's error object.
func (s *session) rpcErr(resp map[string]any) map[string]any {
	s.t.Helper()
	e, ok := resp["error"].(map[string]any)
	if !ok {
		s.t.Fatalf("response is not an error: %v", resp)
	}
	return e
}

func (s *session) close() {
	s.in.Close()
	select {
	case err := <-s.done:
		if err != nil {
			s.t.Errorf("Serve returned %v, want a clean EOF exit", err)
		}
	case <-time.After(5 * time.Second):
		s.t.Error("Serve did not return after stdin closed")
	}
}

// initialize runs the handshake and returns its result.
func (s *session) initialize(version string) map[string]any {
	s.t.Helper()
	s.send(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-host", "version": "1"},
		},
	})
	return s.result(s.recv())
}

// callTool runs tools/call and returns the joined text plus the error flag.
func (s *session) callTool(id int, name string, args map[string]any) (string, bool) {
	s.t.Helper()
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params})
	res := s.result(s.recv())

	content, ok := res["content"].([]any)
	if !ok {
		s.t.Fatalf("tools/call result has no content array: %v", res)
	}
	var text strings.Builder
	for _, c := range content {
		block := c.(map[string]any)
		if block["type"] != "text" {
			s.t.Errorf("content block type = %v, want text", block["type"])
		}
		text.WriteString(block["text"].(string))
	}
	isErr, _ := res["isError"].(bool)
	return text.String(), isErr
}

// fakeGateway is a stand-in for a running prism serve.
type fakeGateway struct {
	*httptest.Server
	mu       sync.Mutex
	lastBody []byte
	lastAuth string
	lastPath string
}

func newGateway(t *testing.T) *fakeGateway {
	t.Helper()
	g := &fakeGateway{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		g.capture(r)
		w.Header().Set("X-Prism-Mode", "auto")
		w.Header().Set("X-Prism-Provider", "ollama")
		w.Header().Set("X-Prism-Model", "qwen-coder")
		w.Header().Set("X-Prism-Reason", "rule 1 matched: task=code")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"cmpl-1","object":"chat.completion","model":"qwen-coder",
		  "choices":[{"index":0,"message":{"role":"assistant","content":"42"},"finish_reason":"stop"}],
		  "usage":{"prompt_tokens":5,"completion_tokens":1}}`)
	})
	mux.HandleFunc("POST /v1/route/explain", func(w http.ResponseWriter, r *http.Request) {
		g.capture(r)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"mode":"auto","reason":"rule 1 matched: task=code","chain":[{"provider":"ollama","model":"qwen-coder","local":true}]}`)
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		g.capture(r)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[
		  {"id":"auto","owned_by":"prism","prism":{}},
		  {"id":"qwen-coder","owned_by":"ollama","prism":{"local":true,"context_window":32768}},
		  {"id":"claude-sonnet-4-5","owned_by":"anthropic","prism":{"local":false,"context_window":200000}}]}`)
	})
	mux.HandleFunc("GET /admin/usage", func(w http.ResponseWriter, r *http.Request) {
		g.capture(r)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"month_total_usd":1.25,"budget_usd":50,"budget_exceeded":false,
		  "models":[{"model":"claude-sonnet-4-5","provider":"anthropic","requests":3,
		             "prompt_tokens":1200,"completion_tokens":400,"cost_usd":1.25}]}`)
	})
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGateway) capture(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastBody, g.lastAuth, g.lastPath = body, r.Header.Get("Authorization"), r.URL.Path
}

func (g *fakeGateway) sent(t *testing.T) map[string]any {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	var v map[string]any
	if err := json.Unmarshal(g.lastBody, &v); err != nil {
		t.Fatalf("gateway received a body that is not JSON: %v (%s)", err, g.lastBody)
	}
	return v
}

func (g *fakeGateway) auth() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastAuth
}

func newServer(gateway string) *Server {
	return &Server{Gateway: gateway, Version: "test", HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func TestInitializeHandshake(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	res := s.initialize("2025-06-18")

	if res["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's supported version echoed back", res["protocolVersion"])
	}
	caps, ok := res["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities = %v, want an object", res["capabilities"])
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities = %v, want a tools capability", caps)
	}
	info, ok := res["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo = %v, want an object", res["serverInfo"])
	}
	if info["name"] != "dl-prism" {
		t.Errorf("serverInfo.name = %v, want dl-prism", info["name"])
	}
	if info["version"] != "test" {
		t.Errorf("serverInfo.version = %v, want the server's version", info["version"])
	}
}

// An unknown protocol revision gets the one we actually speak.
func TestInitializeUnsupportedVersionFallsBack(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	res := s.initialize("1999-01-01")
	if res["protocolVersion"] != defaultProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", res["protocolVersion"], defaultProtocolVersion)
	}
}

// An older supported revision is echoed rather than upgraded.
func TestInitializeEchoesOlderSupportedVersion(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	res := s.initialize("2024-11-05")
	if res["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want the client's 2024-11-05", res["protocolVersion"])
	}
}

func TestToolsListShape(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.initialize("2025-06-18")
	s.send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	res := s.result(s.recv())

	list, ok := res["tools"].([]any)
	if !ok {
		t.Fatalf("result has no tools array: %v", res)
	}
	byName := map[string]map[string]any{}
	for _, item := range list {
		tool := item.(map[string]any)
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tool without a name: %v", tool)
		}
		if desc, _ := tool["description"].(string); desc == "" {
			t.Errorf("tool %s has no description", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s has no inputSchema object", name)
		}
		if schema["type"] != "object" {
			t.Errorf("tool %s: inputSchema.type = %v, want object", name, schema["type"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Errorf("tool %s: inputSchema has no properties object", name)
		}
		byName[name] = tool
	}
	for _, want := range []string{"prism_chat", "prism_explain", "prism_models", "prism_usage"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("tools/list is missing %q (got %v)", want, byName)
		}
	}
	if len(byName) != 4 {
		t.Errorf("tools/list returned %d tools, want 4", len(byName))
	}

	// The declared requirements have to match what the tools actually need.
	for _, name := range []string{"prism_chat", "prism_explain"} {
		schema := byName[name]["inputSchema"].(map[string]any)
		req, _ := schema["required"].([]any)
		if len(req) != 1 || req[0] != "prompt" {
			t.Errorf("tool %s: required = %v, want [prompt]", name, schema["required"])
		}
		props := schema["properties"].(map[string]any)
		prompt, ok := props["prompt"].(map[string]any)
		if !ok || prompt["type"] != "string" {
			t.Errorf("tool %s: prompt property = %v, want a string", name, props["prompt"])
		}
	}
	for _, name := range []string{"prism_models", "prism_usage"} {
		schema := byName[name]["inputSchema"].(map[string]any)
		if req, ok := schema["required"]; ok {
			t.Errorf("tool %s takes no arguments but declares required = %v", name, req)
		}
	}
}

func TestToolCallChat(t *testing.T) {
	g := newGateway(t)
	s := newSession(t, newServer(g.URL))
	s.initialize("2025-06-18")

	text, isErr := s.callTool(3, "prism_chat", map[string]any{
		"prompt": "what is 6 times 7", "system": "answer with digits only",
	})
	if isErr {
		t.Fatalf("prism_chat reported an error: %s", text)
	}
	if !strings.Contains(text, "42") {
		t.Errorf("result = %q, want the model's reply", text)
	}
	want := "— routed ollama/qwen-coder (auto: rule 1 matched: task=code)"
	if !strings.Contains(text, want) {
		t.Errorf("result = %q, want the routing footer %q", text, want)
	}

	sent := g.sent(t)
	if sent["model"] != "auto" {
		t.Errorf("gateway model = %v, want auto by default", sent["model"])
	}
	if sent["stream"] != false {
		t.Errorf("gateway stream = %v, want false", sent["stream"])
	}
	msgs, _ := sent["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("gateway messages = %v, want the system prompt plus the user turn", msgs)
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "answer with digits only" {
		t.Errorf("first message = %v, want the system prompt", first)
	}
	second := msgs[1].(map[string]any)
	if second["role"] != "user" || second["content"] != "what is 6 times 7" {
		t.Errorf("second message = %v, want the user prompt", second)
	}
}

// An explicit model overrides the "auto" default.
func TestToolCallChatHonoursModel(t *testing.T) {
	g := newGateway(t)
	s := newSession(t, newServer(g.URL))
	s.callTool(1, "prism_chat", map[string]any{"prompt": "hi", "model": "qwen-coder"})
	if sent := g.sent(t); sent["model"] != "qwen-coder" {
		t.Errorf("gateway model = %v, want qwen-coder", sent["model"])
	}
}

func TestToolCallUsage(t *testing.T) {
	g := newGateway(t)
	s := newSession(t, newServer(g.URL))
	s.initialize("2025-06-18")

	text, isErr := s.callTool(4, "prism_usage", map[string]any{})
	if isErr {
		t.Fatalf("prism_usage reported an error: %s", text)
	}
	for _, want := range []string{"$1.2500", "$50.00 budget", "claude-sonnet-4-5", "anthropic", "1200", "1.2500"} {
		if !strings.Contains(text, want) {
			t.Errorf("usage output %q is missing %q", text, want)
		}
	}
}

func TestToolCallModels(t *testing.T) {
	g := newGateway(t)
	s := newSession(t, newServer(g.URL))

	text, isErr := s.callTool(1, "prism_models", nil)
	if isErr {
		t.Fatalf("prism_models reported an error: %s", text)
	}
	for _, want := range []string{"MODEL", "PROVIDER", "auto", "qwen-coder", "ollama", "32768", "yes"} {
		if !strings.Contains(text, want) {
			t.Errorf("models output %q is missing %q", text, want)
		}
	}
}

func TestToolCallExplain(t *testing.T) {
	g := newGateway(t)
	s := newSession(t, newServer(g.URL))

	text, isErr := s.callTool(1, "prism_explain", map[string]any{"prompt": "write a unit test"})
	if isErr {
		t.Fatalf("prism_explain reported an error: %s", text)
	}
	if !strings.Contains(text, "\n  \"mode\": \"auto\"") {
		t.Errorf("explain output = %q, want indented JSON", text)
	}
	if sent := g.sent(t); sent["model"] != "auto" {
		t.Errorf("gateway model = %v, want auto", sent["model"])
	}
}

// The API key rides along on gateway calls.
func TestGatewayAPIKeyIsSent(t *testing.T) {
	g := newGateway(t)
	srv := newServer(g.URL)
	srv.APIKey = "sk-prism-test"
	s := newSession(t, srv)

	s.callTool(1, "prism_models", nil)
	if got := g.auth(); got != "Bearer sk-prism-test" {
		t.Errorf("Authorization = %q, want the configured bearer token", got)
	}
}

// A gateway that is not running is a tool failure, not a protocol failure:
// the model must see a message it can act on.
func TestToolCallUnreachableGateway(t *testing.T) {
	// Port 1 is privileged and unbound, so the dial fails immediately.
	s := newSession(t, newServer("http://127.0.0.1:1"))

	text, isErr := s.callTool(1, "prism_chat", map[string]any{"prompt": "hi"})
	if !isErr {
		t.Fatalf("isError = false, want a tool error for an unreachable gateway (%s)", text)
	}
	for _, want := range []string{"cannot reach the Prism gateway", "prism serve", "--gateway"} {
		if !strings.Contains(text, want) {
			t.Errorf("error text %q is missing %q", text, want)
		}
	}
}

// A gateway error status is relayed with its message.
func TestToolCallGatewayError(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid or missing API key","type":"auth_error"}}`)
	}))
	defer bad.Close()

	s := newSession(t, newServer(bad.URL))
	text, isErr := s.callTool(1, "prism_usage", nil)
	if !isErr {
		t.Fatalf("isError = false, want a tool error (%s)", text)
	}
	if !strings.Contains(text, "invalid or missing API key") {
		t.Errorf("error text = %q, want the gateway's message", text)
	}
	if !strings.Contains(text, "PRISM_API_KEY") {
		t.Errorf("error text = %q, want the 401 to hint at the api key options", text)
	}
}

// A missing required argument is a protocol error: the schema said it was
// required.
func TestToolCallMissingRequiredArgument(t *testing.T) {
	g := newGateway(t)
	s := newSession(t, newServer(g.URL))
	s.send(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": "prism_chat", "arguments": map[string]any{}}})

	e := s.rpcErr(s.recv())
	if e["code"] != float64(codeInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], codeInvalidParams)
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "prompt") {
		t.Errorf("message = %q, want it to name the missing argument", msg)
	}
}

func TestToolCallUnknownTool(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.send(map[string]any{"jsonrpc": "2.0", "id": 8, "method": "tools/call",
		"params": map[string]any{"name": "prism_nope"}})

	e := s.rpcErr(s.recv())
	if e["code"] != float64(codeInvalidParams) {
		t.Errorf("code = %v, want %d", e["code"], codeInvalidParams)
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "prism_nope") {
		t.Errorf("message = %q, want it to name the unknown tool", msg)
	}
}

func TestUnknownMethod(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.send(map[string]any{"jsonrpc": "2.0", "id": 9, "method": "resources/list"})

	resp := s.recv()
	if resp["id"] != float64(9) {
		t.Errorf("id = %v, want the request's id echoed", resp["id"])
	}
	e := s.rpcErr(resp)
	if e["code"] != float64(codeMethodNotFound) {
		t.Errorf("code = %v, want %d (method not found)", e["code"], codeMethodNotFound)
	}
}

func TestMalformedLineIsAParseError(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.sendRaw(`{"jsonrpc":"2.0","id":1,`)

	resp := s.recv()
	e := s.rpcErr(resp)
	if e["code"] != float64(codeParseError) {
		t.Errorf("code = %v, want %d (parse error)", e["code"], codeParseError)
	}
	if resp["id"] != nil {
		t.Errorf("id = %v, want null: the id could not be read", resp["id"])
	}
	// The connection survives: the next request still works.
	s.send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "ping"})
	if next := s.recv(); next["id"] != float64(2) {
		t.Errorf("id = %v, want the server to keep serving after a bad line", next["id"])
	}
}

// Notifications get no reply at all — the next response on the wire must be
// the one for the following request.
func TestNotificationsGetNoResponse(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.initialize("2025-06-18")

	s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled",
		"params": map[string]any{"requestId": 1}})
	// An unknown notification must not produce an error response either.
	s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/unheard_of"})
	s.send(map[string]any{"jsonrpc": "2.0", "id": 42, "method": "ping"})

	resp := s.recv()
	if resp["id"] != float64(42) {
		t.Fatalf("next response id = %v, want 42: a notification was answered", resp["id"])
	}
	if len(s.result(resp)) != 0 {
		t.Errorf("ping result = %v, want an empty object", resp["result"])
	}
}

// Blank lines between messages are ignored rather than treated as garbage.
func TestBlankLinesAreIgnored(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.sendRaw("")
	s.sendRaw("   ")
	s.send(map[string]any{"jsonrpc": "2.0", "id": 5, "method": "ping"})
	if resp := s.recv(); resp["id"] != float64(5) {
		t.Errorf("id = %v, want 5", resp["id"])
	}
}

// Every response is exactly one line: the stdio transport has no other framing.
func TestResponsesAreNewlineDelimited(t *testing.T) {
	g := newGateway(t)
	var out strings.Builder
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"prism_models"}}` + "\n")

	if err := newServer(g.URL).Serve(in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d output lines, want 3 (the notification is unanswered):\n%s", len(lines), out.String())
	}
	for i, line := range lines {
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Errorf("line %d is not one JSON object: %v (%s)", i, err, line)
		}
	}
}

// A request without a method is an invalid request, not a crash.
func TestRequestWithoutMethod(t *testing.T) {
	s := newSession(t, newServer("http://127.0.0.1:1"))
	s.sendRaw(`{"jsonrpc":"2.0","id":11}`)
	e := s.rpcErr(s.recv())
	if e["code"] != float64(codeInvalidRequest) {
		t.Errorf("code = %v, want %d (invalid request)", e["code"], codeInvalidRequest)
	}
}
