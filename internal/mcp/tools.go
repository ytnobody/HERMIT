package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/git"
	"github.com/ytnobody/hermit/internal/lessons"
	"github.com/ytnobody/hermit/internal/risk"
)

func registerTools(s *server.MCPServer, client *gh.Client, rateLimitThreshold int, rootDir string, branchPrefix string) {
	s.AddTool(
		mcp.NewTool("list_issues",
			mcp.WithDescription("未着手の GitHub Issue 一覧を返す"),
			mcp.WithString("label", mcp.Description("絞り込むラベル名（省略可）")),
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
			mcp.WithDescription("Issue を処理中としてマークする（ラベル付与 + アサイン）"),
			mcp.WithNumber("issue_number", mcp.Description("Issue 番号"), mcp.Required()),
			mcp.WithString("assignee", mcp.Description("アサインするユーザー名"), mcp.Required()),
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
			mcp.WithDescription("CI 通過確認後に PR をマージし、ワークツリーを削除してレッスンを採点する。HIGH リスクの場合は拒否してコメントを投稿する"),
			mcp.WithNumber("pr_number", mcp.Description("PR 番号"), mcp.Required()),
			mcp.WithString("worktree_path", mcp.Description("マージ後に削除するワークツリーのパス（省略可）")),
			mcp.WithString("branch", mcp.Description("マージ後に削除するブランチ名（省略可）")),
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
			mcp.WithDescription("Issue または PR にコメントを投稿する（例: 曖昧さの確認や分割提案）"),
			mcp.WithNumber("issue_number", mcp.Description("Issue 番号"), mcp.Required()),
			mcp.WithString("body", mcp.Description("コメント本文"), mcp.Required()),
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
		mcp.NewTool("get_lessons",
			mcp.WithDescription("これまでの失敗から学んだ教訓一覧を返す。Superintendent はパトロール開始時にこれを参照して同じミスを繰り返さないようにする"),
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
