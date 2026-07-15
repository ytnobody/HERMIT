package git

// REQ-named test for the requirements reconcile sweep (Issue #152). See
// REQUIREMENTS.md, REQ-005.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestREQ005_CreateWorktree_CreatesBranchAndWorktree verifies REQ-005:
// create_worktree creates a new branch forked from base_branch and a git
// worktree checked out on it, and the returned path/branch match what exists
// on the filesystem.
func TestREQ005_CreateWorktree_CreatesBranchAndWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	run("init", "-b", "main")
	run("config", "user.email", "hermit-test@example.com")
	run("config", "user.name", "hermit-test")
	run("commit", "--allow-empty", "-m", "init")

	// CreateWorktree invokes git in the process working directory.
	t.Chdir(repo)

	// The worktree path is derived as /tmp/<prefix>-<issue>, so use a unique
	// prefix to avoid collisions between test runs.
	prefix := fmt.Sprintf("hermit-req005-%d-%d", os.Getpid(), time.Now().UnixNano())
	issueNumber := 5

	path, branch, err := CreateWorktree(issueNumber, "main", prefix)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	t.Cleanup(func() {
		_ = CloseWorktree(path, branch)
		_ = os.RemoveAll(path)
	})

	wantBranch := fmt.Sprintf("%s/issue-%d", prefix, issueNumber)
	if branch != wantBranch {
		t.Errorf("branch = %q, want %q", branch, wantBranch)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("returned worktree path %s does not exist: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("returned worktree path %s is not a directory", path)
	}

	// The worktree must be checked out on the returned branch.
	out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse in worktree: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != branch {
		t.Errorf("worktree HEAD branch = %q, want %q", got, branch)
	}

	// The new branch must fork from the base branch.
	baseSHA := strings.TrimSpace(string(run("rev-parse", "main")))
	branchSHA := strings.TrimSpace(string(run("rev-parse", branch)))
	if baseSHA != branchSHA {
		t.Errorf("branch %q = %s, want to point at base branch main = %s", branch, branchSHA, baseSHA)
	}
}
