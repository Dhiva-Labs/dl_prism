package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dhivadhiva/dl_prism/internal/config"
	"github.com/dhivadhiva/dl_prism/internal/workbench"
)

// newWorkbenchFixture points the workbench's data directory at a temporary
// one before the handler is built, so no test touches the real
// ~/.local/share/prism.
func newWorkbenchFixture(t *testing.T, tweak ...func(*config.Config)) *fixture {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	return newFixture(t, tweak...)
}

// req runs one request with an arbitrary method and body.
func (f *fixture) req(method, path, body string, headers ...[2]string) *httptest.ResponseRecorder {
	f.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

func decodeInto(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
}

// makeProject creates a project through the API and returns it.
func makeProject(t *testing.T, f *fixture, body string) workbench.Project {
	t.Helper()
	w := f.post("/admin/projects", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /admin/projects = %d (%s)", w.Code, w.Body.String())
	}
	var p workbench.Project
	decodeInto(t, w, &p)
	return p
}

func makeChat(t *testing.T, f *fixture, projectID string) workbench.Chat {
	t.Helper()
	w := f.post("/admin/projects/"+projectID+"/chats", `{"title":"first"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST chats = %d (%s)", w.Code, w.Body.String())
	}
	var c workbench.Chat
	decodeInto(t, w, &c)
	return c
}

// --- projects --------------------------------------------------------

func TestWorkbenchProjectCRUD(t *testing.T) {
	f := newWorkbenchFixture(t)

	// Empty to start with, and [] rather than null.
	w := f.get("/admin/projects")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/projects = %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"projects":[]`) {
		t.Errorf("empty list = %s, want an empty projects array", w.Body.String())
	}

	p := makeProject(t, f, `{"name":"Research","settings":{"agent_enabled":true,"approval_mode":"always","model":"gpt-test","system_prompt":"be terse"}}`)
	if p.ID == "" {
		t.Fatal("the created project has no id")
	}
	if p.Name != "Research" || !p.Settings.AgentEnabled || p.Settings.ApprovalMode != "always" {
		t.Errorf("created project = %+v", p)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Error("the created project has no timestamps")
	}

	// Defaults when settings are omitted.
	plain := makeProject(t, f, `{"name":"Plain"}`)
	if plain.Settings.ApprovalMode != "mutations" || plain.Settings.AgentEnabled {
		t.Errorf("default settings = %+v, want mutations and tools off", plain.Settings)
	}

	var list struct {
		Projects []workbench.Project `json:"projects"`
	}
	decodeInto(t, f.get("/admin/projects"), &list)
	if len(list.Projects) != 2 {
		t.Fatalf("%d projects listed, want 2", len(list.Projects))
	}

	var got workbench.Project
	decodeInto(t, f.get("/admin/projects/"+p.ID), &got)
	if got.ID != p.ID || got.Name != "Research" {
		t.Errorf("GET project = %+v", got)
	}

	w = f.req(http.MethodPatch, "/admin/projects/"+p.ID, `{"settings":{"approval_mode":"never"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d (%s)", w.Code, w.Body.String())
	}
	var patched workbench.Project
	decodeInto(t, w, &patched)
	if patched.Settings.ApprovalMode != "never" {
		t.Errorf("approval_mode = %q, want never", patched.Settings.ApprovalMode)
	}
	if patched.Settings.Model != "gpt-test" || patched.Settings.SystemPrompt != "be terse" {
		t.Errorf("a partial PATCH cleared other settings: %+v", patched.Settings)
	}
	if !patched.Settings.AgentEnabled {
		t.Error("a partial PATCH turned the agent off")
	}

	w = f.req(http.MethodDelete, "/admin/projects/"+p.ID, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d (%s)", w.Code, w.Body.String())
	}
	if w := f.get("/admin/projects/" + p.ID); w.Code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", w.Code)
	}
	if w := f.req(http.MethodDelete, "/admin/projects/"+p.ID, ""); w.Code != http.StatusNotFound {
		t.Errorf("second DELETE = %d, want 404", w.Code)
	}
}

func TestWorkbenchProjectValidation(t *testing.T) {
	f := newWorkbenchFixture(t)
	tests := []struct {
		name string
		body string
	}{
		{"no name", `{"name":"  "}`},
		{"bad approval mode", `{"name":"x","settings":{"approval_mode":"whenever"}}`},
		{"malformed json", `{`},
		{"empty body", ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := f.post("/admin/projects", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (%s)", w.Code, w.Body.String())
			}
		})
	}
	for _, path := range []string{"/admin/projects/nope", "/admin/projects/0123456789abcdef"} {
		if w := f.get(path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}
	// A traversal attempt never resolves to a project: the mux redirects the
	// cleaned path and the store refuses the id either way.
	if w := f.get("/admin/projects/%2e%2e%2f%2e%2e"); w.Code == http.StatusOK {
		t.Errorf("an encoded traversal returned 200: %s", w.Body.String())
	}
}

// Project cards show how many conversations a project holds, so the count
// rides along on both the list and the single-project read.
func TestWorkbenchProjectChatCount(t *testing.T) {
	f := newWorkbenchFixture(t)
	busy := makeProject(t, f, `{"name":"Busy"}`)
	idle := makeProject(t, f, `{"name":"Idle"}`)

	if busy.ChatCount != 0 {
		t.Errorf("a new project reports chat_count = %d, want 0", busy.ChatCount)
	}
	// The field is always present, including as zero.
	if !strings.Contains(f.get("/admin/projects/"+idle.ID).Body.String(), `"chat_count":0`) {
		t.Errorf("single project = %s, want an explicit chat_count", f.get("/admin/projects/"+idle.ID).Body.String())
	}

	first := makeChat(t, f, busy.ID)
	makeChat(t, f, busy.ID)

	var single workbench.Project
	decodeInto(t, f.get("/admin/projects/"+busy.ID), &single)
	if single.ChatCount != 2 {
		t.Errorf("GET project chat_count = %d, want 2", single.ChatCount)
	}

	var list struct {
		Projects []workbench.Project `json:"projects"`
	}
	decodeInto(t, f.get("/admin/projects"), &list)
	counts := map[string]int{}
	for _, p := range list.Projects {
		counts[p.Name] = p.ChatCount
	}
	if counts["Busy"] != 2 || counts["Idle"] != 0 {
		t.Errorf("listed chat counts = %v, want Busy=2 Idle=0", counts)
	}

	// It tracks deletions rather than being frozen at creation.
	if w := f.req(http.MethodDelete, "/admin/chats/"+first.ID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE chat = %d", w.Code)
	}
	decodeInto(t, f.get("/admin/projects/"+busy.ID), &single)
	if single.ChatCount != 1 {
		t.Errorf("chat_count after a delete = %d, want 1", single.ChatCount)
	}
}

// --- chats -----------------------------------------------------------

func TestWorkbenchChatCRUD(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f, `{"name":"proj"}`)

	c := makeChat(t, f, p.ID)
	if c.Title != "first" || c.ProjectID != p.ID {
		t.Errorf("created chat = %+v", c)
	}
	// A titleless create is legal.
	w := f.post("/admin/projects/"+p.ID+"/chats", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("POST chats without a body = %d (%s)", w.Code, w.Body.String())
	}

	var list struct {
		Chats []workbench.Chat `json:"chats"`
	}
	decodeInto(t, f.get("/admin/projects/"+p.ID+"/chats"), &list)
	if len(list.Chats) != 2 {
		t.Fatalf("%d chats listed, want 2", len(list.Chats))
	}

	var single struct {
		Chat   workbench.Chat    `json:"chat"`
		Events []workbench.Event `json:"events"`
	}
	decodeInto(t, f.get("/admin/chats/"+c.ID), &single)
	if single.Chat.ID != c.ID {
		t.Errorf("GET chat = %+v", single.Chat)
	}
	if single.Events == nil {
		t.Error("events is null; the API must send an empty array")
	}

	if w := f.req(http.MethodDelete, "/admin/chats/"+c.ID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE chat = %d (%s)", w.Code, w.Body.String())
	}
	if w := f.get("/admin/chats/" + c.ID); w.Code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", w.Code)
	}
	if w := f.post("/admin/projects/0123456789abcdef/chats", `{}`); w.Code != http.StatusNotFound {
		t.Errorf("POST chat on an unknown project = %d, want 404", w.Code)
	}
}

// --- attached knowledge ----------------------------------------------

func multipartUpload(t *testing.T, field, filename, content string) (string, io.Reader) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw.FormDataContentType(), &buf
}

func TestWorkbenchFileUpload(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f, `{"name":"proj"}`)

	ct, body := multipartUpload(t, "file", "brief.md", "ship by friday")
	req := httptest.NewRequest(http.MethodPost, "/admin/projects/"+p.ID+"/files", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d (%s)", w.Code, w.Body.String())
	}
	var uploaded workbench.File
	decodeInto(t, w, &uploaded)
	if uploaded.Name != "brief.md" || uploaded.Size != 14 {
		t.Errorf("uploaded = %+v", uploaded)
	}
	if !uploaded.InContext {
		t.Error("a small text attachment should be folded into the prompt")
	}

	// A raw body upload with ?name= works too.
	w = f.post("/admin/projects/"+p.ID+"/files?name=data.csv", "a,b\n1,2\n")
	if w.Code != http.StatusCreated {
		t.Fatalf("raw upload = %d (%s)", w.Code, w.Body.String())
	}

	var list struct {
		Files []workbench.File `json:"files"`
	}
	decodeInto(t, f.get("/admin/projects/"+p.ID+"/files"), &list)
	if len(list.Files) != 2 {
		t.Fatalf("%d files listed, want 2", len(list.Files))
	}

	w = f.get("/admin/projects/" + p.ID + "/files/brief.md")
	if w.Code != http.StatusOK {
		t.Fatalf("download = %d (%s)", w.Code, w.Body.String())
	}
	if w.Body.String() != "ship by friday" {
		t.Errorf("download = %q", w.Body.String())
	}
	// User-supplied bytes must never be served as renderable markup.
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("attachments are served without nosniff")
	}

	if w := f.req(http.MethodDelete, "/admin/projects/"+p.ID+"/files?name=brief.md", ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE file = %d (%s)", w.Code, w.Body.String())
	}
	if w := f.get("/admin/projects/" + p.ID + "/files/brief.md"); w.Code != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", w.Code)
	}
	if w := f.req(http.MethodDelete, "/admin/projects/"+p.ID+"/files", ""); w.Code != http.StatusBadRequest {
		t.Errorf("DELETE without ?name= = %d, want 400", w.Code)
	}
	// The RESTful spelling deletes too, so a client need not guess.
	if w := f.req(http.MethodDelete, "/admin/projects/"+p.ID+"/files/data.csv", ""); w.Code != http.StatusNoContent {
		t.Errorf("DELETE by sub-path = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	var left struct {
		Files []workbench.File `json:"files"`
	}
	decodeInto(t, f.get("/admin/projects/"+p.ID+"/files"), &left)
	if len(left.Files) != 0 {
		t.Errorf("%d files left, want none", len(left.Files))
	}
}

func TestWorkbenchFileUploadLimits(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f, `{"name":"proj"}`)

	// No name at all.
	if w := f.post("/admin/projects/"+p.ID+"/files", "body without a name"); w.Code != http.StatusBadRequest {
		t.Errorf("nameless upload = %d, want 400", w.Code)
	}
	// A multipart body without the file field.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("notfile", "x")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/projects/"+p.ID+"/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("multipart without a file field = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// Past the 10 MB cap.
	ct, body := multipartUpload(t, "file", "huge.bin", strings.Repeat("x", (10<<20)+1024))
	req = httptest.NewRequest(http.MethodPost, "/admin/projects/"+p.ID+"/files", body)
	req.Header.Set("Content-Type", ct)
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized upload = %d, want 413 (%s)", w.Code, w.Body.String())
	}
	if w := f.get("/admin/projects/" + p.ID + "/files"); strings.Contains(w.Body.String(), "huge.bin") {
		t.Error("the oversized upload was stored anyway")
	}
}

// --- auth ------------------------------------------------------------

func TestWorkbenchRoutesRequireAuth(t *testing.T) {
	withKeys := func(cfg *config.Config) { cfg.Server.APIKeys = []string{"sk-prism-test"} }
	f := newWorkbenchFixture(t, withKeys)

	paths := [][3]string{
		{http.MethodGet, "/admin/projects", ""},
		{http.MethodPost, "/admin/projects", `{"name":"x"}`},
		{http.MethodGet, "/admin/projects/0123456789abcdef", ""},
		{http.MethodPatch, "/admin/projects/0123456789abcdef", `{"name":"y"}`},
		{http.MethodDelete, "/admin/projects/0123456789abcdef", ""},
		{http.MethodGet, "/admin/projects/0123456789abcdef/chats", ""},
		{http.MethodPost, "/admin/projects/0123456789abcdef/chats", `{}`},
		{http.MethodGet, "/admin/chats/0123456789abcdef", ""},
		{http.MethodDelete, "/admin/chats/0123456789abcdef", ""},
		{http.MethodGet, "/admin/projects/0123456789abcdef/files", ""},
		{http.MethodPost, "/admin/projects/0123456789abcdef/files?name=a.txt", "x"},
		{http.MethodDelete, "/admin/projects/0123456789abcdef/files?name=a.txt", ""},
		{http.MethodGet, "/admin/projects/0123456789abcdef/files/a.txt", ""},
		{http.MethodPost, "/v1/agent/run", `{"chat_id":"0123456789abcdef","message":"hi"}`},
		{http.MethodPost, "/v1/agent/approve", `{"chat_id":"0123456789abcdef","tool_call_id":"c1","approve":true}`},
	}
	for _, p := range paths {
		t.Run(p[0]+" "+p[1], func(t *testing.T) {
			w := f.req(p[0], p[1], p[2])
			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (%s)", w.Code, w.Body.String())
			}
			// With a key the route answers something other than 401.
			w = f.req(p[0], p[1], p[2], [2]string{"Authorization", "Bearer sk-prism-test"})
			if w.Code == http.StatusUnauthorized {
				t.Errorf("authenticated status = %d, want the request to be let through", w.Code)
			}
		})
	}
}

// --- the agent run ---------------------------------------------------

// agentEvent is one parsed frame of the agent stream.
type agentEvent struct {
	name string
	data workbench.Event
}

// parseSSE splits an agent stream into its frames, checking the framing.
func parseSSE(t *testing.T, body string) []agentEvent {
	t.Helper()
	var out []agentEvent
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var ev agentEvent
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				payload := strings.TrimPrefix(line, "data: ")
				if err := json.Unmarshal([]byte(payload), &ev.data); err != nil {
					t.Fatalf("event data is not JSON: %v (%s)", err, payload)
				}
			default:
				t.Fatalf("unexpected SSE line %q", line)
			}
		}
		if ev.name == "" {
			t.Fatalf("frame %q has no event name", frame)
		}
		if ev.name != ev.data.Type {
			t.Errorf("frame names the event %q but its payload says %q", ev.name, ev.data.Type)
		}
		out = append(out, ev)
	}
	return out
}

func agentTypes(events []agentEvent) []string {
	var out []string
	for _, ev := range events {
		out = append(out, ev.name)
	}
	return out
}

// scriptUpstream makes the fake provider answer with each body in turn.
func scriptUpstream(t *testing.T, u *upstream, bodies ...string) {
	t.Helper()
	var mu sync.Mutex
	var n int
	u.serve(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		if i >= len(bodies) {
			t.Errorf("the upstream was called %d times, more than the script allows", i+1)
			http.Error(w, `{"error":{"message":"unscripted call"}}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, bodies[i])
		w.(http.Flusher).Flush()
	})
}

