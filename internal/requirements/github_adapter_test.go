package requirements

import (
	"testing"

	gh "github.com/ytnobody/hermit/internal/github"
)

type fakeGHClient struct {
	openIssues []gh.Issue
	createdN   int
	createErr  error
	listErr    error
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

func TestGitHubIssueClient_FindOpenIssue_MatchesMarker(t *testing.T) {
	fake := &fakeGHClient{openIssues: []gh.Issue{
		{Number: 1, Title: "unrelated", Body: "just some text"},
		{Number: 2, Title: "REQ-001: implement", Body: "please implement.\n\n" + marker("REQ-001", KindImplement)},
	}}
	client := NewGitHubIssueClient(fake)

	found, err := client.FindOpenIssue("REQ-001", KindImplement)
	if err != nil {
		t.Fatalf("FindOpenIssue() error = %v", err)
	}
	if !found {
		t.Errorf("expected to find the open issue via its marker")
	}

	// Different kind for the same REQ-ID must not match.
	found, err = client.FindOpenIssue("REQ-001", KindRegression)
	if err != nil {
		t.Fatalf("FindOpenIssue() error = %v", err)
	}
	if found {
		t.Errorf("should not match an issue with a different kind marker")
	}
}

func TestGitHubIssueClient_CreateIssue_EmbedsMarker(t *testing.T) {
	fake := &fakeGHClient{}
	client := NewGitHubIssueClient(fake)

	if err := client.CreateIssue("REQ-005", KindRegression, "title", "some body\n\n"+marker("REQ-005", KindRegression)); err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	if len(fake.openIssues) != 1 {
		t.Fatalf("expected 1 issue to be created, got %d", len(fake.openIssues))
	}

	found, err := client.FindOpenIssue("REQ-005", KindRegression)
	if err != nil {
		t.Fatalf("FindOpenIssue() error = %v", err)
	}
	if !found {
		t.Errorf("expected the just-created issue to be found by FindOpenIssue")
	}
}

func TestGitHubIssueClient_FindOpenIssue_PropagatesListError(t *testing.T) {
	fake := &fakeGHClient{listErr: errFake}
	client := NewGitHubIssueClient(fake)
	if _, err := client.FindOpenIssue("REQ-001", KindImplement); err == nil {
		t.Fatalf("expected error to propagate from ListOpenIssues")
	}
}

var errFake = &fakeErr{"boom"}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
