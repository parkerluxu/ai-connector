package codexobserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestObserverBuildsMinimalSessionAndDoesNotLeakContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026", "08", "12", "rollout-demo.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_1\",\"cwd\":\"C:/work/demo\",\"source\":\"cli\"}}\n" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn_1\",\"message\":\"secret prompt\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"shell_command\",\"arguments\":{\"command\":\"cat secret\"}}}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	session, ok := observer.Get("sess_1")
	if !ok {
		t.Fatal("session not discovered")
	}
	if session.Status != StatusActive || session.CurrentTool == nil || session.CurrentTool.Name != "shell_command" {
		t.Fatalf("unexpected session: %#v", session)
	}
	if session.SessionDirectory != "2026/08/12" {
		t.Fatalf("unexpected session directory: %#v", session)
	}
	if session.Title == "secret prompt" || session.CWD != "C:/work/demo" {
		t.Fatalf("unexpected safe fields: %#v", session)
	}
	_ = observer.Close()
}

func TestObserverBuffersShortWritesAndHandlesTruncation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-demo.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_2\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", PollInterval: time.Millisecond, MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := observer.Get("sess_2"); ok {
		t.Fatal("short write must not be parsed")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("}\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := observer.Get("sess_2"); !ok {
		t.Fatal("completed line was not parsed")
	}
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := observer.Get("sess_2"); ok {
		t.Fatal("truncation should remove stale state")
	}
	_ = observer.Close()
}

func TestObserverReducesDesktopLifecycleAndRebuildsAfterRestart(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-desktop.jsonl")
	dbPath := filepath.Join(root, "observer.db")
	data := "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_desktop\",\"cwd\":\"/work/desktop\",\"source\":\"desktop\"}}\n" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn_1\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call\",\"name\":\"browser\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call_output\",\"status\":\"failed\"}}\n" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"turn_aborted\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	session, ok := observer.Get("sess_desktop")
	if !ok || session.Source != "desktop" || session.Status != StatusAborted || session.CurrentTool == nil || session.CurrentTool.Status != ToolFailed {
		t.Fatalf("unexpected reduced desktop session: %#v", session)
	}
	_ = observer.Close()

	restarted, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Get("sess_desktop"); !ok {
		t.Fatal("observer restart must rebuild sessions from local JSONL")
	}
}

func TestObserverRecognizesDesktopOriginator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-originator.jsonl")
	data := "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_originator\",\"cwd\":\"/work\",\"originator\":\"Codex Desktop\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	session, ok := observer.Get("sess_originator")
	if !ok || session.Source != "desktop" {
		t.Fatalf("unexpected source: %#v", session)
	}
}

func TestObserverMarksMalformedKnownSessionUnknown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-unknown.jsonl")
	data := "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_unknown\",\"cwd\":\"/work\"}}\n{not-json}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	session, ok := observer.Get("sess_unknown")
	if !ok || session.Status != StatusUnknown || session.StateConfidence != "inferred" {
		t.Fatalf("unexpected unknown session: %#v", session)
	}
}

func TestObserverKeepsSessionWhenItsJSONLFileIsRenamed(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "rollout-original.jsonl")
	renamed := filepath.Join(root, "rollout-renamed.jsonl")
	data := "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_rename\",\"cwd\":\"/work\"}}\n"
	if err := os.WriteFile(original, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := observer.Get("sess_rename"); !ok {
		t.Fatal("renaming a JSONL file must not remove its observed session")
	}
}

func TestObserverDetailReadsOnlySupportedContentEvents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026", "08", "12", "rollout-detail.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_detail\",\"cwd\":\"C:/work\"}}\n" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"Please inspect this task\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"shell_command\",\"arguments\":\"{\\\"command\\\":\\\"dir\\\"}\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"function_call_output\",\"output\":\"file list\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"encrypted_content\":\"must not be exposed\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	detail, err := observer.Detail("sess_detail")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 3 || detail.Events[1].ToolName != "shell_command" || detail.Events[2].Content != "file list" {
		t.Fatalf("unexpected detail: %#v", detail)
	}
}

func TestObserverAliasesResumeFileToParentSession(t *testing.T) {
	root := t.TempDir()
	parentPath := filepath.Join(root, "2026", "08", "13", "rollout-parent.jsonl")
	resumePath := filepath.Join(root, "2026", "08", "13", "rollout-resume.jsonl")
	startedAt := time.Now().UTC().Truncate(time.Second)
	parentAt := startedAt.Add(-time.Minute).Format(time.RFC3339)
	resumeAt := startedAt.Add(time.Second).Format(time.RFC3339)
	activityAt := startedAt.Add(2 * time.Second).Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(parentPath), 0o700); err != nil {
		t.Fatal(err)
	}
	parentData := "{\"timestamp\":\"" + parentAt + "\",\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_parent\",\"cwd\":\"C:/work\"}}\n" +
		"{\"timestamp\":\"" + parentAt + "\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"original\"}}\n"
	if err := os.WriteFile(parentPath, []byte(parentData), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	observer.RegisterResume("sess_parent", "C:/work", startedAt)
	resumeData := "{\"timestamp\":\"" + resumeAt + "\",\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_new\",\"cwd\":\"C:/work\",\"context_window\":{\"window_id\":\"sess_parent\"}}}\n" +
		"{\"timestamp\":\"" + resumeAt + "\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn_new\"}}\n" +
		"{\"timestamp\":\"" + activityAt + "\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"continued\"}}\n"
	if err := os.WriteFile(resumePath, []byte(resumeData), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, exists := observer.Get("sess_new"); exists {
		t.Fatalf("resumed JSONL must not create a separate observed session: %#v", observer.Sessions())
	}
	parent, exists := observer.Get("sess_parent")
	if !exists || parent.Status != StatusActive || parent.LastActivityAt != activityAt {
		t.Fatalf("resume state was not projected onto parent: %#v", parent)
	}
	detail, err := observer.Detail("sess_parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 2 || detail.Events[0].Content != "original" || detail.Events[1].Content != "continued" {
		t.Fatalf("expected merged parent and continuation detail: %#v", detail)
	}
	_ = observer.Close()
	restarted, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, exists := restarted.Get("sess_new"); exists {
		t.Fatal("persisted continuation must remain aliased after restart")
	}
}

func TestObserverDiscardsContinuationMappingWithoutLocalParentRollout(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026", "08", "13", "rollout-parent.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"sess_parent\",\"cwd\":\"C:/work\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1", MetadataPath: filepath.Join(root, "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.metadata.SaveContinuation("sess_parent", "context_window_only"); err != nil {
		t.Fatal(err)
	}
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, exists := observer.Get("context_window_only"); exists {
		t.Fatalf("an internal context-window ID must not replace a local rollout ID: %#v", observer.Sessions())
	}
	if _, exists := observer.Get("sess_parent"); !exists {
		t.Fatalf("local rollout session was not retained: %#v", observer.Sessions())
	}
	parentID, err := observer.metadata.ContinuationParent("sess_parent")
	if err != nil || parentID != "" {
		t.Fatalf("invalid continuation mapping was not removed: %q, %v", parentID, err)
	}
}
