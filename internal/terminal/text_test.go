package terminal

import "testing"

func TestTextExtractorStripsSplitControlSequences(t *testing.T) {
	var extractor TextExtractor
	if actual := extractor.Write([]byte("\x1b[2")); actual != "" {
		t.Fatalf("expected incomplete ANSI sequence to be hidden, got %q", actual)
	}
	actual := extractor.Write([]byte("JHello\r\n\x1b]0;title\aWorld"))
	if actual != "Hello\nWorld" {
		t.Fatalf("unexpected cleaned output %q", actual)
	}
}

func TestTextExtractorRemovesCursorMovementAndPreservesUTF8(t *testing.T) {
	var extractor TextExtractor
	actual := extractor.Write([]byte("\x1b[H进度\t完成\x1b[0m"))
	if actual != "进度 完成" {
		t.Fatalf("unexpected cleaned output %q", actual)
	}
}

func TestTextExtractorDoesNotSplitTerminalRedrawsAtBareCarriageReturns(t *testing.T) {
	var extractor TextExtractor
	actual := extractor.Write([]byte("old\rnew\n"))
	if actual != "oldnew\n" {
		t.Fatalf("unexpected redraw output %q", actual)
	}
}
