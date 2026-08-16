package codexobserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/agentboard/ai-connector/internal/observation"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	maxDetailEvents       = 200
	maxDetailContentRunes = 8_192
	maxDetailTotalRunes   = 256 * 1024
)

type DetailEvent = observation.DetailEvent
type SessionDetail = observation.SessionDetail

// Detail reads a single local JSONL file only when a device owner explicitly
// asks for it. Its result is never stored in the observer metadata database.
func (o *Observer) Detail(sessionID string) (SessionDetail, error) {
	o.mu.RLock()
	paths := make([]string, 0, 2)
	for candidate, state := range o.files {
		if state.sessionID == sessionID {
			paths = append(paths, candidate)
		}
	}
	o.mu.RUnlock()
	if len(paths) == 0 {
		return SessionDetail{}, errors.New("session detail source is unavailable")
	}
	sort.Strings(paths)

	detail := SessionDetail{SessionID: sessionID, Events: make([]DetailEvent, 0, 32)}
	totalRunes := 0
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return SessionDetail{}, fmt.Errorf("open session detail: %w", err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			event, ok := detailEvent(scanner.Bytes(), len(detail.Events)+1)
			if !ok {
				continue
			}
			contentRunes := len([]rune(event.Content))
			if len(detail.Events) >= maxDetailEvents || totalRunes+contentRunes > maxDetailTotalRunes {
				detail.Truncated = true
				continue
			}
			detail.Events = append(detail.Events, event)
			totalRunes += contentRunes
		}
		err = scanner.Err()
		_ = file.Close()
		if err != nil {
			return SessionDetail{}, fmt.Errorf("read session detail: %w", err)
		}
	}
	sort.SliceStable(detail.Events, func(i, j int) bool { return detail.Events[i].At < detail.Events[j].At })
	for index := range detail.Events {
		detail.Events[index].ID = fmt.Sprintf("event_%d", index+1)
	}
	return detail, nil
}

func detailEvent(line []byte, index int) (DetailEvent, bool) {
	var event map[string]any
	if json.Unmarshal(line, &event) != nil {
		return DetailEvent{}, false
	}
	payload := mapValue(event["payload"])
	if payload == nil {
		return DetailEvent{}, false
	}
	at := eventTime(event)
	outerType := stringValue(event["type"])
	eventType := stringValue(payload["type"])
	kind, content, tool := "", "", ""
	switch outerType {
	case "event_msg":
		switch eventType {
		case "user_message":
			kind, content = "user_message", detailText(payload["message"])
			if content == "" {
				content = detailText(payload["text_elements"])
			}
		case "agent_message":
			kind, content = "assistant_message", detailText(payload["message"])
		}
	case "response_item":
		switch eventType {
		case "message":
			role := strings.ToLower(stringValue(payload["role"]))
			if role == "user" {
				kind = "user_message"
			} else if role == "assistant" {
				kind = "assistant_message"
			}
			content = detailText(payload["content"])
		case "function_call", "custom_tool_call":
			kind, tool = "tool_call", toolName(payload)
			content = detailText(firstDetailValue(payload, "arguments", "input"))
		case "function_call_output", "custom_tool_call_output":
			kind = "tool_result"
			content = repairToolOutputEncoding(detailText(payload["output"]))
		}
	}
	content = sanitizeDetail(content)
	if kind == "" || content == "" {
		return DetailEvent{}, false
	}
	return DetailEvent{ID: fmt.Sprintf("event_%d", index), At: at, Kind: kind, Content: content, ToolName: sanitize(tool, 256)}, true
}

// Some Windows terminal integrations preserve tool output that has been
// decoded as GBK before it reaches Codex's UTF-8 JSONL. Recover that common,
// reversible case only for the explicitly requested detail view.
func repairToolOutputEncoding(value string) string {
	if value == "" || !containsMojibakeMarker(value) {
		return value
	}
	var repaired strings.Builder
	changed := false
	for start := 0; start < len(value); {
		end := start
		for end < len(value) {
			r, size := utf8.DecodeRuneInString(value[end:])
			if r == utf8.RuneError && size == 1 || r <= unicode.MaxASCII || !unicode.IsLetter(r) {
				break
			}
			end += size
		}
		if end == start {
			_, size := utf8.DecodeRuneInString(value[start:])
			repaired.WriteString(value[start : start+size])
			start += size
			continue
		}

		run := value[start:end]
		if decoded, ok := decodeMojibakeRun(run); ok {
			repaired.WriteString(decoded)
			changed = true
		} else {
			repaired.WriteString(run)
		}
		start = end
	}
	if !changed {
		return value
	}
	return repaired.String()
}

func decodeMojibakeRun(value string) (string, bool) {
	if !containsMojibakeMarker(value) {
		return "", false
	}
	encoded, _, err := transform.String(simplifiedchinese.GBK.NewEncoder(), value)
	if err != nil || !utf8.ValidString(encoded) {
		return "", false
	}
	decoded := string([]byte(encoded))
	if decoded == value || !containsReadableText(decoded) {
		return "", false
	}
	return decoded, true
}

func containsMojibakeMarker(value string) bool {
	// These are high-frequency glyphs produced when UTF-8 Chinese text is read
	// as GBK (for example, "瀹炴椂" instead of "实时"). Requiring at least
	// one keeps ordinary UTF-8 tool output unchanged.
	for _, marker := range []string{"瀹炴", "椂浼", "锛氭", "銆傚", "鐨勮", "浠ｇ爜", "鍙互", "缁х画", "闂", "璇锋", "妫€鏌", "寮€濮"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func containsReadableText(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func firstDetailValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, exists := payload[key]; exists {
			return value
		}
	}
	return nil
}

func detailText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := detailText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "message", "content", "output"} {
			if text := detailText(typed[key]); text != "" {
				return text
			}
		}
		encoded, err := json.Marshal(typed)
		if err == nil {
			return string(encoded)
		}
	default:
		encoded, err := json.Marshal(typed)
		if err == nil {
			return string(encoded)
		}
	}
	return ""
}

func sanitizeDetail(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(value))
	runes := []rune(value)
	if len(runes) <= maxDetailContentRunes {
		return value
	}
	return string(runes[:maxDetailContentRunes])
}
