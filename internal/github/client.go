package github

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// issueRefRe matches common issue reference patterns in PR bodies/titles
// e.g. "closes #31", "fixes #31", "#31"
var issueRefRe = regexp.MustCompile(`(?i)(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)?\s*#(\d+)`)

// GetGitHubLogin returns the authenticated GitHub username by calling
// "gh api user --jq '.login'". If the gh CLI is unavailable or fails,
// an empty string and a non-nil error are returned.
func GetGitHubLogin() (string, error) {
	out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return "", fmt.Errorf("gh api user: %w", err)
	}
	login := strings.TrimSpace(string(out))
	if login == "" {
		return "", fmt.Errorf("gh api user returned empty login")
	}
	return login, nil
}

// RepoConfig identifies a single GitHub repository and an optional label filter
// used for multi-repo mode.
type RepoConfig struct {
	Owner string
	Repo  string
	Label string
}

type Issue struct {
	Number int      `json:"Number"`
	Title  string   `json:"Title"`
	Body   string   `json:"Body"`
	Labels []string `json:"Labels"`
	// Owner and Repo are populated in multi-repo mode to identify the source repo.
	Owner string `json:"Owner,omitempty"`
	Repo  string `json:"Repo,omitempty"`
}

// PRInfo holds a summary of an open pull request returned by ListOpenPRs.
type PRInfo struct {
	PRNumber    int    `json:"pr_number"`
	Title       string `json:"title"`
	HeadBranch  string `json:"head_branch"`
	IssueNumber int    `json:"issue_number,omitempty"` // 0 means not detected
}

