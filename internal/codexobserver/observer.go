// Package codexobserver discovers Codex JSONL sessions and reduces them into
// the small, privacy-safe observation model that may leave the device.
package codexobserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/agentboard/ai-connector/internal/observerstore"
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

// Session is intentionally an outbound DTO. It contains no transcript,
// arguments, output, raw JSONL data, or source filename.
type Session struct {
	SessionID        string          `json:"session_id"`
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
}

type Config struct {
	SessionsRoot string
	DeviceID     string
	PollInterval time.Duration
	MetadataPath string
}

type fileState struct {
	offset    int64
	pending   []byte
	sessionID string
	// sourceSessionID preserves the ID written in this file when the file is
	// a continuation aliased to its parent session for observation.
	sourceSessionID string
	fileID          string
	fingerprint     string
}

type resumeRequest struct {
	parentID string
	cwd      string
	started  time.Time
}

type Observer struct {
	config Config

	mu       sync.RWMutex
	files    map[string]*fileState
	sessions map[string]Session
	resumes  []resumeRequest
	updates  chan Session
	removals chan string
	metadata *observerstore.Store
}

// RegisterResume tells the observer that the next Codex JSONL session created
// for this working directory belongs to parentID. Codex CLI resume writes a
// fresh JSONL file, so the observer aliases that file back to the parent row.
func (o *Observer) RegisterResume(parentID, cwd string, startedAt time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.resumes = append(o.resumes, resumeRequest{parentID: parentID, cwd: strings.TrimSpace(cwd), started: startedAt})
}

// CancelResume removes a pending registration when the child Codex process
// could not be started, so an unrelated future session cannot be aliased.
func (o *Observer) CancelResume(parentID string, startedAt time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for index := len(o.resumes) - 1; index >= 0; index-- {
		request := o.resumes[index]
		if request.parentID == parentID && request.started.Equal(startedAt) {
			o.resumes = append(o.resumes[:index], o.resumes[index+1:]...)
			return
		}
	}
}

func DefaultSessionsRoot() string {
	if root := strings.TrimSpace(os.Getenv("CODEX_HOME")); root != "" {
		return filepath.Join(root, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex/sessions"
	}
	return filepath.Join(home, ".codex", "sessions")
}

func DefaultMetadataPath() string {
	if path := strings.TrimSpace(os.Getenv("AGENTBOARD_OBSERVER_DB_PATH")); path != "" {
		return path
	}
	configDirectory, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDirectory) == "" {
		return filepath.Join(".agentboard", "observer.db")
	}
	return filepath.Join(configDirectory, "AgentBoard", "observer.db")
}

func New(config Config) (*Observer, error) {
	if strings.TrimSpace(config.SessionsRoot) == "" {
		config.SessionsRoot = DefaultSessionsRoot()
	}
	if strings.TrimSpace(config.DeviceID) == "" {
		return nil, errors.New("device id is required")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 500 * time.Millisecond
	}
	if strings.TrimSpace(config.MetadataPath) == "" {
		config.MetadataPath = DefaultMetadataPath()
	}
	metadata, err := observerstore.Open(config.MetadataPath)
	if err != nil {
		return nil, err
	}
	return &Observer{
		config: config, files: make(map[string]*fileState), sessions: make(map[string]Session),
		updates: make(chan Session, 128), removals: make(chan string, 32), metadata: metadata,
	}, nil
}

func (o *Observer) Close() error { return o.metadata.Close() }

func (o *Observer) Updates() <-chan Session { return o.updates }
func (o *Observer) Removals() <-chan string { return o.removals }

func (o *Observer) Sessions() []Session {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]Session, 0, len(o.sessions))
	for _, session := range o.sessions {
		result = append(result, copySession(session))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastActivityAt > result[j].LastActivityAt })
	return result
}

func (o *Observer) Get(sessionID string) (Session, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	session, ok := o.sessions[sessionID]
	return copySession(session), ok
}

