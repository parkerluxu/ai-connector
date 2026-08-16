package geminiobserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentboard/ai-connector/internal/observation"
)

func TestObserverReducesGeminiSessionAndMapsProjectRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTBOARD_GEMINI_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "projects.json"), []byte("{\"projects\":{\"C:/work/gemini\":\"project_hash\"}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "tmp")
	path := filepath.Join(root, "project", "chats", "session-demo.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "{\"sessionId\":\"gemini_1\",\"projectHash\":\"project_hash\",\"startTime\":\"2026/8/16 00:00:00\"}\n" +
		"{\"type\":\"user\",\"timestamp\":\"2026/8/16 00:00:01\",\"content\":[{\"text\":\"secret prompt\"}]}\n" +
		"{\"type\":\"gemini\",\"timestamp\":\"2026/8/16 00:00:02\",\"content\":\"safe response\",\"thoughts\":[\"must not expose\"]}\n"
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
	session, ok := observer.Get("gemini_1")
	if !ok {
		t.Fatal("session not discovered")
	}
	if session.Agent != observation.AgentGeminiCLI || session.Status != observation.StatusIdle || session.CWD != "C:/work/gemini" || !session.CanResume {
		t.Fatalf("unexpected session: %#v", session)
	}
	detail, err := observer.Detail("gemini_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 2 || detail.Events[1].Content != "safe response" {
		t.Fatalf("unexpected detail: %#v", detail)
	}
}
