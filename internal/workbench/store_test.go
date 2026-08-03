package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// clock makes timestamps deterministic and strictly increasing so ordering
// assertions do not depend on how fast the test machine is.
func (s *Store) clock(start time.Time) {
	tick := start
	s.now = func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
}

func mustProject(t *testing.T, s *Store, name string) *Project {
	t.Helper()
	p, err := s.CreateProject(name, Settings{})
	if err != nil {
		t.Fatalf("CreateProject(%q): %v", name, err)
	}
	return p
}

func mustChat(t *testing.T, s *Store, projectID, title string) *Chat {
	t.Helper()
	c, err := s.CreateChat(projectID, title)
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	return c
}

func TestDefaultDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if want := filepath.Join("/xdg/data", "prism", "workbench"); dir != want {
		t.Errorf("DefaultDir = %q, want %q", dir, want)
	}

	t.Setenv("XDG_DATA_HOME", "")
	dir, err = DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, ".local", "share", "prism", "workbench"); dir != want {
		t.Errorf("DefaultDir = %q, want %q", dir, want)
	}
}

func TestCreateProjectLaysOutTheTree(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "  Research  ")

	if p.Name != "Research" {
		t.Errorf("name = %q, want the trimmed name", p.Name)
	}
	if !validID(p.ID) {
		t.Errorf("id = %q, want 16 hex characters", p.ID)
	}
	if p.Settings.ApprovalMode != ApprovalMutations {
		t.Errorf("approval_mode = %q, want the mutations default", p.Settings.ApprovalMode)
	}
	if p.Settings.AgentEnabled {
		t.Error("agent_enabled defaults to true; tools must be opt-in")
	}
	for _, sub := range []string{"chats", "files", "workspace"} {
		if info, err := os.Stat(filepath.Join(s.projectDir(p.ID), sub)); err != nil || !info.IsDir() {
			t.Errorf("%s/ was not created: %v", sub, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(s.projectDir(p.ID), "project.json"))
	if err != nil {
		t.Fatalf("project.json: %v", err)
	}
	var onDisk Project
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("project.json is not JSON: %v", err)
	}
	if onDisk.Name != "Research" {
		t.Errorf("on-disk name = %q, want Research", onDisk.Name)
	}
	// The chat count is derived on every read; storing it would let the file
	// disagree with the directory.
	if strings.Contains(string(raw), "chat_count") {
		t.Errorf("project.json stores the derived chat count:\n%s", raw)
	}
}

