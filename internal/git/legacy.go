package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// DetectLegacyBranches returns a list of branches matching the hermit/issue-* pattern.
func DetectLegacyBranches() []string {
	out, err := exec.Command("git", "branch", "--list", "hermit/issue-*").Output()
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(string(out), "\n") {
		b := strings.TrimSpace(strings.TrimPrefix(line, "* "))
		if b != "" {
			branches = append(branches, b)
		}
	}
	return branches
}

// DetectZombieWorktrees returns worktree paths whose associated branch has been deleted.
// A worktree is considered a zombie when `git worktree list --porcelain` shows it as
// "prunable" or its branch ref no longer exists.
func DetectZombieWorktrees() []string {
	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}

	type wtInfo struct {
		path   string
		branch string
	}

	var zombies []string
	var current wtInfo
	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = wtInfo{path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			// ref is like refs/heads/hermit/issue-3
			current.branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "prunable":
			// git itself marks this worktree as prunable (path gone)
			if current.path != "" {
				zombies = append(zombies, current.path)
			}
		case line == "":
			// blank line separates entries; check if branch ref exists
			if current.path != "" && current.branch != "" {
				err := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+current.branch).Run()
				if err != nil {
					// branch ref is gone — zombie worktree
					zombies = append(zombies, current.path)
				}
			}
			current = wtInfo{}
		}
	}
	return zombies
}

// CleanupLegacyBranches deletes the given branches safely using git branch -d.
// It skips branches that have unmerged changes (uses -d not -D).
func CleanupLegacyBranches(branches []string) error {
	var errs []string
	for _, b := range branches {
		out, err := exec.Command("git", "branch", "-d", b).CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Sprintf("branch %s: %s", b, strings.TrimSpace(string(out))))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("CleanupLegacyBranches errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}
