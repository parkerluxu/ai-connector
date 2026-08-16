// Package claudeobserver observes Claude Code's project JSONL transcripts.
package claudeobserver

import (
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
	if root := strings.TrimSpace(os.Getenv("AGENTBOARD_CLAUDE_HOME")); root != "" {
		return filepath.Join(root, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "projects")
	}
	return filepath.Join(home, ".claude", "projects")
}

func New(config Config) (*Observer, error) {
	if strings.TrimSpace(config.SessionsRoot) == "" {
		config.SessionsRoot = DefaultSessionsRoot()
	}
	base, err := lineobserver.New(lineobserver.Config{
		Root: config.SessionsRoot, DeviceID: config.DeviceID, Agent: observation.AgentClaudeCode, PollInterval: config.PollInterval,
		Reduce: reduce, Detail: detail,
	})
	if err != nil {
		return nil, err
	}
	return &Observer{Observer: base}, nil
}

func reduce(path string, session observation.Session, event map[string]any, fallback time.Time) (observation.Session, bool) {
	nativeID := lineobserver.String(event["sessionId"])
	if nativeID == "" {
		return session, false
	}
	if session.NativeSessionID == "" {
		session = observation.Session{
			SessionID: nativeID, NativeSessionID: nativeID, Agent: observation.AgentClaudeCode,
			SessionDirectory: "unknown", Source: "cli", Title: "Claude Code session",
			Status: observation.StatusIdle, StateConfidence: "inferred",
		}
	}
	if cwd := lineobserver.String(event["cwd"]); cwd != "" {
		session.CWD = lineobserver.Sanitize(cwd, 1024)
	}
	session.LastActivityAt = lineobserver.Time(event, fallback)
	switch lineobserver.String(event["type"]) {
	case "user":
		session.Status, session.StateConfidence = observation.StatusActive, "observed"
		for _, item := range lineobserver.List(messageContent(event)) {
			part := lineobserver.Map(item)
			if lineobserver.String(part["type"]) == "tool_result" && session.CurrentTool != nil {
				session.CurrentTool.Status = observation.ToolCompleted
				session.CurrentTool.FinishedAt = session.LastActivityAt
			}
		}
	case "assistant":
		session.Status, session.StateConfidence = observation.StatusIdle, "inferred"
		for _, item := range lineobserver.List(messageContent(event)) {
			part := lineobserver.Map(item)
			if lineobserver.String(part["type"]) != "tool_use" {
				continue
			}
			session.Status, session.StateConfidence = observation.StatusActive, "observed"
			session.CurrentTool = &observation.Tool{Name: defaultName(lineobserver.String(part["name"])), Status: observation.ToolRunning, StartedAt: session.LastActivityAt}
		}
	case "system":
		if lineobserver.String(event["subtype"]) == "error" {
			session.Status, session.StateConfidence = observation.StatusAborted, "observed"
		}
	}
	return session, true
}

func detail(event map[string]any, fallback time.Time) []observation.DetailEvent {
	at := lineobserver.Time(event, fallback)
	switch lineobserver.String(event["type"]) {
	case "user":
		content := messageContent(event)
		for _, item := range lineobserver.List(content) {
			part := lineobserver.Map(item)
			if lineobserver.String(part["type"]) == "tool_result" {
				return []observation.DetailEvent{{At: at, Kind: "tool_result", Content: lineobserver.Text(part["content"])}}
			}
		}
		return []observation.DetailEvent{{At: at, Kind: "user_message", Content: lineobserver.Text(content)}}
	case "assistant":
		result := make([]observation.DetailEvent, 0, 2)
		for _, item := range lineobserver.List(messageContent(event)) {
			part := lineobserver.Map(item)
			switch lineobserver.String(part["type"]) {
			case "text":
				result = append(result, observation.DetailEvent{At: at, Kind: "assistant_message", Content: lineobserver.Text(part["text"])})
			case "tool_use":
				result = append(result, observation.DetailEvent{At: at, Kind: "tool_call", ToolName: defaultName(lineobserver.String(part["name"])), Content: "Tool invocation"})
			}
		}
		return result
	default:
		return nil
	}
}

func messageContent(event map[string]any) any {
	if message := lineobserver.Map(event["message"]); message != nil {
		return message["content"]
	}
	return event["content"]
}

func defaultName(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
