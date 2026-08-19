// Package multiobserver combines local agent adapters behind one Connector
// observation stream. It never stores transcripts or exposes native IDs.
package multiobserver

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentboard/ai-connector/internal/claudeobserver"
	"github.com/agentboard/ai-connector/internal/codexobserver"
	"github.com/agentboard/ai-connector/internal/geminiobserver"
	"github.com/agentboard/ai-connector/internal/observation"
)

type Config struct {
	DeviceID          string
	CodexSessionsRoot string
	MetadataPath      string
	PollInterval      time.Duration
}

type adapter interface {
	Agent() observation.Agent
	Close() error
	Scan() error
	Run(stop <-chan struct{}) error
	Sessions() []observation.Session
	Get(sessionID string) (observation.Session, bool)
	Detail(sessionID string) (observation.SessionDetail, error)
	SetManaged(sessionID string, process *observation.ManagedProcess)
	Updates() <-chan observation.Session
	Removals() <-chan string
}

type resumeRegistrar interface {
	RegisterResume(parentID, cwd string, startedAt time.Time)
	CancelResume(parentID string, startedAt time.Time)
}

type location struct {
	agent  observation.Agent
	native string
}

type Observer struct {
	adapters map[observation.Agent]adapter

	mu        sync.RWMutex
	sessions  map[string]observation.Session
	locations map[string]location
	updates   chan observation.Session
	removals  chan string
}

func New(config Config) (*Observer, error) {
	codex, err := codexobserver.New(codexobserver.Config{
		SessionsRoot: config.CodexSessionsRoot, DeviceID: config.DeviceID,
		PollInterval: config.PollInterval, MetadataPath: config.MetadataPath,
	})
	if err != nil {
		return nil, err
	}
	claude, err := claudeobserver.New(claudeobserver.Config{DeviceID: config.DeviceID, PollInterval: config.PollInterval})
	if err != nil {
		_ = codex.Close()
		return nil, err
	}
	gemini, err := geminiobserver.New(geminiobserver.Config{DeviceID: config.DeviceID, PollInterval: config.PollInterval})
	if err != nil {
		_ = codex.Close()
		_ = claude.Close()
		return nil, err
	}
	return &Observer{
		adapters: map[observation.Agent]adapter{observation.AgentCodex: codex, observation.AgentClaudeCode: claude, observation.AgentGeminiCLI: gemini},
		sessions: make(map[string]observation.Session), locations: make(map[string]location),
		updates: make(chan observation.Session, 256), removals: make(chan string, 64),
	}, nil
}

func (o *Observer) Close() error {
	var result error
	for _, current := range o.adapters {
		if err := current.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}

func (o *Observer) Updates() <-chan observation.Session { return o.updates }
func (o *Observer) Removals() <-chan string             { return o.removals }

func (o *Observer) Scan() error {
	for _, current := range o.adapters {
		if err := current.Scan(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	o.rebuild()
	return nil
}

func (o *Observer) Run(stop <-chan struct{}) error {
	done := make(chan struct{})
	var forwards sync.WaitGroup
	for _, current := range o.adapters {
		forwards.Add(1)
		go func(current adapter) {
			defer forwards.Done()
			o.forward(current, done)
		}(current)
		go func(current adapter) { _ = current.Run(stop) }(current)
	}
	<-stop
	close(done)
	forwards.Wait()
	return nil
}

func (o *Observer) Sessions() []observation.Session {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]observation.Session, 0, len(o.sessions))
	for _, session := range o.sessions {
		result = append(result, observation.CopySession(session))
	}
	return result
}

func (o *Observer) Get(sessionID string) (observation.Session, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	session, ok := o.sessions[sessionID]
	return observation.CopySession(session), ok
}

func (o *Observer) Detail(sessionID string) (observation.SessionDetail, error) {
	location, ok := o.location(sessionID)
	if !ok {
		return observation.SessionDetail{}, errors.New("session detail source is unavailable")
	}
	detail, err := o.adapters[location.agent].Detail(location.native)
	if err != nil {
		return observation.SessionDetail{}, err
	}
	detail.SessionID = sessionID
	return detail, nil
}

func (o *Observer) SetManaged(sessionID string, process *observation.ManagedProcess) {
	location, ok := o.location(sessionID)
	if !ok {
		return
	}
	o.adapters[location.agent].SetManaged(location.native, process)
}

func (o *Observer) RegisterResume(sessionID, cwd string, startedAt time.Time) {
	location, ok := o.location(sessionID)
	if !ok {
		return
	}
	if current, ok := o.adapters[location.agent].(resumeRegistrar); ok {
		current.RegisterResume(location.native, cwd, startedAt)
	}
}

func (o *Observer) CancelResume(sessionID string, startedAt time.Time) {
	location, ok := o.location(sessionID)
	if !ok {
		return
	}
	if current, ok := o.adapters[location.agent].(resumeRegistrar); ok {
		current.CancelResume(location.native, startedAt)
	}
}

func (o *Observer) ResumeCommand(sessionID, prompt string) (*exec.Cmd, error) {
	session, ok := o.Get(sessionID)
	if !ok {
		return nil, errors.New("session is no longer available")
	}
	if session.NativeSessionID == "" {
		return nil, errors.New("native session identifier is unavailable")
	}
	switch session.Agent {
	case observation.AgentCodex:
		return exec.Command(codexBinary(), "exec", "resume", "--skip-git-repo-check", session.NativeSessionID, prompt), nil
	case observation.AgentClaudeCode:
		return exec.Command(claudeBinary(), "--resume", session.NativeSessionID, "--print", prompt), nil
	case observation.AgentGeminiCLI:
		return exec.Command(geminiBinary(), "--resume", session.NativeSessionID, "--prompt", prompt), nil
	default:
		return nil, fmt.Errorf("unsupported agent %q", session.Agent)
	}
}

// StartCommand only accepts directories that are already represented by an
// observed Codex session. The browser never gets to choose an arbitrary local
// path for process execution.
func (o *Observer) StartCommand(cwd, prompt string) (*exec.Cmd, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil, errors.New("working directory is required")
	}
	normalized := filepath.Clean(cwd)
	observed := false
	observedCWD := ""
	o.mu.RLock()
	for _, session := range o.sessions {
		if session.Agent == observation.AgentCodex && session.CWD != "" && sameWorkingDirectory(session.CWD, normalized) {
			observed = true
			observedCWD = session.CWD
			break
		}
	}
	o.mu.RUnlock()
	if !observed {
		return nil, errors.New("working directory is not an observed Codex directory")
	}
	command := exec.Command(codexBinary(), "exec", "--skip-git-repo-check", prompt)
	command.Dir = observedCWD
	return command, nil
}

