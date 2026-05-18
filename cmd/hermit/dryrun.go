package main

import (
	"fmt"
	"strings"

	gh "github.com/ytnobody/hermit/internal/github"
)

// maxDryRunIssues is the same cap as the Superintendent cycle.
const maxDryRunIssues = 4

// cmdDryRun lists open, unassigned Issues and prints what the Superintendent
// would do without performing any mutating operations.
// It always exits 0.
func cmdDryRun() {
	cfg := loadConfig()
	token := githubToken()
	client := gh.NewClient(token, cfg.GitHub.Owner, cfg.GitHub.Repo)
	prefix := resolveBranchPrefix(cfg)

	fmt.Println("Dry-run mode: showing what would happen")
	fmt.Println()

	issues, err := client.ListOpenIssues("")
	if err != nil {
		fmt.Printf("Warning: could not fetch issues: %v\n", err)
		fmt.Println("No changes made.")
		return
	}

	if len(issues) == 0 {
		fmt.Println("No open issues found.")
		fmt.Println()
		fmt.Println("No changes made.")
		return
	}

	// Cap at maxDryRunIssues, mirroring the Superintendent cycle.
	candidates := issues
	if len(candidates) > maxDryRunIssues {
		candidates = candidates[:maxDryRunIssues]
	}

	fmt.Printf("Issues to process (max %d):\n", maxDryRunIssues)
	for _, issue := range candidates {
		branch := fmt.Sprintf("%s/issue-%d", prefix, issue.Number)
		safePrefixDir := strings.ReplaceAll(prefix, "/", "-")
		worktree := fmt.Sprintf("/tmp/%s-%d", safePrefixDir, issue.Number)
		fmt.Printf("  #%d: %s → branch: %s, worktree: %s\n",
			issue.Number, issue.Title, branch, worktree)
	}

	fmt.Println()
	fmt.Println("No changes made.")
}