func TestProjectChatCountIsDerived(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")
	if p.ChatCount != 0 {
		t.Errorf("chat_count = %d on a new project, want 0", p.ChatCount)
	}

	first := mustChat(t, s, p.ID, "one")
	mustChat(t, s, p.ID, "two")

	got, err := s.Project(p.ID)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.ChatCount != 2 {
		t.Errorf("chat_count = %d, want 2", got.ChatCount)
	}
	// It is always present in the API shape, including as zero.
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"chat_count":2`) {
		t.Errorf("marshalled project = %s, want chat_count", b)
	}
	empty := mustProject(t, s, "empty")
	b, _ = json.Marshal(empty)
	if !strings.Contains(string(b), `"chat_count":0`) {
		t.Errorf("empty project = %s, want an explicit chat_count of 0", b)
	}

	list, err := s.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	counts := map[string]int{}
	for _, item := range list {
		counts[item.Name] = item.ChatCount
	}
	if counts["proj"] != 2 || counts["empty"] != 0 {
		t.Errorf("listed counts = %v, want proj=2 empty=0", counts)
	}

	// Deleting a chat brings it back down, and a PATCH does not freeze it.
	if err := s.DeleteChat(first.ID); err != nil {
		t.Fatal(err)
	}
	name := "renamed"
	updated, err := s.UpdateProject(p.ID, ProjectPatch{Name: &name})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.ChatCount != 1 {
		t.Errorf("chat_count after a delete = %d, want 1", updated.ChatCount)
	}
	raw, _ := os.ReadFile(filepath.Join(s.projectDir(p.ID), "project.json"))
	if strings.Contains(string(raw), "chat_count") {
		t.Errorf("a PATCH wrote the derived count to disk:\n%s", raw)
	}
}

func TestCreateProjectRejectsBadInput(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreateProject("   ", Settings{}); err == nil {
		t.Error("an empty project name was accepted")
	}
	if _, err := s.CreateProject("x", Settings{ApprovalMode: "sometimes"}); err == nil {
		t.Error("an unknown approval_mode was accepted")
	}
}

func TestProjectsListNewestFirst(t *testing.T) {
	s := newStore(t)
	s.clock(time.Unix(1_700_000_000, 0).UTC())
	first := mustProject(t, s, "first")
	second := mustProject(t, s, "second")

	list, err := s.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d projects, want 2", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("order = [%s %s], want the newest first", list[0].Name, list[1].Name)
	}
}

func TestProjectsIsEmptyNotNil(t *testing.T) {
	s := newStore(t)
	list, err := s.Projects()
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if list == nil {
		t.Fatal("Projects returned nil; the API must serialise [] not null")
	}
	b, _ := json.Marshal(list)
	if string(b) != "[]" {
		t.Errorf("marshalled = %s, want []", b)
	}
}

func TestUpdateProjectPatchesOneFieldAtATime(t *testing.T) {
	s := newStore(t)
	p, err := s.CreateProject("proj", Settings{
		AgentEnabled: true, ApprovalMode: ApprovalAlways, Model: "gpt-test", SystemPrompt: "be terse",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	mode := ApprovalNever
	updated, err := s.UpdateProject(p.ID, ProjectPatch{Settings: &SettingsPatch{ApprovalMode: &mode}})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Settings.ApprovalMode != ApprovalNever {
		t.Errorf("approval_mode = %q, want never", updated.Settings.ApprovalMode)
	}
	if updated.Settings.Model != "gpt-test" {
		t.Errorf("model = %q, want the untouched gpt-test", updated.Settings.Model)
	}
	if updated.Settings.SystemPrompt != "be terse" {
		t.Errorf("system_prompt = %q, want it left alone", updated.Settings.SystemPrompt)
	}
	if !updated.Settings.AgentEnabled {
		t.Error("agent_enabled was cleared by a patch that did not mention it")
	}

	name := "renamed"
	updated, err = s.UpdateProject(p.ID, ProjectPatch{Name: &name})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != "renamed" || updated.Settings.ApprovalMode != ApprovalNever {
		t.Errorf("after rename = %+v, want the settings preserved", updated.Settings)
	}

	// The change is durable.
	reread, err := s.Project(p.ID)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if reread.Name != "renamed" {
		t.Errorf("reread name = %q, want renamed", reread.Name)
	}

	bad := "whenever"
	if _, err := s.UpdateProject(p.ID, ProjectPatch{Settings: &SettingsPatch{ApprovalMode: &bad}}); err == nil {
		t.Error("an unknown approval_mode was accepted by PATCH")
	}
}

func TestDeleteProjectRemovesEverything(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "doomed")
	c := mustChat(t, s, p.ID, "chat")
	if _, err := s.PutFile(p.ID, "note.txt", []byte("hi")); err != nil {
		t.Fatalf("PutFile: %v", err)
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := os.Stat(s.projectDir(p.ID)); !os.IsNotExist(err) {
		t.Errorf("project directory survived deletion: %v", err)
	}
	if _, err := s.Project(p.ID); err != ErrNotFound {
		t.Errorf("Project after delete = %v, want ErrNotFound", err)
	}
	if _, _, err := s.Chat(c.ID); err != ErrNotFound {
		t.Errorf("Chat after project delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteProject(p.ID); err != ErrNotFound {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

// Ids arriving from a URL must never steer a file operation: only 16 hex
// characters are addressable.
func TestIdsAreValidatedNotJoined(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")

	for _, id := range []string{
		"..", "../..", "../" + p.ID, "/etc", strings.Repeat("a", 16) + "/x", "", "ZZZZZZZZZZZZZZZZ",
	} {
		if _, err := s.Project(id); err != ErrNotFound {
			t.Errorf("Project(%q) = %v, want ErrNotFound", id, err)
		}
		if _, _, err := s.Chat(id); err != ErrNotFound {
			t.Errorf("Chat(%q) = %v, want ErrNotFound", id, err)
		}
		if err := s.DeleteProject(id); err != ErrNotFound {
			t.Errorf("DeleteProject(%q) = %v, want ErrNotFound", id, err)
		}
	}
	if !validID(p.ID) {
		t.Errorf("a real id %q failed validation", p.ID)
	}
}

func TestChatLifecycle(t *testing.T) {
	s := newStore(t)
	s.clock(time.Unix(1_700_000_000, 0).UTC())
	p := mustProject(t, s, "proj")

	c := mustChat(t, s, p.ID, "  Design notes  ")
	if c.Title != "Design notes" {
		t.Errorf("title = %q, want it trimmed", c.Title)
	}
	if c.ProjectID != p.ID {
		t.Errorf("project_id = %q, want %q", c.ProjectID, p.ID)
	}
	untitled := mustChat(t, s, p.ID, "")
	if untitled.Title != "New chat" {
		t.Errorf("default title = %q, want New chat", untitled.Title)
	}

	chats, err := s.Chats(p.ID)
	if err != nil {
		t.Fatalf("Chats: %v", err)
	}
	if len(chats) != 2 {
		t.Fatalf("got %d chats, want 2", len(chats))
	}
	if chats[0].ID != untitled.ID {
		t.Errorf("first chat = %q, want the most recently updated", chats[0].Title)
	}

	// Chats are addressable by id alone, across projects.
	other := mustProject(t, s, "other")
	mustChat(t, s, other.ID, "elsewhere")
	found, _, err := s.Chat(c.ID)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if found.ProjectID != p.ID || found.Title != "Design notes" {
		t.Errorf("Chat = %+v, want the design-notes chat in %s", found, p.ID)
	}
	if owner, err := s.Owner(c.ID); err != nil || owner != p.ID {
		t.Errorf("Owner = %q, %v, want %q", owner, err, p.ID)
	}

	if err := s.DeleteChat(c.ID); err != nil {
		t.Fatalf("DeleteChat: %v", err)
	}
	if _, _, err := s.Chat(c.ID); err != ErrNotFound {
		t.Errorf("Chat after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteChat(c.ID); err != ErrNotFound {
		t.Errorf("second DeleteChat = %v, want ErrNotFound", err)
	}
	if _, err := s.CreateChat("0123456789abcdef", "x"); err != ErrNotFound {
		t.Errorf("CreateChat on an unknown project = %v, want ErrNotFound", err)
	}
}

// The timeline is a JSONL file: one event per line, every field surviving the
// round trip in order.
func TestChatEventsRoundTripThroughJSONL(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")
	c := mustChat(t, s, p.ID, "run")

	want := []Event{
		{Type: "message", Role: "user", Content: "list the files"},
		{Type: EventToolCall, ID: "call_1", Name: ToolListDir, Args: json.RawMessage(`{"path":"."}`)},
		{Type: EventToolResult, ID: "call_1", Name: ToolListDir, Output: "a.txt\t3 bytes\n", Truncated: true},
		{Type: "message", Role: "assistant", Content: "there is one file"},
		{Type: EventError, Message: "boom"},
	}
	for _, ev := range want {
		if err := s.Append(c.ID, ev); err != nil {
			t.Fatalf("Append(%s): %v", ev.Type, err)
		}
	}

	raw, err := os.ReadFile(s.chatPath(p.ID, c.ID))
	if err != nil {
		t.Fatalf("reading the chat file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != len(want)+1 {
		t.Fatalf("file has %d lines, want %d (meta + events)", len(lines), len(want)+1)
	}
	for i, line := range lines {
		var probe map[string]any
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("line %d is not one JSON object: %v (%s)", i, err, line)
		}
		if probe["ts"] == nil {
			t.Errorf("line %d has no ts", i)
		}
	}
	var meta Event
	json.Unmarshal([]byte(lines[0]), &meta)
	if meta.Type != "meta" || meta.Title != "run" || meta.ProjectID != p.ID {
		t.Errorf("first line = %+v, want the chat header", meta)
	}

	chat, got, err := s.Chat(c.ID)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read back %d events, want %d", len(got), len(want))
	}
	if chat.Events != len(want) {
		t.Errorf("chat.Events = %d, want %d", chat.Events, len(want))
	}
	for i := range want {
		if got[i].Type != want[i].Type || got[i].ID != want[i].ID || got[i].Content != want[i].Content ||
			got[i].Output != want[i].Output || got[i].Truncated != want[i].Truncated ||
			got[i].Message != want[i].Message || got[i].Role != want[i].Role {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
		if got[i].TS.IsZero() {
			t.Errorf("event %d lost its timestamp", i)
		}
	}
	if string(got[1].Args) != `{"path":"."}` {
		t.Errorf("args = %s, want the raw JSON object preserved", got[1].Args)
	}
	if !chat.UpdatedAt.After(chat.CreatedAt) {
		t.Errorf("updated_at %v did not advance past created_at %v", chat.UpdatedAt, chat.CreatedAt)
	}
}

// One unreadable line must not hide the rest of a conversation.
func TestChatSurvivesACorruptLine(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")
	c := mustChat(t, s, p.ID, "run")
	if err := s.Append(c.ID, Event{Type: "message", Role: "user", Content: "one"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.chatPath(p.ID, c.ID), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{not json at all\n\n")
	f.Close()
	if err := s.Append(c.ID, Event{Type: "message", Role: "assistant", Content: "two"}); err != nil {
		t.Fatal(err)
	}

	_, events, err := s.Chat(c.ID)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want the 2 readable ones", len(events))
	}
}

func TestHistoryReplaysMessagesOnly(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")
	c := mustChat(t, s, p.ID, "run")

	for _, ev := range []Event{
		{Type: "message", Role: "user", Content: "hello"},
		{Type: EventToolCall, ID: "call_1", Name: ToolShell},
		{Type: EventToolResult, ID: "call_1", Output: "output nobody should replay"},
		{Type: "message", Role: "assistant", Content: "hi"},
		{Type: EventError, Message: "an error is not a turn"},
		{Type: "message", Role: "user", Content: ""}, // empty content is skipped
	} {
		if err := s.Append(c.ID, ev); err != nil {
			t.Fatal(err)
		}
	}

	history, err := s.History(c.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	want := []HistoryMessage{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if len(history) != len(want) {
		t.Fatalf("history = %+v, want %+v", history, want)
	}
	for i := range want {
		if history[i] != want[i] {
			t.Errorf("history[%d] = %+v, want %+v", i, history[i], want[i])
		}
	}
	if _, err := s.History("0123456789abcdef"); err != ErrNotFound {
		t.Errorf("History on an unknown chat = %v, want ErrNotFound", err)
	}
}

func TestAppendToUnknownChat(t *testing.T) {
	s := newStore(t)
	if err := s.Append("0123456789abcdef", Event{Type: "message"}); err != ErrNotFound {
		t.Errorf("Append = %v, want ErrNotFound", err)
	}
}

// --- attached knowledge ----------------------------------------------

func TestFileNamesAreReducedToOnePathElement(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")

	for _, bad := range []string{"", "  ", ".", "..", "../..", ".hidden", "/", "a\x00b", "x\ny"} {
		if _, err := s.PutFile(p.ID, bad, []byte("x")); err == nil {
			t.Errorf("PutFile(%q) was accepted", bad)
		}
	}
	long := strings.Repeat("n", 200) + ".txt"
	if _, err := s.PutFile(p.ID, long, []byte("x")); err == nil {
		t.Error("an overlong file name was accepted")
	}

	// A directory-flavoured name (browsers still send them) keeps only its
	// last element, so an upload always lands inside the project's files/.
	for _, tc := range []struct{ given, want string }{
		{"some/dir/notes.md", "notes.md"},
		{`..\..\windows\system32\hosts`, "hosts"},
		{"../../etc/passwd", "passwd"},
	} {
		f, err := s.PutFile(p.ID, tc.given, []byte("body"))
		if err != nil {
			t.Fatalf("PutFile(%q): %v", tc.given, err)
		}
		if f.Name != tc.want {
			t.Errorf("PutFile(%q) stored %q, want %q", tc.given, f.Name, tc.want)
		}
		if _, err := os.Stat(filepath.Join(s.FilesDir(p.ID), tc.want)); err != nil {
			t.Errorf("%q did not land in files/: %v", tc.given, err)
		}
	}
	entries, _ := os.ReadDir(s.FilesDir(p.ID))
	if len(entries) != 3 {
		t.Errorf("files/ holds %d entries, want 3 flat files", len(entries))
	}
	// Nothing escaped upwards.
	if _, err := os.Stat(filepath.Join(s.projectDir(p.ID), "passwd")); err == nil {
		t.Error("an upload escaped the files/ directory")
	}
}

func TestFilesCRUD(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")

	if _, err := s.PutFile(p.ID, "a.txt", []byte("alpha")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutFile(p.ID, "b.bin", []byte{0x00, 0x01, 0x02}); err != nil {
		t.Fatal(err)
	}
	files, err := s.Files(p.ID)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 2 || files[0].Name != "a.txt" || files[1].Name != "b.bin" {
		t.Fatalf("files = %+v, want a.txt and b.bin sorted", files)
	}
	if !files[0].InContext {
		t.Error("a small text file should be folded into the system message")
	}
	if files[1].InContext {
		t.Error("a binary file must not be folded into the system message")
	}
	if files[0].Size != 5 {
		t.Errorf("size = %d, want 5", files[0].Size)
	}

	data, err := s.ReadFile(p.ID, "a.txt")
	if err != nil || string(data) != "alpha" {
		t.Errorf("ReadFile = %q, %v", data, err)
	}
	// Overwrite in place.
	if _, err := s.PutFile(p.ID, "a.txt", []byte("beta")); err != nil {
		t.Fatal(err)
	}
	data, _ = s.ReadFile(p.ID, "a.txt")
	if string(data) != "beta" {
		t.Errorf("after overwrite = %q, want beta", data)
	}

	if err := s.DeleteFile(p.ID, "a.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := s.ReadFile(p.ID, "a.txt"); err != ErrNotFound {
		t.Errorf("ReadFile after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteFile(p.ID, "a.txt"); err != ErrNotFound {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
	if _, err := s.Files("0123456789abcdef"); err != ErrNotFound {
		t.Errorf("Files on an unknown project = %v, want ErrNotFound", err)
	}
}

func TestKnowledgeFoldsSmallTextAndNamesTheRest(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")

	if _, err := s.PutFile(p.ID, "brief.md", []byte("ship it by friday\n")); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", maxKnowledgeFile+1)
	if _, err := s.PutFile(p.ID, "corpus.txt", []byte(big)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutFile(p.ID, "image.bin", []byte{0, 1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	k, err := s.Knowledge(p.ID)
	if err != nil {
		t.Fatalf("Knowledge: %v", err)
	}
	if !strings.HasPrefix(k, "Project knowledge:") {
		t.Errorf("knowledge = %q, want the contracted prefix", k)
	}
	if !strings.Contains(k, "ship it by friday") {
		t.Error("the small text attachment was not folded in")
	}
	if !strings.Contains(k, "--- brief.md ---") {
		t.Error("the folded attachment is not labelled with its name")
	}
	if strings.Contains(k, big) {
		t.Error("an oversized attachment was folded into the prompt")
	}
	for _, name := range []string{"corpus.txt", "image.bin"} {
		if !strings.Contains(k, name) {
			t.Errorf("knowledge does not mention the tool-readable file %q", name)
		}
	}
	if !strings.Contains(k, "files/") {
		t.Error("knowledge does not tell the model how to read the rest")
	}
}

// The fold budget is a total, not a per-file allowance.
func TestKnowledgeStopsAtTheTotalBudget(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")

	chunk := strings.Repeat("a", maxKnowledgeFile)
	for _, name := range []string{"1.txt", "2.txt", "3.txt"} {
		if _, err := s.PutFile(p.ID, name, []byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	k, err := s.Knowledge(p.ID)
	if err != nil {
		t.Fatalf("Knowledge: %v", err)
	}
	if len(k) > knowledgeBudget+1024 {
		t.Errorf("knowledge is %d bytes, want it held near the %d budget", len(k), knowledgeBudget)
	}
	files, _ := s.Files(p.ID)
	folded := 0
	for _, f := range files {
		if f.InContext {
			folded++
		}
	}
	if folded != 1 {
		t.Errorf("%d files folded, want 1 to fit the budget", folded)
	}
}

func TestKnowledgeEmptyWithoutAttachments(t *testing.T) {
	s := newStore(t)
	p := mustProject(t, s, "proj")
	k, err := s.Knowledge(p.ID)
	if err != nil {
		t.Fatalf("Knowledge: %v", err)
	}
	if k != "" {
		t.Errorf("knowledge = %q, want empty", k)
	}
}

func TestNewIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id := newID()
		if !validID(id) {
			t.Fatalf("newID produced %q", id)
		}
		if seen[id] {
			t.Fatalf("newID repeated %q after %d draws", id, i)
		}
		seen[id] = true
	}
}
