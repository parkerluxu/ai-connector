package codexobserver

import "testing"

func TestDetailEventRepairsGBKMojibakeToolOutput(t *testing.T) {
	event, ok := detailEvent([]byte(`{"timestamp":"2026-08-13T12:00:00Z","type":"response_item","payload":{"type":"custom_tool_call_output","output":"# 瀹炴椂浼氳瘽\n工具执行完成"}}`), 1)
	if !ok {
		t.Fatal("expected tool output detail event")
	}
	if event.Content != "# 实时会话\n工具执行完成" {
		t.Fatalf("unexpected repaired output: %q", event.Content)
	}
}

func TestDetailEventLeavesNormalToolOutputUnchanged(t *testing.T) {
	event, ok := detailEvent([]byte(`{"timestamp":"2026-08-13T12:00:00Z","type":"response_item","payload":{"type":"function_call_output","output":"正常 UTF-8 工具结果"}}`), 1)
	if !ok {
		t.Fatal("expected tool output detail event")
	}
	if event.Content != "正常 UTF-8 工具结果" {
		t.Fatalf("normal output changed: %q", event.Content)
	}
}

func TestFindDuplicateCandidateCollapsesCodexUserRepresentations(t *testing.T) {
	candidates := []detailCandidate{
		{event: DetailEvent{At: "2026-08-13T12:00:00.000Z", Kind: "user_message", Content: "hello"}, source: "response_item"},
	}
	candidate := detailCandidate{
		event: DetailEvent{At: "2026-08-13T12:00:00.010Z", Kind: "user_message", Content: "hello"}, source: "event_msg",
	}
	if index := findDuplicateCandidate(candidates, candidate); index != 0 {
		t.Fatalf("expected duplicate at index 0, got %d", index)
	}
	candidates[0] = candidate
	if index := findDuplicateCandidate(candidates, detailCandidate{
		event: DetailEvent{At: "2026-08-13T12:00:00.020Z", Kind: "user_message", Content: "hello"}, source: "event_msg",
	}); index != -1 {
		t.Fatalf("same-source user messages must remain distinct, got %d", index)
	}
}

func TestFindDuplicateCandidateKeepsSeparateTurns(t *testing.T) {
	previous := detailCandidate{
		event: DetailEvent{At: "2026-08-13T12:00:00.000Z", Kind: "assistant_message", Content: "hello"}, source: "event_msg",
	}
	candidate := detailCandidate{
		event: DetailEvent{At: "2026-08-13T12:00:02.000Z", Kind: "assistant_message", Content: "hello"}, source: "response_item",
	}
	if index := findDuplicateCandidate([]detailCandidate{previous}, candidate); index != -1 {
		t.Fatalf("separate assistant turns must remain distinct, got %d", index)
	}
}
