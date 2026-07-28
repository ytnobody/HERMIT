package selfaudit

import (
	"strings"

	gh "github.com/ytnobody/hermit/internal/github"
)

// ghClient is the subset of *github.Client the GitHub-backed IssueClient
// adapter needs. It is defined locally (rather than depending on the
// concrete *github.Client) so it can be satisfied by a lightweight fake in
// tests, mirroring internal/requirements/github_adapter.go and
// internal/healthcheck/github_adapter.go.
type ghClient interface {
	// ListIssuesAnyState is used (rather than ListOpenIssues) because a
	// self-audit finding whose Issue was already filed and since closed
	// (fixed, or intentionally won't-fix'd) must not be re-filed just
	// because the Issue is no longer open (Issue #164 acceptance criteria:
	// dedupe against existing open/closed Issues).
	ListIssuesAnyState(label string) ([]gh.Issue, error)
	CreateIssue(title, body string) (int, error)
	AddLabel(number int, label string) error
}

// GitHubIssueClient adapts a GitHub client to the IssueClient interface used
// by File, deduping against open and closed Issues by matching the
// normalized TitlePrefix-stripped title (see NormalizeTitle).
type GitHubIssueClient struct {
	client ghClient
}

// NewGitHubIssueClient wraps client for use as a File IssueClient.
func NewGitHubIssueClient(client ghClient) *GitHubIssueClient {
	return &GitHubIssueClient{client: client}
}

func (g *GitHubIssueClient) FindDuplicate(normalizedTitle string) (int, bool, error) {
	issues, err := g.client.ListIssuesAnyState("")
	if err != nil {
		return 0, false, err
	}
	for _, issue := range issues {
		if !strings.HasPrefix(issue.Title, TitlePrefix) {
			continue
		}
		if NormalizeTitle(issue.Title) == normalizedTitle {
			return issue.Number, true, nil
		}
	}
	return 0, false, nil
}

func (g *GitHubIssueClient) CreateFindingIssue(title, body string) (int, error) {
	num, err := g.client.CreateIssue(title, body)
	if err != nil {
		return 0, err
	}
	if err := g.client.AddLabel(num, Label); err != nil {
		return num, err
	}
	return num, nil
}