type IssueComment struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type PRComment struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	Path      string `json:"path"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type PRFile struct {
	Filename  string
	Additions int
	Deletions int
}

type PRStatus struct {
	Number      int
	Additions   int
	Deletions   int
	Files       []PRFile
	CIPassing   bool
	IssueNumber int // issue number detected from the PR body/title; 0 means not detected
}

type Client struct {
	gh    *gogithub.Client
	owner string
	repo  string
}

func NewClient(token, owner, repo string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{
		gh:    gogithub.NewClient(tc),
		owner: owner,
		repo:  repo,
	}
}

// listOpenIssuesFromRepo fetches open issues from a specific owner/repo pair,
// optionally filtering by label. Each returned Issue has its Owner and Repo
// fields set to the provided values.
func (c *Client) listOpenIssuesFromRepo(owner, repo, label string) ([]Issue, error) {
	opts := &gogithub.IssueListByRepoOptions{
		State: "open",
	}
	if label != "" {
		opts.Labels = []string{label}
	}
	issues, _, err := c.gh.Issues.ListByRepo(context.Background(), owner, repo, opts)
	if err != nil {
		return nil, err
	}
	var result []Issue
	for _, i := range issues {
		if i.PullRequestLinks != nil {
			continue // skip PRs
		}
		var labels []string
		for _, l := range i.Labels {
			labels = append(labels, l.GetName())
		}
		result = append(result, Issue{
			Number: i.GetNumber(),
			Title:  i.GetTitle(),
			Body:   i.GetBody(),
			Labels: labels,
			Owner:  owner,
			Repo:   repo,
		})
	}
	return result, nil
}

// ListOpenIssues returns open issues from the client's primary repository,
// optionally filtered by label. Owner/Repo fields are NOT set in single-repo
// mode to preserve backward compatibility.
func (c *Client) ListOpenIssues(label string) ([]Issue, error) {
	issues, err := c.listOpenIssuesFromRepo(c.owner, c.repo, label)
	if err != nil {
		return nil, err
	}
	// Clear Owner/Repo in single-repo mode for backward compat (callers don't expect them).
	for i := range issues {
		issues[i].Owner = ""
		issues[i].Repo = ""
	}
	return issues, nil
}

// ListAllIssues fetches open issues from all provided repos. If repos is
// empty, it falls back to the client's primary repo (same as ListOpenIssues
// but with Owner/Repo fields populated). The label filter in each RepoConfig
// is applied per-repo.
func (c *Client) ListAllIssues(repos []RepoConfig) ([]Issue, error) {
	if len(repos) == 0 {
		// Fallback: single-repo mode with owner/repo fields set.
		return c.listOpenIssuesFromRepo(c.owner, c.repo, "")
	}
	var all []Issue
	for _, r := range repos {
		issues, err := c.listOpenIssuesFromRepo(r.Owner, r.Repo, r.Label)
		if err != nil {
			return nil, fmt.Errorf("listing issues for %s/%s: %w", r.Owner, r.Repo, err)
		}
		all = append(all, issues...)
	}
	return all, nil
}

// extractIssueNumber parses the first issue reference from the given text
// (e.g. "Closes #31" → 31). Returns 0 if none found.
func extractIssueNumber(text string) int {
	m := issueRefRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ListOpenPRs returns open pull requests. When issueNum > 0 only PRs
// referencing that issue (detected via body/title) are returned.
func (c *Client) ListOpenPRs(issueNum int) ([]PRInfo, error) {
	prs, _, err := c.gh.PullRequests.List(context.Background(), c.owner, c.repo, &gogithub.PullRequestListOptions{
		State: "open",
	})
	if err != nil {
		return nil, err
	}
	var result []PRInfo
	for _, pr := range prs {
		detected := extractIssueNumber(pr.GetBody())
		if detected == 0 {
			detected = extractIssueNumber(pr.GetTitle())
		}
		if issueNum > 0 && detected != issueNum {
			continue
		}
		result = append(result, PRInfo{
			PRNumber:    pr.GetNumber(),
			Title:       pr.GetTitle(),
			HeadBranch:  pr.GetHead().GetRef(),
			IssueNumber: detected,
		})
	}
	return result, nil
}

// CountPRsForIssue returns the total number of pull requests, in any state
// (open, closed, merged), whose title or body references the given issue
// number. Used to detect when an issue required multiple attempted PRs.
//
// It pages through all results rather than stopping at the first page: with
// no explicit sort order the GitHub API returns the 100 most-recently-created
// PRs per page, so a single-page fetch would silently undercount once the
// repo accumulates more than 100 PRs (older PRs referencing this issue would
// never be seen).
func (c *Client) CountPRsForIssue(issueNum int) (int, error) {
	if issueNum <= 0 {
		return 0, nil
	}
	count := 0
	opts := &gogithub.PullRequestListOptions{
		State:       "all",
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}
	for {
		prs, resp, err := c.gh.PullRequests.List(context.Background(), c.owner, c.repo, opts)
		if err != nil {
			return 0, err
		}
		for _, pr := range prs {
			detected := extractIssueNumber(pr.GetBody())
			if detected == 0 {
				detected = extractIssueNumber(pr.GetTitle())
			}
			if detected == issueNum {
				count++
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return count, nil
}

// resolveRepo returns the owner/repo pair to use for an API call.
// If the provided owner or repo strings are empty, the client's primary values
// are used instead.
func (c *Client) resolveRepo(owner, repo string) (string, string) {
	if owner == "" {
		owner = c.owner
	}
	if repo == "" {
		repo = c.repo
	}
	return owner, repo
}

func (c *Client) AssignIssue(number int, assignee string) error {
	return c.AssignIssueInRepo(number, assignee, "", "")
}

// AssignIssueInRepo assigns an issue in a specific repo. Pass empty strings to
// use the client's primary owner/repo.
func (c *Client) AssignIssueInRepo(number int, assignee, owner, repo string) error {
	owner, repo = c.resolveRepo(owner, repo)
	_, _, err := c.gh.Issues.AddAssignees(context.Background(), owner, repo, number, []string{assignee})
	if err != nil {
		return err
	}
	_, _, err = c.gh.Issues.AddLabelsToIssue(context.Background(), owner, repo, number, []string{"in-progress"})
	return err
}

func (c *Client) GetPRStatus(number int) (*PRStatus, error) {
	return c.GetPRStatusInRepo(number, "", "")
}

// GetPRStatusInRepo returns the status of a PR in a specific repo. Pass empty
// strings to use the client's primary owner/repo.
func (c *Client) GetPRStatusInRepo(number int, owner, repo string) (*PRStatus, error) {
	owner, repo = c.resolveRepo(owner, repo)
	pr, _, err := c.gh.PullRequests.Get(context.Background(), owner, repo, number)
	if err != nil {
		return nil, err
	}
	files, _, err := c.gh.PullRequests.ListFiles(context.Background(), owner, repo, number, nil)
	if err != nil {
		return nil, err
	}
	var prFiles []PRFile
	for _, f := range files {
		prFiles = append(prFiles, PRFile{
			Filename:  f.GetFilename(),
			Additions: f.GetAdditions(),
			Deletions: f.GetDeletions(),
		})
	}

	ciPassing, err := c.IsCIPassingInRepo(number, pr.GetHead().GetSHA(), owner, repo)
	if err != nil {
		ciPassing = false
	}

	issueNum := extractIssueNumber(pr.GetBody())
	if issueNum == 0 {
		issueNum = extractIssueNumber(pr.GetTitle())
	}

	return &PRStatus{
		Number:      number,
		Additions:   pr.GetAdditions(),
		Deletions:   pr.GetDeletions(),
		Files:       prFiles,
		CIPassing:   ciPassing,
		IssueNumber: issueNum,
	}, nil
}

func (c *Client) IsCIPassing(prNumber int, sha string) (bool, error) {
	return c.IsCIPassingInRepo(prNumber, sha, "", "")
}

// IsCIPassingInRepo checks CI status for a specific repo. Pass empty strings
// to use the client's primary owner/repo.
//
// CI status is derived from two independent GitHub APIs, since GitHub Actions
// results are exposed as "check-runs" (Checks API) rather than legacy commit
// statuses (Status API) — a repo using Actions-based CI (the common case)
// will have zero legacy commit statuses, so relying on the Status API alone
// makes this always report "no CI configured".
//
// Returns true when:
//   - all legacy commit statuses (if any) report state == "success", AND
//   - all check-runs (if any) are "completed" with a non-blocking conclusion
//     ("success", "neutral", or "skipped"), AND
//   - there is at least one status/check-run — OR there are truly zero of
//     both (no CI configured at all; treat as passing so repos without any
//     CI are not permanently blocked from auto-merge).
//
// A check-run that hasn't completed yet ("queued"/"in_progress") counts as
// not-passing (pending), not as a failure and not as a success.
func (c *Client) IsCIPassingInRepo(prNumber int, sha, owner, repo string) (bool, error) {
	owner, repo = c.resolveRepo(owner, repo)
	result, err := c.fetchCombinedCIStatus(context.Background(), owner, repo, sha)
	if err != nil {
		return false, err
	}
	return result.Passing, nil
}

// combinedCIResult holds the merged CI status computed from both the legacy
// Commit Status API and the GitHub Actions Checks API for a single ref.
type combinedCIResult struct {
	Passing    bool
	State      string
	TotalCount int
	Checks     []CICheckResult
	FailedOnly []CICheckResult
}

// checkRunResultState maps a GitHub Actions check-run's status/conclusion
// into the same "success"/"failure"/"pending" vocabulary used for legacy
// commit statuses, so callers (and the CICheckResult JSON shape) don't need
// to special-case check-runs.
//
// "neutral" and "skipped" conclusions are treated as success because GitHub
// itself does not block merges on them. A check-run that hasn't completed
// yet ("queued"/"in_progress") maps to "pending".
func checkRunResultState(run *gogithub.CheckRun) string {
	if run.GetStatus() != "completed" {
		return "pending"
	}
	switch run.GetConclusion() {
	case "success", "neutral", "skipped":
		return "success"
	default:
		return "failure"
	}
}

// fetchCombinedCIStatus fetches both legacy commit statuses and GitHub
// Actions check-runs for the given ref and combines them into a single
// result. It is the shared implementation behind IsCIPassingInRepo and
// GetCIDetailsInRepo so the combination logic only lives in one place.
func (c *Client) fetchCombinedCIStatus(ctx context.Context, owner, repo, ref string) (*combinedCIResult, error) {
	status, _, err := c.gh.Repositories.GetCombinedStatus(ctx, owner, repo, ref, nil)
	if err != nil {
		return nil, fmt.Errorf("get combined status: %w", err)
	}

	checkRunsResult, _, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, nil)
	if err != nil {
		return nil, fmt.Errorf("list check runs: %w", err)
	}
	var checkRuns []*gogithub.CheckRun
	if checkRunsResult != nil {
		checkRuns = checkRunsResult.CheckRuns
	}

	// Legacy commit-status passing, preserving the original semantics:
	// total_count == 0 means no legacy statuses at all (not a failure signal).
	totalStatuses := status.GetTotalCount()
	legacyState := status.GetState()
	legacyPassing := totalStatuses == 0 || legacyState == "success" || legacyState == ""

	var checks []CICheckResult
	for _, s := range status.Statuses {
		checks = append(checks, CICheckResult{
			Name:        s.GetContext(),
			State:       s.GetState(),
			Description: s.GetDescription(),
			TargetURL:   s.GetTargetURL(),
		})
	}

	checkRunsPassing := true
	for _, run := range checkRuns {
		state := checkRunResultState(run)
		if state != "success" {
			checkRunsPassing = false
		}
		checks = append(checks, CICheckResult{
			Name:        run.GetName(),
			State:       state,
			Description: run.GetOutput().GetSummary(),
			TargetURL:   run.GetHTMLURL(),
		})
	}

	var failed []CICheckResult
	for _, ch := range checks {
		if ch.State == "failure" || ch.State == "error" {
			failed = append(failed, ch)
		}
	}

	total := totalStatuses + len(checkRuns)
	passing := legacyPassing && checkRunsPassing

	// Derive an overall state string. Kept aligned with the legacy status
	// semantics when there's no check-run signal to add, but reflects the
	// combined result once check-runs are in play.
	var state string
	switch {
	case total == 0:
		state = ""
	case passing:
		state = "success"
	case len(failed) > 0:
		state = "failure"
	default:
		state = "pending"
	}

	return &combinedCIResult{
		Passing:    passing,
		State:      state,
		TotalCount: total,
		Checks:     checks,
		FailedOnly: failed,
	}, nil
}

// CICheckResult holds information about a single CI check.
type CICheckResult struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url,omitempty"`
}

