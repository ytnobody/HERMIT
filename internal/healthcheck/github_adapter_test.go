package healthcheck

import (
	"strings"
	"testing"
	"time"

	gh "github.com/ytnobody/hermit/internal/github"
)

type fakeGHClient struct {
	openIssues []gh.Issue
	comments   map[int][]string
	createdN   int

	listErr        error
	createErr      error
	addLabelErr    error
	hasCommentErr  error
	postCommentErr error

	addedLabels []string
	posted      []string
}

func newFakeGHClient() *fakeGHClient {
	return &fakeGHClient{comments: map[int][]string{}}
}

func (f *fakeGHClient) ListOpenIssues(label string) ([]gh.Issue, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.openIssues, nil
}

func (f *fakeGHClient) CreateIssue(title, body string) (int, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.createdN++
	f.openIssues = append(f.openIssues, gh.Issue{Number: f.createdN, Title: title, Body: body})
	return f.createdN, nil
}

func (f *fakeGHClient) AddLabel(number int, label string) error {
	if f.addLabelErr != nil {
		return f.addLabelErr
	}
	f.addedLabels = append(f.addedLabels, label)
	return nil
}

func (f *fakeGHClient) HasCommentMatching(number int, trigger string) (bool, error) {
	if f.hasCommentErr != nil {
		return false, f.hasCommentErr
	}
	for _, c := range f.comments[number] {
		if strings.Contains(strings.ToLower(c), strings.ToLower(trigger)) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeGHClient) PostComment(number int, body string) error {
	if f.postCommentErr != nil {
		return f.postCommentErr
	}
	f.comments[number] = append(f.comments[number], body)
	f.posted = append(f.posted, body)
	return nil
}

func TestGitHubIssueClient_FindOpenIssue_MatchesTitlePrefix(t *testing.T) {
	fake := newFakeGHClient()
	fake.openIssues = []gh.Issue{
		{Number: 1, Title: "unrelated issue"},
		{Number: 2, Title: TitlePrefix("api-health") + " health check failing"},
	}
	client := NewGitHubIssueClient(fake)

	num, found, err := client.FindOpenIssue("api-health")
	if err != nil {
		t.Fatalf("FindOpenIssue() error = %v", err)
	}
	if !found || num != 2 {
		t.Errorf("FindOpenIssue() = (%d, %v), want (2, true)", num, found)
	}

	// A different check name must not match.
	_, found, err = client.FindOpenIssue("other-check")
	if err != nil {
		t.Fatalf("FindOpenIssue() error = %v", err)
	}
	if found {
		t.Errorf("should not match an issue for a different check name")
	}
}

func TestGitHubIssueClient_CreateIncidentIssue_EmbedsDetailsAndLabels(t *testing.T) {
	fake := newFakeGHClient()
	client := NewGitHubIssueClient(fake)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	num, err := client.CreateIncidentIssue("api-health", "curl -sf https://example.com/healthz", "connection refused", now)
	if err != nil {
		t.Fatalf("CreateIncidentIssue() error = %v", err)
	}
	if len(fake.openIssues) != 1 {
		t.Fatalf("expected 1 issue to be created, got %d", len(fake.openIssues))
	}
	issue := fake.openIssues[0]
	if issue.Number != num {
		t.Errorf("issue.Number = %d, want %d", issue.Number, num)
	}
	if !strings.HasPrefix(issue.Title, TitlePrefix("api-health")) {
		t.Errorf("issue.Title = %q, want it to start with %q", issue.Title, TitlePrefix("api-health"))
	}
	for _, want := range []string{"api-health", "curl -sf https://example.com/healthz", "connection refused", "2026-07-28T12:00:00Z"} {
		if !strings.Contains(issue.Body, want) {
			t.Errorf("issue.Body = %q, want it to contain %q", issue.Body, want)
		}
	}
	if len(fake.addedLabels) != 1 || fake.addedLabels[0] != IncidentLabel {
		t.Errorf("addedLabels = %v, want [%q]", fake.addedLabels, IncidentLabel)
	}

	// The just-created issue must now be discoverable by FindOpenIssue too.
	found, ok, err := client.FindOpenIssue("api-health")
	if err != nil || !ok || found != num {
		t.Errorf("FindOpenIssue() = (%d, %v, %v), want (%d, true, nil)", found, ok, err, num)
	}
}

func TestGitHubIssueClient_RecoveredComment_OnceOnly(t *testing.T) {
	fake := newFakeGHClient()
	client := NewGitHubIssueClient(fake)
	now := time.Date(2026, 7, 28, 13, 30, 0, 0, time.UTC)

	has, err := client.HasRecoveredComment(1)
	if err != nil {
		t.Fatalf("HasRecoveredComment() error = %v", err)
	}
	if has {
		t.Errorf("expected no recovered comment yet")
	}

	if err := client.PostRecoveredComment(1, now); err != nil {
		t.Fatalf("PostRecoveredComment() error = %v", err)
	}
	if len(fake.posted) != 1 || !strings.Contains(fake.posted[0], "2026-07-28T13:30:00Z") {
		t.Errorf("posted = %v, want a comment mentioning the timestamp", fake.posted)
	}

	has, err = client.HasRecoveredComment(1)
	if err != nil {
		t.Fatalf("HasRecoveredComment() error = %v", err)
	}
	if !has {
		t.Errorf("expected HasRecoveredComment to report true after posting")
	}
}

func TestGitHubIssueClient_FindOpenIssue_PropagatesListError(t *testing.T) {
	fake := newFakeGHClient()
	fake.listErr = errFake
	client := NewGitHubIssueClient(fake)
	if _, _, err := client.FindOpenIssue("api-health"); err == nil {
		t.Fatalf("expected error to propagate from ListOpenIssues")
	}
}

var errFake = &fakeErr{"boom"}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
