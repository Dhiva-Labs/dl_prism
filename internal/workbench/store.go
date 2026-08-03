// Package workbench implements the local-first project workspace behind the
// DL Prism Workbench: projects with persistent chats, attached knowledge
// files, a per-project agent sandbox, and the tool-calling agent loop that
// drives them.
//
// Every byte of state is a plain file under the workbench root — greppable,
// backupable, and free of any database dependency:
//
//	workbench/projects/<id>/project.json      # name, settings, timestamps
//	                        chats/<id>.jsonl  # one event per line
//	                        files/            # attached knowledge
//	                        workspace/        # the agent's sandbox cwd
package workbench

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Approval modes for agent tool calls.
const (
	ApprovalAlways    = "always"    // every tool call waits for the user
	ApprovalMutations = "mutations" // only write_file and shell wait
	ApprovalNever     = "never"     // tools run unattended
)

// knowledgeBudget caps how much attached text is folded into the system
// message. Anything past it stays tool-readable only.
const knowledgeBudget = 32 << 10

// maxKnowledgeFile is the largest single attachment considered for folding;
// one huge file must not eat the whole budget.
const maxKnowledgeFile = 24 << 10

// ErrNotFound is returned for unknown project, chat, or file ids.
var ErrNotFound = errors.New("not found")

// Settings are the per-project agent knobs.
type Settings struct {
	// AgentEnabled gates tool use; false makes a project a plain persistent
	// chat with no filesystem or network reach.
	AgentEnabled bool   `json:"agent_enabled"`
	ApprovalMode string `json:"approval_mode"`
	// Model is the default model for this project's turns; empty means the
	// router's automatic mode.
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
}

// normalize fills defaults and rejects unknown approval modes.
func (s *Settings) normalize() error {
	if s.ApprovalMode == "" {
		s.ApprovalMode = ApprovalMutations
	}
	switch s.ApprovalMode {
	case ApprovalAlways, ApprovalMutations, ApprovalNever:
	default:
		return fmt.Errorf("unknown approval_mode %q (want always, mutations, or never)", s.ApprovalMode)
	}
	return nil
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Settings  Settings  `json:"settings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// ChatCount is derived from the chats directory on every read and never
	// written to project.json, so it cannot go stale on disk.
	ChatCount int `json:"chat_count"`
}

// diskProject is what lands in project.json. Its fields are listed out
// rather than embedded so that a derived field added to Project can never
// leak onto disk and go stale there.
type diskProject struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Settings  Settings  `json:"settings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Chat struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Events counts the persisted timeline entries, excluding the header.
	Events int `json:"events"`
}

// File is one attached knowledge file.
type File struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// InContext reports whether this file is small enough to be folded into
	// the system message; the rest are readable with the read_file tool.
	InContext bool `json:"in_context"`
}

// Usage is the token total a run reports in its done event.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Route names the provider and model that answered a turn, mirroring the
// X-Prism-* headers the gateway sets on an ordinary completion. Agent runs
// stream SSE, so their routing is reported in the done event instead.
type Route struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Event is both a persisted chat timeline entry and a streamed SSE payload.
// Persisted types are meta, message, tool_call, tool_result and error;
// streamed types are message_delta, tool_call, tool_await_approval,
// tool_result, done and error. Fields not carried by a type are omitted.
type Event struct {
	Type string    `json:"type"`
	TS   time.Time `json:"ts"`

	// message
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// message_delta
	Text string `json:"text,omitempty"`
	// tool_call / tool_await_approval / tool_result
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	Output    string          `json:"output,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	// Approved is set to false on a tool_result the user refused, and is
	// absent otherwise. Its presence is what marks a denial in a replayed
	// transcript; a result that ran — gated or not — carries no field.
	Approved *bool `json:"approved,omitempty"`
	// done
	Usage *Usage `json:"usage,omitempty"`
	Route *Route `json:"route,omitempty"`
	// error
	Message string `json:"message,omitempty"`
	// meta
	Title     string `json:"title,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// HistoryMessage is one turn replayed into a model request.
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Store owns the workbench directory tree. A single mutex serialises every
// operation: this is a single-user local tool, and correctness beats
// concurrency for a handful of small files.
type Store struct {
	mu   sync.Mutex
	root string
	now  func() time.Time
}

// DefaultDir honours XDG_DATA_HOME, falling back to ~/.local/share.
func DefaultDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "prism", "workbench"), nil
}