// SetManaged projects only a Connector-owned local PTY into the outbound DTO.
// Connector restarts intentionally begin with no managed process state.
func (o *Observer) SetManaged(sessionID string, process *ManagedProcess) {
	o.mu.Lock()
	defer o.mu.Unlock()
	session, ok := o.sessions[sessionID]
	if !ok {
		return
	}
	if process == nil {
		session.ManagedProcess = nil
		if o.metadata != nil {
			_ = o.metadata.DeleteManagedProcess(sessionID)
		}
	} else {
		copy := *process
		copy.Capabilities = append([]string(nil), process.Capabilities...)
		session.ManagedProcess = &copy
		if o.metadata != nil {
			_ = o.metadata.SaveManagedProcess(sessionID, copy.PID, copy.StartedAt)
		}
	}
	o.setSessionLocked(session)
}

func (o *Observer) Run(stop <-chan struct{}) error {
	if err := o.Scan(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	ticker := time.NewTicker(o.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
			_ = o.Scan() // File-level errors produce unknown state instead of stopping observation.
		}
	}
}

// Scan tails each known JSONL file. A final line without a newline remains in
// fileState.pending until later bytes complete it, which handles short writes.
func (o *Observer) Scan() error {
	seen := make(map[string]struct{})
	err := filepath.WalkDir(o.config.SessionsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			return nil
		}
		seen[path] = struct{}{}
		return o.scanFile(path)
	})
	if err != nil {
		return err
	}

	o.mu.Lock()
	for path, state := range o.files {
		if _, exists := seen[path]; exists {
			continue
		}
		delete(o.files, path)
		if o.metadata != nil {
			_ = o.metadata.DeleteFile(state.fileID)
		}
		if state.sessionID != "" && !o.sessionReferencedLocked(state.sessionID) {
			delete(o.sessions, state.sessionID)
			o.emitRemovalLocked(state.sessionID)
		}
	}
	o.mu.Unlock()
	return nil
}

func (o *Observer) scanFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	o.mu.Lock()
	state := o.files[path]
	if state == nil {
		state = &fileState{fileID: stableFileID(path)}
		o.files[path] = state
	}
	if info.Size() < state.offset {
		if state.sessionID != "" && !o.sessionReferencedExceptLocked(state.sessionID, state) {
			delete(o.sessions, state.sessionID)
			o.emitRemovalLocked(state.sessionID)
		}
		*state = fileState{}
		state.fileID = stableFileID(path)
	}
	o.mu.Unlock()

	if info.Size() == state.offset {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open session jsonl: %w", err)
	}
	defer file.Close()
	if _, err := file.Seek(state.offset, 0); err != nil {
		return fmt.Errorf("seek session jsonl: %w", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read session jsonl: %w", err)
	}
	o.mu.Lock()
	state.offset += int64(len(data))
	data = append(append([]byte(nil), state.pending...), data...)
	parts := bytes.Split(data, []byte{'\n'})
	if len(parts) > 1 {
		state.pending = append(state.pending[:0], parts[len(parts)-1]...)
	} else {
		state.pending = append(state.pending[:0], data...)
	}
	o.mu.Unlock()
	for _, line := range parts[:max(0, len(parts)-1)] {
		o.reduceLine(path, line)
	}
	o.persistFileState(path)
	return nil
}

