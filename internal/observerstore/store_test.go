package observerstore

import (
	"path/filepath"
	"testing"
)

func TestStorePersistsOnlyOperationalMetadataAndClearsStaleManagedProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observer.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFile(FileMetadata{
		FileID: "file-hash", SessionID: "session-id", CommittedOffset: 42, LastEventFingerprint: "event-hash",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManagedProcess("session-id", 1234, "2026-08-12T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContinuation("continued-session", "session-id"); err != nil {
		t.Fatal(err)
	}
	var fileID, sessionID, fingerprint string
	var offset int64
	if err := store.db.QueryRow(`SELECT file_id, session_id, committed_offset, last_event_fingerprint FROM observed_files`).Scan(&fileID, &sessionID, &offset, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if fileID != "file-hash" || sessionID != "session-id" || offset != 42 || fingerprint != "event-hash" {
		t.Fatalf("unexpected persisted file metadata: %q %q %d %q", fileID, sessionID, offset, fingerprint)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	var managedCount int
	if err := restarted.db.QueryRow(`SELECT COUNT(*) FROM managed_processes`).Scan(&managedCount); err != nil {
		t.Fatal(err)
	}
	if managedCount != 0 {
		t.Fatalf("stale managed PID must not survive a connector restart, found %d", managedCount)
	}
	parentID, err := restarted.ContinuationParent("continued-session")
	if err != nil || parentID != "session-id" {
		t.Fatalf("continuation mapping must survive restart: %q, %v", parentID, err)
	}
}
