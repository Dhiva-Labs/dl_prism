package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// newTools builds a sandbox with a workspace, a knowledge directory, and an
// "outside" directory the tools must never be able to reach.
func newTools(t *testing.T) (*Tools, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	files := filepath.Join(root, "files")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{workspace, files, outside} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Tools{Workspace: workspace, Files: files}, outside
}

func call(t *testing.T, tools *Tools, name string, args any) (string, bool, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tools.Execute(context.Background(), name, raw)
}

func TestDefinitionsCoverTheWaveATools(t *testing.T) {
	defs := Definitions()
	if len(defs) != 5 {
		t.Fatalf("got %d tool definitions, want 5", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		if d["type"] != "function" {
			t.Errorf("definition %v is not a function tool", d)
		}
		fn, ok := d["function"].(map[string]any)
		if !ok {
			t.Fatalf("definition %v has no function object", d)
		}
		name, _ := fn["name"].(string)
		names[name] = true
		if fn["description"] == "" {
			t.Errorf("%s has no description", name)
		}
		params, ok := fn["parameters"].(map[string]any)
		if !ok || params["type"] != "object" {
			t.Errorf("%s has no object parameter schema", name)
		}
		if _, ok := params["required"].([]string); !ok {
			t.Errorf("%s has no required list", name)
		}
		// The whole definition must survive a JSON round trip: it goes on the
		// wire to the upstream verbatim.
		if _, err := json.Marshal(d); err != nil {
			t.Errorf("%s does not marshal: %v", name, err)
		}
	}
	for _, want := range []string{ToolListDir, ToolReadFile, ToolWriteFile, ToolShell, ToolWebFetch} {
		if !names[want] {
			t.Errorf("tool %q is missing from the definitions", want)
		}
	}
}

func TestNeedsApproval(t *testing.T) {
	tests := []struct {
		mode string
		tool string
		want bool
	}{
		{ApprovalAlways, ToolReadFile, true},
		{ApprovalAlways, ToolListDir, true},
		{ApprovalAlways, ToolShell, true},
		{ApprovalMutations, ToolReadFile, false},
		{ApprovalMutations, ToolListDir, false},
		{ApprovalMutations, ToolWebFetch, false},
		{ApprovalMutations, ToolWriteFile, true},
		{ApprovalMutations, ToolShell, true},
		{ApprovalNever, ToolShell, false},
		{ApprovalNever, ToolWriteFile, false},
		// An unset mode must behave like the safe default, not like "never".
		{"", ToolShell, true},
		{"", ToolReadFile, false},
	}
	for _, tc := range tests {
		if got := NeedsApproval(tc.mode, tc.tool); got != tc.want {
			t.Errorf("NeedsApproval(%q, %q) = %t, want %t", tc.mode, tc.tool, got, tc.want)
		}
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	tools, _ := newTools(t)
	if _, _, err := call(t, tools, "rm_rf", map[string]any{}); err == nil {
		t.Error("an unknown tool name was executed")
	}
}

// --- path containment ------------------------------------------------

func TestPathEscapesAreRejected(t *testing.T) {
	tools, outside := newTools(t)

	// A symlink planted inside the workspace pointing out of it.
	if err := os.Symlink(outside, filepath.Join(tools.Workspace, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"),
		filepath.Join(tools.Workspace, "secret-link.txt")); err != nil {
		t.Fatal(err)
	}
	// And a nested directory holding one too.
	nested := filepath.Join(tools.Workspace, "sub")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(nested, "up")); err != nil {
		t.Fatal(err)
	}

	escapes := []string{
		"../outside/secret.txt",
		"../../etc/passwd",
		"sub/../../outside/secret.txt",
		"./../outside/secret.txt",
		outside + "/secret.txt",
		"/etc/passwd",
		"~/.ssh/id_rsa",
		"escape/secret.txt",
		"secret-link.txt",
		"sub/up/secret.txt",
	}
	for _, p := range escapes {
		t.Run("read "+p, func(t *testing.T) {
			out, _, err := call(t, tools, ToolReadFile, map[string]any{"path": p})
			if err == nil {
				t.Fatalf("read_file(%q) succeeded and returned %q", p, out)
			}
			if strings.Contains(out, "TOP SECRET") {
				t.Fatalf("read_file(%q) leaked the file outside the workspace", p)
			}
		})
		t.Run("write "+p, func(t *testing.T) {
			if _, _, err := call(t, tools, ToolWriteFile,
				map[string]any{"path": p, "content": "pwned"}); err == nil {
				t.Fatalf("write_file(%q) succeeded", p)
			}
		})
	}
	for _, p := range []string{"..", "../outside", "escape", "sub/up", "/etc", outside} {
		if _, _, err := call(t, tools, ToolListDir, map[string]any{"path": p}); err == nil {
			t.Errorf("list_dir(%q) succeeded", p)
		}
	}

	// Nothing was created outside the workspace by the write attempts.
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the outside directory now holds %d entries, want only secret.txt", len(entries))
	}
}

// A symlink that stays inside the workspace is fine: containment is about
// where a path lands, not about symlinks as such.
func TestSymlinkInsideTheWorkspaceIsAllowed(t *testing.T) {
	tools, _ := newTools(t)
	target := filepath.Join(tools.Workspace, "real.txt")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(tools.Workspace, "alias.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	out, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "alias.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if out != "inside" {
		t.Errorf("read_file = %q, want inside", out)
	}
}

func TestKnowledgeFilesAreReadableAndReadOnly(t *testing.T) {
	tools, _ := newTools(t)
	if err := os.WriteFile(filepath.Join(tools.Files, "brief.md"), []byte("the brief"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "files/brief.md"})
	if err != nil {
		t.Fatalf("read_file(files/brief.md): %v", err)
	}
	if out != "the brief" {
		t.Errorf("read_file = %q, want the attachment's content", out)
	}
	listing, _, err := call(t, tools, ToolListDir, map[string]any{"path": "files/"})
	if err != nil {
		t.Fatalf("list_dir(files/): %v", err)
	}
	if !strings.Contains(listing, "brief.md") {
		t.Errorf("list_dir(files/) = %q, want brief.md", listing)
	}
	if _, _, err := call(t, tools, ToolListDir, map[string]any{"path": "files"}); err != nil {
		t.Errorf("list_dir(files): %v", err)
	}
	// Escapes still apply under the prefix.
	if _, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "files/../../outside/secret.txt"}); err == nil {
		t.Error("the files/ prefix let a path escape")
	}

	// Writing "files/..." must land in the workspace, never in the knowledge
	// directory: attachments are the user's, not the model's.
	if _, _, err := call(t, tools, ToolWriteFile,
		map[string]any{"path": "files/brief.md", "content": "overwritten"}); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tools.Files, "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "the brief" {
		t.Errorf("the attachment was overwritten by the agent: %q", data)
	}
	if _, err := os.Stat(filepath.Join(tools.Workspace, "files", "brief.md")); err != nil {
		t.Errorf("the write did not land in the workspace: %v", err)
	}
}

