//go:build windows

package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
)

type windowsSession struct {
	console  *conpty.ConPty
	close    sync.Once
	closeErr error
}

// Start attaches the command to Windows ConPTY. The command line is constructed
// solely from local exec.Cmd arguments and quoted with Windows parsing rules.
func Start(command *exec.Cmd, size Size) (Session, error) {
	commandLine, err := windowsCommandLine(command)
	if err != nil {
		return nil, err
	}
	options := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(size.Width, size.Height),
	}
	if command.Dir != "" {
		options = append(options, conpty.ConPtyWorkDir(command.Dir))
	}
	if command.Env != nil {
		options = append(options, conpty.ConPtyEnv(command.Env))
	}
	console, err := conpty.Start(commandLine, options...)
	if err != nil {
		return nil, fmt.Errorf("start ConPTY: %w", err)
	}
	return &windowsSession{console: console}, nil
}

func (s *windowsSession) Read(value []byte) (int, error)  { return s.console.Read(value) }
func (s *windowsSession) Write(value []byte) (int, error) { return s.console.Write(value) }
func (s *windowsSession) Close() error {
	s.close.Do(func() { s.closeErr = s.console.Close() })
	return s.closeErr
}
func (s *windowsSession) PID() int { return s.console.Pid() }

func (s *windowsSession) Wait() error {
	exitCode, err := s.console.Wait(context.Background())
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("process exited with code %d", exitCode)
	}
	return nil
}

func (s *windowsSession) ReleaseAfterWait() error { return s.Close() }

func (s *windowsSession) Resize(size Size) error {
	return s.console.Resize(size.Width, size.Height)
}

func windowsCommandLine(command *exec.Cmd) (string, error) {
	if command == nil || command.Path == "" {
		return "", fmt.Errorf("missing local command path")
	}
	arguments := []string{command.Path}
	if len(command.Args) > 1 {
		arguments = append(arguments, command.Args[1:]...)
	}
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quoted = append(quoted, quoteWindowsArgument(argument))
	}
	return strings.Join(quoted, " "), nil
}

func quoteWindowsArgument(argument string) string {
	if argument != "" && !strings.ContainsAny(argument, " \t\n\v\"") {
		return argument
	}
	var result strings.Builder
	result.WriteByte('"')
	for index := 0; index < len(argument); {
		backslashes := 0
		for index < len(argument) && argument[index] == '\\' {
			backslashes++
			index++
		}
		if index == len(argument) {
			result.WriteString(strings.Repeat("\\", backslashes*2))
			break
		}
		if argument[index] == '"' {
			result.WriteString(strings.Repeat("\\", backslashes*2+1))
			result.WriteByte('"')
			index++
			continue
		}
		result.WriteString(strings.Repeat("\\", backslashes))
		result.WriteByte(argument[index])
		index++
	}
	result.WriteByte('"')
	return result.String()
}