func (o *Observer) reduceLine(path string, line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var event map[string]any
	if err := json.Unmarshal(line, &event); err != nil {
		o.markUnknown(path)
		return
	}
	typeName := stringValue(event["type"])
	payload := mapValue(event["payload"])
	if payload == nil {
		payload = event
	}
	at := eventTime(event)
	o.mu.Lock()
	defer o.mu.Unlock()
	state := o.files[path]
	if state == nil {
		return
	}
	state.fingerprint = eventFingerprint(line)
	if typeName == "session_meta" {
		sourceSessionID := firstString(payload["session_id"], payload["id"], event["session_id"], event["id"])
		if sourceSessionID == "" {
			return
		}
		sourceSessionID = sanitize(sourceSessionID, 256)
		cwd := sanitize(firstString(payload["cwd"], payload["working_directory"], event["cwd"]), 1024)
		sessionID := sourceSessionID
		if parentID, _ := o.metadata.ContinuationParent(sourceSessionID); parentID != "" {
			if o.hasLocalRolloutLocked(parentID) {
				sessionID = parentID
			} else {
				_ = o.metadata.DeleteContinuation(sourceSessionID)
			}
		} else if parentID, index := o.matchResumeLocked(sourceSessionID, cwd, at); parentID != "" {
			sessionID = parentID
			o.resumes = append(o.resumes[:index], o.resumes[index+1:]...)
			_ = o.metadata.SaveContinuation(sourceSessionID, parentID)
		}
		if state.sessionID != "" && state.sessionID != sessionID {
			if !o.sessionReferencedExceptLocked(state.sessionID, state) {
				delete(o.sessions, state.sessionID)
				o.emitRemovalLocked(state.sessionID)
			}
		}
		state.sessionID = sessionID
		state.sourceSessionID = sourceSessionID
		session := o.sessions[state.sessionID]
		if session.SessionID == "" {
			session = Session{SessionID: state.sessionID, DeviceID: o.config.DeviceID, SessionDirectory: sessionDirectory(o.config.SessionsRoot, path), Title: fmt.Sprintf("Codex session %d", len(o.sessions)+1), Status: StatusUnknown, StateConfidence: "inferred"}
		}
		session.CWD = cwd
		session.Source = sourceValue(firstString(payload["source"], payload["origin"], payload["originator"], payload["client"], event["source"], event["originator"]))
		if session.LastActivityAt == "" || isLaterEvent(at, session.LastActivityAt) {
			session.LastActivityAt = at
		}
		o.setSessionLocked(session)
		return
	}
	if state.sessionID == "" {
		return
	}
	session := o.sessions[state.sessionID]
	if session.SessionID == "" {
		return
	}
	if state.sourceSessionID != state.sessionID && session.LastActivityAt != "" && isLaterEvent(session.LastActivityAt, at) {
		return
	}
	session.LastActivityAt = at
	eventType := typeName
	if typeName == "event_msg" {
		eventType = firstString(payload["type"], payload["event_type"])
	}
	if typeName == "response_item" {
		eventType = firstString(payload["type"], payload["item_type"])
	}
	switch eventType {
	case "task_started":
		session.Status, session.StateConfidence = StatusActive, "observed"
		session.ActiveTurnID = sanitize(firstString(payload["turn_id"], payload["id"]), 256)
	case "task_complete":
		session.Status, session.StateConfidence, session.ActiveTurnID = StatusCompleted, "observed", ""
	case "turn_aborted":
		session.Status, session.StateConfidence, session.ActiveTurnID = StatusAborted, "observed", ""
	case "function_call", "custom_tool_call":
		session.Status, session.StateConfidence = StatusActive, "observed"
		session.CurrentTool = &Tool{Name: toolName(payload), Status: ToolRunning, StartedAt: at}
	case "function_call_output", "custom_tool_call_output":
		if session.CurrentTool != nil {
			session.CurrentTool.Status, session.CurrentTool.FinishedAt = toolResultStatus(payload), at
		}
	case "user_message":
		if session.Status == StatusCompleted || session.Status == StatusAborted {
			session.Status, session.StateConfidence = StatusIdle, "inferred"
		}
	}
	o.setSessionLocked(session)
}

func isLaterEvent(candidate, current string) bool {
	candidateAt, candidateErr := time.Parse(time.RFC3339Nano, candidate)
	currentAt, currentErr := time.Parse(time.RFC3339Nano, current)
	if candidateErr != nil || currentErr != nil {
		return candidate > current
	}
	return candidateAt.After(currentAt)
}

func (o *Observer) matchResumeLocked(sourceSessionID, cwd, at string) (string, int) {
	when, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		when = time.Now().UTC()
	}
	now := time.Now().UTC()
	for index := len(o.resumes) - 1; index >= 0; index-- {
		request := o.resumes[index]
		if now.Sub(request.started) > 10*time.Minute || request.parentID == sourceSessionID {
			o.resumes = append(o.resumes[:index], o.resumes[index+1:]...)
			continue
		}
		if request.cwd != "" && !strings.EqualFold(filepath.Clean(request.cwd), filepath.Clean(cwd)) {
			continue
		}
		if when.Before(request.started.Add(-2 * time.Minute)) {
			continue
		}
		if _, exists := o.sessions[sourceSessionID]; exists {
			continue
		}
		return request.parentID, index
	}
	return "", -1
}

