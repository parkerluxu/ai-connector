package observerrelay

import (
	"testing"
)

func TestLimitedBufferKeepsTheLatestDiagnostic(t *testing.T) {
	buffer := &limitedBuffer{}
	if _, err := buffer.Write([]byte("resume failed")); err != nil {
		t.Fatal(err)
	}
	if buffer.String() != "resume failed" {
		t.Fatalf("unexpected diagnostic: %q", buffer.String())
	}
}