// chunk wraps an OpenAI streaming delta.
func chunk(delta string) string {
	return `data: {"id":"cmpl-a","object":"chat.completion.chunk","choices":[{"index":0,"delta":` +
		delta + `}]}` + "\n\n"
}

// toolCallStream scripts a model turn that asks for one tool, with the
// arguments split across chunks the way OpenAI really sends them.
func toolCallStream(id, name, argsHead, argsTail string, prompt, completion int) string {
	return chunk(`{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"`+id+
		`","type":"function","function":{"name":"`+name+`","arguments":""}}]}`) +
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":`+jsonString(argsHead)+`}}]}`) +
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":`+jsonString(argsTail)+`}}]}`) +
		fmt.Sprintf(`data: {"id":"cmpl-a","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],`+
			`"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`, prompt, completion) + "\n\n" +
		"data: [DONE]\n\n"
}

// textStream scripts a plain streamed answer.
func textStream(parts []string, prompt, completion int) string {
	var b strings.Builder
	b.WriteString(chunk(`{"role":"assistant","content":""}`))
	for _, p := range parts {
		b.WriteString(chunk(`{"content":` + jsonString(p) + `}`))
	}
	fmt.Fprintf(&b, `data: {"id":"cmpl-a","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+
		`"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`+"\n\n", prompt, completion)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// waitForToolCall blocks until a chat's stored timeline shows a tool call,
// which the agent records immediately before it parks for approval.
func waitForToolCall(t *testing.T, f *fixture, chatID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var stored struct {
			Events []workbench.Event `json:"events"`
		}
		w := f.get("/admin/chats/" + chatID)
		if w.Code == http.StatusOK {
			decodeInto(t, w, &stored)
			for _, ev := range stored.Events {
				if ev.Type == "tool_call" {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run never reached its tool call")
}

// A full agent turn through the real HTTP surface: the upstream asks for a
// tool, the tool runs in the project's sandbox, and the answer streams back.
func TestAgentRunEndToEnd(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f,
		`{"name":"proj","settings":{"agent_enabled":true,"approval_mode":"never","model":"gpt-test"}}`)
	c := makeChat(t, f, p.ID)

	scriptUpstream(t, f.primary,
		toolCallStream("call_1", "write_file", `{"path":"out.txt",`, `"content":"hello"}`, 120, 12),
		textStream([]string{"The file ", "is written."}, 200, 6),
	)

	w := f.post("/v1/agent/run",
		fmt.Sprintf(`{"chat_id":%q,"message":"write hello into out.txt"}`, c.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("run = %d (%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if !w.Flushed {
		t.Error("the agent stream was never flushed")
	}

	events := parseSSE(t, w.Body.String())
	want := []string{"tool_call", "tool_result", "message_delta", "message_delta", "done"}
	if got := agentTypes(events); len(got) != len(want) {
		t.Fatalf("event types = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("event types = %v, want %v", got, want)
			}
		}
	}

	call := events[0].data
	if call.ID != "call_1" || call.Name != "write_file" {
		t.Errorf("tool_call = %+v", call)
	}
	// The fragmented arguments were reassembled into one JSON object.
	var args struct{ Path, Content string }
	if err := json.Unmarshal(call.Args, &args); err != nil {
		t.Fatalf("tool_call args are not an object: %v (%s)", err, call.Args)
	}
	if args.Path != "out.txt" || args.Content != "hello" {
		t.Errorf("args = %+v, want the streamed fragments joined", args)
	}
	if !strings.Contains(events[1].data.Output, "wrote 5 bytes") {
		t.Errorf("tool_result = %q", events[1].data.Output)
	}
	if events[2].data.Text != "The file " || events[3].data.Text != "is written." {
		t.Errorf("message deltas = %q / %q", events[2].data.Text, events[3].data.Text)
	}
	done := events[4].data
	if done.Usage == nil || done.Usage.PromptTokens != 320 || done.Usage.CompletionTokens != 18 {
		t.Errorf("done usage = %+v, want both model calls summed (320/18)", done.Usage)
	}
	// The stream carries no X-Prism-* headers, so the done event has to say
	// which candidate answered.
	if done.Route == nil {
		t.Fatal("the done event carries no route")
	}
	if done.Route.Provider != "primary" || done.Route.Model != "gpt-test" {
		t.Errorf("route = %+v, want primary/gpt-test", done.Route)
	}
	if done.Route.Mode != "manual" || !strings.Contains(done.Route.Reason, "gpt-test") {
		t.Errorf("route = %+v, want the routing explanation", done.Route)
	}

	// Both model calls went through the normal chain, so both are billed.
	usage, err := f.ledger.Summary()
	if err != nil {
		t.Fatalf("ledger.Summary: %v", err)
	}
	if len(usage) != 1 {
		t.Fatalf("ledger = %+v, want one primary row", usage)
	}
	if usage[0].Provider != "primary" || usage[0].Model != "gpt-test" {
		t.Errorf("ledger row = %s/%s", usage[0].Provider, usage[0].Model)
	}
	if usage[0].Requests != 2 {
		t.Errorf("ledger requests = %d, want both agent model calls recorded", usage[0].Requests)
	}
	if usage[0].PromptTokens != 320 || usage[0].CompletionTokens != 18 {
		t.Errorf("ledger tokens = %d/%d, want 320/18 from the upstream's usage chunks",
			usage[0].PromptTokens, usage[0].CompletionTokens)
	}
	if usage[0].CostUSD <= 0 {
		t.Error("the agent's turns were not priced")
	}

	// The tools definition really went upstream.
	var sent struct {
		Tools []struct {
			Function struct{ Name string } `json:"function"`
		} `json:"tools"`
		Stream   bool `json:"stream"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(f.primary.LastBody(), &sent); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	if len(sent.Tools) != 5 {
		t.Errorf("%d tools sent upstream, want 5", len(sent.Tools))
	}
	if !sent.Stream {
		t.Error("the agent's model call was not a streaming request")
	}

	// The conversation is on disk and rereadable.
	var stored struct {
		Chat   workbench.Chat    `json:"chat"`
		Events []workbench.Event `json:"events"`
	}
	decodeInto(t, f.get("/admin/chats/"+c.ID), &stored)
	types := []string{}
	for _, ev := range stored.Events {
		types = append(types, ev.Type)
	}
	wantStored := []string{"message", "tool_call", "tool_result", "message"}
	if strings.Join(types, ",") != strings.Join(wantStored, ",") {
		t.Errorf("stored timeline = %v, want %v", types, wantStored)
	}
	if stored.Events[3].Content != "The file is written." {
		t.Errorf("stored answer = %q", stored.Events[3].Content)
	}
}

// An upstream that ignores "stream": true still yields a usable turn.
func TestAgentRunHandlesANonStreamingUpstream(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f, `{"name":"proj","settings":{"model":"gpt-test"}}`)
	c := makeChat(t, f, p.ID)
	f.primary.respond(http.StatusOK, okCompletion)

	w := f.post("/v1/agent/run", fmt.Sprintf(`{"chat_id":%q,"message":"hi"}`, c.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("run = %d (%s)", w.Code, w.Body.String())
	}
	events := parseSSE(t, w.Body.String())
	if len(events) != 2 || events[0].name != "message_delta" || events[1].name != "done" {
		t.Fatalf("event types = %v, want a delta then done", agentTypes(events))
	}
	if events[0].data.Text != "hello" {
		t.Errorf("text = %q, want the completion's content", events[0].data.Text)
	}
	if events[1].data.Usage.PromptTokens != 12 {
		t.Errorf("usage = %+v, want the completion's usage", events[1].data.Usage)
	}
}

// A run whose model call fails reports the reason on the stream, because the
// SSE response is already committed by then... unless nothing was sent yet,
// in which case it is a normal HTTP error.
func TestAgentRunReportsUpstreamFailure(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f, `{"name":"proj","settings":{"model":"gpt-test"}}`)
	c := makeChat(t, f, p.ID)
	f.primary.respond(http.StatusInternalServerError, `{"error":{"message":"primary exploded"}}`)
	f.backup.respond(http.StatusInternalServerError, `{"error":{"message":"backup exploded"}}`)

	w := f.post("/v1/agent/run", fmt.Sprintf(`{"chat_id":%q,"message":"hi"}`, c.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("run = %d (%s)", w.Code, w.Body.String())
	}
	events := parseSSE(t, w.Body.String())
	if len(events) != 1 || events[0].name != "error" {
		t.Fatalf("events = %v, want a single error", agentTypes(events))
	}
	if !strings.Contains(events[0].data.Message, "all candidates failed") {
		t.Errorf("error = %q, want the routing failure relayed", events[0].data.Message)
	}
	if usage, _ := f.ledger.Summary(); len(usage) != 0 {
		t.Errorf("ledger = %+v, want nothing billed for a failed turn", usage)
	}
}

func TestAgentRunRejectsBadRequests(t *testing.T) {
	f := newWorkbenchFixture(t)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"no chat id", `{"message":"hi"}`, http.StatusBadRequest},
		{"unknown chat", `{"chat_id":"0123456789abcdef","message":"hi"}`, http.StatusNotFound},
		{"malformed", `{`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := f.post("/v1/agent/run", tc.body)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (%s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want a JSON error envelope", ct)
			}
		})
	}

	// An empty message is rejected before anything is streamed.
	p := makeProject(t, f, `{"name":"proj"}`)
	c := makeChat(t, f, p.ID)
	w := f.post("/v1/agent/run", fmt.Sprintf(`{"chat_id":%q,"message":"   "}`, c.ID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty message = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestAgentApproveValidation(t *testing.T) {
	f := newWorkbenchFixture(t)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"missing ids", `{"approve":true}`, http.StatusBadRequest},
		{"no pending call", `{"chat_id":"0123456789abcdef","tool_call_id":"c1","approve":true}`, http.StatusNotFound},
		{"malformed", `{`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := f.post("/v1/agent/approve", tc.body)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (%s)", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// The approval round trip over real HTTP: the run parks mid-stream, a second
// request approves it, and the run finishes.
func TestAgentApprovalOverHTTP(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f,
		`{"name":"proj","settings":{"agent_enabled":true,"approval_mode":"mutations","model":"gpt-test"}}`)
	c := makeChat(t, f, p.ID)
	scriptUpstream(t, f.primary,
		toolCallStream("call_1", "shell", `{"command":`, `"echo approved"}`, 10, 2),
		textStream([]string{"it said approved"}, 20, 3),
	)

	srv := httptest.NewServer(f.handler)
	defer srv.Close()

	type result struct {
		body string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		res, err := srv.Client().Post(srv.URL+"/v1/agent/run", "application/json",
			strings.NewReader(fmt.Sprintf(`{"chat_id":%q,"message":"run echo"}`, c.ID)))
		if err != nil {
			done <- result{err: err}
			return
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		done <- result{body: string(b), err: err}
	}()

	// Wait for the run to park, then approve.
	deadline := time.Now().Add(5 * time.Second)
	var approved bool
	for time.Now().Before(deadline) {
		res, err := srv.Client().Post(srv.URL+"/v1/agent/approve", "application/json",
			strings.NewReader(fmt.Sprintf(`{"chat_id":%q,"tool_call_id":"call_1","approve":true}`, c.ID)))
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			if !strings.Contains(string(body), `"ok":true`) {
				t.Errorf("approve body = %s", body)
			}
			approved = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !approved {
		t.Fatal("the run never parked on an approval")
	}

	got := <-done
	if got.err != nil {
		t.Fatalf("run: %v", got.err)
	}
	events := parseSSE(t, got.body)
	want := []string{"tool_call", "tool_await_approval", "tool_result", "message_delta", "done"}
	if strings.Join(agentTypes(events), ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %v, want %v", agentTypes(events), want)
	}
	if !strings.Contains(events[2].data.Output, "approved") {
		t.Errorf("tool_result = %q, want the command output", events[2].data.Output)
	}
}

// A denial reaches the model as a tool result, and a second run while one is
// parked is a 409.
func TestAgentDenialAndConcurrentRunConflict(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f,
		`{"name":"proj","settings":{"agent_enabled":true,"approval_mode":"always","model":"gpt-test"}}`)
	c := makeChat(t, f, p.ID)
	scriptUpstream(t, f.primary,
		toolCallStream("call_1", "shell", `{"command":`, `"rm -rf ."}`, 10, 2),
		textStream([]string{"understood"}, 20, 3),
	)

	srv := httptest.NewServer(f.handler)
	defer srv.Close()

	body := fmt.Sprintf(`{"chat_id":%q,"message":"delete everything"}`, c.ID)
	done := make(chan string, 1)
	go func() {
		res, err := srv.Client().Post(srv.URL+"/v1/agent/run", "application/json", strings.NewReader(body))
		if err != nil {
			done <- "ERR " + err.Error()
			return
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		done <- string(b)
	}()

	// Wait for the run to reach its gated tool call, which it records before
	// parking.
	waitForToolCall(t, f, c.ID)

	// While that run holds the chat, a second one is refused outright: the
	// 409 comes back immediately, with no stream attached.
	res, err := srv.Client().Post(srv.URL+"/v1/agent/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second run = %d (%s), want 409", res.StatusCode, b)
	}
	if !strings.Contains(string(b), "already active") {
		t.Errorf("409 body = %s, want it to explain the conflict", b)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("409 Content-Type = %q, want a JSON error envelope, not a stream", ct)
	}

	res, err = srv.Client().Post(srv.URL+"/v1/agent/approve", "application/json",
		strings.NewReader(fmt.Sprintf(`{"chat_id":%q,"tool_call_id":"call_1","approve":false}`, c.ID)))
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("deny = %d", res.StatusCode)
	}

	stream := <-done
	events := parseSSE(t, stream)
	want := []string{"tool_call", "tool_await_approval", "tool_result", "message_delta", "done"}
	if strings.Join(agentTypes(events), ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %v, want %v", agentTypes(events), want)
	}
	if events[2].data.Output != "denied by user" {
		t.Errorf("tool_result = %q, want the denial", events[2].data.Output)
	}
	if events[2].data.Approved == nil || *events[2].data.Approved {
		t.Errorf("tool_result approved = %v, want an explicit false", events[2].data.Approved)
	}
	if !strings.Contains(stream, `"approved":false`) {
		t.Errorf("the denied tool_result frame does not carry approved:false:\n%s", stream)
	}

	// The denied command never ran.
	var stored struct {
		Events []workbench.Event `json:"events"`
	}
	decodeInto(t, f.get("/admin/chats/"+c.ID), &stored)
	for _, ev := range stored.Events {
		if ev.Type == "tool_result" && ev.Output != "denied by user" {
			t.Errorf("a denied tool produced output: %q", ev.Output)
		}
	}
}

// The workbench's model calls must not leak their transparency headers onto
// the agent's own SSE response.
func TestAgentRunDoesNotLeakUpstreamHeaders(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f, `{"name":"proj","settings":{"model":"gpt-test"}}`)
	c := makeChat(t, f, p.ID)
	scriptUpstream(t, f.primary, textStream([]string{"hi"}, 1, 1))

	w := f.post("/v1/agent/run", fmt.Sprintf(`{"chat_id":%q,"message":"hi"}`, c.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("run = %d (%s)", w.Code, w.Body.String())
	}
	for _, h := range []string{"X-Prism-Mode", "X-Prism-Provider", "X-Prism-Model", "X-Prism-Reason"} {
		if got := w.Header().Get(h); got != "" {
			t.Errorf("%s = %q on the agent stream, want the per-turn headers kept off it", h, got)
		}
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want the stream's own type", ct)
	}
}

// A project's attached knowledge and system prompt reach the model.
func TestAgentRunSendsProjectContext(t *testing.T) {
	f := newWorkbenchFixture(t)
	p := makeProject(t, f,
		`{"name":"Apollo","settings":{"agent_enabled":true,"model":"gpt-test","system_prompt":"Answer briefly."}}`)
	c := makeChat(t, f, p.ID)
	if w := f.post("/admin/projects/"+p.ID+"/files?name=brief.md", "launch on tuesday"); w.Code != http.StatusCreated {
		t.Fatalf("upload = %d (%s)", w.Code, w.Body.String())
	}
	scriptUpstream(t, f.primary, textStream([]string{"ok"}, 1, 1))

	if w := f.post("/v1/agent/run", fmt.Sprintf(`{"chat_id":%q,"message":"when?"}`, c.ID)); w.Code != http.StatusOK {
		t.Fatalf("run = %d (%s)", w.Code, w.Body.String())
	}

	var sent struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(f.primary.LastBody(), &sent); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v, want a system message then the user's", sent.Messages)
	}
	for _, want := range []string{"Answer briefly.", "Project knowledge:", "launch on tuesday", "Apollo"} {
		if !strings.Contains(sent.Messages[0].Content, want) {
			t.Errorf("system message is missing %q:\n%s", want, sent.Messages[0].Content)
		}
	}
	if sent.Messages[1].Content != "when?" {
		t.Errorf("user message = %q", sent.Messages[1].Content)
	}
}

// --- the turn collector ----------------------------------------------

func TestTurnCollectorMergesStreamedToolCalls(t *testing.T) {
	col := &turnCollector{header: http.Header{}}
	col.header.Set("Content-Type", "text/event-stream")
	col.WriteHeader(http.StatusOK)

	stream := chunk(`{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"one","arguments":"{\"x\":"}}]}`) +
		chunk(`{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"two","arguments":"{}"}}]}`) +
		chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}`) +
		"data: [DONE]\n\n"
	// Feed it in awkward slices: SSE lines do not arrive whole.
	for i := 0; i < len(stream); i += 7 {
		end := min(i+7, len(stream))
		if _, err := col.Write([]byte(stream[i:end])); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	turn := col.turn()
	if len(turn.ToolCalls) != 2 {
		t.Fatalf("%d tool calls, want 2: %+v", len(turn.ToolCalls), turn.ToolCalls)
	}
	if turn.ToolCalls[0].ID != "a" || turn.ToolCalls[0].Name != "one" || turn.ToolCalls[0].Arguments != `{"x":1}` {
		t.Errorf("first call = %+v, want the fragments joined", turn.ToolCalls[0])
	}
	if turn.ToolCalls[1].ID != "b" || turn.ToolCalls[1].Name != "two" {
		t.Errorf("second call = %+v", turn.ToolCalls[1])
	}
}

func TestTurnCollectorReadsANonStreamedCompletion(t *testing.T) {
	col := &turnCollector{header: http.Header{}}
	col.header.Set("Content-Type", "application/json")
	col.WriteHeader(http.StatusOK)
	col.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"plain",
	    "tool_calls":[{"id":"z","type":"function","function":{"name":"shell","arguments":"{}"}}]}}],
	    "usage":{"prompt_tokens":7,"completion_tokens":2}}`))

	turn := col.turn()
	if turn.Text != "plain" {
		t.Errorf("text = %q", turn.Text)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "shell" || turn.ToolCalls[0].ID != "z" {
		t.Errorf("tool calls = %+v", turn.ToolCalls)
	}
	if turn.PromptTokens != 7 || turn.CompletionTokens != 2 {
		t.Errorf("usage = %d/%d, want 7/2", turn.PromptTokens, turn.CompletionTokens)
	}
}

func TestTurnCollectorReportsFailures(t *testing.T) {
	col := &turnCollector{header: http.Header{}}
	col.fail(nil, http.StatusBadGateway, "all candidates failed", "upstream_error")
	col.fail(nil, http.StatusTeapot, "a later message must not win", "x")
	err := col.failure()
	if err == nil || !strings.Contains(err.Error(), "all candidates failed") {
		t.Errorf("failure = %v, want the first reported reason", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("failure = %v, want the status included", err)
	}
	if (&turnCollector{}).failure() == nil {
		t.Error("a collector with no message still has to report an error")
	}
	// A stream cut off mid-turn reports as such, not as a silent failure.
	cut := &turnCollector{header: http.Header{}}
	cut.WriteHeader(http.StatusOK)
	if err := cut.failure(); err == nil || !strings.Contains(err.Error(), "ended before") {
		t.Errorf("failure after a partial stream = %v, want it to say the stream was cut", err)
	}
}
