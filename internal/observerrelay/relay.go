// Package observerrelay owns the authenticated Connector observation socket.
package observerrelay

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agentboard/ai-connector/internal/connectorconfig"
	"github.com/agentboard/ai-connector/internal/multiobserver"
	"github.com/agentboard/ai-connector/internal/observation"
	"github.com/agentboard/ai-connector/internal/processmanager"
	"github.com/gorilla/websocket"
)

const protocolVersion = "1.0"
const resumeCooldown = 5 * time.Second
const startCooldown = 5 * time.Second

type Config struct {
	Connector    connectorconfig.Config
	PrivateKey   ed25519.PrivateKey
	SessionsRoot string
}

type envelope struct {
	ProtocolVersion string         `json:"protocol_version"`
	MessageID       string         `json:"message_id"`
	Type            string         `json:"type"`
	DeviceID        string         `json:"device_id"`
	SentAt          string         `json:"sent_at"`
	TraceID         string         `json:"trace_id"`
	Payload         map[string]any `json:"payload"`
	Error           string         `json:"error,omitempty"`
}

type managedProcess struct {
	command         *exec.Cmd
	started         time.Time
	requestID       string
	stderr          *limitedBuffer
	cancelRequested bool
}

type limitedBuffer struct {
	data []byte
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	const maxBytes = 4 * 1024
	if len(value) >= maxBytes {
		b.data = append(b.data[:0], value[len(value)-maxBytes:]...)
		return len(value), nil
	}
	if overflow := len(b.data) + len(value) - maxBytes; overflow > 0 {
		b.data = append(b.data[:0], b.data[overflow:]...)
	}
	b.data = append(b.data, value...)
	return len(value), nil
}

func (b *limitedBuffer) String() string { return string(b.data) }

type relay struct {
	config       Config
	observer     *multiobserver.Observer
	writeMu      sync.Mutex
	managedMu    sync.Mutex
	managed      map[string]managedProcess
	starting     map[string]struct{}
	cooldowns    map[string]time.Time
	newStarting  map[string]struct{}
	newCooldowns map[string]time.Time
	requestsMu   sync.Mutex
	requests     map[string]time.Time
	subscribeMu  sync.RWMutex
	subscribed   bool
}

func Run(ctx context.Context, config Config) error {
	observer, err := multiobserver.New(multiobserver.Config{CodexSessionsRoot: config.SessionsRoot, DeviceID: config.Connector.DeviceID})
	if err != nil {
		return err
	}
	defer observer.Close()
	// Do not announce this device as available until its first snapshot can be
	// served. Large local history trees otherwise make the browser time out.
	if err := observer.Scan(); err != nil {
		return fmt.Errorf("initial agent session scan: %w", err)
	}
	r := &relay{
		config: config, observer: observer, managed: make(map[string]managedProcess),
		starting: make(map[string]struct{}), cooldowns: make(map[string]time.Time),
		newStarting: make(map[string]struct{}), newCooldowns: make(map[string]time.Time), requests: make(map[string]time.Time),
	}
	observerStop := make(chan struct{})
	defer close(observerStop)
	go func() { _ = observer.Run(observerStop) }()
	backoff := time.Second
	for ctx.Err() == nil {
		if err := r.connect(ctx); err != nil && ctx.Err() == nil {
			fmt.Printf("observer connection ended: %v\n", err)
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return ctx.Err()
}

func (r *relay) connect(ctx context.Context) error {
	u, err := url.Parse(r.config.Connector.WSURL)
	if err != nil {
		return err
	}
	query := u.Query()
	query.Set("device_id", r.config.Connector.DeviceID)
	query.Set("credential_id", r.config.Connector.CredentialID)
	u.RawQuery = query.Encode()
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}
	defer connection.Close()
	if err := r.authenticate(connection); err != nil {
		return err
	}
	stop := make(chan struct{})
	defer close(stop)
	go r.heartbeat(connection, stop)
	updatesDone := make(chan struct{})
	go r.forwardObservation(connection, stop, updatesDone)
	defer func() { <-updatesDone }()
	return r.readLoop(connection)
}

func (r *relay) authenticate(connection *websocket.Conn) error {
	if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	defer connection.SetReadDeadline(time.Time{})
	for {
		var message envelope
		if err := connection.ReadJSON(&message); err != nil {
			return err
		}
		switch message.Type {
		case "device.challenge":
			challenge, _ := message.Payload["challenge"].(string)
			if challenge == "" {
				return errors.New("device challenge is missing")
			}
			if err := r.write(connection, "device.authenticate", map[string]any{"credential_id": r.config.Connector.CredentialID, "challenge": challenge, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(r.config.PrivateKey, []byte(challenge)))}); err != nil {
				return err
			}
		case "device.session_ready":
			return nil
		case "protocol.error":
			return fmt.Errorf("authentication rejected: %s", message.Error)
		default:
			return fmt.Errorf("unexpected authentication message: %s", message.Type)
		}
	}
}

