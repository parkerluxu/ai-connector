package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/agentboard/ai-connector/internal/claudeobserver"
	"github.com/agentboard/ai-connector/internal/codexobserver"
	"github.com/agentboard/ai-connector/internal/connectorconfig"
	"github.com/agentboard/ai-connector/internal/geminiobserver"
	"github.com/agentboard/ai-connector/internal/multiobserver"
	"github.com/agentboard/ai-connector/internal/observerrelay"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ai-connector:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "doctor":
		return doctor(args[1:])
	case "pair":
		return pair(args[1:])
	case "status":
		return status()
	case "observe":
		return observe(args[1:])
	case "service":
		return service(args[1:])
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: ai-connector <version|doctor|pair|status|observe|service>\n" +
		"  version\n" +
		"  pair [--code <pairing-code>]\n" +
		"  observe <serve|status>\n" +
		"  service <install|uninstall|start|stop|status>")
}

func doctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	config, err := connectorconfig.LoadDefault()
	if err != nil {
		return err
	}
	config.FillDefaults()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := map[string]any{
		"api_url": config.APIURL, "ws_url": config.WSURL,
		"configured": paired(config), "agent_sessions_roots": map[string]string{
			"codex": codexobserver.DefaultSessionsRoot(), "claude_code": claudeobserver.DefaultSessionsRoot(), "gemini_cli": geminiobserver.DefaultSessionsRoot(),
		},
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(config.APIURL, "/")+"/v1/health", nil)
	if requestErr == nil {
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			requestErr = err
		} else {
			_ = response.Body.Close()
			if response.StatusCode/100 != 2 {
				requestErr = fmt.Errorf("health endpoint returned %s", response.Status)
			}
		}
	}
	if requestErr != nil {
		result["api_health"] = map[string]any{"ok": false, "error": requestErr.Error()}
	} else {
		result["api_health"] = map[string]any{"ok": true}
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	pretty, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(pretty))
	return nil
}

func status() error {
	config, err := connectorconfig.LoadDefault()
	if err != nil {
		return err
	}
	config.FillDefaults()
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"configured": paired(config), "device_id": config.DeviceID, "credential_id": config.CredentialID,
		"api_url": config.APIURL, "ws_url": config.WSURL, "agent_sessions_roots": map[string]string{
			"codex": codexobserver.DefaultSessionsRoot(), "claude_code": claudeobserver.DefaultSessionsRoot(), "gemini_cli": geminiobserver.DefaultSessionsRoot(),
		},
	})
}

func pair(args []string) error {
	flags := flag.NewFlagSet("pair", flag.ContinueOnError)
	code := flags.String("code", "", "one-time pairing code")
	name := flags.String("name", hostname(), "device name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*code) == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("--code is required when standard input is not a terminal")
		}
		fmt.Fprint(os.Stderr, "Enter the one-time pairing code: ")
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read pairing code: %w", err)
		}
		*code = strings.TrimSpace(string(value))
	}
	if strings.TrimSpace(*code) == "" {
		return errors.New("pairing code is required")
	}
	configPath := connectorconfig.DefaultPath()
	config, err := connectorconfig.LoadDefault()
	if err != nil {
		return err
	}
	config.FillDefaults()
	publicKey, _, err := config.EnsureIdentity()
	if err != nil {
		return err
	}
	payload := map[string]any{
		"device_id": config.DeviceID, "credential_id": config.CredentialID, "public_key": base64.StdEncoding.EncodeToString(publicKey),
		"name": *name, "os": runtime.GOOS, "arch": runtime.GOARCH, "connector_version": version,
	}
	payload["code"] = strings.TrimSpace(*code)
	if err := postJSON(strings.TrimSuffix(config.APIURL, "/")+"/v1/devices/pair/claim", payload); err != nil {
		return err
	}
	if err := connectorconfig.Save(configPath, config); err != nil {
		return err
	}
	fmt.Printf("paired device %s with credential %s\n", config.DeviceID, config.CredentialID)
	return nil
}

func observe(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ai-connector observe <serve|status>")
	}
	if args[0] == "status" {
		return observeStatus()
	}
	if args[0] != "serve" {
		return errors.New("usage: ai-connector observe <serve|status>")
	}
	config, err := connectorconfig.LoadDefault()
	if err != nil {
		return err
	}
	config.FillDefaults()
	if !paired(config) {
		return errors.New("connector is not paired; run ai-connector pair first")
	}
	_, privateKey, err := config.EnsureIdentity()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return observerrelay.Run(ctx, observerrelay.Config{Connector: config, PrivateKey: privateKey, SessionsRoot: codexobserver.DefaultSessionsRoot()})
}

func observeStatus() error {
	config, err := connectorconfig.LoadDefault()
	if err != nil {
		return err
	}
	config.FillDefaults()
	if !paired(config) {
		return errors.New("connector is not paired; run ai-connector pair first")
	}
	observer, err := multiobserver.New(multiobserver.Config{
		CodexSessionsRoot: codexobserver.DefaultSessionsRoot(),
		DeviceID:          config.DeviceID,
		MetadataPath:      ":memory:", // Status must not touch the running service metadata.
	})
	if err != nil {
		return err
	}
	defer observer.Close()
	if err := observer.Scan(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"configured": true, "agent_sessions_roots": map[string]string{
			"codex": codexobserver.DefaultSessionsRoot(), "claude_code": claudeobserver.DefaultSessionsRoot(), "gemini_cli": geminiobserver.DefaultSessionsRoot(),
		},
		"sessions_found": len(observer.Sessions()), "sessions": observer.Sessions(),
	})
}

func service(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ai-connector service <install|uninstall|start|stop|status>")
	}
	config, err := connectorconfig.LoadDefault()
	if err != nil {
		return err
	}
	config.FillDefaults()
	if args[0] != "uninstall" && !paired(config) {
		return errors.New("connector is not paired; run ai-connector pair first")
	}
	switch args[0] {
	case "install":
		return installService()
	case "uninstall":
		return uninstallService()
	case "start":
		return startService()
	case "stop":
		return stopService()
	case "status":
		return serviceStatus()
	default:
		return errors.New("usage: ai-connector service <install|uninstall|start|stop|status>")
	}
}

func paired(config connectorconfig.Config) bool {
	return config.DeviceID != "" && config.CredentialID != "" && config.PrivateKey != ""
}

func postJSON(endpoint string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode/100 == 2 {
		return nil
	}
	var responseEnvelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(responseBody, &responseEnvelope) == nil && responseEnvelope.Error != nil && responseEnvelope.Error.Message != "" {
		return fmt.Errorf("%s returned %s: %s", endpoint, response.Status, responseEnvelope.Error.Message)
	}
	return fmt.Errorf("%s returned %s", endpoint, response.Status)
}

func hostname() string {
	value, err := os.Hostname()
	if err != nil || value == "" {
		return "AgentBoard Connector"
	}
	return value
}
