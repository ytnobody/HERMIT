package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	gogithub "github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

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

func (c *Client) Owner() string { return c.owner }
func (c *Client) Repo() string  { return c.repo }
