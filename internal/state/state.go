// Package state owns .hermit/superintendent-state.json, the persisted
// cadence/liveness state for HERMIT's autonomous loop (Issue #181).
//
// Two independent writers share this one file:
//
//   - `hermit run` (internal/runloop), the long-lived process that owns the
//     ticker, writes LastSuccessTick / ConsecutiveFailures directly after
//     each pass.
//   - The get_loop_state / update_loop_state MCP tools (internal/mcp),
//     called from inside a Superintendent pass, own
//     PRCommentsSince / IssueCommentsSince / RequirementsSweepSince — the
//     three "since" timestamps the Superintendent cycle previously had to
//     hand-write into the file itself.
//
// Either writer only ever does a load-modify-save round trip, and the two
// never run concurrently by construction: `hermit run` blocks on the
// Invoke call (which runs the Superintendent pass, including any
// update_loop_state calls) before it touches the file itself again. Save
// still writes atomically (temp file + rename) as cheap insurance against a
// half-written file if the process is killed mid-write.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DirName is the HERMIT state directory name, relative to the project root.
const DirName = ".hermit"

// FileName is the state file name within DirName.
const FileName = "superintendent-state.json"

// Dir returns the .hermit directory path under rootDir.
func Dir(rootDir string) string {
	return filepath.Join(rootDir, DirName)
}

// Path returns the full path to the state file under rootDir.
func Path(rootDir string) string {
	return filepath.Join(Dir(rootDir), FileName)
}

// LoopState is the JSON shape of .hermit/superintendent-state.json. All
// fields are optional (pointers/zero values) so a project that has never
// run any part of the loop yet round-trips a valid, empty state.
type LoopState struct {
	// PRCommentsSince is the "since" timestamp for the last PR-review-comment
	// check (get_recent_pr_comments), set via update_loop_state.
	PRCommentsSince *time.Time `json:"pr_comments_since,omitempty"`
	// IssueCommentsSince is the "since" timestamp for the last Issue-comment
	// check (get_issue_comments), set via update_loop_state.
	IssueCommentsSince *time.Time `json:"issue_comments_since,omitempty"`
	// RequirementsSweepSince is the last time run_requirements_sweep ran, set
	// via update_loop_state.
	RequirementsSweepSince *time.Time `json:"requirements_sweep_since,omitempty"`
	// LastSuccessTick is the wall-clock time of the most recent
	// `hermit run` pass that completed without error. Written directly by
	// internal/runloop, not via an MCP tool.
	LastSuccessTick *time.Time `json:"last_success_tick,omitempty"`
	// ConsecutiveFailures counts consecutive failed `hermit run` passes
	// since the last success; reset to 0 on success. Written directly by
	// internal/runloop.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`
}

// Load reads the state file at path. A missing file is not an error: it
// returns the zero-value LoopState, matching a project where the loop has
// never recorded any state yet.
func Load(path string) (LoopState, error) {
	var st LoopState
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if len(b) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

// Save atomically writes st to path, creating the parent directory (e.g.
// .hermit/) if it does not already exist. The write goes through a temp
// file in the same directory followed by a rename, so a process killed
// mid-write can never leave a truncated/corrupt state file behind.
func Save(path string, st LoopState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".superintendent-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
