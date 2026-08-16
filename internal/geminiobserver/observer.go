// Package geminiobserver observes Gemini CLI's local session JSONL files.
package geminiobserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentboard/ai-connector/internal/lineobserver"
	"github.com/agentboard/ai-connector/internal/observation"
)

type Config struct {
	SessionsRoot string
	DeviceID     string
	PollInterval time.Duration
}

type Observer struct{ *lineobserver.Observer }

func DefaultSessionsRoot() string {
	if root := strings.TrimSpace(os.Getenv("AGENTBOARD_GEMINI_HOME")); root != "" {
		return filepath.Join(root, "tmp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".gemini", "tmp")
	}
	return filepath.Join(home, ".gemini", "tmp")
}

func New(config Config) (*Observer, error) {
	if strings.TrimSpace(config.SessionsRoot) == "" {
		config.SessionsRoot = DefaultSessionsRoot()
	}
	projects := projectRoots()
	base, err := lineobserver.New(lineobserver.Config{
		Root: config.SessionsRoot, DeviceID: config.DeviceID, Agent: observation.AgentGeminiCLI, PollInterval: config.PollInterval,
		Reduce: func(path string, session observation.Session, event map[string]any, fallback time.Time) (observation.Session, bool) {
			return reduce(session, event, fallback, projects)
		},
		Detail: detail,
	})
	if err != nil {
		return nil, err
	}
	return &Observer{Observer: base}, nil
}

func reduce(session observation.Session, event map[string]any, fallback time.Time, projects map[string]string) (observation.Session, bool) {
	nativeID := lineobserver.String(event["sessionId"])
	if nativeID == "" {
		nativeID = session.NativeSessionID
	}
	if nativeID == "" {
		return session, false
	}
	if session.NativeSessionID == "" {
		session = observation.Session{
			SessionID: nativeID, NativeSessionID: nativeID, Agent: observation.AgentGeminiCLI,
			SessionDirectory: "unknown", Source: "cli", Title: "Gemini CLI session",
			Status: observation.StatusIdle, StateConfidence: "inferred",
		}
		if cwd := projects[lineobserver.String(event["projectHash"])]; cwd != "" {
			session.CWD = lineobserver.Sanitize(cwd, 1024)
		}
	}
	session.LastActivityAt = lineobserver.Time(event, fallback)
	switch lineobserver.String(event["type"]) {
	case "user":
		session.Status, session.StateConfidence = observation.StatusActive, "observed"
	case "gemini":
		session.Status, session.StateConfidence = observation.StatusIdle, "inferred"
	}
	return session, true
}

func detail(event map[string]any, fallback time.Time) []observation.DetailEvent {
	at := lineobserver.Time(event, fallback)
	switch lineobserver.String(event["type"]) {
	case "user":
		return []observation.DetailEvent{{At: at, Kind: "user_message", Content: lineobserver.Text(event["content"])}}
	case "gemini":
		return []observation.DetailEvent{{At: at, Kind: "assistant_message", Content: lineobserver.Text(event["content"])}}
	default:
		return nil
	}
}

func projectRoots() map[string]string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(home, ".gemini", "projects.json")
	if root := strings.TrimSpace(os.Getenv("AGENTBOARD_GEMINI_HOME")); root != "" {
		path = filepath.Join(root, "projects.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload struct {
		Projects map[string]string `json:"projects"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	result := make(map[string]string, len(payload.Projects))
	for directory, hash := range payload.Projects {
		if strings.TrimSpace(hash) != "" && strings.TrimSpace(directory) != "" {
			result[hash] = directory
		}
	}
	return result
}
