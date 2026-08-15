//go:build !windows

package terminal

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestUnixSessionProvidesInteractiveInputAndOutput(t *testing.T) {
	session, err := Start(exec.Command("sh", "-c", "read value; printf 'received:%s\\n' \"$value\""), Size{Width: 100, Height: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	output, _ := io.ReadAll(session) // Linux PTYs commonly finish with EIO after the child exits.
	if err := session.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "received:hello") {
		t.Fatalf("unexpected PTY output %q", output)
	}
}
