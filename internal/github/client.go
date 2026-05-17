package github

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

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

type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
}

// PRInfo holds a summary of an open pull request returned by ListOpenPRs.
type PRInfo struct {
	PRNumber    int    `json:"pr_number"`
	Title       string `json:"title"`
	HeadBranch  string `json:"head_branch"`
	IssueNumber int    `json:"issue_number,omitempty"` // 0 means not detected
}

type PRFile struct {
	Filename  string
	Additions int
	Deletions int
}

type PRStatus struct {
	Number    int
	Additions int
	Deletions int
	Files     []PRFile
	CIPassing bool
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

func (c *Client) ListOpenIssues(label string) ([]Issue, error) {
	opts := &gogithub.IssueListByRepoOptions{
		State: "open",
	}
	if label != "" {
		opts.Labels = []string{label}
	}
	issues, _, err := c.gh.Issues.ListByRepo(context.Background(), c.owner, c.repo, opts)
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
		})
	}
	return result, nil
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

func (c *Client) AssignIssue(number int, assignee string) error {
	_, _, err := c.gh.Issues.AddAssignees(context.Background(), c.owner, c.repo, number, []string{assignee})
	if err != nil {
		return err
	}
	_, _, err = c.gh.Issues.AddLabelsToIssue(context.Background(), c.owner, c.repo, number, []string{"in-progress"})
	return err
}

func (c *Client) GetPRStatus(number int) (*PRStatus, error) {
	pr, _, err := c.gh.PullRequests.Get(context.Background(), c.owner, c.repo, number)
	if err != nil {
		return nil, err
	}
	files, _, err := c.gh.PullRequests.ListFiles(context.Background(), c.owner, c.repo, number, nil)
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

	ciPassing, err := c.IsCIPassing(number, pr.GetHead().GetSHA())
	if err != nil {
		ciPassing = false
	}

	return &PRStatus{
		Number:    number,
		Additions: pr.GetAdditions(),
		Deletions: pr.GetDeletions(),
		Files:     prFiles,
		CIPassing: ciPassing,
	}, nil
}

func (c *Client) IsCIPassing(prNumber int, sha string) (bool, error) {
	status, _, err := c.gh.Repositories.GetCombinedStatus(context.Background(), c.owner, c.repo, sha, nil)
	if err != nil {
		return false, err
	}
	state := status.GetState()
	return state == "success" || state == "", nil
}

func (c *Client) MergePR(number int) error {
	opts := &gogithub.PullRequestOptions{MergeMethod: "squash"}
	_, _, err := c.gh.PullRequests.Merge(context.Background(), c.owner, c.repo, number, "", opts)
	return err
}

func (c *Client) PostComment(number int, body string) error {
	comment := &gogithub.IssueComment{Body: gogithub.String(body)}
	_, _, err := c.gh.Issues.CreateComment(context.Background(), c.owner, c.repo, number, comment)
	return err
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
	if comment != "" {
		if err := c.PostComment(number, comment); err != nil {
			return err
		}
	}
	state := "closed"
	_, _, err := c.gh.Issues.Edit(context.Background(), c.owner, c.repo, number, &gogithub.IssueRequest{State: &state})
	return err
}

func (c *Client) Owner() string { return c.owner }
func (c *Client) Repo() string  { return c.repo }

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
	sb.WriteString(fmt.Sprintf("- **Files changed**: %d\n", len(files)))
	sb.WriteString(fmt.Sprintf("- **Lines added**: +%d\n", additions))
	sb.WriteString(fmt.Sprintf("- **Lines removed**: -%d\n", deletions))
	sb.WriteString("\n")

	// Risk assessment
	sb.WriteString("### Risk Assessment\n\n")
	sb.WriteString(fmt.Sprintf("**Level**: %s\n", level))
	if len(reasons) > 0 {
		sb.WriteString("\nReasons:\n")
		for _, r := range reasons {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
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
	sb.WriteString(fmt.Sprintf("- %s Tests present (`_test.go` files changed)\n", testMark))
	sb.WriteString(fmt.Sprintf("- %s Docs updated (`docs/` or `.md` files changed)\n", docMark))
	sb.WriteString(fmt.Sprintf("- %s Possible breaking changes (large deletions or HIGH risk path)\n", breakMark))

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
