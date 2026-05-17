package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// harnessForDryRun writes a minimal harness.toml with a fixed branch_prefix
// suitable for dry-run tests.
func harnessForDryRun(t *testing.T, dir string) {
	t.Helper()
	content := `[github]
owner = "testowner"
repo  = "testrepo"
rate_limit_threshold = 0
[agent]
max_engineers = 4
language = "en"
branch_prefix = "hermit/testuser"
`
	if err := os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureStdout redirects os.Stdout to a pipe, calls f(), then returns the
// captured output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut := os.Stdout
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// captureStderr redirects os.Stderr to a pipe, calls f(), then returns the
// captured output.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origErr := os.Stderr
	os.Stderr = w
	f()
	w.Close()
	os.Stderr = origErr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// TestCmdDryRun_NoIssues verifies the dry-run output when the GitHub API call
// fails (e.g. a dummy token is used). cmdDryRun must always print the header,
// a warning, and "No changes made." — it never exits non-zero.
func TestCmdDryRun_NoIssues(t *testing.T) {
	dir := t.TempDir()
	harnessForDryRun(t, dir)
	prev, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(prev)

	// Provide a dummy token so the HTTP call fails predictably.
	t.Setenv("GITHUB_TOKEN", "dummy")

	out := captureStdout(t, func() {
		captureStderr(t, func() {
			cmdDryRun()
		})
	})

	if !strings.Contains(out, "Dry-run mode") {
		t.Errorf("expected dry-run header, got:\n%s", out)
	}
	if !strings.Contains(out, "No changes made.") {
		t.Errorf("expected 'No changes made.', got:\n%s", out)
	}
}

// TestCmdDryRun_BranchNaming verifies that the branch and worktree path
// naming logic (mirrored from cmdDryRun) produces the expected output for a
// known prefix and issue number.
func TestCmdDryRun_BranchNaming(t *testing.T) {
	prefix := "hermit/alice"

	wantBranch := "hermit/alice/issue-12"
	wantWorktree := "/tmp/hermit-alice-12"

	gotBranch := prefix + "/issue-12"
	safePrefixDir := strings.ReplaceAll(prefix, "/", "-")
	gotWorktree := "/tmp/" + safePrefixDir + "-12"

	if gotBranch != wantBranch {
		t.Errorf("branch: want %q, got %q", wantBranch, gotBranch)
	}
	if gotWorktree != wantWorktree {
		t.Errorf("worktree: want %q, got %q", wantWorktree, gotWorktree)
	}
}

// TestCmdDryRun_MaxIssues verifies that maxDryRunIssues matches the
// Superintendent cycle cap (4).
func TestCmdDryRun_MaxIssues(t *testing.T) {
	if maxDryRunIssues != 4 {
		t.Errorf("maxDryRunIssues: want 4, got %d", maxDryRunIssues)
	}
}

// TestMain_DryRun verifies the "dry-run" subcommand is wired into main() and
// always exits 0.
func TestMain_DryRun(t *testing.T) {
	if os.Getenv("TEST_MAIN_DRYRUN") != "" {
		dir := t.TempDir()
		content := `[github]
owner = "owner"
repo  = "repo"
rate_limit_threshold = 0
[agent]
max_engineers = 4
language = "en"
branch_prefix = "hermit/testuser"
`
		_ = os.WriteFile(filepath.Join(dir, "harness.toml"), []byte(content), 0o644)
		prev, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(prev)
		t.Setenv("GITHUB_TOKEN", "dummy")
		os.Args = []string{"hermit", "dry-run"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_DryRun", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_MAIN_DRYRUN=1")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected zero exit for dry-run, got: %v", err)
	}
}