// --- filesystem tools ------------------------------------------------

func TestListDir(t *testing.T) {
	tools, _ := newTools(t)
	if _, _, err := call(t, tools, ToolListDir, map[string]any{}); err != nil {
		t.Fatalf("list_dir on an empty workspace: %v", err)
	}
	out, _, _ := call(t, tools, ToolListDir, map[string]any{})
	if out != "(empty directory)" {
		t.Errorf("list_dir = %q, want the empty marker", out)
	}

	os.WriteFile(filepath.Join(tools.Workspace, "b.txt"), []byte("hi"), 0o600)
	os.Mkdir(filepath.Join(tools.Workspace, "adir"), 0o700)
	out, truncated, err := call(t, tools, ToolListDir, map[string]any{"path": "."})
	if err != nil {
		t.Fatalf("list_dir: %v", err)
	}
	if truncated {
		t.Error("a two-entry listing was reported as truncated")
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "adir/") {
		t.Errorf("list_dir = %q, want directories marked and entries sorted", out)
	}
	if !strings.Contains(lines[1], "b.txt") || !strings.Contains(lines[1], "2 bytes") {
		t.Errorf("file line = %q, want the name and size", lines[1])
	}
	if _, _, err := call(t, tools, ToolListDir, map[string]any{"path": "nope"}); err == nil {
		t.Error("list_dir on a missing directory succeeded")
	}
}