func (r *relay) heartbeat(connection *websocket.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			_ = r.write(connection, "device.heartbeat", map[string]any{})
		}
	}
}

func (r *relay) forwardObservation(connection *websocket.Conn, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	type pendingDelta struct {
		session   *observation.Session
		sessionID string
	}
	pending := make(map[string]pendingDelta)
	for {
		select {
		case <-stop:
			return
		case session := <-r.observer.Updates():
			copy := session
			pending[session.SessionID] = pendingDelta{session: &copy, sessionID: session.SessionID}
		case sessionID := <-r.observer.Removals():
			pending[sessionID] = pendingDelta{sessionID: sessionID}
		case <-ticker.C:
			if !r.isSubscribed() {
				clear(pending)
				continue
			}
			for sessionID, delta := range pending {
				if delta.session == nil {
					_ = r.write(connection, "observe.delta", map[string]any{"operation": "remove", "session_id": delta.sessionID})
				} else {
					_ = r.write(connection, "observe.delta", map[string]any{"operation": "upsert", "session": *delta.session})
				}
				delete(pending, sessionID)
			}
		}
	}
}

func (r *relay) readLoop(connection *websocket.Conn) error {
	for {
		_, raw, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		var message envelope
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		switch message.Type {
		case "device.revoke":
			return errors.New("device credential revoked")
		case "observe.subscribe":
			requestID := stringPayload(message.Payload, "request_id")
			if requestID == "" {
				continue
			}
			r.setSubscribed(true)
			_ = r.write(connection, "observe.snapshot", map[string]any{"request_id": requestID, "sessions": r.observer.Sessions()})
		case "observe.unsubscribe":
			r.setSubscribed(false)
		case "observe.detail":
			r.handleDetail(connection, message.Payload)
		case "observe.start":
			r.handleStart(connection, message.Payload)
		case "observe.resume":
			r.handleResume(connection, message.Payload)
		case "observe.managed_input":
			r.handleInput(connection, message.Payload)
		case "observe.managed_cancel":
			r.handleCancel(connection, message.Payload)
		}
	}
}