// CIDetails holds detailed CI/CD status for a PR.
type CIDetails struct {
	PRNumber   int             `json:"pr_number"`
	SHA        string          `json:"sha"`
	State      string          `json:"state"`
	Passing    bool            `json:"passing"`
	TotalCount int             `json:"total_count"`
	Checks     []CICheckResult `json:"checks"`
	FailedOnly []CICheckResult `json:"failed_only"`
}

// GetCIDetails returns detailed CI/CD status for a given PR including
// per-check results and a list of failing checks.
func (c *Client) GetCIDetails(prNumber int) (*CIDetails, error) {
	return c.GetCIDetailsInRepo(prNumber, "", "")
}

// GetCIDetailsInRepo returns detailed CI/CD status for a PR in a specific
// repo. Pass empty strings to use the client's primary owner/repo.
func (c *Client) GetCIDetailsInRepo(prNumber int, owner, repo string) (*CIDetails, error) {
	owner, repo = c.resolveRepo(owner, repo)

	pr, _, err := c.gh.PullRequests.Get(context.Background(), owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("get PR: %w", err)
	}
	sha := pr.GetHead().GetSHA()

	result, err := c.fetchCombinedCIStatus(context.Background(), owner, repo, sha)
	if err != nil {
		return nil, err
	}

	return &CIDetails{
		PRNumber:   prNumber,
		SHA:        sha,
		State:      result.State,
		Passing:    result.Passing,
		TotalCount: result.TotalCount,
		Checks:     result.Checks,
		FailedOnly: result.FailedOnly,
	}, nil
}

