package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/git"
	"github.com/ytnobody/hermit/internal/risk"
)

func registerTools(s *server.MCPServer, client *gh.Client) {
	s.AddTool(
		mcp.NewTool("list_issues",
			mcp.WithDescription("未着手の GitHub Issue 一覧を返す"),
			mcp.WithString("label", mcp.Description("絞り込むラベル名（省略可）")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			mcp.WithDescription("Issue を処理中としてマークする（ラベル付与 + アサイン）"),
			mcp.WithNumber("issue_number", mcp.Description("Issue 番号"), mcp.Required()),
			mcp.WithString("assignee", mcp.Description("アサインするユーザー名"), mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			mcp.WithDescription("Issue 用のブランチと git worktree を作成する"),
			mcp.WithNumber("issue_number", mcp.Description("Issue 番号"), mcp.Required()),
			mcp.WithString("base_branch", mcp.Description("ベースブランチ名"), mcp.Required()),
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
			path, branch, err := git.CreateWorktree(num, base)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]string{"worktree_path": path, "branch": branch})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("evaluate_risk",
			mcp.WithDescription("PR の変更量・影響範囲からリスクレベルを返す"),
			mcp.WithNumber("pr_number", mcp.Description("PR 番号"), mcp.Required()),
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
			mcp.WithDescription("CI 通過確認後に PR をマージする。HIGH リスクの場合は拒否してコメントを投稿する"),
			mcp.WithNumber("pr_number", mcp.Description("PR 番号"), mcp.Required()),
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
				msg := fmt.Sprintf("⚠️ HERMIT: HIGH リスクのため自動マージをスキップします。\n理由: %v", reasons)
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
			return mcp.NewToolResultText(`{"merged":true}`), nil
		},
	)

	s.AddTool(
		mcp.NewTool("close_worktree",
			mcp.WithDescription("マージ完了後にワークツリーとブランチを削除する"),
			mcp.WithString("worktree_path", mcp.Description("ワークツリーのパス"), mcp.Required()),
			mcp.WithString("branch", mcp.Description("削除するブランチ名"), mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			path, err := req.RequireString("worktree_path")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			branch, err := req.RequireString("branch")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := git.CloseWorktree(path, branch); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(`{"success":true}`), nil
		},
	)
}
