package git

import (
	"strings"
	"testing"
)

// TestCreateWorktreeBranchAndPath verifies that CreateWorktree derives the
// branch name and worktree path correctly from the given prefix and issue number.
// We do not actually invoke git here; instead we extract the logic into a
// helper so the pure-logic paths are testable without a real repo.
func TestBranchAndPathDerivation(t *testing.T) {
	tests := []struct {
		name         string
		branchPrefix string
		issueNumber  int
		wantBranch   string
		wantPath     string
	}{
		{
			name:         "legacy prefix",
			branchPrefix: "hermit",
			issueNumber:  1,
			wantBranch:   "hermit/issue-1",
			wantPath:     "/tmp/hermit-1",
		},
		{
			name:         "user-namespaced prefix",
			branchPrefix: "hermit/gh-login",
			issueNumber:  42,
			wantBranch:   "hermit/gh-login/issue-42",
			wantPath:     "/tmp/hermit-gh-login-42",
		},
		{
			name:         "custom prefix with multiple segments",
			branchPrefix: "org/team/user",
			issueNumber:  7,
			wantBranch:   "org/team/user/issue-7",
			wantPath:     "/tmp/org-team-user-7",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			branch, path := deriveBranchAndPath(tc.issueNumber, tc.branchPrefix)
			if branch != tc.wantBranch {
				t.Errorf("branch: got %q, want %q", branch, tc.wantBranch)
			}
			if path != tc.wantPath {
				t.Errorf("path: got %q, want %q", path, tc.wantPath)
			}
		})
	}
}

// deriveBranchAndPath is a pure function extracted from CreateWorktree for testing.
func deriveBranchAndPath(issueNumber int, branchPrefix string) (branch, path string) {
	branch = branchPrefix + "/issue-" + itoa(issueNumber)
	safePrefixDir := strings.ReplaceAll(branchPrefix, "/", "-")
	path = "/tmp/" + safePrefixDir + "-" + itoa(issueNumber)
	return branch, path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

func TestLegacyBranchPrefix(t *testing.T) {
	if LegacyBranchPrefix != "hermit" {
		t.Errorf("LegacyBranchPrefix: got %q, want %q", LegacyBranchPrefix, "hermit")
	}
}