func (c *Client) MergePR(number int) error {
	return c.MergePRInRepo(number, "", "")
}

// MergePRInRepo merges a PR in a specific repo. Pass empty strings to use the
// client's primary owner/repo.
func (c *Client) MergePRInRepo(number int, owner, repo string) error {
	owner, repo = c.resolveRepo(owner, repo)
	opts := &gogithub.PullRequestOptions{MergeMethod: "squash"}
	_, _, err := c.gh.PullRequests.Merge(context.Background(), owner, repo, number, "", opts)
	return err
}

func (c *Client) PostComment(number int, body string) error {
	return c.PostCommentInRepo(number, body, "", "")
}

// PostCommentInRepo posts a comment on an issue/PR in a specific repo. Pass
// empty strings to use the client's primary owner/repo.
func (c *Client) PostCommentInRepo(number int, body, owner, repo string) error {
	owner, repo = c.resolveRepo(owner, repo)
	comment := &gogithub.IssueComment{Body: gogithub.String(body)}
	_, _, err := c.gh.Issues.CreateComment(context.Background(), owner, repo, number, comment)
	return err
}

// CreateIssue opens a new issue on the client's primary repository and
// returns its issue number.
func (c *Client) CreateIssue(title, body string) (int, error) {
	return c.CreateIssueInRepo(title, body, "", "")
}