// Open prepares the workbench tree at root, creating it if needed.
func Open(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("workbench: empty root")
	}
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root, now: time.Now}, nil
}

// Root is the workbench directory this store owns.
func (s *Store) Root() string { return s.root }

// newID returns a 16-hex-character identifier from the cryptographic RNG.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on any supported platform; a broken RNG is
		// not something this process can paper over.
		panic("workbench: crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// validID guards every id that arrives from a URL path, so no request can
// steer a file operation out of the workbench tree.
func validID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Store) projectDir(id string) string { return filepath.Join(s.root, "projects", id) }

// WorkspaceDir is the agent's sandbox cwd for a project.
func (s *Store) WorkspaceDir(id string) string { return filepath.Join(s.projectDir(id), "workspace") }

// FilesDir holds a project's attached knowledge.
func (s *Store) FilesDir(id string) string { return filepath.Join(s.projectDir(id), "files") }

func (s *Store) chatPath(projectID, chatID string) string {
	return filepath.Join(s.projectDir(projectID), "chats", chatID+".jsonl")
}

// --- projects --------------------------------------------------------

func (s *Store) CreateProject(name string, set Settings) (*Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("project name is required")
	}
	if err := set.normalize(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	p := &Project{ID: newID(), Name: name, Settings: set, CreatedAt: now, UpdatedAt: now}
	dir := s.projectDir(p.ID)
	for _, sub := range []string{"chats", "files", "workspace"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	if err := s.writeProject(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) writeProject(p *Project) error {
	return writeFileAtomic(filepath.Join(s.projectDir(p.ID), "project.json"), mustJSON(diskProject{
		ID:        p.ID,
		Name:      p.Name,
		Settings:  p.Settings,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}))
}

// Projects lists every project, newest first.
func (s *Store) Projects() ([]*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(filepath.Join(s.root, "projects"))
	if err != nil {
		if os.IsNotExist(err) {
			return []*Project{}, nil
		}
		return nil, err
	}
	out := make([]*Project, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		p, err := s.readProject(e.Name())
		if err != nil {
			continue // a half-written or hand-edited project is skipped, not fatal
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) Project(id string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readProject(id)
}

func (s *Store) readProject(id string) (*Project, error) {
	if !validID(id) {
		return nil, ErrNotFound
	}
	raw, err := os.ReadFile(filepath.Join(s.projectDir(id), "project.json"))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var p Project
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("project %s is corrupt: %w", id, err)
	}
	p.ChatCount = s.countChats(id)
	return &p, nil
}

// countChats counts a project's conversations. The caller holds s.mu.
func (s *Store) countChats(projectID string) int {
	entries, err := os.ReadDir(filepath.Join(s.projectDir(projectID), "chats"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if id := strings.TrimSuffix(e.Name(), ".jsonl"); !e.IsDir() && id != e.Name() && validID(id) {
			n++
		}
	}
	return n
}

// SettingsPatch updates individual settings; nil fields are left alone so a
// partial PATCH cannot silently clear a field the client did not send.
type SettingsPatch struct {
	AgentEnabled *bool   `json:"agent_enabled"`
	ApprovalMode *string `json:"approval_mode"`
	Model        *string `json:"model"`
	SystemPrompt *string `json:"system_prompt"`
}

type ProjectPatch struct {
	Name     *string        `json:"name"`
	Settings *SettingsPatch `json:"settings"`
}

func (s *Store) UpdateProject(id string, patch ProjectPatch) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.readProject(id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			return nil, errors.New("project name is required")
		}
		p.Name = name
	}
	if sp := patch.Settings; sp != nil {
		if sp.AgentEnabled != nil {
			p.Settings.AgentEnabled = *sp.AgentEnabled
		}
		if sp.ApprovalMode != nil {
			p.Settings.ApprovalMode = *sp.ApprovalMode
		}
		if sp.Model != nil {
			p.Settings.Model = *sp.Model
		}
		if sp.SystemPrompt != nil {
			p.Settings.SystemPrompt = *sp.SystemPrompt
		}
	}
	if err := p.Settings.normalize(); err != nil {
		return nil, err
	}
	p.UpdatedAt = s.now().UTC()
	if err := s.writeProject(p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteProject removes a project and everything under it, workspace included.
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(id); err != nil {
		return err
	}
	return os.RemoveAll(s.projectDir(id))
}

// --- chats -----------------------------------------------------------

func (s *Store) CreateChat(projectID, title string) (*Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New chat"
	}
	now := s.now().UTC()
	c := &Chat{ID: newID(), ProjectID: projectID, Title: title, CreatedAt: now, UpdatedAt: now}
	if err := os.MkdirAll(filepath.Dir(s.chatPath(projectID, c.ID)), 0o700); err != nil {
		return nil, err
	}
	meta := Event{Type: "meta", TS: now, ID: c.ID, ProjectID: projectID, Title: title}
	if err := s.appendLine(s.chatPath(projectID, c.ID), meta); err != nil {
		return nil, err
	}
	return c, nil
}

// Chats lists a project's chats, most recently updated first.
func (s *Store) Chats(projectID string) ([]*Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(s.projectDir(projectID), "chats"))
	if err != nil {
		if os.IsNotExist(err) {
			return []*Chat{}, nil
		}
		return nil, err
	}
	out := make([]*Chat, 0, len(entries))
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if e.IsDir() || id == e.Name() || !validID(id) {
			continue
		}
		c, _, err := s.readChat(projectID, id)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Chat returns a chat's header and full event timeline. Chats are addressed
// by id alone, so the owning project is located by scan.
func (s *Store) Chat(id string) (*Chat, []Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectID, err := s.ownerOf(id)
	if err != nil {
		return nil, nil, err
	}
	return s.readChat(projectID, id)
}

// ownerOf finds the project holding a chat. The caller holds s.mu.
func (s *Store) ownerOf(chatID string) (string, error) {
	if !validID(chatID) {
		return "", ErrNotFound
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "projects"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		if _, err := os.Stat(s.chatPath(e.Name(), chatID)); err == nil {
			return e.Name(), nil
		}
	}
	return "", ErrNotFound
}

// Owner reports which project a chat belongs to.
func (s *Store) Owner(chatID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ownerOf(chatID)
}

func (s *Store) readChat(projectID, chatID string) (*Chat, []Event, error) {
	if !validID(projectID) || !validID(chatID) {
		return nil, nil, ErrNotFound
	}
	path := s.chatPath(projectID, chatID)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	c := &Chat{ID: chatID, ProjectID: projectID, Title: "New chat"}
	events := []Event{}
	sc := bufio.NewScanner(f)
	// Tool output can be long; give the scanner room for a capped result line.
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if json.Unmarshal(line, &ev) != nil {
			continue // never let one bad line hide a whole conversation
		}
		if ev.Type == "meta" {
			c.Title = ev.Title
			c.CreatedAt = ev.TS
			c.UpdatedAt = ev.TS
			continue
		}
		if !ev.TS.IsZero() {
			c.UpdatedAt = ev.TS
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	c.Events = len(events)
	return c, events, nil
}

func (s *Store) DeleteChat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectID, err := s.ownerOf(id)
	if err != nil {
		return err
	}
	return os.Remove(s.chatPath(projectID, id))
}

// Append adds one event to a chat's timeline.
func (s *Store) Append(chatID string, ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectID, err := s.ownerOf(chatID)
	if err != nil {
		return err
	}
	if ev.TS.IsZero() {
		ev.TS = s.now().UTC()
	}
	return s.appendLine(s.chatPath(projectID, chatID), ev)
}

func (s *Store) appendLine(path string, ev Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// History replays a chat's message events into model-ready turns. Tool calls
// and their results are deliberately left out: they are timeline detail for
// the UI, and replaying half a tool exchange would confuse the next request.
func (s *Store) History(chatID string) ([]HistoryMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	projectID, err := s.ownerOf(chatID)
	if err != nil {
		return nil, err
	}
	_, events, err := s.readChat(projectID, chatID)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryMessage, 0, len(events))
	for _, ev := range events {
		if ev.Type != "message" || ev.Content == "" {
			continue
		}
		out = append(out, HistoryMessage{Role: ev.Role, Content: ev.Content})
	}
	return out, nil
}

// --- attached knowledge ----------------------------------------------

// safeName reduces a client-supplied filename to a single harmless path
// element.
func safeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	// Browsers still send whole paths as an upload's filename; keep the last
	// element only, whichever separator the client used.
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = strings.TrimSpace(name[i+1:])
	}
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("invalid file name %q", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("invalid file name %q", name)
		}
	}
	if len(name) > 128 {
		return "", fmt.Errorf("file name is too long (max 128 bytes)")
	}
	return name, nil
}