func TestReadFile(t *testing.T) {
	tools, _ := newTools(t)
	os.WriteFile(filepath.Join(tools.Workspace, "hello.txt"), []byte("hello world"), 0o600)
	os.WriteFile(filepath.Join(tools.Workspace, "blob.bin"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o600)
	os.Mkdir(filepath.Join(tools.Workspace, "dir"), 0o700)

	out, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "hello.txt"})
	if err != nil || out != "hello world" {
		t.Errorf("read_file = %q, %v", out, err)
	}
	if _, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "missing.txt"}); err == nil {
		t.Error("read_file on a missing file succeeded")
	}
	if _, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "dir"}); err == nil {
		t.Error("read_file on a directory succeeded")
	}
	if _, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "blob.bin"}); err == nil {
		t.Error("read_file returned binary content")
	}
	if _, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "  "}); err == nil {
		t.Error("read_file with no path succeeded")
	}
}

func TestReadFileTruncatesAtTheOutputCap(t *testing.T) {
	tools, _ := newTools(t)
	big := strings.Repeat("x", maxToolOutput+5000)
	os.WriteFile(filepath.Join(tools.Workspace, "big.txt"), []byte(big), 0o600)

	out, truncated, err := call(t, tools, ToolReadFile, map[string]any{"path": "big.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !truncated {
		t.Error("an oversized file was not reported as truncated")
	}
	if !strings.HasSuffix(out, truncationMarker) {
		t.Errorf("output does not end with the truncation marker: %q", out[max(0, len(out)-80):])
	}
	if len(out) > maxToolOutput+len(truncationMarker) {
		t.Errorf("output is %d bytes, want at most %d", len(out), maxToolOutput+len(truncationMarker))
	}

	// A file exactly on the limit is complete, not truncated.
	os.WriteFile(filepath.Join(tools.Workspace, "exact.txt"), []byte(strings.Repeat("y", maxToolOutput)), 0o600)
	out, truncated, err = call(t, tools, ToolReadFile, map[string]any{"path": "exact.txt"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if truncated || len(out) != maxToolOutput {
		t.Errorf("a file on the limit came back truncated=%t len=%d", truncated, len(out))
	}
}

func TestWriteFile(t *testing.T) {
	tools, _ := newTools(t)
	out, _, err := call(t, tools, ToolWriteFile,
		map[string]any{"path": "notes/today.md", "content": "# today"})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(out, "notes/today.md") || !strings.Contains(out, "7 bytes") {
		t.Errorf("write_file = %q, want the path and byte count", out)
	}
	data, err := os.ReadFile(filepath.Join(tools.Workspace, "notes", "today.md"))
	if err != nil {
		t.Fatalf("the file was not created: %v", err)
	}
	if string(data) != "# today" {
		t.Errorf("content = %q", data)
	}
	if _, _, err := call(t, tools, ToolWriteFile, map[string]any{"content": "x"}); err == nil {
		t.Error("write_file without a path succeeded")
	}
}

func TestExecuteRejectsUnparseableArguments(t *testing.T) {
	tools, _ := newTools(t)
	if _, _, err := tools.Execute(context.Background(), ToolReadFile, json.RawMessage(`not json`)); err == nil {
		t.Error("malformed tool arguments were accepted")
	}
}

// --- shell -----------------------------------------------------------

func TestShellRunsInTheWorkspace(t *testing.T) {
	tools, _ := newTools(t)
	out, truncated, err := call(t, tools, ToolShell, map[string]any{"command": "pwd && echo hi"})
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if truncated {
		t.Error("a two-line output was reported as truncated")
	}
	// The workspace may sit behind a symlinked temp dir, so compare resolved.
	real, _ := filepath.EvalSymlinks(tools.Workspace)
	if !strings.Contains(out, real) {
		t.Errorf("shell output %q does not show the workspace cwd %q", out, real)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("shell output = %q, want hi", out)
	}
}

func TestShellReportsStderrAndExitStatus(t *testing.T) {
	tools, _ := newTools(t)
	out, _, err := call(t, tools, ToolShell,
		map[string]any{"command": "echo to-stdout; echo to-stderr >&2; exit 3"})
	if err != nil {
		t.Fatalf("a non-zero exit must be a result, not an error: %v", err)
	}
	for _, want := range []string{"to-stdout", "to-stderr", "exit status 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("shell output %q is missing %q", out, want)
		}
	}
	out, _, _ = call(t, tools, ToolShell, map[string]any{"command": "true"})
	if out != "(no output)" {
		t.Errorf("silent command produced %q, want the no-output marker", out)
	}
	if _, _, err := call(t, tools, ToolShell, map[string]any{"command": "   "}); err == nil {
		t.Error("an empty command was accepted")
	}
}

func TestShellTimesOutAndIsKilled(t *testing.T) {
	tools, _ := newTools(t)
	tools.ShellTimeout = 300 * time.Millisecond

	start := time.Now()
	out, _, err := call(t, tools, ToolShell, map[string]any{"command": "echo starting; sleep 30"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a timeout must come back as a result, not an error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("the command ran for %v, want it killed near the 300ms budget", elapsed)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("output = %q, want it to say the command timed out", out)
	}
	if !strings.Contains(out, "starting") {
		t.Errorf("output = %q, want the bytes produced before the timeout", out)
	}
}

func TestShellCapsItsOutput(t *testing.T) {
	tools, _ := newTools(t)
	out, truncated, err := call(t, tools, ToolShell,
		map[string]any{"command": `head -c 200000 /dev/zero | tr '\0' 'a'`})
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !truncated {
		t.Fatal("a 200 KB output was not reported as truncated")
	}
	if !strings.HasSuffix(out, truncationMarker) {
		t.Error("the capped output carries no truncation marker")
	}
	if len(out) > maxToolOutput+len(truncationMarker) {
		t.Errorf("output is %d bytes, want at most %d", len(out), maxToolOutput+len(truncationMarker))
	}
}

// The gateway's own environment holds provider API keys; a model-authored
// command must not see them.
func TestShellEnvironmentIsScrubbed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-must-not-leak")
	t.Setenv("PRISM_SECRET", "hunter2")
	tools, _ := newTools(t)

	out, _, err := call(t, tools, ToolShell, map[string]any{"command": "env"})
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	for _, leak := range []string{"sk-must-not-leak", "OPENAI_API_KEY", "PRISM_SECRET", "hunter2"} {
		if strings.Contains(out, leak) {
			t.Errorf("the command environment leaked %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "PATH=") {
		t.Errorf("PATH was not passed through:\n%s", out)
	}
	vars := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if k, _, ok := strings.Cut(line, "="); ok {
			vars[k] = true
		}
	}
	for k := range vars {
		switch k {
		case "PATH", "HOME", "LANG", "PWD", "SHLVL", "_":
		default:
			t.Errorf("unexpected variable %q in the scrubbed environment", k)
		}
	}
}

func TestShellWithoutAWorkspaceFails(t *testing.T) {
	tools := &Tools{}
	if _, _, err := tools.shell(context.Background(), "echo hi"); err == nil {
		t.Error("shell ran without a workspace")
	}
}

// --- web_fetch -------------------------------------------------------

func TestWebFetchRefusesLocalAndPrivateAddresses(t *testing.T) {
	tools, _ := newTools(t)
	// Any DNS name in this test resolves to the gateway's own loopback: the
	// refusal must not depend on the URL looking local.
	tools.LookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}

	urls := []string{
		"http://127.0.0.1:8130/admin/usage",
		"http://127.0.0.1/",
		"https://[::1]:8130/",
		"http://10.0.0.5/internal",
		"http://192.168.1.1/",
		"http://172.16.4.4/",
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://[fd00::1]/",                        // IPv6 unique-local
		"http://[fe80::1]/",                        // IPv6 link-local
		"http://0.0.0.0:8130/",
		"http://100.64.1.1/",         // carrier-grade NAT
		"http://localhost:8130/",     // by name
		"http://api.localhost:8130/", // and by suffix
		"http://gateway.internal/",   // resolves to loopback through the hook
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			out, _, err := call(t, tools, ToolWebFetch, map[string]any{"url": u})
			if err == nil {
				t.Fatalf("web_fetch(%s) succeeded and returned %q", u, out)
			}
			if !strings.Contains(err.Error(), "refusing") {
				t.Errorf("error = %v, want the refusal message", err)
			}
		})
	}
}

