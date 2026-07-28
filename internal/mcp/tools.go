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
	"github.com/ytnobody/hermit/internal/healthcheck"
	"github.com/ytnobody/hermit/internal/lessons"
	"github.com/ytnobody/hermit/internal/notification"
	"github.com/ytnobody/hermit/internal/readiness"
	"github.com/ytnobody/hermit/internal/requirements"
	"github.com/ytnobody/hermit/internal/risk"
	"github.com/ytnobody/hermit/internal/selfaudit"
	"github.com/ytnobody/hermit/internal/state"
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
	// AddLabel is used by the run_health_checks tool to label newly-opened
	// production-incident issues via healthcheck.NewGitHubIssueClient — see
	// healthcheck.ghClient, which this interface must remain a superset of
	// (Issue #190). It is also used by run_self_audit to label newly-opened
	// self-audit-finding issues via selfaudit.NewGitHubIssueClient (Issue
	// #164).
	AddLabel(number int, label string) error
	// ListIssuesAnyState is used by the run_self_audit tool to dedupe
	// findings against both open and closed issues via
	// selfaudit.NewGitHubIssueClient — see selfaudit.ghClient, which this
	// interface must remain a superset of (Issue #164). Unlike
	// ListOpenIssues, a self-audit finding whose issue was already filed and
	// since closed must not be re-filed.
	ListIssuesAnyState(label string) ([]gh.Issue, error)
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

