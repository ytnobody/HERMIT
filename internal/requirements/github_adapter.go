package requirements

import (
	"strings"

	gh "github.com/ytnobody/hermit/internal/github"
)

// ghClient is the subset of *github.Client the GitHub-backed IssueClient
// adapter needs. It is defined locally (rather than depending on the
// concrete *github.Client) so it can be satisfied by a lightweight fake in
// tests.
type ghClient interface {
	ListOpenIssues(label string) ([]gh.Issue, error)
	CreateIssue(title, body string) (int, error)
}

// GitHubIssueClient adapts a GitHub client to the IssueClient interface used
// by Sweep, deduping against open issues by searching for this package's
// marker comment in each open issue's body.
type GitHubIssueClient struct {
	client ghClient
}

// NewGitHubIssueClient wraps client for use as a Sweep IssueClient.
func NewGitHubIssueClient(client ghClient) *GitHubIssueClient {
	return &GitHubIssueClient{client: client}
}

func (g *GitHubIssueClient) FindOpenIssue(reqID string, kind IssueKind) (bool, error) {
	issues, err := g.client.ListOpenIssues("")
	if err != nil {
		return false, err
	}
	want := marker(reqID, kind)
	for _, issue := range issues {
		if strings.Contains(issue.Body, want) {
			return true, nil
		}
	}
	return false, nil
}

func (g *GitHubIssueClient) CreateIssue(reqID string, kind IssueKind, title, body string) error {
	_, err := g.client.CreateIssue(title, body)
	return err
}