func TestWebFetchRejectsNonHTTPSchemes(t *testing.T) {
	tools, _ := newTools(t)
	for _, u := range []string{
		"file:///etc/passwd", "ftp://example.com/x", "gopher://example.com/",
		"data:text/plain,hi", "//example.com/x", "  ",
	} {
		if _, _, err := call(t, tools, ToolWebFetch, map[string]any{"url": u}); err == nil {
			t.Errorf("web_fetch(%q) succeeded", u)
		}
	}
}

func TestWebFetchReportsResolutionFailure(t *testing.T) {
	tools, _ := newTools(t)
	tools.LookupIP = func(context.Context, string) ([]net.IP, error) { return nil, nil }
	if _, _, err := call(t, tools, ToolWebFetch, map[string]any{"url": "https://nowhere.example/"}); err == nil {
		t.Error("a host that resolves to nothing was fetched")
	}
}

// fakeInternet points an injected client at a test server while the SSRF
// check sees a public address, which is what a real public host looks like.
func fakeInternet(t *testing.T, tools *Tools, srv *httptest.Server) {
	t.Helper()
	addr := srv.Listener.Addr().String()
	tools.LookupIP = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	tools.Client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
}

func TestWebFetchReturnsReadableText(t *testing.T) {
	var gotMethod, gotAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAgent = r.Method, r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><title>T</title>
			<style>body{color:red}</style>
			<script>var leak = "do not show this";</script></head>
			<body><!-- a comment --><h1>Heading</h1><p>First &amp; second.</p>
			<ul><li>one</li><li>two</li></ul></body></html>`))
	}))
	defer srv.Close()

	tools, _ := newTools(t)
	fakeInternet(t, tools, srv)

	out, truncated, err := call(t, tools, ToolWebFetch, map[string]any{"url": "http://docs.example/page"})
	if err != nil {
		t.Fatalf("web_fetch: %v", err)
	}
	if truncated {
		t.Error("a small page was reported as truncated")
	}
	if gotMethod != http.MethodGet {
		t.Errorf("upstream method = %s, want GET", gotMethod)
	}
	if !strings.Contains(gotAgent, "prism") {
		t.Errorf("User-Agent = %q, want it to identify the gateway", gotAgent)
	}
	if !strings.HasPrefix(out, "GET http://docs.example/page -> 200") {
		t.Errorf("output does not start with the request summary: %q", out)
	}
	for _, want := range []string{"Heading", "First & second.", "one", "two"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q is missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"do not show this", "color:red", "a comment", "<h1>", "<p>"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output %q still contains %q", out, unwanted)
		}
	}
}

func TestWebFetchPassesPlainTextThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"a":"<b>not markup</b>"}`))
	}))
	defer srv.Close()

	tools, _ := newTools(t)
	fakeInternet(t, tools, srv)
	out, _, err := call(t, tools, ToolWebFetch, map[string]any{"url": "http://api.example/v1"})
	if err != nil {
		t.Fatalf("web_fetch: %v", err)
	}
	if !strings.Contains(out, `{"a":"<b>not markup</b>"}`) {
		t.Errorf("output = %q, want the JSON body untouched", out)
	}
}

func TestWebFetchCapsHugeResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		chunk := strings.Repeat("a", 64<<10)
		for i := 0; i < 100; i++ { // 6.4 MB, past both caps
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	tools, _ := newTools(t)
	fakeInternet(t, tools, srv)
	out, truncated, err := call(t, tools, ToolWebFetch, map[string]any{"url": "http://big.example/"})
	if err != nil {
		t.Fatalf("web_fetch: %v", err)
	}
	if !truncated {
		t.Error("a 6 MB response was not reported as truncated")
	}
	if len(out) > maxToolOutput+len(truncationMarker) {
		t.Errorf("output is %d bytes, want at most %d", len(out), maxToolOutput+len(truncationMarker))
	}
}

// A public host that redirects into the private network is the classic SSRF
// bypass; every hop is checked.
func TestWebFetchRefusesPrivateRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	tools, _ := newTools(t)
	fakeInternet(t, tools, srv)
	out, _, err := call(t, tools, ToolWebFetch, map[string]any{"url": "http://redirector.example/"})
	if err == nil {
		t.Fatalf("the redirect was followed and returned %q", out)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("error = %v, want the refusal message", err)
	}
}

func TestWebFetchTimesOut(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer func() { close(done); srv.Close() }()

	tools, _ := newTools(t)
	fakeInternet(t, tools, srv)
	tools.FetchTimeout = 200 * time.Millisecond

	start := time.Now()
	if _, _, err := call(t, tools, ToolWebFetch, map[string]any{"url": "http://slow.example/"}); err == nil {
		t.Fatal("a hanging fetch returned successfully")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the fetch took %v, want it cut at the 200ms budget", elapsed)
	}
}