func registerTools(s *server.MCPServer, client githubClient, rateLimitThreshold int, rootDir string, branchPrefix string, loopInterval int, webhookURL string, webhookType string, repos []gh.RepoConfig, triggerComment string, readinessCfg readiness.Config, defaultRiskConfig risk.Config, repoRiskConfigs map[string]risk.Config, model ModelConfig, requirementsCfg RequirementsConfig, maxEngineers int, healthChecks []healthcheck.Check) {
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
		mcp.NewTool("get_loop_state",
			mcp.WithDescription("Returns the cadence-tracking timestamps persisted in .hermit/superintendent-state.json: pr_comments_since, issue_comments_since, requirements_sweep_since, health_checks_since, and self_audit_since (RFC3339, omitted if never recorded) — the 'since' values the Superintendent cycle uses to decide when it last checked PR comments, checked Issue comments, ran the requirements sweep, ran the health-check sweep, and ran the idle-time self-audit sweep. Also reports status ('running', 'paused', or 'quit' — set via `hermit pause`/`hermit resume`/`hermit quit`; defaults to 'running' when never recorded), last_success_tick, and consecutive_failures, written by `hermit run`'s own tick loop. This file is owned by HERMIT's Go side: read/write these cadence timestamps only via this tool and update_loop_state, never by hand-writing the JSON file."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			st, err := state.Load(state.Path(rootDir))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(loopStateResponse(st))
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("update_loop_state",
			mcp.WithDescription("Updates one or more of the cadence-tracking timestamps in .hermit/superintendent-state.json: pr_comments_since, issue_comments_since, requirements_sweep_since, health_checks_since, self_audit_since, each an RFC3339 timestamp. Only the fields provided are changed; omitted fields are left as-is. Call the now tool first to get an authoritative current timestamp to pass in, then use this instead of writing the JSON file directly. Returns the full updated state."),
			mcp.WithString("pr_comments_since", mcp.Description("RFC3339 timestamp to record as the last PR-review-comment check time")),
			mcp.WithString("issue_comments_since", mcp.Description("RFC3339 timestamp to record as the last Issue-comment check time")),
			mcp.WithString("requirements_sweep_since", mcp.Description("RFC3339 timestamp to record as the last requirements-sweep time")),
			mcp.WithString("health_checks_since", mcp.Description("RFC3339 timestamp to record as the last health-check sweep time")),
			mcp.WithString("self_audit_since", mcp.Description("RFC3339 timestamp to record as the last self-audit sweep time")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			statePath := state.Path(rootDir)
			st, err := state.Load(statePath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if v := req.GetString("pr_comments_since", ""); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("pr_comments_since: %v", err)), nil
				}
				st.PRCommentsSince = &t
			}
			if v := req.GetString("issue_comments_since", ""); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("issue_comments_since: %v", err)), nil
				}
				st.IssueCommentsSince = &t
			}
			if v := req.GetString("requirements_sweep_since", ""); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("requirements_sweep_since: %v", err)), nil
				}
				st.RequirementsSweepSince = &t
			}
			if v := req.GetString("health_checks_since", ""); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("health_checks_since: %v", err)), nil
				}
				st.HealthChecksSince = &t
			}
			if v := req.GetString("self_audit_since", ""); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("self_audit_since: %v", err)), nil
				}
				st.SelfAuditSince = &t
			}
			if err := state.Save(statePath, st); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, _ := json.Marshal(loopStateResponse(st))
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

	s.AddTool(
		mcp.NewTool("run_health_checks",
			mcp.WithDescription("Runs the production health checks configured under [[health_checks]] in harness.toml (Issue #190): executes each check's command (30s timeout) and returns {name, ok, output} for each. For any failing check (ok:false) with no existing open GitHub issue (matched by an \"[health-check: <name>]\" title prefix), opens a new issue labeled production-incident containing the check name, command, output, and detection time — deduped so a check that is still failing does not open a second issue. For any check that has returned to passing (ok:true) while a matching open issue exists, posts a one-time \"recovered at <time>\" comment on that issue (never auto-closes it). No-ops when no health_checks are configured (returns an empty results list), so unconfigured projects see no change in behavior. The Superintendent loop should call this roughly every 5 minutes (tracking its own \"last health-check\" timestamp via health_checks_since, the same way it tracks requirements_sweep_since), not on every cycle."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if len(healthChecks) == 0 {
				b, _ := json.Marshal(map[string]any{
					"results":            []healthcheck.Result{},
					"issues_opened":      0,
					"recovered_comments": 0,
				})
				return mcp.NewToolResultText(string(b)), nil
			}
			results := healthcheck.RunChecks(healthChecks)
			summary, err := healthcheck.Reconcile(healthChecks, results, healthcheck.NewGitHubIssueClient(client), time.Now())
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, err := json.Marshal(map[string]any{
				"results":            summary.Results,
				"issues_opened":      summary.IssuesOpened,
				"recovered_comments": summary.RecoveredComments,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("run_self_audit",
			mcp.WithDescription("Runs the idle-time self-audit sweep (Issue #164): a lightweight code-review pass looking for bugs, missing test coverage, and security holes, used to keep HERMIT finding work when the Issue queue is empty. This tool is two-phase and stateless between calls — it does not run any analysis itself:\n\n1. Call with no 'findings' argument. The response's 'instructions' field describes the review to perform (see selfaudit.Instructions) — read it and actually perform that review against the current codebase.\n2. For each concrete finding, call this tool again passing 'findings': an array of {\"title\", \"body\"} objects, one per finding. Each finding is deduped against existing open AND closed GitHub Issues by a normalized-title match (so a finding whose Issue was already filed and since closed is not re-filed) and, if not a duplicate, filed as a new Issue labeled 'self-audit'. Returns a summary of how many issues were opened vs. skipped as duplicates.\n\nThe caller (Superintendent) must never fix a finding itself — only file the Issue and leave implementation to the normal Engineer pipeline (same 'coordinator, not implementer' rule as every other step). The Superintendent loop should call this roughly hourly when the Issue queue is empty (tracking its own 'last self-audit' timestamp via self_audit_since, the same way it tracks requirements_sweep_since), not on every cycle. The on-demand caller (e.g. a human in a chat session) may call it any time without waiting for that cadence — both paths share the exact same dedupe/filing logic (internal/selfaudit.File)."),
			mcp.WithArray("findings",
				mcp.Description("Findings to file as (deduped) GitHub Issues, one call after the review described in 'instructions' has actually been performed. Omit entirely (or pass an empty array) to just receive the instructions without filing anything."),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Short human-readable summary of the finding, e.g. \"nil pointer dereference in foo.Bar when cfg is empty\". Do not include a [self-audit] prefix — it is added automatically.",
						},
						"body": map[string]any{
							"type":        "string",
							"description": "Full finding detail: what/where the problem is, why it matters, and file references. Becomes the Issue body verbatim.",
						},
					},
					"required": []string{"title", "body"},
				}),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			rawFindings, hasFindings := req.GetArguments()["findings"]
			findingsList, _ := rawFindings.([]any)
			if !hasFindings || len(findingsList) == 0 {
				b, _ := json.Marshal(map[string]any{
					"instructions": selfaudit.Instructions,
					"note":         "No findings provided — no Issues were filed. Perform the review described in 'instructions', then call run_self_audit again with a non-empty 'findings' array for each concrete problem found.",
				})
				return mcp.NewToolResultText(string(b)), nil
			}

			findings := make([]selfaudit.Finding, 0, len(findingsList))
			for i, raw := range findingsList {
				obj, ok := raw.(map[string]any)
				if !ok {
					return mcp.NewToolResultError(fmt.Sprintf("findings[%d]: expected an object with title/body", i)), nil
				}
				title, _ := obj["title"].(string)
				body, _ := obj["body"].(string)
				if title == "" {
					return mcp.NewToolResultError(fmt.Sprintf("findings[%d]: title is required", i)), nil
				}
				findings = append(findings, selfaudit.Finding{Title: title, Body: body})
			}

			summary, err := selfaudit.File(findings, selfaudit.NewGitHubIssueClient(client))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			b, err := json.Marshal(map[string]any{
				"findings_received":  summary.FindingsReceived,
				"issues_opened":      summary.IssuesOpened,
				"duplicates_skipped": summary.Duplicates,
				"results":            summary.Results,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(b)), nil
		},
	)
}

// loopStateResponse converts a state.LoopState into the JSON-friendly shape
// returned by get_loop_state / update_loop_state: each timestamp is an
// RFC3339 string when set, or omitted entirely when nil, so callers can
// treat a missing key the same way as "never recorded".
func loopStateResponse(st state.LoopState) map[string]any {
	status := st.Status
	if status == "" {
		status = state.StatusRunning
	}
	resp := map[string]any{
		"consecutive_failures": st.ConsecutiveFailures,
		"status":               status,
	}
	setIfNotNil := func(key string, t *time.Time) {
		if t != nil {
			resp[key] = t.UTC().Format(time.RFC3339)
		}
	}
	setIfNotNil("pr_comments_since", st.PRCommentsSince)
	setIfNotNil("issue_comments_since", st.IssueCommentsSince)
	setIfNotNil("requirements_sweep_since", st.RequirementsSweepSince)
	setIfNotNil("health_checks_since", st.HealthChecksSince)
	setIfNotNil("self_audit_since", st.SelfAuditSince)
	setIfNotNil("last_success_tick", st.LastSuccessTick)
	return resp
}
