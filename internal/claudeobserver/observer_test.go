package claudeobserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentboard/ai-connector/internal/observation"
)

func TestObserverReducesClaudeSessionWithoutLeakingTranscript(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "claude-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "{\"type\":\"user\",\"sessionId\":\"claude_1\",\"cwd\":\"C:/work/claude\",\"timestamp\":\"2026-08-16T00:00:00Z\",\"message\":{\"content\":\"secret prompt\"}}\n" +
		"{\"type\":\"assistant\",\"sessionId\":\"claude_1\",\"cwd\":\"C:/work/claude\",\"timestamp\":\"2026-08-16T00:00:01Z\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\":{\"command\":\"cat secret\"}}]}}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{SessionsRoot: root, DeviceID: "dev_1"})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	session, ok := observer.Get("claude_1")
	if !ok {
		t.Fatal("session not discovered")
	}
	if session.Agent != observation.AgentClaudeCode || session.Status != observation.StatusActive || session.CurrentTool == nil || session.CurrentTool.Name != "Bash" {
		t.Fatalf("unexpected session: %#v", session)
	}
	if session.Title == "secret prompt" || session.CWD != "C:/work/claude" {
		t.Fatalf("unsafe session projection: %#v", session)
	}
	detail, err := observer.Detail("claude_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 2 || detail.Events[1].Content != "Tool invocation" {
		t.Fatalf("unexpected detail: %#v", detail)
	}
}