// PutFile stores an attachment, replacing any file of the same name.
func (s *Store) PutFile(projectID, name string, data []byte) (*File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return nil, err
	}
	clean, err := safeName(name)
	if err != nil {
		return nil, err
	}
	dir := s.FilesDir(projectID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, clean), data); err != nil {
		return nil, err
	}
	return &File{
		Name:      clean,
		Size:      int64(len(data)),
		Modified:  s.now().UTC(),
		InContext: foldable(data),
	}, nil
}

func (s *Store) Files(projectID string) ([]File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return nil, err
	}
	return s.listFiles(projectID)
}

func (s *Store) listFiles(projectID string) ([]File, error) {
	entries, err := os.ReadDir(s.FilesDir(projectID))
	if err != nil {
		if os.IsNotExist(err) {
			return []File{}, nil
		}
		return nil, err
	}
	out := make([]File, 0, len(entries))
	budget := knowledgeBudget
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		f := File{Name: e.Name(), Size: info.Size(), Modified: info.ModTime().UTC()}
		if info.Size() <= maxKnowledgeFile && int(info.Size()) <= budget {
			if data, err := os.ReadFile(filepath.Join(s.FilesDir(projectID), e.Name())); err == nil && foldable(data) {
				f.InContext = true
				budget -= len(data)
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadFile returns one attachment's bytes.
func (s *Store) ReadFile(projectID, name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return nil, err
	}
	clean, err := safeName(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.FilesDir(projectID), clean))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return data, err
}

func (s *Store) DeleteFile(projectID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return err
	}
	clean, err := safeName(name)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(s.FilesDir(projectID), clean))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

