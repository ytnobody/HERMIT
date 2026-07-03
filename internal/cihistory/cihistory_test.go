package cihistory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWasFailing_NoHistory(t *testing.T) {
	dir := t.TempDir()

	failing, err := WasFailing(dir, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failing {
		t.Error("expected false when no history recorded")
	}
}

func TestRecordFailure_ThenWasFailing(t *testing.T) {
	dir := t.TempDir()

	if err := RecordFailure(dir, 42); err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}

	failing, err := WasFailing(dir, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !failing {
		t.Error("expected true after RecordFailure")
	}
}

func TestRecordFailure_IdempotentAndFileLayout(t *testing.T) {
	dir := t.TempDir()

	if err := RecordFailure(dir, 7); err != nil {
		t.Fatalf("first RecordFailure error: %v", err)
	}
	if err := RecordFailure(dir, 7); err != nil {
		t.Fatalf("second RecordFailure error: %v", err)
	}

	path := filepath.Join(dir, ".hermit", "ci_failures", "7")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected marker file at %s: %v", path, err)
	}
}

func TestWasFailing_DoesNotAffectOtherPRs(t *testing.T) {
	dir := t.TempDir()

	if err := RecordFailure(dir, 1); err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}

	failing, err := WasFailing(dir, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failing {
		t.Error("expected false for a PR that never had a recorded failure")
	}
}

func TestClearFailure_RemovesHistory(t *testing.T) {
	dir := t.TempDir()

	if err := RecordFailure(dir, 99); err != nil {
		t.Fatalf("RecordFailure error: %v", err)
	}
	if err := ClearFailure(dir, 99); err != nil {
		t.Fatalf("ClearFailure error: %v", err)
	}

	failing, err := WasFailing(dir, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failing {
		t.Error("expected false after ClearFailure")
	}
}

func TestClearFailure_MissingIsNotError(t *testing.T) {
	dir := t.TempDir()

	if err := ClearFailure(dir, 123); err != nil {
		t.Fatalf("expected no error clearing nonexistent history, got %v", err)
	}
}