func (o *Observer) persistFileState(path string) {
	if o.metadata == nil {
		return
	}
	o.mu.RLock()
	state := o.files[path]
	if state == nil {
		o.mu.RUnlock()
		return
	}
	metadata := observerstore.FileMetadata{
		FileID: state.fileID, SessionID: state.sessionID,
		CommittedOffset: state.offset - int64(len(state.pending)), LastEventFingerprint: state.fingerprint,
	}
	o.mu.RUnlock()
	_ = o.metadata.SaveFile(metadata)
}

func (o *Observer) markUnknown(path string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	state := o.files[path]
	if state == nil || state.sessionID == "" {
		return
	}
	session := o.sessions[state.sessionID]
	if session.SessionID == "" {
		return
	}
	session.Status, session.StateConfidence, session.LastActivityAt = StatusUnknown, "inferred", time.Now().UTC().Format(time.RFC3339Nano)
	o.setSessionLocked(session)
}

func (o *Observer) setSessionLocked(session Session) {
	session.DeviceID = o.config.DeviceID
	session.CanResume = session.Status != StatusActive && session.CWD != "" && session.SessionID != "" && session.ManagedProcess == nil
	o.sessions[session.SessionID] = copySession(session)
	select {
	case o.updates <- copySession(session):
	default:
	}
}

func (o *Observer) emitRemovalLocked(sessionID string) {
	if sessionID == "" {
		return
	}
	select {
	case o.removals <- sessionID:
	default:
	}
}

func (o *Observer) sessionReferencedLocked(sessionID string) bool {
	for _, state := range o.files {
		if state.sessionID == sessionID {
			return true
		}
	}
	return false
}

func (o *Observer) sessionReferencedExceptLocked(sessionID string, excluded *fileState) bool {
	for _, state := range o.files {
		if state != excluded && state.sessionID == sessionID {
			return true
		}
	}
	return false
}

func (o *Observer) hasLocalRolloutLocked(sessionID string) bool {
	if _, exists := o.sessions[sessionID]; exists {
		return true
	}
	for _, state := range o.files {
		if state.sourceSessionID == sessionID {
			return true
		}
	}
	return false
}

func copySession(session Session) Session {
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

func eventTime(event map[string]any) string {
	for _, value := range []any{event["timestamp"], event["created_at"], event["time"]} {
		if text := stringValue(value); text != "" {
			if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
				return text
			}
		}
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func mapValue(value any) map[string]any { result, _ := value.(map[string]any); return result }
func stringValue(value any) string      { text, _ := value.(string); return strings.TrimSpace(text) }
func firstString(values ...any) string {
	for _, value := range values {
		if text := stringValue(value); text != "" {
			return text
		}
	}
	return ""
}
func sourceValue(value string) string {
	value = strings.ToLower(value)
	if strings.Contains(value, "cli") {
		return "cli"
	}
	if strings.Contains(value, "desktop") || strings.Contains(value, "vscode") {
		return "desktop"
	}
	return "unknown"
}
func toolName(payload map[string]any) string {
	if name := sanitize(firstString(payload["name"], payload["tool_name"]), 256); name != "" {
		return name
	}
	return "unknown"
}
func toolResultStatus(payload map[string]any) ToolStatus {
	status := strings.ToLower(firstString(payload["status"], payload["result"]))
	if strings.Contains(status, "fail") || strings.Contains(status, "error") {
		return ToolFailed
	}
	return ToolCompleted
}
func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func sanitize(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func stableFileID(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absPath)))
	return fmt.Sprintf("%x", sum[:])
}

func eventFingerprint(line []byte) string {
	sum := sha256.Sum256(line)
	return fmt.Sprintf("%x", sum[:])
}

func sessionDirectory(sessionsRoot, path string) string {
	relative, err := filepath.Rel(sessionsRoot, filepath.Dir(path))
	if err != nil {
		return "unknown"
	}
	parts := strings.FieldsFunc(filepath.ToSlash(relative), func(value rune) bool { return value == '/' })
	if len(parts) < 3 {
		return "unknown"
	}
	candidate := strings.Join(parts[len(parts)-3:], "/")
	if len(candidate) == 10 && candidate[4] == '/' && candidate[7] == '/' {
		return candidate
	}
	return "unknown"
}
