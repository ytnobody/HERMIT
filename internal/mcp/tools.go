package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ytnobody/hermit/internal/cihistory"
	"github.com/ytnobody/hermit/internal/git"
	gh "github.com/ytnobody/hermit/internal/github"
	"github.com/ytnobody/hermit/internal/lessons"
	"github.com/ytnobody/hermit/internal/notification"
	"github.com/ytnobody/hermit/internal/readiness"
	"github.com/ytnobody/hermit/internal/requirements"
	"github.com/ytnobody/hermit/internal/risk"
)

// clarificationTrigger is the marker HERMIT looks for in Issue/PR comments to
// detect that a human had to step in and request clarification mid-flow. See
// docs/madflow-comparison.md.
const clarificationTrigger = "[Clarification Needed]"

type githubClient interface {
	CheckRateLimit(threshold int) error
	ListOpenIssues(label string) ([]gh.Issue, error)
	ListAllIssues(repos []gh.RepoConfig) ([]gh.Issue, error)
	AssignIssue(number int, assignee string) error
	AssignIssueInRepo(number int, assignee, owner, repo string) error
	GetPRStatus(number int) (*gh.PRStatus, error)
	GetPRStatusInRepo(number int, owner, repo string) (*gh.PRStatus, error)
	PostComment(number int, body string) error
	PostCommentInRepo(number int, body, owner, repo string) error
	MergePR(number int) error
	MergePRInRepo(number int, owner, repo string) error
	CloseIssue(number int, comment string) error
	ListOpenPRs(issueNum int) ([]gh.PRInfo, error)
	CountPRsForIssue(issueNum int) (int, error)
	ReviewPR(num int) (string, error)
	GetIssueComments(issueNumber int, since string) ([]gh.IssueComment, error)
	GetIssueCommentsInRepo(issueNumber int, since, owner, repo string) ([]gh.IssueComment, error)
	HasCommentMatching(number int, trigger string) (bool, error)
	AddLabelInRepo(number int, label, owner, repo string) error
	RemoveLabelInRepo(number int, label, owner, repo string) error
	GetDefaultBranch() (string, error)
	GetCIDetailsInRepo(num int, owner, repo string) (*gh.CIDetails, error)
	GetRecentPRComments(prNumber int, since string) ([]gh.PRComment, error)
	// CreateIssue is used by the run_requirements_sweep tool to open issues
	// for unimplemented/regressed/text-changed requirements via
	// requirements.NewGitHubIssueClient — see requirements.ghClient, which
	// this interface must remain a superset of.
	CreateIssue(title, body string) (int, error)
}

// resolveRiskConfig returns the risk.Config to apply for the given owner/repo
// pair: the per-repo override in repoRiskConfigs when present, otherwise
// defaultRiskConfig. An empty owner/repo (the primary repo in single-repo
// mode, or the unqualified default in multi-repo mode) always resolves to
// defaultRiskConfig.
func resolveRiskConfig(owner, repo string, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config) risk.Config {
	if owner == "" && repo == "" {
		return defaultRiskConfig
	}
	if cfg, ok := repoRiskConfigs[owner+"/"+repo]; ok {
		return cfg
	}
	return defaultRiskConfig
}