// CreateIssueInRepo opens a new issue in a specific repo. Pass empty strings
// to use the client's primary owner/repo.
func (c *Client) CreateIssueInRepo(title, body, owner, repo string) (int, error) {
	owner, repo = c.resolveRepo(owner, repo)
	issue, _, err := c.gh.Issues.Create(context.Background(), owner, repo, &gogithub.IssueRequest{
		Title: gogithub.String(title),
		Body:  gogithub.String(body),
	})
	if err != nil {
		return 0, err
	}
	return issue.GetNumber(), nil
}

func (c *Client) FindPRForBranch(branch string) (int, error) {
	prs, _, err := c.gh.PullRequests.List(context.Background(), c.owner, c.repo, &gogithub.PullRequestListOptions{
		State: "open",
		Head:  fmt.Sprintf("%s:%s", c.owner, branch),
	})
	if err != nil {
		return 0, err
	}
	if len(prs) == 0 {
		return 0, fmt.Errorf("no open PR found for branch %s", branch)
	}
	return prs[0].GetNumber(), nil
}

func (c *Client) CloseIssue(number int, comment string) error {
	return c.CloseIssueInRepo(number, comment, "", "")
}

// CloseIssueInRepo closes an issue in a specific repo. Pass empty strings to
// use the client's primary owner/repo.
func (c *Client) CloseIssueInRepo(number int, comment, owner, repo string) error {
	owner, repo = c.resolveRepo(owner, repo)
	if comment != "" {
		if err := c.PostCommentInRepo(number, comment, owner, repo); err != nil {
			return err
		}
	}
	state := "closed"
	_, _, err := c.gh.Issues.Edit(context.Background(), owner, repo, number, &gogithub.IssueRequest{State: &state})
	return err
}

// HasCommentMatching returns true when any comment on the given issue contains
// the provided trigger string (case-insensitive substring match).
func (c *Client) HasCommentMatching(number int, trigger string) (bool, error) {
	return c.HasCommentMatchingInRepo(number, trigger, "", "")
}

