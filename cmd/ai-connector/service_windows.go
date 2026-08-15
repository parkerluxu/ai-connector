//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const serviceName = "AgentBoardCodexObserver"
const runRegistryKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func installService() error {
	binary, err := connectorBinary()
	if err != nil {
		return err
	}
	command := fmt.Sprintf("\"%s\" observe serve", binary)
	if err := runWindowsCommand("reg", "add", runRegistryKey, "/v", serviceName, "/t", "REG_SZ", "/d", command, "/f"); err != nil {
		return fmt.Errorf("install current-user startup entry: %w", err)
	}
	fmt.Println("installed current-user logon startup entry")
	return nil
}

func uninstallService() error {
	installed, err := runEntryInstalled()
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}
	if err := runWindowsCommand("reg", "delete", runRegistryKey, "/v", serviceName, "/f"); err != nil {
		return fmt.Errorf("remove current-user startup entry: %w", err)
	}
	fmt.Println("removed current-user logon startup entry")
	return nil
}

func startService() error {
	installed, err := runEntryInstalled()
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("observer startup entry is not installed")
	}
	running, err := observerProcessIDs()
	if err != nil {
		return err
	}
	if len(running) > 0 {
		fmt.Printf("observer already running (%s %s)\n", processLabel(len(running)), formatProcessIDs(running))
		if len(running) > 1 {
			fmt.Println("warning: multiple observer processes are running; run ai-connector service stop, then service start once")
		}
		return nil
	}
	binary, err := connectorBinary()
	if err != nil {
		return err
	}
	command := exec.Command(binary, "observe", "serve")
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start observer: %w", err)
	}
	fmt.Printf("started observer process %d\n", command.Process.Pid)
	return nil
}

func stopService() error {
	observers, err := observerProcessIDs()
	if err != nil {
		return err
	}
	runners, err := observerRunnerProcessIDs()
	if err != nil {
		return err
	}
	processes := append(observers, runners...)
	if len(processes) == 0 {
		fmt.Println("observer process is not running")
		return nil
	}
	processes = uniqueProcessIDs(processes)
	processIDArguments := make([]string, len(processes))
	for i, pid := range processes {
		processIDArguments[i] = strconv.Itoa(pid)
	}
	command := fmt.Sprintf("Stop-Process -Id %s -Force -ErrorAction SilentlyContinue", strings.Join(processIDArguments, ","))
	if err := runWindowsCommand("powershell", "-NoProfile", "-NonInteractive", "-Command", command); err != nil {
		return fmt.Errorf("stop observer processes: %w", err)
	}
	fmt.Printf("stopped observer %s %s\n", processLabel(len(processes)), formatProcessIDs(processes))
	return nil
}

func serviceStatus() error {
	installed, err := runEntryInstalled()
	if err != nil {
		return err
	}
	if installed {
		fmt.Println("current-user logon startup entry: installed")
	} else {
		fmt.Println("current-user logon startup entry: not installed")
	}
	running, err := observerProcessIDs()
	if err != nil {
		return err
	}
	if len(running) == 0 {
		fmt.Println("observer process: stopped")
		return nil
	}
	fmt.Printf("observer process: running (%s %s)\n", processLabel(len(running)), formatProcessIDs(running))
	if len(running) > 1 {
		fmt.Println("warning: multiple observer processes are running; run ai-connector service stop, then service start once")
	}
	return nil
}

func observerProcessIDs() ([]int, error) {
	return processIDsForPowerShellFilter("$_.Name -eq 'ai-connector.exe' -and $_.CommandLine -match '(?i)(?:^|\\s)observe\\s+serve(?:\\s|$)'")
}

func observerRunnerProcessIDs() ([]int, error) {
	return processIDsForPowerShellFilter("$_.Name -eq 'go.exe' -and $_.CommandLine -match '(?i)ai-connector\\s+observe\\s+serve(?:\\s|$)'")
}

func processIDsForPowerShellFilter(filter string) ([]int, error) {
	command := fmt.Sprintf("Get-CimInstance Win32_Process | Where-Object { %s } | Select-Object -ExpandProperty ProcessId", filter)
	output, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("find observer processes: %w: %s", err, strings.TrimSpace(string(output)))
	}

	var processIDs []int
	for _, field := range strings.Fields(string(output)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("parse observer process id %q: %w", field, err)
		}
		processIDs = append(processIDs, pid)
	}
	sort.Ints(processIDs)
	return processIDs, nil
}

func uniqueProcessIDs(processIDs []int) []int {
	seen := make(map[int]struct{}, len(processIDs))
	unique := make([]int, 0, len(processIDs))
	for _, pid := range processIDs {
		if _, exists := seen[pid]; exists {
			continue
		}
		seen[pid] = struct{}{}
		unique = append(unique, pid)
	}
	sort.Ints(unique)
	return unique
}

func formatProcessIDs(processIDs []int) string {
	parts := make([]string, len(processIDs))
	for i, pid := range processIDs {
		parts[i] = strconv.Itoa(pid)
	}
	return strings.Join(parts, ", ")
}

func processLabel(count int) string {
	if count == 1 {
		return "PID"
	}
	return "PIDs"
}

func connectorBinary() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	return binary, nil
}

func runEntryInstalled() (bool, error) {
	output, err := exec.Command("reg", "query", runRegistryKey, "/v", serviceName).CombinedOutput()
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(string(output)), "unable to find") {
		return false, nil
	}
	return false, fmt.Errorf("query current-user startup entry: %w: %s", err, strings.TrimSpace(string(output)))
}

func runWindowsCommand(name string, args ...string) error {
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