func (r *relay) handleStart(connection *websocket.Conn, payload map[string]any) {
	requestID, cwd, prompt := stringPayload(payload, "request_id"), stringPayload(payload, "cwd"), stringPayload(payload, "prompt")
	if !r.claimRequest(requestID) {
		return
	}
	if cwd == "" || len([]rune(cwd)) > 1024 {
		r.sendError(connection, requestID, "invalid_working_directory", "The working directory is invalid")
		return
	}
	if prompt == "" || len([]rune(prompt)) > 16_384 {
		r.sendError(connection, requestID, "invalid_prompt", "The initial prompt is invalid")
		return
	}
	if code := r.reserveNewSession(cwd, time.Now()); code != "" {
		r.sendError(connection, requestID, code, startReservationMessage(code))
		return
	}
	reserved := true
	defer func() {
		if reserved {
			r.releaseNewSession(cwd)
		}
	}()
	command, err := r.observer.StartCommand(cwd, prompt)
	if err != nil {
		r.sendError(connection, requestID, "start_directory_not_observed", "The working directory is not an observed Codex directory")
		return
	}
	command.Stdout = io.Discard
	stderr := &limitedBuffer{}
	command.Stderr = stderr
	processmanager.Configure(command)
	if err := command.Start(); err != nil {
		r.releaseNewSession(cwd)
		reserved = false
		r.sendError(connection, requestID, "start_failed", "Could not start a new local Codex process")
		return
	}
	reserved = false
	_ = r.write(connection, "observe.start.result", map[string]any{"request_id": requestID, "cwd": cwd, "status": "started"})
	go r.watchNewSession(connection, cwd, managedProcess{command: command, started: time.Now().UTC(), requestID: requestID, stderr: stderr})
}

func (r *relay) watchNewSession(connection *websocket.Conn, cwd string, process managedProcess) {
	err := process.command.Wait()
	r.releaseNewSessionAfterExit(cwd, time.Now())
	if err != nil {
		code, message := r.observer.StartFailure(err, process.stderr.String())
		r.sendError(connection, process.requestID, code, message)
	}
}

func (r *relay) handleDetail(connection *websocket.Conn, payload map[string]any) {
	requestID, sessionID := stringPayload(payload, "request_id"), stringPayload(payload, "session_id")
	if !r.claimRequest(requestID) {
		return
	}
	if _, ok := r.observer.Get(sessionID); !ok {
		r.sendError(connection, requestID, "session_not_found", "The observed session is no longer available")
		return
	}
	detail, err := r.observer.Detail(sessionID)
	if err != nil {
		r.sendError(connection, requestID, "detail_unavailable", "The local session detail could not be read")
		return
	}
	_ = r.write(connection, "observe.detail.result", map[string]any{
		"request_id": requestID,
		"session_id": sessionID,
		"events":     detail.Events,
		"truncated":  detail.Truncated,
	})
}

func (r *relay) handleResume(connection *websocket.Conn, payload map[string]any) {
	requestID, sessionID, prompt := stringPayload(payload, "request_id"), stringPayload(payload, "session_id"), stringPayload(payload, "prompt")
	if !r.claimRequest(requestID) {
		return
	}
	session, ok := r.observer.Get(sessionID)
	if !ok {
		r.sendError(connection, requestID, "session_not_found", "The observed session is no longer available")
		return
	}
	if session.Status == observation.StatusActive {
		r.sendError(connection, requestID, "session_active", "The session is still active and cannot be resumed")
		return
	}
	if !session.CanResume || session.CWD == "" {
		r.sendError(connection, requestID, "session_not_resumable", "The session cannot be resumed from its local metadata")
		return
	}
	if prompt == "" || len([]rune(prompt)) > 16_384 {
		r.sendError(connection, requestID, "invalid_prompt", "The continuation prompt is invalid")
		return
	}
	if code := r.reserveResume(sessionID, time.Now()); code != "" {
		r.sendError(connection, requestID, code, resumeReservationMessage(code))
		return
	}
	reserved := true
	defer func() {
		if reserved {
			r.releaseResumeReservation(sessionID)
		}
	}()
	command, err := r.observer.ResumeCommand(sessionID, prompt)
	if err != nil {
		r.sendError(connection, requestID, "resume_unsupported", "The local agent cannot resume this session")
		return
	}
	command.Dir = session.CWD
	command.Stdout = io.Discard
	stderr := &limitedBuffer{}
	command.Stderr = stderr
	processmanager.Configure(command)
	started := time.Now().UTC()
	r.observer.RegisterResume(sessionID, session.CWD, started)
	if err := command.Start(); err != nil {
		r.observer.CancelResume(sessionID, started)
		r.sendError(connection, requestID, "resume_start_failed", "Could not start a local agent resume process")
		return
	}
	r.activateManagedResume(sessionID, managedProcess{command: command, started: started, requestID: requestID, stderr: stderr})
	reserved = false
	r.observer.SetManaged(sessionID, &observation.ManagedProcess{PID: command.Process.Pid, StartedAt: started.Format(time.RFC3339Nano), Capabilities: []string{"cancel"}})
	_ = r.write(connection, "observe.resume.result", map[string]any{"request_id": requestID, "session_id": sessionID, "status": "started"})
	go r.watchManaged(connection, sessionID, command)
}

