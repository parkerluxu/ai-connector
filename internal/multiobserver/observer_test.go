package multiobserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResumeCommandsAreFixedPerAgent(t *testing.T) {
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	claudeHome := filepath.Join(root, "claude")
	geminiHome := filepath.Join(root, "gemini")
	t.Setenv("AGENTBOARD_CLAUDE_HOME", claudeHome)
	t.Setenv("AGENTBOARD_GEMINI_HOME", geminiHome)
	t.Setenv("AGENTBOARD_CODEX_BIN", "codex-test")
	t.Setenv("AGENTBOARD_CLAUDE_BIN", "claude-test")
	t.Setenv("AGENTBOARD_GEMINI_BIN", "gemini-test")
	writeFixture(t, filepath.Join(codexRoot, "2026", "08", "16", "session.jsonl"), "{\"type\":\"session_meta\",\"payload\":{\"session_id\":\"codex_1\",\"cwd\":\"C:/work/codex\"}}\n")
	writeFixture(t, filepath.Join(claudeHome, "projects", "project", "session.jsonl"), "{\"type\":\"assistant\",\"sessionId\":\"claude_1\",\"cwd\":\"C:/work/claude\",\"timestamp\":\"2026-08-16T00:00:00Z\",\"message\":{\"content\":\"done\"}}\n")
	writeFixture(t, filepath.Join(geminiHome, "projects.json"), "{\"projects\":{\"C:/work/gemini\":\"project_hash\"}}")
	writeFixture(t, filepath.Join(geminiHome, "tmp", "project", "chats", "session.jsonl"), "{\"sessionId\":\"gemini_1\",\"projectHash\":\"project_hash\",\"startTime\":\"2026/8/16 00:00:00\"}\n")
	observer, err := New(Config{DeviceID: "dev_1", CodexSessionsRoot: codexRoot, MetadataPath: filepath.Join(root, "metadata.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id   string
		want []string
	}{
		{publicID("codex", "codex_1"), []string{"codex-test", "exec", "resume", "--skip-git-repo-check", "codex_1", "continue"}},
		{publicID("claude_code", "claude_1"), []string{"claude-test", "--resume", "claude_1", "--print", "continue"}},
		{publicID("gemini_cli", "gemini_1"), []string{"gemini-test", "--resume", "gemini_1", "--prompt", "continue"}},
	}
	for _, test := range tests {
		command, err := observer.ResumeCommand(test.id, "continue")
		if err != nil {
			t.Fatalf("%s: %v", test.id, err)
		}
		if !reflect.DeepEqual(command.Args, test.want) {
			t.Fatalf("%s: got %#v, want %#v", test.id, command.Args, test.want)
		}
	}
	projected, ok := observer.Get(publicID("claude_code", "claude_1"))
	if !ok {
		t.Fatal("projected Claude session not found")
	}
	payload, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "claude_1") {
		t.Fatalf("native session id leaked from observation payload: %s", payload)
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
