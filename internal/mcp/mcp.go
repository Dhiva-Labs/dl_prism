// Package mcp implements a Model Context Protocol server over stdio, so
// Claude Code, Codex and any other MCP host can reach Prism's companion
// models as tools.
//
// The stdio transport is newline-delimited JSON-RPC 2.0: one JSON object per
// line on stdin, one per line on stdout, no Content-Length framing. stdout
// therefore carries nothing but protocol messages — all logging goes to
// stderr. Every tool is a thin client of the running gateway's HTTP API, so
// routing, circuit-breaker and ledger state stay in exactly one place.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultProtocolVersion is what we answer with when the client asks for a
// version we do not know.
const defaultProtocolVersion = "2025-06-18"

// serverName identifies this server to the host.
const serverName = "dl-prism"

// supportedProtocolVersions are the revisions this server speaks; the client's
// requested version is echoed back when it is one of them.
var supportedProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Server answers MCP requests by calling a running Prism gateway.
type Server struct {
	// Gateway is the gateway's base URL, e.g. http://127.0.0.1:8130.
	Gateway string
	// APIKey, when set, is sent as a bearer token on every gateway call.
	APIKey string
	// Version is reported in serverInfo.
	Version string
	// HTTP is the client used for gateway calls; nil uses a default with a
	// generous timeout (a cold local model can take minutes to answer).
	HTTP *http.Client
	// Logf receives diagnostics. Nil discards them. It must never write to
	// stdout: that channel belongs to the protocol.
	Logf func(format string, v ...any)
}

func (s *Server) logf(format string, v ...any) {
	if s.Logf != nil {
		s.Logf(format, v...)
	}
}

func (s *Server) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// request is one inbound JSON-RPC message. A message without an id (or with a
// null id) is a notification and gets no reply.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (r *request) isNotification() bool {
	id := strings.TrimSpace(string(r.ID))
	return id == "" || id == "null"
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// Serve runs the JSON-RPC loop until in reaches EOF.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false) // tool output is prose and code, not HTML
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			if resp := s.handle(line); resp != nil {
				if werr := enc.Encode(resp); werr != nil {
					return werr
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// handle dispatches one line, returning the response to write (nil for
// notifications, which by definition get none).
func (s *Server) handle(line string) *response {
	var req request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		// A line we cannot parse has no id to answer against; JSON-RPC says to
		// report it with a null id.
		return errorResponse(json.RawMessage("null"), &rpcError{
			Code: codeParseError, Message: "parse error: " + err.Error(),
		})
	}
	if req.Method == "" {
		if req.isNotification() {
			return nil
		}
		return errorResponse(req.ID, &rpcError{Code: codeInvalidRequest, Message: "request has no method"})
	}

	result, rerr := s.call(req.Method, req.Params)
	if req.isNotification() {
		if rerr != nil {
			s.logf("mcp: notification %s failed: %s", req.Method, rerr.Message)
		}
		return nil
	}
	if rerr != nil {
		return errorResponse(req.ID, rerr)
	}
	if result == nil {
		result = map[string]any{}
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func errorResponse(id json.RawMessage, err *rpcError) *response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &response{JSONRPC: "2.0", ID: id, Error: err}
}

// call runs one method. A nil result with a nil error means "nothing to say"
// (an accepted notification).
func (s *Server) call(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return s.initialize(params), nil
	case "notifications/initialized", "notifications/cancelled", "notifications/progress":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		return s.callTool(params)
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q not found", method)}
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name string `json:"name"`
		} `json:"clientInfo"`
	}
	json.Unmarshal(params, &p) // an unreadable initialize still gets our defaults

	version := defaultProtocolVersion
	if supportedProtocolVersions[p.ProtocolVersion] {
		version = p.ProtocolVersion
	}
	s.logf("mcp: initialize from %q (protocol %s) against gateway %s",
		p.ClientInfo.Name, version, s.Gateway)
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": serverName, "version": s.Version},
		"instructions": "Companion models behind the DL Prism gateway. prism_chat asks a routed " +
			"model, prism_explain shows which model a prompt would route to, prism_models lists " +
			"the registry, prism_usage reports this month's spend.",
	}
}
