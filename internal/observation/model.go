// Package observation contains the privacy-safe session model shared by every
// local AI agent adapter. Native session IDs are Connector-local only.
package observation

type Agent string

const (
	AgentCodex      Agent = "codex"
	AgentClaudeCode Agent = "claude_code"
	AgentGeminiCLI  Agent = "gemini_cli"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusIdle      Status = "idle"
	StatusCompleted Status = "completed"
	StatusAborted   Status = "aborted"
	StatusUnknown   Status = "unknown"
)

type ToolStatus string

const (
	ToolRunning   ToolStatus = "running"
	ToolCompleted ToolStatus = "completed"
	ToolFailed    ToolStatus = "failed"
	ToolUnknown   ToolStatus = "unknown"
)

type Tool struct {
	Name       string     `json:"name"`
	Status     ToolStatus `json:"status"`
	StartedAt  string     `json:"started_at,omitempty"`
	FinishedAt string     `json:"finished_at,omitempty"`
}

type ManagedProcess struct {
	PID          int      `json:"pid"`
	StartedAt    string   `json:"started_at"`
	Capabilities []string `json:"capabilities"`
}

// Session is intentionally an outbound DTO. NativeSessionID is not serialized
// and is used only by the Connector when it invokes an adapter's fixed resume
// command.
type Session struct {
	SessionID        string          `json:"session_id"`
	Agent            Agent           `json:"agent"`
	DeviceID         string          `json:"device_id"`
	SessionDirectory string          `json:"session_directory"`
	Source           string          `json:"source"`
	CWD              string          `json:"cwd"`
	Title            string          `json:"title"`
	Status           Status          `json:"status"`
	StateConfidence  string          `json:"state_confidence"`
	LastActivityAt   string          `json:"last_activity_at"`
	ActiveTurnID     string          `json:"active_turn_id,omitempty"`
	CurrentTool      *Tool           `json:"current_tool,omitempty"`
	CanResume        bool            `json:"can_resume"`
	ManagedProcess   *ManagedProcess `json:"managed_process,omitempty"`
	NativeSessionID  string          `json:"-"`
}

type DetailEvent struct {
	ID       string `json:"id"`
	At       string `json:"at"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	ToolName string `json:"tool_name,omitempty"`
}

type SessionDetail struct {
	SessionID string        `json:"session_id"`
	Events    []DetailEvent `json:"events"`
	Truncated bool          `json:"truncated"`
}

func CopySession(session Session) Session {
	if session.CurrentTool != nil {
		tool := *session.CurrentTool
		session.CurrentTool = &tool
	}
	if session.ManagedProcess != nil {
		process := *session.ManagedProcess
		process.Capabilities = append([]string(nil), process.Capabilities...)
		session.ManagedProcess = &process
	}
	return session
}
