package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Tool names.
const (
	ToolListDir   = "list_dir"
	ToolReadFile  = "read_file"
	ToolWriteFile = "write_file"
	ToolShell     = "shell"
	ToolWebFetch  = "web_fetch"
)

const (
	// maxToolOutput caps what any tool hands back to the model.
	maxToolOutput = 64 << 10
	// maxFetchBytes caps what web_fetch pulls off the wire before stripping.
	maxFetchBytes = 5 << 20
	// truncationMarker tells the model its view was cut short, so it can ask
	// for a narrower slice instead of assuming it saw everything.
	truncationMarker = "\n\n[output truncated: limit reached]"

	defaultShellTimeout = 60 * time.Second
	defaultFetchTimeout = 30 * time.Second
)

// knowledgePrefix addresses a project's attached files from the tools that
// read; everything else resolves inside the workspace.
const knowledgePrefix = "files/"

// mutatingTools are the ones gated by the "mutations" approval mode.
var mutatingTools = map[string]bool{ToolWriteFile: true, ToolShell: true}

// NeedsApproval reports whether a tool call must wait for the user under the
// given approval mode.
func NeedsApproval(mode, tool string) bool {
	switch mode {
	case ApprovalNever:
		return false
	case ApprovalAlways:
		return true
	default: // mutations, and any unset value: fail closed on the safe side
		return mutatingTools[tool]
	}
}

// Tools executes agent tool calls for one project. Zero values of the hooks
// pick production defaults; tests override them.
type Tools struct {
	// Workspace is the agent's sandbox: every path resolves inside it.
	Workspace string
	// Files is the project's attached-knowledge directory, reachable
	// read-only through the files/ prefix.
	Files string

	ShellTimeout time.Duration
	FetchTimeout time.Duration

	// LookupIP resolves a hostname for the web_fetch SSRF check. Nil uses the
	// system resolver.
	LookupIP func(ctx context.Context, host string) ([]net.IP, error)
	// Client fetches URLs. Nil builds a client whose dialer re-checks every
	// address it is about to connect to, which also defeats DNS rebinding.
	Client *http.Client
}

// Definitions is the OpenAI tools array advertised to the model.
func Definitions() []map[string]any {
	fn := func(name, desc string, params map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters": map[string]any{
					"type":       "object",
					"properties": params,
					"required":   required,
				},
			},
		}
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	return []map[string]any{
		fn(ToolListDir,
			"List the entries of a directory in the project workspace. Use the files/ prefix to list the project's attached knowledge files.",
			map[string]any{"path": str("Directory path relative to the workspace root. Defaults to the root.")}),
		fn(ToolReadFile,
			"Read a UTF-8 text file from the project workspace, or an attached knowledge file with the files/ prefix.",
			map[string]any{"path": str("File path relative to the workspace root, or files/<name> for an attachment.")},
			"path"),
		fn(ToolWriteFile,
			"Create or overwrite a text file in the project workspace. Attached knowledge files are read-only.",
			map[string]any{
				"path":    str("File path relative to the workspace root."),
				"content": str("Full file content to write."),
			},
			"path", "content"),
		fn(ToolShell,
			"Run a shell command with bash in the project workspace. The command has a 60 second budget and its combined output is capped.",
			map[string]any{"command": str("Shell command to run, as bash -c would receive it.")},
			"command"),
		fn(ToolWebFetch,
			"Fetch a public http(s) URL with GET and return its readable text. Private, loopback, and link-local addresses are refused.",
			map[string]any{"url": str("Absolute http:// or https:// URL.")},
			"url"),
	}
}

