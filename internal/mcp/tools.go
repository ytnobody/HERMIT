package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ytnobody/hermit/internal/git"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/lessons"
	"github.com/ytnobody/hermit/internal/notification"
	"github.com/ytnobody/hermit/internal/risk"
)

func registerTools(s *server.MCPServer, client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig) {
	s.AddTool(
		mcp.NewTool("list_issues",
			mcp.WithDescription("Returns a list of open GitHub Issues that have not been started. In multi-repo mode all configured repos are queried."),
			mcp.WithString("label", mcp.Description("Label name to filter by (optional, single-repo mode only)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var issues []gh.Issue
			var err error
			if len(repos) > 0 {
				// Multi-repo mode: label filter is set per-repo in RepoConfig.
				issues, err = client.ListAllIssues(repos)
			} else {
				label := req.GetString("label", "")
				issues, err = client.ListOpenIssues(label)
			}
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, err := json.Marshal(issues)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("assign_issue",
			mcp.WithDescription("Marks an Issue as in-progress (adds label + assigns)"),
			mcp.WithNumber("issue_number", mcp.Description("Issue number"), mcp.Required()),
			mcp.WithString("assignee", mcp.Description("Username to assign"), mcp.Required()),
			mcp.WithString("owner", mcp.Description("Repository owner (optional, defaults to primary repo)")),
			mcp.WithString("repo", mcp.Description("Repository name (optional, defaults to primary repo)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			num, err := req.RequireInt("issue_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			assignee, err := req.RequireString("assignee")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			owner := req.GetString("owner", "")
			repo := req.GetString("repo", "")
			if err := client.AssignIssueInRepo(num, assignee, owner, repo); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"success":true}`), nil
		},
	)

	s.AddTool(
		mcp.NewTool("create_worktree",
			mcp.WithDescription("Creates a branch and git worktree for an Issue"),
			mcp.WithNumber("issue_number", mcp.Description("Issue number"), mcp.Required()),
			mcp.WithString("base_branch", mcp.Description("Base branch name"), mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("issue_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			base, err := req.RequireString("base_branch")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			path, branch, err := git.CreateWorktree(num, base, branchPrefix)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]string{"worktree_path": path, "branch": branch})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("evaluate_risk",
			mcp.WithDescription("Returns the risk level based on the PR's change volume and impact area"),
			mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
			mcp.WithString("owner", mcp.Description("Repository owner (optional, defaults to primary repo)")),
			mcp.WithString("repo", mcp.Description("Repository name (optional, defaults to primary repo)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			owner := req.GetString("owner", "")
			repo := req.GetString("repo", "")
			status, err := client.GetPRStatusInRepo(num, owner, repo)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			level, reasons := risk.Evaluate(status.Files, status.Additions, status.Deletions)
			b, _ := json.Marshal(map[string]any{"level": level, "reasons": reasons})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("merge_pr",
			mcp.WithDescription("Merges the PR after CI passes, removes the worktree, and scores the lesson. Rejects and posts a comment if HIGH risk."),
			mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
			mcp.WithString("worktree_path", mcp.Description("Path to the worktree to remove after merge (optional)")),
			mcp.WithString("branch", mcp.Description("Branch name to remove after merge (optional)")),
			mcp.WithString("owner", mcp.Description("Repository owner (optional, defaults to primary repo)")),
			mcp.WithString("repo", mcp.Description("Repository name (optional, defaults to primary repo)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			owner := req.GetString("owner", "")
			repo := req.GetString("repo", "")
			status, err := client.GetPRStatusInRepo(num, owner, repo)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			level, reasons := risk.Evaluate(status.Files, status.Additions, status.Deletions)
			if level == risk.High {
				msg := fmt.Sprintf("⚠️ HERMIT: Skipping auto-merge due to HIGH risk.\nReasons: %v", reasons)
				_ = client.PostCommentInRepo(num, msg, owner, repo)
				b, _ := json.Marshal(map[string]any{"merged": false, "reason": "HIGH risk"})
				return mcp.NewToolResultText(string(b)), nil
			}
			if !status.CIPassing {
				b, _ := json.Marshal(map[string]any{"merged": false, "reason": "CI failing"})
				return mcp.NewToolResultText(string(b)), nil
			}
			if err := client.MergePRInRepo(num, owner, repo); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Clean up worktree if provided
			wtPath := req.GetString("worktree_path", "")
			branch := req.GetString("branch", "")
			if wtPath != "" && branch != "" {
				_ = git.CloseWorktree(wtPath, branch)
			}

			// Score and record lesson
			score, lesson, _ := lessons.ProcessMergedPR(
				rootDir,
				strings.ToUpper(string(level)),
				false,
				false,
				false,
			)

			result := map[string]any{"merged": true, "score": score}
			if lesson != "" {
				result["lesson"] = lesson
			}
			b, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("add_issue_comment",
			mcp.WithDescription("Posts a comment on an Issue or PR (e.g. for clarification requests or split suggestions)"),
			mcp.WithNumber("issue_number", mcp.Description("Issue number"), mcp.Required()),
			mcp.WithString("body", mcp.Description("Comment body"), mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("issue_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body, err := req.RequireString("body")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := client.PostComment(num, body); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"success":true}`), nil
		},
	)

	s.AddTool(
		mcp.NewTool("close_issue",
			mcp.WithDescription("Closes a GitHub Issue, optionally posting a comment before closing"),
			mcp.WithNumber("issue_number", mcp.Description("Issue number"), mcp.Required()),
			mcp.WithString("comment", mcp.Description("Comment to post before closing (optional)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("issue_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			comment := req.GetString("comment", "")
			if err := client.CloseIssue(num, comment); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]any{"success": true, "issue_number": num})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("list_prs",
			mcp.WithDescription("Returns a list of open pull requests. Optionally filter by issue number."),
			mcp.WithNumber("issue_number", mcp.Description("If provided, only return PRs referencing this Issue number (optional)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			issueNum := req.GetInt("issue_number", 0)
			prs, err := client.ListOpenPRs(issueNum)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, err := json.Marshal(prs)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("get_lessons",
			mcp.WithDescription("Returns a list of lessons learned from past failures. The Superintendent should consult this at the start of each patrol to avoid repeating the same mistakes."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ls, err := lessons.ReadLessons(rootDir)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]any{"lessons": ls, "count": len(ls)})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("get_config",
			mcp.WithDescription("Returns the current HERMIT configuration values. Use this to read settings such as loop_interval."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			b, _ := json.Marshal(map[string]any{"loop_interval": loopInterval})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("review_pr",
			mcp.WithDescription("Posts a structured automated review comment on a PR based on static analysis of the diff"),
			mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			comment, err := client.ReviewPR(num)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := client.PostComment(num, comment); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]any{"pr_number": num, "comment_posted": true})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("check_ci_status",
			mcp.WithDescription("Checks the CI/CD status for a PR. Returns the overall state, per-check results, and a list of failing checks to aid investigation."),
			mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
			mcp.WithString("owner", mcp.Description("Repository owner (optional, defaults to primary repo)")),
			mcp.WithString("repo", mcp.Description("Repository name (optional, defaults to primary repo)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			owner := req.GetString("owner", "")
			repo := req.GetString("repo", "")
			details, err := client.GetCIDetailsInRepo(num, owner, repo)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			// If CI is failing, post an investigation comment on the PR.
			if !details.Passing && len(details.FailedOnly) > 0 {
				var failNames []string
				for _, f := range details.FailedOnly {
					failNames = append(failNames, f.Name)
				}
				msg := fmt.Sprintf("⚠️ HERMIT: CI/CD failure detected on PR #%d (SHA: %s).\nFailing checks: %s\nPlease investigate and fix before merging.",
					num, details.SHA, strings.Join(failNames, ", "))
				_ = client.PostCommentInRepo(num, msg, owner, repo)
			}
			b, err := json.Marshal(details)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("notify",
			mcp.WithDescription("Sends a notification to the configured webhook (Slack, Discord, or generic). Silently no-ops if no webhook_url is configured."),
			mcp.WithString("event", mcp.Description("Event name (e.g. issue_assigned, pr_merged, high_risk_detected)"), mcp.Required()),
			mcp.WithString("message", mcp.Description("Human-readable notification message"), mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			event, err := req.RequireString("event")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			message, err := req.RequireString("message")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := notification.Send(webhookURL, webhookType, event, message); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]any{"sent": webhookURL != "", "event": event})
			return mcp.NewToolResultText(string(b)), nil
		},
	)
}
