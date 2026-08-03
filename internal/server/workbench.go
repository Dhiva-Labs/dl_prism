package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/dhivadhiva/dl_prism/internal/provider"
	"github.com/dhivadhiva/dl_prism/internal/workbench"
)

const (
	// maxWorkbenchBody bounds the JSON bodies of the workbench admin API;
	// uploads go through the multipart path with its own cap.
	maxWorkbenchBody = 1 << 20
	// maxUploadBytes caps one attached knowledge file.
	maxUploadBytes = 10 << 20
)

// workbenchRoutes owns the workbench's state. It lives in the closure the
// route registration builds rather than on Server, and opens the store on
// first use so a Server that never serves a workbench request never touches
// the data directory.
type workbenchRoutes struct {
	s *Server

	once  sync.Once
	store *workbench.Store
	agent *workbench.Agent
	err   error
}

// routeWorkbench registers the workbench API. Called from Handler.
func (s *Server) routeWorkbench(mux *http.ServeMux) {
	wb := &workbenchRoutes{s: s}

	mux.HandleFunc("GET /admin/projects", s.auth(wb.listProjects))
	mux.HandleFunc("POST /admin/projects", s.auth(wb.createProject))
	mux.HandleFunc("GET /admin/projects/{id}", s.auth(wb.getProject))
	mux.HandleFunc("PATCH /admin/projects/{id}", s.auth(wb.patchProject))
	mux.HandleFunc("DELETE /admin/projects/{id}", s.auth(wb.deleteProject))

	mux.HandleFunc("GET /admin/projects/{id}/chats", s.auth(wb.listChats))
	mux.HandleFunc("POST /admin/projects/{id}/chats", s.auth(wb.createChat))
	mux.HandleFunc("GET /admin/chats/{id}", s.auth(wb.getChat))
	mux.HandleFunc("DELETE /admin/chats/{id}", s.auth(wb.deleteChat))

	mux.HandleFunc("GET /admin/projects/{id}/files", s.auth(wb.listFiles))
	mux.HandleFunc("POST /admin/projects/{id}/files", s.auth(wb.uploadFile))
	mux.HandleFunc("DELETE /admin/projects/{id}/files", s.auth(wb.deleteFile))
	mux.HandleFunc("GET /admin/projects/{id}/files/{name}", s.auth(wb.getFile))
	// The contracted delete selects with ?name=; the RESTful spelling is
	// accepted too so a client need not guess.
	mux.HandleFunc("DELETE /admin/projects/{id}/files/{name}", s.auth(wb.deleteFile))

	mux.HandleFunc("POST /v1/agent/run", s.auth(wb.runAgent))
	mux.HandleFunc("POST /v1/agent/approve", s.auth(wb.approve))
}

// ready lazily opens the store and builds the agent, reporting a 500 once if
// the data directory is unusable.
func (wb *workbenchRoutes) ready(w http.ResponseWriter) bool {
	wb.once.Do(func() {
		dir, err := workbench.DefaultDir()
		if err != nil {
			wb.err = err
			return
		}
		store, err := workbench.Open(dir)
		if err != nil {
			wb.err = err
			return
		}
		wb.store = store
		wb.agent = &workbench.Agent{Store: store, Caller: &agentCaller{s: wb.s}}
	})
	if wb.err != nil {
		writeErr(w, http.StatusInternalServerError,
			fmt.Sprintf("workbench storage is unavailable: %v", wb.err), "prism_internal_error")
		return false
	}
	return true
}

// workbenchError maps a store or agent failure onto its HTTP status.
func workbenchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workbench.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error(), "not_found_error")
	case errors.Is(err, workbench.ErrRunActive):
		writeErr(w, http.StatusConflict, err.Error(), "conflict_error")
	case errors.Is(err, workbench.ErrNoPendingApproval):
		writeErr(w, http.StatusNotFound, err.Error(), "not_found_error")
	default:
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
	}
}

// decodeBody reads a bounded JSON request body.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWorkbenchBody))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		writeErr(w, http.StatusBadRequest, "request body is empty", "invalid_request_error")
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error")
		return false
	}
	return true
}

func writeCreated(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(v)
}

// --- projects --------------------------------------------------------

func (wb *workbenchRoutes) listProjects(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	projects, err := wb.store.Projects()
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, map[string]any{"projects": projects})
}