func TestBlockedIPCoverage(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1", "0.0.0.0", "0.1.2.3",
		"10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.0.1",
		"169.254.169.254", "fe80::1", "fc00::1", "fd12:3456::1",
		"224.0.0.1", "ff02::1", "100.64.0.1", "100.127.255.255",
		"::ffff:127.0.0.1", // IPv4-mapped loopback
	}
	for _, s := range blocked {
		if !blockedIP(net.ParseIP(s)) {
			t.Errorf("blockedIP(%s) = false, want it refused", s)
		}
	}
	allowed := []string{
		"8.8.8.8", "93.184.216.34", "1.1.1.1", "2606:4700:4700::1111",
		"172.32.0.1", "100.63.255.255", "100.128.0.1", "11.0.0.1",
	}
	for _, s := range allowed {
		if blockedIP(net.ParseIP(s)) {
			t.Errorf("blockedIP(%s) = true, want a public address allowed", s)
		}
	}
	if !blockedIP(nil) {
		t.Error("blockedIP(nil) = false, want an unparseable address refused")
	}
}

func TestRenderFetchedIsIdempotentOnPlainText(t *testing.T) {
	body := []byte("line one\nline two\n")
	if got := renderFetched("text/plain", body); got != string(body) {
		t.Errorf("renderFetched = %q, want the body unchanged", got)
	}
	// A body that smells like HTML is stripped even without the header.
	got := renderFetched("application/octet-stream", []byte("<html><body>hi</body></html>"))
	if strings.Contains(got, "<body>") {
		t.Errorf("renderFetched = %q, want the markup stripped", got)
	}
}

func TestCapOutputKeepsValidUTF8(t *testing.T) {
	// A multi-byte rune straddling the limit must not be cut in half.
	s := strings.Repeat("a", maxToolOutput-1) + "é" + "tail"
	out, truncated, err := capOutput(s)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("an oversized string was not truncated")
	}
	if !utf8.ValidString(out) {
		t.Error("truncation produced invalid UTF-8")
	}
	if strings.Contains(out, "tail") {
		t.Error("truncation kept content past the limit")
	}
}

func TestToolsErrorMessageDoesNotLeakTheRealPath(t *testing.T) {
	tools, outside := newTools(t)
	_, _, err := call(t, tools, ToolReadFile, map[string]any{"path": "../outside/secret.txt"})
	if err == nil {
		t.Fatal("the escape succeeded")
	}
	if strings.Contains(err.Error(), outside) && !strings.Contains(err.Error(), "../outside") {
		t.Errorf("error %q leaks the resolved host path", err)
	}
	if !strings.Contains(err.Error(), "outside the project workspace") {
		t.Errorf("error = %q, want the containment message", err)
	}
	_ = fmt.Sprint(err)
}