func registerTools(s *server.MCPServer, client githubClient, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, readinessCfg readiness.Config, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config, model ModelConfig, requirementsCfg RequirementsConfig, maxEngineers int) {
	s.AddTool(
		mcp.NewTool("get_default_branch",
			mcp.WithDescription("リポジトリのデフォルトブランチ名を返す"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			branch, err := client.GetDefaultBranch()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(map[string]string{"default_branch": branch})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

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
				issues, err = client.ListAllIssues(repos)
			} else {
				label := req.GetString("label", "")
				issues, err = client.ListOpenIssues(label)
			}
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Standing requirements-hearing Issues (Issue #104 — addressed
			// to a human, not an Engineer) are excluded from the queue up
			// front (cheap check against the labels already returned by the
			// list call — no extra API round-trip). They return to the queue
			// once a human removes the label.
			var candidates []gh.Issue
			for _, issue := range issues {
				if readiness.HasLabel(issue.Labels, requirements.HearingLabel) {
					continue
				}
				candidates = append(candidates, issue)
			}
			issues = candidates

			// Issues already flagged as needing clarification are excluded
			// from the queue up front too, UNLESS HERMIT itself posted a
			// hearing comment and the owner has since answered it in
			// comments (Issue #149) — in that case the label is stale, so
			// remove it here and let the Issue re-enter the queue instead of
			// requiring a human to remove the label by hand (Issue #156).
			// Issues that carry the label without a matching hearing/answer
			// trail (e.g. a human applied it directly) are left untouched:
			// they stay excluded until a human removes the label.
			var afterClarification []gh.Issue
			for _, issue := range issues {
				if !readiness.HasLabel(issue.Labels, readinessCfg.Label) {
					afterClarification = append(afterClarification, issue)
					continue
				}

				comments, err := client.GetIssueCommentsInRepo(issue.Number, "", issue.Owner, issue.Repo)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				commentBodies := make([]string, 0, len(comments))
				for _, c := range comments {
					commentBodies = append(commentBodies, c.Body)
				}

				if !readiness.HasHearingComment(commentBodies) {
					// No HERMIT hearing on record for this label — leave it
					// alone (matches pre-#156 behavior).
					continue
				}

				result := readiness.EvaluateWithComments(issue.Body, commentBodies, readinessCfg)
				if !result.Ready {
					// Hearing posted but not yet answered (or answered
					// insufficiently) — keep the Issue excluded.
					continue
				}

				// Answers were posted in comments after the hearing — remove
				// the now-stale label so the Issue re-enters the queue
				// (Issue #156).
				if err := client.RemoveLabelInRepo(issue.Number, readinessCfg.Label, issue.Owner, issue.Repo); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				afterClarification = append(afterClarification, issue)
			}
			issues = afterClarification

			if triggerComment != "" {
				var filtered []gh.Issue
				for _, issue := range issues {
					matched, err := client.HasCommentMatching(issue.Number, triggerComment)
					if err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
					if matched {
						filtered = append(filtered, issue)
					}
				}
				issues = filtered
			}

			// Deterministically judge whether each remaining Issue has enough
			// information to start implementation. Issues judged not ready get
			// a structured hearing comment (posted at most once, idempotently)
			// and the readiness label, then are excluded from the returned
			// queue until a human addresses the feedback.
			var ready []gh.Issue
			for _, issue := range issues {
				result := readiness.Evaluate(issue.Body, readinessCfg)
				if result.Ready {
					ready = append(ready, issue)
					continue
				}

				// The body alone is not ready. The hearing comment asks the
				// owner to answer in comments, so answers may live there
				// rather than in the body (Issue #149): re-evaluate with any
				// comments posted after the hearing comment before deciding
				// to (re-)label. Without this, an Issue whose hearing was
				// answered in comments and whose label was manually removed
				// would be re-labelled forever and never re-enter the queue.
				comments, err := client.GetIssueCommentsInRepo(issue.Number, "", issue.Owner, issue.Repo)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				commentBodies := make([]string, 0, len(comments))
				for _, c := range comments {
					commentBodies = append(commentBodies, c.Body)
				}
				result = readiness.EvaluateWithComments(issue.Body, commentBodies, readinessCfg)
				if result.Ready {
					ready = append(ready, issue)
					continue
				}

				if !readiness.HasHearingComment(commentBodies) {
					comment := readiness.HearingComment(result.Reasons)
					if err := client.PostCommentInRepo(issue.Number, comment, issue.Owner, issue.Repo); err != nil {
						return mcp.NewToolResultError(err.Error()), nil
					}
				}
				if err := client.AddLabelInRepo(issue.Number, readinessCfg.Label, issue.Owner, issue.Repo); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				// Excluded from the returned queue either way.
			}
			issues = ready

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
			riskCfg := resolveRiskConfig(owner, repo, defaultRiskConfig, repoRiskConfigs)
			level, reasons := risk.EvaluateWithConfig(status.Files, status.Additions, status.Deletions, riskCfg)
			b, _ := json.Marshal(map[string]any{"level": level, "reasons": reasons})
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("merge_pr",
			mcp.WithDescription("Merges the PR after CI passes, removes the worktree, and scores the lesson. Posts a risk comment and, by default, blocks the merge when the risk level is HIGH (returns merged: false). When harness.toml's [risk].require_human_approval is true (\"warm-up mode\"), auto-merge is always blocked regardless of risk level. Pass force: true to merge anyway in either case."),
			mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
			mcp.WithString("worktree_path", mcp.Description("Path to the worktree to remove after merge (optional)")),
			mcp.WithString("branch", mcp.Description("Branch name to remove after merge (optional)")),
			mcp.WithString("owner", mcp.Description("Repository owner (optional, defaults to primary repo)")),
			mcp.WithString("repo", mcp.Description("Repository name (optional, defaults to primary repo)")),
			mcp.WithBoolean("force", mcp.Description("If true, merges even when the risk level is HIGH (optional, default false)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			owner := req.GetString("owner", "")
			repo := req.GetString("repo", "")
			force := req.GetBool("force", false)
			status, err := client.GetPRStatusInRepo(num, owner, repo)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			riskCfg := resolveRiskConfig(owner, repo, defaultRiskConfig, repoRiskConfigs)
			level, reasons := risk.EvaluateWithConfig(status.Files, status.Additions, status.Deletions, riskCfg)
			if level == risk.High {
				msg := fmt.Sprintf("⚠️ HERMIT: HIGH risk detected.\nReasons: %v", reasons)
				_ = client.PostCommentInRepo(num, msg, owner, repo)
			}
			// Warm-up mode (riskCfg.RequireHumanApproval) blocks auto-merge via
			// the same gate as a HIGH risk level, regardless of the level
			// actually computed above. evaluate_risk's own level/reasons are
			// unaffected — this only changes merge_pr's gating decision. force
			// overrides both HIGH risk and warm-up mode identically.
			if !force && (level == risk.High || riskCfg.RequireHumanApproval) {
				reason := "high risk"
				if riskCfg.RequireHumanApproval && level != risk.High {
					reason = "human approval required (warm-up mode)"
				}
				b, _ := json.Marshal(map[string]any{"merged": false, "reason": reason, "risk_reasons": reasons})
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

			// Gather real signals for lessons scoring.
			ciWasFailing, _ := cihistory.WasFailing(rootDir, num)

			hasMultiplePRs := false
			if status.IssueNumber > 0 {
				if count, err := client.CountPRsForIssue(status.IssueNumber); err == nil && count > 1 {
					hasMultiplePRs = true
				}
			}

			hasClarification := false
			if matched, err := client.HasCommentMatching(num, clarificationTrigger); err == nil && matched {
				hasClarification = true
			}
			if !hasClarification && status.IssueNumber > 0 {
				if matched, err := client.HasCommentMatching(status.IssueNumber, clarificationTrigger); err == nil && matched {
					hasClarification = true
				}
			}

			// Score and record lesson
			score, lesson, _ := lessons.ProcessMergedPR(
				rootDir,
				strings.ToUpper(string(level)),
				ciWasFailing,
				hasMultiplePRs,
				hasClarification,
			)
			_ = cihistory.ClearFailure(rootDir, num)

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
		mcp.NewTool("get_issue_comments",
			mcp.WithDescription("Returns comments on a GitHub Issue. Use the since parameter to retrieve only comments updated after a given timestamp (RFC3339), which lets the Superintendent detect new activity since the issue was last checked."),
			mcp.WithNumber("issue_number", mcp.Description("Issue number"), mcp.Required()),
			mcp.WithString("since", mcp.Description("RFC3339 timestamp; only comments updated at or after this time are returned (optional)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			num, err := req.RequireInt("issue_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			since := req.GetString("since", "")
			comments, err := client.GetIssueComments(num, since)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, err := json.Marshal(map[string]any{"issue_number": num, "comments": comments, "count": len(comments)})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
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
			mcp.WithDescription("Returns the current HERMIT configuration values. Use this to read settings such as loop_interval, max_engineers (the [agent].max_engineers parallel-Engineer cap from harness.toml), the risk-evaluation policy, and the model/reasoning-effort configured for each role (superintendent, engineer, analyst)."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resp := map[string]any{
				"loop_interval": loopInterval,
				"max_engineers": maxEngineers,
				"risk":          defaultRiskConfig,
				"model": map[string]any{
					"superintendent":        model.Superintendent,
					"engineer":              model.Engineer,
					"analyst":               model.Analyst,
					"superintendent_effort": model.SuperintendentEffort,
					"engineer_effort":       model.EngineerEffort,
					"analyst_effort":        model.AnalystEffort,
				},
			}
			if len(repoRiskConfigs) > 0 {
				resp["risk_overrides"] = repoRiskConfigs
			}
			b, _ := json.Marshal(resp)
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("now",
			mcp.WithDescription("Returns the current wall-clock time as an RFC3339 string. Use this as an authoritative 'now' when computing elapsed time for cadence tracking (e.g. the PR-comment check, Issue-comment check, and requirements-sweep 'since'/last-run timestamps in the background cycle), instead of estimating the current time from context."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			b, _ := json.Marshal(map[string]any{"now": time.Now().UTC().Format(time.RFC3339)})
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
			// If CI is failing, post an investigation comment on the PR and
			// record the failure so it counts against the lessons score even
			// if the PR later passes and gets merged.
			if !details.Passing && len(details.FailedOnly) > 0 {
				var failNames []string
				for _, f := range details.FailedOnly {
					failNames = append(failNames, f.Name)
				}
				msg := fmt.Sprintf("⚠️ HERMIT: CI/CD failure detected on PR #%d (SHA: %s).\nFailing checks: %s\nPlease investigate and fix before merging.",
					num, details.SHA, strings.Join(failNames, ", "))
				_ = client.PostCommentInRepo(num, msg, owner, repo)
				_ = cihistory.RecordFailure(rootDir, num)
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

	s.AddTool(
		mcp.NewTool("get_recent_pr_comments",
			mcp.WithDescription("Returns inline review comments on a pull request, optionally filtered by a timestamp. Use this during the Superintendent loop to detect new PR review activity since the last check."),
			mcp.WithNumber("pr_number", mcp.Description("PR number"), mcp.Required()),
			mcp.WithString("since", mcp.Description("RFC3339 timestamp; only comments updated at or after this time are returned (optional)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := client.CheckRateLimit(rateLimitThreshold); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			num, err := req.RequireInt("pr_number")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			since := req.GetString("since", "")
			comments, err := client.GetRecentPRComments(num, since)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, err := json.Marshal(map[string]any{"pr_number": num, "comments": comments, "count": len(comments)})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("run_requirements_sweep",
			mcp.WithDescription("Runs the requirements reconcile sweep (Issue #106) on demand against the configured requirements document and [requirements].test_command: parses \"## REQ-xxx:\" blocks, runs each requirement's test, and opens (deduped) GitHub issues for requirements that are unimplemented, regressed, or whose text changed since the last sweep. Returns a summary of counts and issues opened. If no requirements document is found or no test_command is configured, returns skipped=true with a reason instead of an error. The Superintendent loop should call this roughly hourly (tracking its own \"last sweep\" timestamp the same way it tracks get_recent_pr_comments' \"since\"), not on every cycle."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			summary, err := requirements.RunReconcileSweep(rootDir, requirementsCfg.Doc, requirementsCfg.TestCommand, requirements.NewGitHubIssueClient(client))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if summary.Skipped {
				b, _ := json.Marshal(map[string]any{
					"skipped": true,
					"reason":  summary.SkipReason,
				})
				return mcp.NewToolResultText(string(b)), nil
			}
			b, err := json.Marshal(map[string]any{
				"skipped":        false,
				"satisfied":      summary.Satisfied,
				"unimplemented":  summary.Unimplemented,
				"regressed":      summary.Regressed,
				"skipped_manual": summary.SkippedManual,
				"issues_opened":  summary.IssuesOpened,
				"summary": fmt.Sprintf("%d satisfied, %d unimplemented, %d regressed, %d skipped (manual), %d issue(s) opened",
					summary.Satisfied, summary.Unimplemented, summary.Regressed, summary.SkippedManual, summary.IssuesOpened),
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)
}