// HasCommentMatchingInRepo is the repo-aware variant of HasCommentMatching.
// Pass empty strings to use the client's primary owner/repo.
func (c *Client) HasCommentMatchingInRepo(number int, trigger, owner, repo string) (bool, error) {
	owner, repo = c.resolveRepo(owner, repo)
	comments, _, err := c.gh.Issues.ListComments(context.Background(), owner, repo, number, nil)
	if err != nil {
		return false, err
	}
	lower := strings.ToLower(trigger)
	for _, cm := range comments {
		if strings.Contains(strings.ToLower(cm.GetBody()), lower) {
			return true, nil
		}
	}
	return false, nil
}

// AddLabel adds a label to an issue in the client's primary repo.
func (c *Client) AddLabel(number int, label string) error {
	return c.AddLabelInRepo(number, label, "", "")
}

// AddLabelInRepo adds a label to an issue in a specific repo. Pass empty
// strings to use the client's primary owner/repo. Adding a label an issue
// already has is a no-op on GitHub's side, so this is safe to call
// repeatedly (idempotent).
func (c *Client) AddLabelInRepo(number int, label, owner, repo string) error {
	owner, repo = c.resolveRepo(owner, repo)
	_, _, err := c.gh.Issues.AddLabelsToIssue(context.Background(), owner, repo, number, []string{label})
	return err
}

// GetIssueComments returns all comments on the given issue number.
// since is an optional RFC3339 timestamp; when non-empty only comments
// updated at or after that time are returned.
func (c *Client) GetIssueComments(number int, since string) ([]IssueComment, error) {
	opts := &gogithub.IssueListCommentsOptions{}
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return nil, fmt.Errorf("invalid since timestamp %q: %w", since, err)
		}
		opts.Since = &t
	}
	comments, _, err := c.gh.Issues.ListComments(context.Background(), c.owner, c.repo, number, opts)
	if err != nil {
		return nil, err
	}
	result := make([]IssueComment, 0, len(comments))
	for _, c := range comments {
		result = append(result, IssueComment{
			ID:        c.GetID(),
			Author:    c.GetUser().GetLogin(),
			Body:      c.GetBody(),
			CreatedAt: c.GetCreatedAt().Format(time.RFC3339),
			UpdatedAt: c.GetUpdatedAt().Format(time.RFC3339),
		})
	}
	return result, nil
}

func (c *Client) GetDefaultBranch() (string, error) {
	repo, _, err := c.gh.Repositories.Get(context.Background(), c.owner, c.repo)
	if err != nil {
		return "", err
	}
	return repo.GetDefaultBranch(), nil
}

func (c *Client) Owner() string { return c.owner }
func (c *Client) Repo() string  { return c.repo }

// GetRecentPRComments returns inline review comments on a pull request.
// since is an optional RFC3339 timestamp; when non-empty only comments
// updated at or after that time are returned.
func (c *Client) GetRecentPRComments(prNumber int, since string) ([]PRComment, error) {
	opts := &gogithub.PullRequestListCommentsOptions{}
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return nil, fmt.Errorf("invalid since timestamp %q: %w", since, err)
		}
		opts.Since = t
	}
	comments, _, err := c.gh.PullRequests.ListComments(context.Background(), c.owner, c.repo, prNumber, opts)
	if err != nil {
		return nil, err
	}
	result := make([]PRComment, 0, len(comments))
	for _, c := range comments {
		result = append(result, PRComment{
			ID:        c.GetID(),
			Author:    c.GetUser().GetLogin(),
			Body:      c.GetBody(),
			Path:      c.GetPath(),
			CreatedAt: c.GetCreatedAt().Format(time.RFC3339),
			UpdatedAt: c.GetUpdatedAt().Format(time.RFC3339),
		})
	}
	return result, nil
}

