package git

import (
	"fmt"
	"os/exec"
	"strings"
)

func CreateWorktree(issueNumber int, baseBranch string) (path, branch string, err error) {
	branch = fmt.Sprintf("hermit/issue-%d", issueNumber)
	path = fmt.Sprintf("/tmp/hermit-%d", issueNumber)

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
