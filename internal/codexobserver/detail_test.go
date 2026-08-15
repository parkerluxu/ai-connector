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