// ReviewPR fetches the PR files and generates a structured review comment.
// It performs static analysis only — no AI/LLM calls.
func (c *Client) ReviewPR(num int) (string, error) {
	pr, _, err := c.gh.PullRequests.Get(context.Background(), c.owner, c.repo, num)
	if err != nil {
		return "", fmt.Errorf("get PR: %w", err)
	}
	files, _, err := c.gh.PullRequests.ListFiles(context.Background(), c.owner, c.repo, num, nil)
	if err != nil {
		return "", fmt.Errorf("list PR files: %w", err)
	}

	// Build PRFile slice for risk evaluation
	var prFiles []PRFile
	for _, f := range files {
		prFiles = append(prFiles, PRFile{
			Filename:  f.GetFilename(),
			Additions: f.GetAdditions(),
			Deletions: f.GetDeletions(),
		})
	}

	additions := pr.GetAdditions()
	deletions := pr.GetDeletions()

	// Checklist checks
	hasTests := false
	hasDocs := false
	for _, f := range files {
		name := f.GetFilename()
		if strings.HasSuffix(name, "_test.go") {
			hasTests = true
		}
		if strings.HasPrefix(name, "docs/") || strings.HasSuffix(name, ".md") {
			hasDocs = true
		}
	}

	// Risk assessment
	level, reasons := reviewEvaluate(prFiles, additions, deletions)

	// Format comment
	var sb strings.Builder
	sb.WriteString("## HERMIT Automated PR Review\n\n")

	// File change summary
	sb.WriteString("### File Change Summary\n\n")
	fmt.Fprintf(&sb, "- **Files changed**: %d\n", len(files))
	fmt.Fprintf(&sb, "- **Lines added**: +%d\n", additions)
	fmt.Fprintf(&sb, "- **Lines removed**: -%d\n", deletions)
	sb.WriteString("\n")

	// Risk assessment
	sb.WriteString("### Risk Assessment\n\n")
	fmt.Fprintf(&sb, "**Level**: %s\n", level)
	if len(reasons) > 0 {
		sb.WriteString("\nReasons:\n")
		for _, r := range reasons {
			fmt.Fprintf(&sb, "- %s\n", r)
		}
	}
	sb.WriteString("\n")

	// Checklist
	sb.WriteString("### Checklist\n\n")
	testMark := "[ ]"
	if hasTests {
		testMark = "[x]"
	}
	docMark := "[ ]"
	if hasDocs {
		docMark = "[x]"
	}
	// Breaking changes: heuristic — deletions >= 50 or high-risk path changes
	breakingChanges := deletions >= 50 || level == "HIGH"
	breakMark := "[ ]"
	if breakingChanges {
		breakMark = "[x]"
	}
	fmt.Fprintf(&sb, "- %s Tests present (`_test.go` files changed)\n", testMark)
	fmt.Fprintf(&sb, "- %s Docs updated (`docs/` or `.md` files changed)\n", docMark)
	fmt.Fprintf(&sb, "- %s Possible breaking changes (large deletions or HIGH risk path)\n", breakMark)

	return sb.String(), nil
}

// reviewEvaluate is a local wrapper that returns Level as a plain string.
// It mirrors risk.Evaluate logic but avoids an import cycle.
// The actual risk.Evaluate is used in the MCP tool layer.
func reviewEvaluate(files []PRFile, additions, deletions int) (string, []string) {
	total := additions + deletions
	var reasons []string

	highPaths := []string{"cmd/", "go.mod", ".github/"}

	if len(files) >= 20 {
		reasons = append(reasons, "20 or more files changed")
	}
	if total >= 500 {
		reasons = append(reasons, "500 or more lines changed")
	}
	for _, f := range files {
		for _, p := range highPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" is in a high-risk path")
			}
		}
	}
	if len(reasons) > 0 {
		return "HIGH", reasons
	}

	if len(files) >= 10 {
		reasons = append(reasons, "10 or more files changed")
	}
	if total >= 200 {
		reasons = append(reasons, "200 or more lines changed")
	}
	for _, f := range files {
		if strings.HasPrefix(f.Filename, "internal/") {
			reasons = append(reasons, f.Filename+" has changes in internal core")
			break
		}
	}
	if len(reasons) > 0 {
		return "MEDIUM", reasons
	}

	return "LOW", nil
}
