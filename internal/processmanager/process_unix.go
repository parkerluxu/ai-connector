//go:build !windows

package processmanager

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func Configure(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func TerminateTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	return TerminatePID(command.Process.Pid)
}

func TerminatePID(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("terminate process group: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("kill process group: %w", err)
	}
	return nil
}
