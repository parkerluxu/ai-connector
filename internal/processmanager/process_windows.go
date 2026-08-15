//go:build windows

package processmanager

import (
	"fmt"
	"os/exec"
	"strconv"
)

func Configure(_ *exec.Cmd) {}

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
	if output, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("terminate process tree: %w: %s", err, output)
	}
	return nil
}
