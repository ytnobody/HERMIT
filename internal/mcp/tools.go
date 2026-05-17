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
	"github.com/ytnobody/hermit/internal/risk"
)

func registerTools(s *server.MCPServer, client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string) {
	s.AddTool(
		mcp.NewTool("list_issues",
			mcp.WithDescription("Returns a list of open GitHub Issues that have not been started"),
			mcp.WithString("label", mcp.Description("Label name to filter by (optional)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			label := req.GetString("label", "")
			issues, err := client.ListOpenIssues(label)
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
			if err := client.AssignIssue(num, assignee); err != nil {
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
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			status, err := client.GetPRStatus(num)
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
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			status, err := client.GetPRStatus(num)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			level, reasons := risk.Evaluate(status.Files, status.Additions, status.Deletions)
			if level == risk.High {
				msg := fmt.Sprintf("⚠️ HERMIT: Skipping auto-merge due to HIGH risk.\nReasons: %v", reasons)
				_ = client.PostComment(num, msg)
				b, _ := json.Marshal(map[string]any{"merged": false, "reason": "HIGH risk"})
				return mcp.NewToolResultText(string(b)), nil
			}
			if !status.CIPassing {
				b, _ := json.Marshal(map[string]any{"merged": false, "reason": "CI failing"})
				return mcp.NewToolResultText(string(b)), nil
			}
			if err := client.MergePR(num); err != nil {
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
}
