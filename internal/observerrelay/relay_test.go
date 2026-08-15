package observerrelay

import (
	"errors"
	"reflect"
	"testing"
)

func TestCodexBinaryDefaultsToCodex(t *testing.T) {
	t.Setenv("AGENTBOARD_CODEX_BIN", "")
	if actual := codexBinary(); actual != "codex" {
		t.Fatalf("unexpected default binary: %q", actual)
	}
}

func TestResumeCommandAllowsExistingSessionsOutsideGitRepository(t *testing.T) {
	command := resumeCommand("codex", "session_1", "continue the task")
	want := []string{"codex", "exec", "resume", "--skip-git-repo-check", "session_1", "continue the task"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("unexpected resume command: got %#v, want %#v", command.Args, want)
	}
}

func TestResumeFailureMessageUsesTheLastDiagnosticLine(t *testing.T) {
	message := resumeFailureMessage(errors.New("exit status 1"), "warning: ignored setting\nerror: Codex authentication expired\n")
	if message != "Codex resume failed: error: Codex authentication expired" {
		t.Fatalf("unexpected failure message: %q", message)
	}
}