func (r *relay) watchManaged(connection *websocket.Conn, sessionID string, command *exec.Cmd) {
	err := command.Wait()
	process, exists := r.releaseManagedResume(sessionID, time.Now())
	r.observer.SetManaged(sessionID, nil)
	if err != nil && exists && !process.cancelRequested {
		code, message := r.observer.ResumeFailure(sessionID, err, process.stderr.String())
		if code == "session_writer_busy" {
			// No child session can have been created when Codex rejects the writer.
			r.observer.CancelResume(sessionID, process.started)
		}
		r.sendError(connection, process.requestID, code, message)
	}
}

func (r *relay) handleInput(connection *websocket.Conn, payload map[string]any) {
	requestID, sessionID, text := stringPayload(payload, "request_id"), stringPayload(payload, "session_id"), stringPayload(payload, "text")
	if !r.claimRequest(requestID) {
		return
	}
	if text == "" || len([]rune(text)) > 16_384 {
		r.sendError(connection, requestID, "invalid_input", "Managed input is invalid")
		return
	}
	r.managedMu.Lock()
	_, ok := r.managed[sessionID]
	r.managedMu.Unlock()
	if !ok {
		r.sendError(connection, requestID, "managed_process_not_found", "No managed process exists for this session")
		return
	}
	r.sendError(connection, requestID, "managed_input_unsupported", "Submit the next message with Continue original session")
}

func (r *relay) handleCancel(connection *websocket.Conn, payload map[string]any) {
	requestID, sessionID := stringPayload(payload, "request_id"), stringPayload(payload, "session_id")
	if !r.claimRequest(requestID) {
		return
	}
	r.managedMu.Lock()
	process, ok := r.managed[sessionID]
	if ok {
		process.cancelRequested = true
		r.managed[sessionID] = process
	}
	r.managedMu.Unlock()
	if !ok {
		r.sendError(connection, requestID, "managed_process_not_found", "No managed process exists for this session")
		return
	}
	if err := processmanager.TerminatePID(process.command.Process.Pid); err != nil {
		r.sendError(connection, requestID, "cancel_failed", "Could not terminate the managed process")
		return
	}
	_ = r.write(connection, "observe.managed_cancel.result", map[string]any{"request_id": requestID, "session_id": sessionID, "status": "cancel_requested"})
}

func (r *relay) claimRequest(requestID string) bool {
	if requestID == "" || len(requestID) > 128 {
		return false
	}
	now := time.Now()
	r.requestsMu.Lock()
	defer r.requestsMu.Unlock()
	for id, expiry := range r.requests {
		if now.After(expiry) {
			delete(r.requests, id)
		}
	}
	if _, exists := r.requests[requestID]; exists {
		return false
	}
	r.requests[requestID] = now.Add(2 * time.Minute)
	return true
}

// reserveResume serializes the gap before a child process is visible in managed.
// It also prevents an immediately completed continuation from being restarted.
func (r *relay) reserveResume(sessionID string, now time.Time) string {
	r.managedMu.Lock()
	defer r.managedMu.Unlock()
	if _, exists := r.managed[sessionID]; exists {
		return "managed_process_exists"
	}
	if _, exists := r.starting[sessionID]; exists {
		return "session_resume_in_progress"
	}
	if until, exists := r.cooldowns[sessionID]; exists {
		if now.Before(until) {
			return "session_resume_cooling_down"
		}
		delete(r.cooldowns, sessionID)
	}
	r.starting[sessionID] = struct{}{}
	return ""
}

