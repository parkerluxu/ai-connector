//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const serviceName = "cn.agentcaseshare.aiboard.connector"

func servicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", serviceName+".plist"), nil
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
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>observe</string><string>serve</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
`, serviceName, xmlEscape(binary))
	if err := os.WriteFile(path, []byte(plist), 0o600); err != nil {
		return err
	}
	_ = runLaunchctl("bootout", "gui/"+fmt.Sprint(os.Getuid()), path)
	return runLaunchctl("bootstrap", "gui/"+fmt.Sprint(os.Getuid()), path)
}

func uninstallService() error {
	path, err := servicePath()
	if err != nil {
		return err
	}
	_ = runLaunchctl("bootout", "gui/"+fmt.Sprint(os.Getuid()), path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func startService() error {
	return runLaunchctl("kickstart", "gui/"+fmt.Sprint(os.Getuid())+"/"+serviceName)
}
func stopService() error {
	return runLaunchctl("kill", "SIGTERM", "gui/"+fmt.Sprint(os.Getuid())+"/"+serviceName)
}
func serviceStatus() error {
	return runLaunchctl("print", "gui/"+fmt.Sprint(os.Getuid())+"/"+serviceName)
}

func runLaunchctl(args ...string) error {
	output, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	fmt.Print(string(output))
	return nil
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
