//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const serviceName = "agentboard-codex-observer.service"

func servicePath() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", serviceName), nil
}
func installService() error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	path, err := servicePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	unit := fmt.Sprintf("[Unit]\nDescription=AgentBoard AI agent observer\n\n[Service]\nExecStart=%q observe serve\nRestart=on-failure\nRestartSec=3\n\n[Install]\nWantedBy=default.target\n", binary)
	if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
		return err
	}
	if err := runServiceCommand("daemon-reload"); err != nil {
		return err
	}
	return runServiceCommand("enable", "--now", serviceName)
}
func uninstallService() error {
	_ = stopService()
	path, err := servicePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runServiceCommand("daemon-reload")
}
func startService() error  { return runServiceCommand("start", serviceName) }
func stopService() error   { return runServiceCommand("stop", serviceName) }
func serviceStatus() error { return runServiceCommand("status", "--no-pager", serviceName) }
func runServiceCommand(args ...string) error {
	output, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	fmt.Print(string(output))
	return nil
}
