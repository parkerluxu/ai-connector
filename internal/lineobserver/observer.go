// Package lineobserver provides a small, tailing JSONL observer for adapters
// whose local history is one JSON object per line.
package lineobserver

import (
	"bufio"
	"bytes"
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

	"github.com/agentboard/ai-connector/internal/observation"
)

const (
	maxDetailEvents       = 200
	maxDetailContentRunes = 8_192
	maxDetailTotalRunes   = 256 * 1024
)

type Config struct {
	Root             string
	DeviceID         string
	Agent            observation.Agent
	PollInterval     time.Duration
	FileMatches      func(path string, entry fs.DirEntry) bool
	SessionDirectory func(root, path string) string
	Reduce           func(path string, session observation.Session, event map[string]any, fallback time.Time) (observation.Session, bool)
	Detail           func(event map[string]any, fallback time.Time) []observation.DetailEvent
}

type fileState struct {
	offset    int64
	pending   []byte
	sessionID string
}

type Observer struct {
	config Config

	mu       sync.RWMutex
	files    map[string]*fileState
	sessions map[string]observation.Session
	updates  chan observation.Session
	removals chan string
}

func New(config Config) (*Observer, error) {
	if strings.TrimSpace(config.Root) == "" {
		return nil, errors.New("sessions root is required")
	}
	if strings.TrimSpace(config.DeviceID) == "" {
		return nil, errors.New("device id is required")
	}
	if config.Agent == "" || config.Reduce == nil || config.Detail == nil {
		return nil, errors.New("agent adapter is incomplete")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 500 * time.Millisecond
	}
	if config.FileMatches == nil {
		config.FileMatches = func(path string, entry fs.DirEntry) bool { return strings.EqualFold(filepath.Ext(path), ".jsonl") }
	}
	if config.SessionDirectory == nil {
		config.SessionDirectory = func(root, path string) string { return "unknown" }
	}
	return &Observer{
		config: config, files: make(map[string]*fileState), sessions: make(map[string]observation.Session),
		updates: make(chan observation.Session, 128), removals: make(chan string, 32),
	}, nil
}

func (o *Observer) Agent() observation.Agent            { return o.config.Agent }
func (o *Observer) Updates() <-chan observation.Session { return o.updates }
func (o *Observer) Removals() <-chan string             { return o.removals }
func (o *Observer) Close() error                        { return nil }

func (o *Observer) Sessions() []observation.Session {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]observation.Session, 0, len(o.sessions))
	for _, session := range o.sessions {
		result = append(result, observation.CopySession(session))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastActivityAt > result[j].LastActivityAt })
	return result
}

func (o *Observer) Get(sessionID string) (observation.Session, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	session, ok := o.sessions[sessionID]
	return observation.CopySession(session), ok
}

func (o *Observer) SetManaged(sessionID string, process *observation.ManagedProcess) {
	o.mu.Lock()
	defer o.mu.Unlock()
	session, ok := o.sessions[sessionID]
	if !ok {
		return
	}
	if process == nil {
		session.ManagedProcess = nil
	} else {
		copy := *process
		copy.Capabilities = append([]string(nil), process.Capabilities...)
		session.ManagedProcess = &copy
	}
	o.setSessionLocked(session)
}

func (o *Observer) Run(stop <-chan struct{}) error {
	if err := o.Scan(); err != nil {
		return err
	}
	ticker := time.NewTicker(o.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
			_ = o.Scan()
		}
	}
}

func (o *Observer) Scan() error {
	seen := make(map[string]struct{})
	err := filepath.WalkDir(o.config.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !o.config.FileMatches(path, entry) {
			return nil
		}
		seen[path] = struct{}{}
		return o.scanFile(path)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	for path, state := range o.files {
		if _, exists := seen[path]; exists {
			continue
		}
		delete(o.files, path)
		if state.sessionID != "" && !o.sessionReferencedLocked(state.sessionID) {
			delete(o.sessions, state.sessionID)
			o.emitRemovalLocked(state.sessionID)
		}
	}
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
		state = &fileState{}
		o.files[path] = state
	}
	if info.Size() < state.offset {
		if state.sessionID != "" && !o.sessionReferencedExceptLocked(state.sessionID, state) {
			delete(o.sessions, state.sessionID)
			o.emitRemovalLocked(state.sessionID)
		}
		*state = fileState{}
	}
	offset := state.offset
	o.mu.Unlock()
	if info.Size() == offset {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open session jsonl: %w", err)
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek session jsonl: %w", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read session jsonl: %w", err)
	}
	o.mu.Lock()
	state = o.files[path]
	if state == nil {
		o.mu.Unlock()
		return nil
	}
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
		o.reduceLine(path, line, info.ModTime())
	}
	return nil
}

