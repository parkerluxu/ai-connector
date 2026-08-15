//go:build windows

package terminal

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/UserExistsError/conpty"
)

func TestWindowsCommandLineQuotesExecutableAndArguments(t *testing.T) {
	command := exec.Command(`C:\Program Files\Agent Board\tool.exe`, `two words`, `quote\"here`, `ends\\`)
	actual, err := windowsCommandLine(command)
	if err != nil {
		t.Fatal(err)
	}
	const expected = `"C:\Program Files\Agent Board\tool.exe" "two words" "quote\\\"here" ends\\`
	if actual != expected {
		t.Fatalf("unexpected command line %q", actual)
	}
}

func TestWindowsSessionForwardsInteractiveInput(t *testing.T) {
	if os.Getenv("AGENTBOARD_RUN_CONPTY_INTEGRATION") != "1" {
		t.Skip("set AGENTBOARD_RUN_CONPTY_INTEGRATION=1 to exercise the Windows ConPTY integration")
	}
	if !conpty.IsConPtyAvailable() {
		t.Skip("ConPTY is unavailable on this version of Windows")
	}
	command := exec.Command("cmd.exe", "/d", "/v:on", "/c", "set /p value= & echo received:!value!")
	session, err := Start(command, Size{Width: 100, Height: 32})
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, session)
		close(readDone)
	}()
	if _, err := session.Write([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- session.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interactive ConPTY process did not exit")
	}
	// ConPTY closes all native handles at once. Close exactly once after Wait
	// to release the reader without relying on a second, invalid close.
	_ = session.Close()
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ConPTY output reader did not finish")
	}
	if !strings.Contains(output.String(), "received:hello") {
		t.Fatalf("unexpected ConPTY output %q", output.String())
	}
}