// Execute runs one tool call. The returned string is the model-facing output;
// truncated reports whether it was cut to fit the cap. A returned error is a
// tool failure the agent surfaces to the model, not a fatal run error.
func (t *Tools) Execute(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Command string `json:"command"`
		URL     string `json:"url"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", false, fmt.Errorf("could not parse arguments: %w", err)
		}
	}
	switch name {
	case ToolListDir:
		return t.listDir(in.Path)
	case ToolReadFile:
		return t.readFile(in.Path)
	case ToolWriteFile:
		return t.writeFile(in.Path, in.Content)
	case ToolShell:
		return t.shell(ctx, in.Command)
	case ToolWebFetch:
		return t.webFetch(ctx, in.URL)
	}
	return "", false, fmt.Errorf("unknown tool %q", name)
}

// --- path safety -----------------------------------------------------

// errEscape is the single message every containment failure reports; the
// model does not need to know which trick was tried.
func errEscape(p string) error {
	return fmt.Errorf("path %q is outside the project workspace", p)
}

// resolve maps a tool-supplied path onto a real one, refusing anything that
// leaves the sandbox. Both the lexical form and the symlink-resolved form are
// checked, so neither "../../etc/passwd" nor a symlink planted inside the
// workspace can reach out of it.
//
// forWrite selects the workspace root unconditionally: attached knowledge is
// read-only.
func (t *Tools) resolve(p string, forWrite bool) (string, error) {
	raw := p
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		p = "."
	}
	root := t.Workspace
	if !forWrite {
		if rest, ok := strings.CutPrefix(p, knowledgePrefix); ok {
			root, p = t.Files, rest
			if strings.TrimSpace(p) == "" {
				p = "."
			}
		} else if p == "files" || p == strings.TrimSuffix(knowledgePrefix, "/") {
			root, p = t.Files, "."
		}
	}
	if root == "" {
		return "", errEscape(raw)
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "~") {
		return "", errEscape(raw)
	}

	// Resolve the root itself first: on systems where a parent is a symlink
	// (a temp dir, an encrypted home) the comparison must be like for like.
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		rootReal = filepath.Clean(root)
	}
	joined := filepath.Join(rootReal, p)
	if !within(rootReal, joined) {
		return "", errEscape(raw)
	}

	// Walk up to the deepest component that exists and resolve it: a symlink
	// anywhere along the path must not land outside the root. Writes create
	// new files, so a missing leaf is expected.
	probe := joined
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !within(rootReal, real) {
				return "", errEscape(raw)
			}
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe || len(parent) < len(rootReal) {
			break
		}
		probe = parent
	}
	return joined, nil
}

// within reports whether p is root or sits underneath it.
func within(root, p string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// rel renders a resolved path the way the model addressed it.
func (t *Tools) rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

// --- filesystem tools ------------------------------------------------

func (t *Tools) listDir(p string) (string, bool, error) {
	full, err := t.resolve(p, false)
	if err != nil {
		return "", false, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, fmt.Errorf("no such directory: %s", p)
		}
		return "", false, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			fmt.Fprintf(&b, "%s/\n", e.Name())
			continue
		}
		size := int64(-1)
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		fmt.Fprintf(&b, "%s\t%d bytes\n", e.Name(), size)
	}
	if b.Len() == 0 {
		return "(empty directory)", false, nil
	}
	return capOutput(b.String())
}

func (t *Tools) readFile(p string) (string, bool, error) {
	if strings.TrimSpace(p) == "" {
		return "", false, errors.New("path is required")
	}
	full, err := t.resolve(p, false)
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, fmt.Errorf("no such file: %s", p)
		}
		return "", false, err
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("%s is a directory; use list_dir", p)
	}
	f, err := os.Open(full)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	// One byte past the cap so a file sitting exactly on it is not reported
	// as truncated.
	data, err := io.ReadAll(io.LimitReader(f, maxToolOutput+1))
	if err != nil {
		return "", false, err
	}
	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("%s is not UTF-8 text (%d bytes)", p, info.Size())
	}
	return capOutput(string(data))
}

func (t *Tools) writeFile(p, content string) (string, bool, error) {
	if strings.TrimSpace(p) == "" {
		return "", false, errors.New("path is required")
	}
	full, err := t.resolve(p, true)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		return "", false, err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), t.rel(t.Workspace, full)), false, nil
}

// --- shell -----------------------------------------------------------

// shellEnv is the whole environment a command gets: the gateway's own
// process environment holds provider API keys, and none of them belong in a
// model-authored command.
func shellEnv() []string {
	env := []string{"PATH=" + os.Getenv("PATH")}
	for _, k := range []string{"HOME", "LANG"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func (t *Tools) shell(ctx context.Context, command string) (string, bool, error) {
	if strings.TrimSpace(command) == "" {
		return "", false, errors.New("command is required")
	}
	if t.Workspace == "" {
		return "", false, errors.New("this project has no workspace")
	}
	if err := os.MkdirAll(t.Workspace, 0o700); err != nil {
		return "", false, err
	}
	timeout := t.ShellTimeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := &capWriter{limit: maxToolOutput}
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = t.Workspace
	cmd.Env = shellEnv()
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Stdin = nil
	// CommandContext kills bash when the deadline passes; WaitDelay stops the
	// wait shortly afterwards so a backgrounded grandchild still holding the
	// output pipe cannot hang the agent loop past its budget.
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	text, truncated := out.result()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		text = strings.TrimRight(text, "\n")
		if text != "" {
			text += "\n"
		}
		return text + fmt.Sprintf("[command timed out after %s and was killed]", timeout), truncated, nil
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			text = strings.TrimRight(text, "\n")
			if text != "" {
				text += "\n"
			}
			return text + fmt.Sprintf("[exit status %d]", ee.ExitCode()), truncated, nil
		}
		return text, truncated, err
	}
	if strings.TrimSpace(text) == "" {
		return "(no output)", truncated, nil
	}
	return text, truncated, nil
}

// capWriter keeps the first limit bytes and remembers that it dropped the
// rest. os/exec guarantees that a writer shared by Stdout and Stderr is
// called from one goroutine at a time, so no lock is needed here.
type capWriter struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (c *capWriter) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	// Always report a full write: the command must not die of a short write.
	return len(p), nil
}

func (c *capWriter) result() (string, bool) {
	s := c.buf.String()
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if c.truncated {
		s += truncationMarker
	}
	return s, c.truncated
}

// capOutput trims a tool result to the output limit.
func capOutput(s string) (string, bool, error) {
	if len(s) <= maxToolOutput {
		return s, false, nil
	}
	s = s[:maxToolOutput]
	// Do not hand the model a half rune.
	s = strings.ToValidUTF8(s, "")
	return s + truncationMarker, true, nil
}

// --- web_fetch -------------------------------------------------------

var errPrivateAddress = errors.New(
	"refusing to fetch a private, loopback, or link-local address: web_fetch reaches the public internet only")

func (t *Tools) webFetch(ctx context.Context, raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, errors.New("url is required")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false, fmt.Errorf("invalid url: %w", err)
	}
	if err := t.checkURL(ctx, u); err != nil {
		return "", false, err
	}

	timeout := t.FetchTimeout
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := t.fetchClient(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "dl-prism-workbench/1 (+agent web_fetch)")
	req.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		// A refusal raised by the redirect or dial guard travels wrapped in a
		// url.Error; report the reason, not the plumbing.
		if errors.Is(err, errPrivateAddress) {
			return "", false, errPrivateAddress
		}
		return "", false, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", false, fmt.Errorf("reading response: %w", err)
	}
	ct := resp.Header.Get("Content-Type")
	text := renderFetched(ct, body)
	if !utf8.ValidString(text) {
		return "", false, fmt.Errorf("%s returned binary content (%s)", u.Redacted(), ct)
	}
	head := fmt.Sprintf("GET %s -> %d %s\n\n", u.Redacted(), resp.StatusCode, strings.TrimSpace(ct))
	return capOutput(head + strings.TrimSpace(text))
}

// fetchClient returns the client to fetch with. An injected client is copied
// so the redirect guard can be installed without mutating the caller's.
func (t *Tools) fetchClient(ctx context.Context, timeout time.Duration) *http.Client {
	var c http.Client
	if t.Client != nil {
		c = *t.Client
	} else {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		// Control sees the address actually being dialled, after the
		// resolver ran — the last line of defence against a hostname that
		// resolves to a public address on the check and a private one here.
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
				return errPrivateAddress
			}
			return nil
		}
		c.Transport = &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			DisableKeepAlives:   true,
			Proxy:               nil, // a proxy would hide the real destination
		}
	}
	c.Timeout = timeout
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return t.checkURL(req.Context(), req.URL)
	}
	return &c
}

// checkURL enforces the web_fetch policy: public http(s) only. Every host is
// resolved and every answer inspected, so a DNS name pointing at the
// gateway's own loopback interface is refused before a connection is opened.
func (t *Tools) checkURL(ctx context.Context, u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported url scheme %q: web_fetch does http and https only", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("url has no host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return errPrivateAddress
		}
		return nil
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return errPrivateAddress
	}
	lookup := t.LookupIP
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("could not resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("could not resolve %s", host)
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return errPrivateAddress
		}
	}
	return nil
}

// carrierGrade is 100.64.0.0/10, which routes inside provider networks and is
// not the public internet either.
var carrierGrade = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// blockedIP reports whether an address belongs to the machine, the local
// network, or another range web_fetch has no business reaching. Anything the
// agent could use to talk back to this gateway, a metadata service, or a
// neighbour on the LAN is refused.
func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLoopback(), ip.IsUnspecified(), ip.IsPrivate(),
		ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return true
	}
	if carrierGrade.Contains(ip) {
		return true
	}
	// 0.0.0.0/8 is "this network" and some stacks route it to localhost.
	if v4 := ip.To4(); v4 != nil && v4[0] == 0 {
		return true
	}
	return false
}

// RE2 has no backreferences, so each container tag gets its own pattern.
var reContainers = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, tag := range []string{"script", "style", "noscript", "template", "svg"} {
		out = append(out, regexp.MustCompile(`(?is)<`+tag+`\b[^>]*>.*?<\s*/\s*`+tag+`\s*>`))
	}
	return out
}()

var (
	reComment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	reBreak      = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/tr|/h[1-6]|/section|/article)\b[^>]*>`)
	reTag        = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
	reTrailing   = regexp.MustCompile(`[ \t]+\n`)
)

// renderFetched turns a response body into something a model can read. HTML
// is reduced crudely — no parser, no external dependency — which is enough
// for the "what does this page say" question web_fetch exists to answer.
func renderFetched(contentType string, body []byte) string {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	s := string(body)
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" &&
		!strings.Contains(s[:min(len(s), 512)], "<html") {
		return s
	}
	for _, re := range reContainers {
		s = re.ReplaceAllString(s, " ")
	}
	s = reComment.ReplaceAllString(s, " ")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reTrailing.ReplaceAllString(s, "\n")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return s
}
