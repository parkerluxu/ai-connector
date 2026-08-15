//go:build !windows

package terminal

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixSession struct {
	file    *os.File
	command *exec.Cmd
}

// Start creates a controlling terminal for the command. pty.Start creates a
// new session, so its PID is also the process-group leader used for cancellation.
func Start(command *exec.Cmd, size Size) (Session, error) {
	file, err := pty.StartWithSize(command, &pty.Winsize{
		Rows: uint16(size.Height),
		Cols: uint16(size.Width),
	})
	if err != nil {
		return nil, err
	}
	return &unixSession{file: file, command: command}, nil
}

func (s *unixSession) Read(value []byte) (int, error)  { return s.file.Read(value) }
func (s *unixSession) Write(value []byte) (int, error) { return s.file.Write(value) }
func (s *unixSession) Close() error                    { return s.file.Close() }
func (s *unixSession) Wait() error                     { return s.command.Wait() }
func (s *unixSession) ReleaseAfterWait() error         { return nil }
func (s *unixSession) PID() int                        { return s.command.Process.Pid }

func (s *unixSession) Resize(size Size) error {
	return pty.Setsize(s.file, &pty.Winsize{Rows: uint16(size.Height), Cols: uint16(size.Width)})
}
