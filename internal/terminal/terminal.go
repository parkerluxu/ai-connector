// Package terminal starts local tools in a pseudo-terminal for interactive runs.
package terminal

import (
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

const (
	defaultWidth  = 80
	defaultHeight = 24
)

// Size is measured in terminal cells.
type Size struct {
	Width  int
	Height int
}

// Session is the bidirectional terminal attached to one local process.
type Session interface {
	io.Reader
	io.Writer
	io.Closer
	Wait() error
	ReleaseAfterWait() error
	PID() int
	Resize(Size) error
}

// LocalSize returns the active output terminal dimensions, with a useful default
// for redirected output and terminals that do not report a size.
func LocalSize(output *os.File) Size {
	if output != nil {
		if width, height, err := term.GetSize(int(output.Fd())); err == nil && width > 0 && height > 0 {
			return Size{Width: width, Height: height}
		}
	}
	return Size{Width: defaultWidth, Height: defaultHeight}
}

// PrepareLocalTerminal switches a local keyboard into raw mode so interactive
// tools receive individual keystrokes, and enables ANSI output where required.
func PrepareLocalTerminal(input, output *os.File) (func(), error) {
	restoreOutput, err := enableOutput(output)
	if err != nil {
		return nil, err
	}
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return restoreOutput, nil
	}
	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		restoreOutput()
		return nil, err
	}
	return func() {
		_ = term.Restore(int(input.Fd()), state)
		restoreOutput()
	}, nil
}

// WatchSize keeps a full-screen program synchronized with its local viewport.
// Polling also works in Windows terminals where SIGWINCH is unavailable.
func WatchSize(session Session, output *os.File, stop <-chan struct{}) {
	current := LocalSize(output)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			next := LocalSize(output)
			if next == current {
				continue
			}
			if err := session.Resize(next); err != nil {
				return
			}
			current = next
		}
	}
}
