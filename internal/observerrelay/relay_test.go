package observerrelay

import (
	"testing"
	"time"
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

func TestReserveResumeRejectsConcurrentAndCoolingRequests(t *testing.T) {
	r := &relay{
		managed:   make(map[string]managedProcess),
		starting:  make(map[string]struct{}),
		cooldowns: make(map[string]time.Time),
	}
	now := time.Now()
	if code := r.reserveResume("session_1", now); code != "" {
		t.Fatalf("first reservation failed: %s", code)
	}
	if code := r.reserveResume("session_1", now); code != "session_resume_in_progress" {
		t.Fatalf("expected in-progress rejection, got %q", code)
	}
	r.releaseResumeReservation("session_1")
	r.cooldowns["session_1"] = now.Add(resumeCooldown)
	if code := r.reserveResume("session_1", now); code != "session_resume_cooling_down" {
		t.Fatalf("expected cooldown rejection, got %q", code)
	}
	if code := r.reserveResume("session_1", now.Add(resumeCooldown)); code != "" {
		t.Fatalf("expired cooldown should allow a reservation, got %q", code)
	}
}

func TestReserveNewSessionRejectsConcurrentAndCoolingRequests(t *testing.T) {
	r := &relay{newStarting: make(map[string]struct{}), newCooldowns: make(map[string]time.Time)}
	now := time.Now()
	if code := r.reserveNewSession("C:/work", now); code != "" {
		t.Fatalf("first reservation failed: %s", code)
	}
	if code := r.reserveNewSession("C:/work", now); code != "new_session_in_progress" {
		t.Fatalf("expected in-progress rejection, got %q", code)
	}
	r.releaseNewSessionAfterExit("C:/work", now)
	if code := r.reserveNewSession("C:/work", now); code != "new_session_cooling_down" {
		t.Fatalf("expected cooldown rejection, got %q", code)
	}
	if code := r.reserveNewSession("C:/work", now.Add(startCooldown)); code != "" {
		t.Fatalf("expired cooldown should allow a reservation, got %q", code)
	}
}