func (o *Observer) reduceLine(path string, line []byte, fallback time.Time) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var event map[string]any
	if err := json.Unmarshal(line, &event); err != nil {
		o.markUnknown(path, fallback)
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	state := o.files[path]
	if state == nil {
		return
	}
	current := o.sessions[state.sessionID]
	next, changed := o.config.Reduce(path, observation.CopySession(current), event, fallback)
	if !changed || strings.TrimSpace(next.NativeSessionID) == "" {
		return
	}
	if state.sessionID != "" && state.sessionID != next.NativeSessionID && !o.sessionReferencedExceptLocked(state.sessionID, state) {
		delete(o.sessions, state.sessionID)
		o.emitRemovalLocked(state.sessionID)
	}
	state.sessionID = next.NativeSessionID
	o.setSessionLocked(next)
}

func (o *Observer) Detail(sessionID string) (observation.SessionDetail, error) {
	o.mu.RLock()
	paths := make([]string, 0, 1)
	for path, state := range o.files {
		if state.sessionID == sessionID {
			paths = append(paths, path)
		}
	}
	o.mu.RUnlock()
	if len(paths) == 0 {
		return observation.SessionDetail{}, errors.New("session detail source is unavailable")
	}
	sort.Strings(paths)
	detail := observation.SessionDetail{SessionID: sessionID, Events: make([]observation.DetailEvent, 0, 32)}
	totalRunes := 0
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return observation.SessionDetail{}, fmt.Errorf("open session detail: %w", err)
		}
		info, _ := file.Stat()
		fallback := time.Now().UTC()
		if info != nil {
			fallback = info.ModTime()
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			var event map[string]any
			if json.Unmarshal(scanner.Bytes(), &event) != nil {
				continue
			}
			for _, item := range o.config.Detail(event, fallback) {
				item.Content = Sanitize(item.Content, maxDetailContentRunes)
				if item.Kind == "" || item.Content == "" {
					continue
				}
				if len(detail.Events) >= maxDetailEvents || totalRunes+len([]rune(item.Content)) > maxDetailTotalRunes {
					detail.Truncated = true
					continue
				}
				detail.Events = append(detail.Events, item)
				totalRunes += len([]rune(item.Content))
			}
		}
		err = scanner.Err()
		_ = file.Close()
		if err != nil {
			return observation.SessionDetail{}, fmt.Errorf("read session detail: %w", err)
		}
	}
	sort.SliceStable(detail.Events, func(i, j int) bool { return detail.Events[i].At < detail.Events[j].At })
	for index := range detail.Events {
		detail.Events[index].ID = fmt.Sprintf("event_%d", index+1)
	}
	return detail, nil
}

func (o *Observer) markUnknown(path string, fallback time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	state := o.files[path]
	if state == nil || state.sessionID == "" {
		return
	}
	session, ok := o.sessions[state.sessionID]
	if !ok {
		return
	}
	session.Status, session.StateConfidence = observation.StatusUnknown, "inferred"
	session.LastActivityAt = fallback.UTC().Format(time.RFC3339Nano)
	o.setSessionLocked(session)
}

func (o *Observer) setSessionLocked(session observation.Session) {
	session.Agent = o.config.Agent
	session.DeviceID = o.config.DeviceID
	if session.SessionID == "" {
		session.SessionID = session.NativeSessionID
	}
	if session.SessionDirectory == "" {
		session.SessionDirectory = "unknown"
	}
	if session.Title == "" {
		session.Title = string(o.config.Agent) + " session"
	}
	if session.LastActivityAt == "" {
		session.LastActivityAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	session.CanResume = session.Status != observation.StatusActive && session.CWD != "" && session.NativeSessionID != "" && session.ManagedProcess == nil
	o.sessions[session.NativeSessionID] = observation.CopySession(session)
	select {
	case o.updates <- observation.CopySession(session):
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

func (o *Observer) emitRemovalLocked(sessionID string) {
	if sessionID == "" {
		return
	}
	select {
	case o.removals <- sessionID:
	default:
	}
}

func String(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func Map(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func List(value any) []any {
	result, _ := value.([]any)
	return result
}

func Time(event map[string]any, fallback time.Time) string {
	for _, key := range []string{"timestamp", "created_at", "lastUpdated", "startTime"} {
		value := String(event[key])
		for _, layout := range []string{time.RFC3339Nano, "2006/1/2 15:04:05", "2006-01-02 15:04:05"} {
			if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return parsed.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	return fallback.UTC().Format(time.RFC3339Nano)
}

func Sanitize(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' || r == 127 {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len([]rune(value)) > limit {
		return string([]rune(value)[:limit]) + "..."
	}
	return value
}

func Text(value any) string {
	switch current := value.(type) {
	case string:
		return current
	case []any:
		parts := make([]string, 0, len(current))
		for _, item := range current {
			if item := Map(item); item != nil {
				if text := Text(item["text"]); text != "" {
					parts = append(parts, text)
				}
				if text := Text(item["content"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := Text(current["text"]); text != "" {
			return text
		}
		return Text(current["content"])
	default:
		return ""
	}
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