func (wb *workbenchRoutes) createProject(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	var in struct {
		Name     string             `json:"name"`
		Settings workbench.Settings `json:"settings"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	p, err := wb.store.CreateProject(in.Name, in.Settings)
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeCreated(w, p)
}

func (wb *workbenchRoutes) getProject(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	p, err := wb.store.Project(r.PathValue("id"))
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, p)
}

func (wb *workbenchRoutes) patchProject(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	var patch workbench.ProjectPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	p, err := wb.store.UpdateProject(r.PathValue("id"), patch)
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, p)
}

func (wb *workbenchRoutes) deleteProject(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	if err := wb.store.DeleteProject(r.PathValue("id")); err != nil {
		workbenchError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- chats -----------------------------------------------------------

func (wb *workbenchRoutes) listChats(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	chats, err := wb.store.Chats(r.PathValue("id"))
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, map[string]any{"chats": chats})
}

func (wb *workbenchRoutes) createChat(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	// A titleless chat is legitimate, so an empty body is not an error here.
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxWorkbenchBody))
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error")
			return
		}
	}
	c, err := wb.store.CreateChat(r.PathValue("id"), in.Title)
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeCreated(w, c)
}

func (wb *workbenchRoutes) getChat(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	chat, events, err := wb.store.Chat(r.PathValue("id"))
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, map[string]any{"chat": chat, "events": events})
}

func (wb *workbenchRoutes) deleteChat(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	if err := wb.store.DeleteChat(r.PathValue("id")); err != nil {
		workbenchError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- attached knowledge ----------------------------------------------

func (wb *workbenchRoutes) listFiles(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	files, err := wb.store.Files(r.PathValue("id"))
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, map[string]any{"files": files})
}

// uploadFile accepts a multipart upload under the "file" field. A plain body
// upload with ?name= is accepted too, which keeps curl and the CLI simple.
func (wb *workbenchRoutes) uploadFile(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	projectID := r.PathValue("id")
	// A little headroom over the cap for the multipart envelope; the real
	// limit is enforced on the decoded bytes below.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+8192)

	name := r.URL.Query().Get("name")
	var data []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			writeErr(w, http.StatusRequestEntityTooLarge,
				"upload failed (10 MB limit): "+err.Error(), "invalid_request_error")
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest,
				`multipart upload needs a "file" field`, "invalid_request_error")
			return
		}
		defer file.Close()
		if name == "" {
			if v := r.FormValue("name"); v != "" {
				name = v
			} else if header != nil {
				name = header.Filename
			}
		}
		data, err = io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
	} else {
		var err error
		data, err = io.ReadAll(io.LimitReader(r.Body, maxUploadBytes+1))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
	}
	if len(data) > maxUploadBytes {
		writeErr(w, http.StatusRequestEntityTooLarge,
			"attached files are limited to 10 MB", "invalid_request_error")
		return
	}
	if strings.TrimSpace(name) == "" {
		writeErr(w, http.StatusBadRequest,
			"a file name is required (multipart filename or ?name=)", "invalid_request_error")
		return
	}
	f, err := wb.store.PutFile(projectID, name, data)
	if err != nil {
		workbenchError(w, err)
		return
	}
	writeCreated(w, f)
}

func (wb *workbenchRoutes) getFile(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	data, err := wb.store.ReadFile(r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		workbenchError(w, err)
		return
	}
	// Attachments are user-supplied bytes: never let a browser interpret one
	// as markup on the gateway's own origin.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment")
	w.Write(data)
}

func (wb *workbenchRoutes) deleteFile(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		name = r.URL.Query().Get("name")
	}
	if name == "" {
		writeErr(w, http.StatusBadRequest, "?name= is required", "invalid_request_error")
		return
	}
	if err := wb.store.DeleteFile(r.PathValue("id"), name); err != nil {
		workbenchError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- agent -----------------------------------------------------------

// sseStream writes the agent's event stream. The response is committed on the
// first event, so a run that fails before emitting anything can still be
// answered with a normal HTTP error.
type sseStream struct {
	w       http.ResponseWriter
	started bool
}

func (s *sseStream) send(ev workbench.Event) {
	if !s.started {
		s.started = true
		h := s.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		// The dashboard streams this from the same origin; no proxy should
		// buffer it into uselessness.
		h.Set("X-Accel-Buffering", "no")
		s.w.WriteHeader(http.StatusOK)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// Both framings at once: named events for addEventListener, and a "type"
	// field for clients that just read onmessage.
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", ev.Type, payload)
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
}

func (wb *workbenchRoutes) runAgent(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	var req workbench.RunRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ChatID) == "" {
		writeErr(w, http.StatusBadRequest, "chat_id is required", "invalid_request_error")
		return
	}
	stream := &sseStream{w: w}
	err := wb.agent.Run(r.Context(), req, stream.send)
	if err == nil {
		return
	}
	if !stream.started {
		workbenchError(w, err)
		return
	}
	// Bytes are already on the wire: the only honest channel left is the
	// stream itself.
	stream.send(workbench.Event{Type: workbench.EventError, Message: err.Error()})
}

func (wb *workbenchRoutes) approve(w http.ResponseWriter, r *http.Request) {
	if !wb.ready(w) {
		return
	}
	var in struct {
		ChatID     string `json:"chat_id"`
		ToolCallID string `json:"tool_call_id"`
		Approve    bool   `json:"approve"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.ChatID == "" || in.ToolCallID == "" {
		writeErr(w, http.StatusBadRequest,
			"chat_id and tool_call_id are required", "invalid_request_error")
		return
	}
	if err := wb.agent.Approve(in.ChatID, in.ToolCallID, in.Approve); err != nil {
		workbenchError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "approved": in.Approve})
}

