package healthcheck

import (
	"fmt"
	"strings"
	"time"

	gh "github.com/ytnobody/hermit/internal/github"
)

// ghClient is the subset of *github.Client the GitHub-backed IssueClient
// adapter needs. It is defined locally (rather than depending on the
// concrete *github.Client) so it can be satisfied by a lightweight fake in
// tests, mirroring internal/requirements/github_adapter.go's ghClient.
type ghClient interface {
	ListOpenIssues(label string) ([]gh.Issue, error)
	CreateIssue(title, body string) (int, error)
	AddLabel(number int, label string) error
	HasCommentMatching(number int, trigger string) (bool, error)
	PostComment(number int, body string) error
}

// GitHubIssueClient adapts a GitHub client to the IssueClient interface used
// by Reconcile, deduping against open issues by matching the
// "[health-check: <name>]" title prefix (see TitlePrefix).
type GitHubIssueClient struct {
	client ghClient
}

// NewGitHubIssueClient wraps client for use as a Reconcile IssueClient.
func NewGitHubIssueClient(client ghClient) *GitHubIssueClient {
	return &GitHubIssueClient{client: client}
}

func (g *GitHubIssueClient) FindOpenIssue(name string) (int, bool, error) {
	issues, err := g.client.ListOpenIssues("")
	if err != nil {
		return 0, false, err
	}
	prefix := TitlePrefix(name)
	for _, issue := range issues {
		if strings.HasPrefix(issue.Title, prefix) {
			return issue.Number, true, nil
		}
	}
	return 0, false, nil
}

func (g *GitHubIssueClient) CreateIncidentIssue(name, command, output string, detectedAt time.Time) (int, error) {
	title := fmt.Sprintf("%s health check failing", TitlePrefix(name))
	body := fmt.Sprintf(
		"## Production health check failure\n\n"+
			"- **Check name**: %s\n"+
			"- **Command**: `%s`\n"+
			"- **Detected at**: %s\n\n"+
			"### Output\n\n```\n%s\n```\n",
		name, command, detectedAt.UTC().Format(time.RFC3339), output,
	)
	num, err := g.client.CreateIssue(title, body)
	if err != nil {
		return 0, err
	}
	if err := g.client.AddLabel(num, IncidentLabel); err != nil {
		return num, err
	}
	return num, nil
}

func (g *GitHubIssueClient) HasRecoveredComment(number int) (bool, error) {
	return g.client.HasCommentMatching(number, RecoveredTrigger)
}

func (g *GitHubIssueClient) PostRecoveredComment(number int, recoveredAt time.Time) error {
	return g.client.PostComment(number, fmt.Sprintf("%s %s", RecoveredTrigger, recoveredAt.UTC().Format(time.RFC3339)))
}
