package git

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// LegacyBranchPrefix is the old branch prefix used before user-namespaced branches.
const LegacyBranchPrefix = "hermit"

// CreateWorktree creates a git worktree for the given issue.
// branchPrefix determines the branch namespace (e.g. "hermit/gh-login").
// The resulting branch name is "<branchPrefix>/issue-<issueNumber>".
// The worktree path is derived from the prefix's last segment and the issue number.
//
// Backward-compatibility: if a branch with the legacy format "hermit/issue-<N>"
// already exists, a warning is logged.
func CreateWorktree(issueNumber int, baseBranch, branchPrefix string) (path, branch string, err error) {
	branch = fmt.Sprintf("%s/issue-%d", branchPrefix, issueNumber)

	// Build a filesystem-safe path component from the prefix.
	// e.g. "hermit/gh-login" → "hermit-gh-login"
	safePrefixDir := strings.ReplaceAll(branchPrefix, "/", "-")
	path = fmt.Sprintf("/tmp/%s-%d", safePrefixDir, issueNumber)

	// Backward-compatibility check: warn if old-style branch exists.
	legacyBranch := fmt.Sprintf("%s/issue-%d", LegacyBranchPrefix, issueNumber)
	if branch != legacyBranch {
		if out, err := exec.Command("git", "branch", "--list", legacyBranch).Output(); err == nil {
			if strings.TrimSpace(string(out)) != "" {
				log.Printf("warn: legacy branch %q exists; consider deleting it with `git branch -D %s`", legacyBranch, legacyBranch)
			}
		}
	}

	cmd := exec.Command("git", "worktree", "add", "-b", branch, path, baseBranch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("git worktree add: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return path, branch, nil
}

func CloseWorktree(path, branch string) error {
	if out, err := exec.Command("git", "worktree", "remove", "--force", path).CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "branch", "-D", branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