// --- the in-process model caller -------------------------------------

// agentCaller runs the agent's model turns through the gateway's own routing
// and failover path, so policies, budgets, circuit breakers, and the usage
// ledger apply to agent traffic exactly as they do to /v1/chat/completions.
type agentCaller struct{ s *Server }

func (c *agentCaller) Complete(ctx context.Context, in workbench.CompleteRequest,
	onText func(string)) (*workbench.Turn, error) {

	body := map[string]any{
		"model":    in.Model,
		"messages": in.Messages,
		"stream":   true,
		// Ask for real usage so agent turns are billed, not estimated.
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(in.Tools) > 0 {
		body["tools"] = in.Tools
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := provider.ParseChatRequest(buf)
	if err != nil {
		return nil, err
	}
	dec, err := c.s.engine.Route(req)
	if err != nil {
		return nil, err
	}

	// The adapters only take the request's context; a synthetic one carries
	// the run's cancellation without dragging the client's body along.
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", http.NoBody)
	if err != nil {
		return nil, err
	}

	col := &turnCollector{header: http.Header{}, onText: onText}
	if !c.s.executeChain(col, upstream, req, dec, col.fail) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("the run was cancelled: %w", ctxErr)
		}
		return nil, col.failure()
	}
	turn := col.turn()
	// executeChain writes its transparency headers onto the collector rather
	// than the client's stream; relay them so the UI can still say which
	// candidate answered.
	turn.Route = &workbench.Route{
		Provider: col.header.Get("X-Prism-Provider"),
		Model:    col.header.Get("X-Prism-Model"),
		Mode:     col.header.Get("X-Prism-Mode"),
		Reason:   col.header.Get("X-Prism-Reason"),
	}
	return turn, nil
}

// collectedCall accumulates one tool call across streamed deltas.
type collectedCall struct {
	id   string
	name string
	args strings.Builder
}

// turnCollector stands in for the client's ResponseWriter while the agent's
// own model call runs, reassembling the answer instead of forwarding it. It
// understands both an OpenAI SSE stream and a single JSON completion, because
// an upstream is free to ignore "stream": true.
type turnCollector struct {
	header http.Header
	onText func(string)

	status  int
	sse     bool
	pending bytes.Buffer // partial SSE line
	body    bytes.Buffer // non-streamed response body

	text  strings.Builder
	calls []*collectedCall
	order []int

	promptTokens     int
	completionTokens int

	failed     bool
	failStatus int
	failMsg    string
}

func (c *turnCollector) Header() http.Header { return c.header }

func (c *turnCollector) WriteHeader(code int) {
	if c.status != 0 {
		return
	}
	c.status = code
	c.sse = strings.HasPrefix(c.header.Get("Content-Type"), "text/event-stream")
}