func (r *relay) reserveNewSession(cwd string, now time.Time) string {
	cwd = newSessionKey(cwd)
	r.managedMu.Lock()
	defer r.managedMu.Unlock()
	if _, exists := r.newStarting[cwd]; exists {
		return "new_session_in_progress"
	}
	if until, exists := r.newCooldowns[cwd]; exists {
		if now.Before(until) {
			return "new_session_cooling_down"
		}
		delete(r.newCooldowns, cwd)
	}
	r.newStarting[cwd] = struct{}{}
	return ""
}

func (r *relay) releaseNewSession(cwd string) {
	cwd = newSessionKey(cwd)
	r.managedMu.Lock()
	delete(r.newStarting, cwd)
	r.managedMu.Unlock()
}

func (r *relay) releaseNewSessionAfterExit(cwd string, now time.Time) {
	cwd = newSessionKey(cwd)
	r.managedMu.Lock()
	delete(r.newStarting, cwd)
	r.newCooldowns[cwd] = now.Add(startCooldown)
	r.managedMu.Unlock()
}

func newSessionKey(cwd string) string {
	return strings.ToLower(filepath.Clean(strings.TrimSpace(cwd)))
}

func (r *relay) releaseResumeReservation(sessionID string) {
	r.managedMu.Lock()
	delete(r.starting, sessionID)
	r.managedMu.Unlock()
}

func (r *relay) activateManagedResume(sessionID string, process managedProcess) {
	r.managedMu.Lock()
	delete(r.starting, sessionID)
	r.managed[sessionID] = process
	r.managedMu.Unlock()
}

func (r *relay) releaseManagedResume(sessionID string, now time.Time) (managedProcess, bool) {
	r.managedMu.Lock()
	defer r.managedMu.Unlock()
	process, exists := r.managed[sessionID]
	if !exists {
		return managedProcess{}, false
	}
	delete(r.managed, sessionID)
	r.cooldowns[sessionID] = now.Add(resumeCooldown)
	return process, true
}

func resumeReservationMessage(code string) string {
	switch code {
	case "managed_process_exists":
		return "The session already has a managed process"
	case "session_resume_in_progress":
		return "A continuation request for this session is already starting"
	case "session_resume_cooling_down":
		return "The session was just resumed. Wait a few seconds before continuing it again"
	default:
		return "The session cannot be continued right now"
	}
}

func startReservationMessage(code string) string {
	switch code {
	case "new_session_in_progress":
		return "A new session is already starting in this working directory"
	case "new_session_cooling_down":
		return "A new session was just started in this working directory. Wait a few seconds before starting another"
	default:
		return "A new session cannot be started in this working directory right now"
	}
}

func (r *relay) setSubscribed(value bool) {
	r.subscribeMu.Lock()
	defer r.subscribeMu.Unlock()
	r.subscribed = value
}

func (r *relay) isSubscribed() bool {
	r.subscribeMu.RLock()
	defer r.subscribeMu.RUnlock()
	return r.subscribed
}

func (r *relay) sendError(connection *websocket.Conn, requestID, code, message string) {
	_ = r.write(connection, "observe.error", map[string]any{"request_id": requestID, "code": code, "message": message})
}
func (r *relay) write(connection *websocket.Conn, messageType string, payload map[string]any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return connection.WriteJSON(envelope{ProtocolVersion: protocolVersion, MessageID: newID("msg"), Type: messageType, DeviceID: r.config.Connector.DeviceID, SentAt: time.Now().UTC().Format(time.RFC3339Nano), TraceID: newID("trace"), Payload: payload})
}
func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
func newID(prefix string) string { return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()) }
