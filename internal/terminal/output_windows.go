//go:build windows

package terminal

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

func enableOutput(output *os.File) (func(), error) {
	if output == nil || !term.IsTerminal(int(output.Fd())) {
		return func() {}, nil
	}
	handle := windows.Handle(output.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return func() {}, nil
	}
	if err := windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return nil, err
	}
	return func() { _ = windows.SetConsoleMode(handle, mode) }, nil
}