func (c *turnCollector) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.WriteHeader(http.StatusOK)
	}
	if !c.sse {
		return c.body.Write(b)
	}
	c.pending.Write(b)
	for {
		buf := c.pending.Bytes()
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(buf[:i]), "\r")
		c.pending.Next(i + 1)
		c.streamLine(line)
	}
	return len(b), nil
}

func (c *turnCollector) Flush() {}

// fail matches the errorWriter signature executeChain reports through.
func (c *turnCollector) fail(_ http.ResponseWriter, status int, msg, _ string) {
	if c.failed {
		return
	}
	c.failed, c.failStatus, c.failMsg = true, status, msg
}

// failure explains why a turn produced nothing usable. executeChain reports
// through fail only while it can still choose a status; once the adapter has
// started writing, a failure arrives as a bare false.
func (c *turnCollector) failure() error {
	if c.failMsg != "" {
		return fmt.Errorf("model call failed (%d): %s", c.failStatus, c.failMsg)
	}
	if c.status != 0 {
		return errors.New("the model stream ended before the turn was complete")
	}
	return errors.New("the model call failed")
}

// openAITurn is the slice of an OpenAI chunk or completion the agent needs.
type openAITurn struct {
	Choices []struct {
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []openAIToolCal `json:"tool_calls"`
		} `json:"delta"`
		Message struct {
			Content   string          `json:"content"`
			ToolCalls []openAIToolCal `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIToolCal struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (c *turnCollector) streamLine(line string) {
	data, ok := strings.CutPrefix(line, "data: ")
	if !ok || data == "[DONE]" {
		return
	}
	var chunk openAITurn
	if json.Unmarshal([]byte(data), &chunk) != nil {
		return
	}
	c.absorb(&chunk, true)
}

// absorb folds one chunk (streaming) or one completion (streaming=false) into
// the turn under construction.
func (c *turnCollector) absorb(chunk *openAITurn, streaming bool) {
	if chunk.Usage != nil {
		c.promptTokens = chunk.Usage.PromptTokens
		c.completionTokens = chunk.Usage.CompletionTokens
	}
	for _, choice := range chunk.Choices {
		content, calls := choice.Delta.Content, choice.Delta.ToolCalls
		if !streaming || (content == "" && len(calls) == 0) {
			if choice.Message.Content != "" || len(choice.Message.ToolCalls) > 0 {
				content, calls = choice.Message.Content, choice.Message.ToolCalls
			}
		}
		if content != "" {
			c.text.WriteString(content)
			if c.onText != nil {
				c.onText(content)
			}
		}
		for i, tc := range calls {
			idx := i
			if tc.Index != nil {
				idx = *tc.Index
			}
			c.mergeCall(idx, tc)
		}
	}
}

// mergeCall appends a tool-call fragment to its slot. OpenAI streams the id
// and name once and then the arguments in pieces, keyed by index.
func (c *turnCollector) mergeCall(index int, tc openAIToolCal) {
	for len(c.calls) <= index {
		c.calls = append(c.calls, nil)
	}
	call := c.calls[index]
	if call == nil {
		call = &collectedCall{}
		c.calls[index] = call
		c.order = append(c.order, index)
	}
	if tc.ID != "" {
		call.id = tc.ID
	}
	if tc.Function.Name != "" {
		call.name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		call.args.WriteString(tc.Function.Arguments)
	}
}

// turn finishes collection: a non-streamed body is parsed here, since it
// arrives in one piece after the adapter has written it all.
func (c *turnCollector) turn() *workbench.Turn {
	if !c.sse && c.body.Len() > 0 {
		var parsed openAITurn
		if json.Unmarshal(c.body.Bytes(), &parsed) == nil {
			c.absorb(&parsed, false)
		}
	}
	out := &workbench.Turn{
		Text:             c.text.String(),
		PromptTokens:     c.promptTokens,
		CompletionTokens: c.completionTokens,
	}
	for _, idx := range c.order {
		call := c.calls[idx]
		if call == nil || call.name == "" {
			continue
		}
		id := call.id
		if id == "" {
			id = fmt.Sprintf("call_%d", idx)
		}
		out.ToolCalls = append(out.ToolCalls, workbench.ToolCall{
			ID: id, Name: call.name, Arguments: call.args.String(),
		})
	}
	return out
}