// foldable reports whether a file is plain text small enough to be worth
// putting in front of the model.
func foldable(data []byte) bool {
	if len(data) == 0 || len(data) > maxKnowledgeFile {
		return false
	}
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

// Knowledge renders the attached text small enough to ride along in the
// system message, plus a pointer to the files that are tool-readable only.
func (s *Store) Knowledge(projectID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.readProject(projectID); err != nil {
		return "", err
	}
	files, err := s.listFiles(projectID)
	if err != nil {
		return "", err
	}
	var folded strings.Builder
	var rest []string
	for _, f := range files {
		if !f.InContext {
			rest = append(rest, f.Name)
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.FilesDir(projectID), f.Name))
		if err != nil {
			rest = append(rest, f.Name)
			continue
		}
		fmt.Fprintf(&folded, "\n--- %s ---\n%s\n", f.Name, strings.TrimRight(string(data), "\n"))
	}
	if folded.Len() == 0 && len(rest) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("Project knowledge:")
	b.WriteString(folded.String())
	if len(rest) > 0 {
		fmt.Fprintf(&b, "\nAlso attached, readable with read_file using the files/ prefix: %s\n",
			strings.Join(rest, ", "))
	}
	return b.String(), nil
}

// --- helpers ---------------------------------------------------------

// writeFileAtomic replaces a file in one step, so a crash mid-write cannot
// leave a truncated project.json behind.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic("workbench: marshalling " + err.Error())
	}
	return append(b, '\n')
}
