package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(Path(dir))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if st.PRCommentsSince != nil || st.IssueCommentsSince != nil || st.RequirementsSweepSince != nil || st.HealthChecksSince != nil || st.SelfAuditSince != nil {
		t.Fatalf("Load on missing file: want zero-value LoopState, got %+v", st)
	}
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("Load on missing file: want ConsecutiveFailures 0, got %d", st.ConsecutiveFailures)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)

	now := time.Now().UTC().Truncate(time.Second)
	want := LoopState{
		PRCommentsSince:        &now,
		IssueCommentsSince:     &now,
		RequirementsSweepSince: &now,
		HealthChecksSince:      &now,
		SelfAuditSince:         &now,
		LastSuccessTick:        &now,
		ConsecutiveFailures:    2,
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.PRCommentsSince == nil || !got.PRCommentsSince.Equal(now) {
		t.Errorf("PRCommentsSince = %v, want %v", got.PRCommentsSince, now)
	}
	if got.IssueCommentsSince == nil || !got.IssueCommentsSince.Equal(now) {
		t.Errorf("IssueCommentsSince = %v, want %v", got.IssueCommentsSince, now)
	}
	if got.RequirementsSweepSince == nil || !got.RequirementsSweepSince.Equal(now) {
		t.Errorf("RequirementsSweepSince = %v, want %v", got.RequirementsSweepSince, now)
	}
	if got.HealthChecksSince == nil || !got.HealthChecksSince.Equal(now) {
		t.Errorf("HealthChecksSince = %v, want %v", got.HealthChecksSince, now)
	}
	if got.SelfAuditSince == nil || !got.SelfAuditSince.Equal(now) {
		t.Errorf("SelfAuditSince = %v, want %v", got.SelfAuditSince, now)
	}
	if got.LastSuccessTick == nil || !got.LastSuccessTick.Equal(now) {
		t.Errorf("LastSuccessTick = %v, want %v", got.LastSuccessTick, now)
	}
	if got.ConsecutiveFailures != 2 {
		t.Errorf("ConsecutiveFailures = %d, want 2", got.ConsecutiveFailures)
	}
}

func TestSaveCreatesHermitDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := os.Stat(Dir(dir)); !os.IsNotExist(err) {
		t.Fatalf(".hermit dir already exists before Save")
	}
	if err := Save(Path(dir), LoopState{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(Dir(dir)); err != nil {
		t.Fatalf(".hermit dir not created by Save: %v", err)
	}
}

func TestSavePartialUpdatePreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := Save(path, LoopState{PRCommentsSince: &t1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	st.IssueCommentsSince = &t2
	if err := Save(path, st); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.PRCommentsSince == nil || !got.PRCommentsSince.Equal(t1) {
		t.Errorf("PRCommentsSince lost after partial update: got %v, want %v", got.PRCommentsSince, t1)
	}
	if got.IssueCommentsSince == nil || !got.IssueCommentsSince.Equal(t2) {
		t.Errorf("IssueCommentsSince = %v, want %v", got.IssueCommentsSince, t2)
	}
}

func TestPathUnderHermitDir(t *testing.T) {
	got := Path("/some/project")
	want := filepath.Join("/some/project", ".hermit", "superintendent-state.json")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