func sameWorkingDirectory(left, right string) bool {
	left, right = filepath.Clean(strings.TrimSpace(left)), filepath.Clean(strings.TrimSpace(right))
	if os.PathSeparator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right || strings.EqualFold(left, right)
}

func (o *Observer) ResumeFailure(sessionID string, commandErr error, stderr string) (string, string) {
	session, _ := o.Get(sessionID)
	if session.Agent == observation.AgentCodex && strings.Contains(strings.ToLower(stderr), "already has an active writer") {
		return "session_writer_busy", "Codex Desktop still owns this session. Wait briefly or continue it in Codex Desktop."
	}
	label := agentLabel(session.Agent)
	for _, line := range reverseLines(stderr) {
		if line = strings.TrimSpace(line); line != "" {
			return "resume_failed", label + " resume failed: " + sanitizeDiagnostic(line)
		}
	}
	return "resume_failed", label + " resume failed: " + commandErr.Error()
}

func (o *Observer) StartFailure(commandErr error, stderr string) (string, string) {
	for _, line := range reverseLines(stderr) {
		if line = strings.TrimSpace(line); line != "" {
			return "start_failed", "Codex start failed: " + sanitizeDiagnostic(line)
		}
	}
	return "start_failed", "Codex start failed: " + commandErr.Error()
}

func (o *Observer) rebuild() {
	nextSessions := make(map[string]observation.Session)
	nextLocations := make(map[string]location)
	for agent, current := range o.adapters {
		for _, session := range current.Sessions() {
			projected := project(agent, session)
			nextSessions[projected.SessionID] = projected
			nextLocations[projected.SessionID] = location{agent: agent, native: projected.NativeSessionID}
		}
	}
	o.mu.Lock()
	o.sessions, o.locations = nextSessions, nextLocations
	o.mu.Unlock()
}

func (o *Observer) forward(current adapter, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case session := <-current.Updates():
			projected := project(current.Agent(), session)
			o.mu.Lock()
			o.sessions[projected.SessionID] = projected
			o.locations[projected.SessionID] = location{agent: current.Agent(), native: projected.NativeSessionID}
			o.mu.Unlock()
			select {
			case o.updates <- observation.CopySession(projected):
			default:
			}
		case nativeID := <-current.Removals():
			publicID := publicID(current.Agent(), nativeID)
			o.mu.Lock()
			delete(o.sessions, publicID)
			delete(o.locations, publicID)
			o.mu.Unlock()
			select {
			case o.removals <- publicID:
			default:
			}
		}
	}
}

func (o *Observer) location(sessionID string) (location, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	location, ok := o.locations[sessionID]
	return location, ok
}

func project(agent observation.Agent, session observation.Session) observation.Session {
	native := session.NativeSessionID
	if native == "" {
		native = session.SessionID
	}
	session.Agent = agent
	session.NativeSessionID = native
	session.SessionID = publicID(agent, native)
	return observation.CopySession(session)
}

func publicID(agent observation.Agent, native string) string {
	sum := sha256.Sum256([]byte(string(agent) + "\x00" + native))
	return fmt.Sprintf("%s:%x", agent, sum[:16])
}
func codexBinary() string  { return binary("AGENTBOARD_CODEX_BIN", "codex") }
func claudeBinary() string { return binary("AGENTBOARD_CLAUDE_BIN", "claude") }
func geminiBinary() string { return binary("AGENTBOARD_GEMINI_BIN", "gemini") }
func binary(environment, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(environment)); value != "" {
		return value
	}
	return fallback
}
func agentLabel(agent observation.Agent) string {
	switch agent {
	case observation.AgentClaudeCode:
		return "Claude Code"
	case observation.AgentGeminiCLI:
		return "Gemini CLI"
	default:
		return "Codex"
	}
}

func reverseLines(value string) []string {
	lines := bytes.Split([]byte(value), []byte{'\n'})
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	result := make([]string, len(lines))
	for index, line := range lines {
		result[index] = string(line)
	}
	return result
}

func sanitizeDiagnostic(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	if len([]rune(value)) > 512 {
		return string([]rune(value)[:512]) + "..."
	}
	return value
}
