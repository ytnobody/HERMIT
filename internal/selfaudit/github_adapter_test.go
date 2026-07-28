package selfaudit

import (
	"testing"

	gh "github.com/ytnobody/hermit/internal/github"
)

type fakeGhClient struct {
	issues        []gh.Issue
	issuesErr     error
	createErr     error
	nextNum       int
	createdTitles []string
	addedLabels   []string
}

func (f *fakeGhClient) ListIssuesAnyState(_ string) ([]gh.Issue, error) {
	return f.issues, f.issuesErr
}

func (f *fakeGhClient) CreateIssue(title, _ string) (int, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.nextNum++
	f.createdTitles = append(f.createdTitles, title)
	return f.nextNum, nil
}

func (f *fakeGhClient) AddLabel(_ int, label string) error {
	f.addedLabels = append(f.addedLabels, label)
	return nil
}

func TestGitHubIssueClient_FindDuplicate_MatchesClosedIssue(t *testing.T) {
	client := &fakeGhClient{
		issues: []gh.Issue{
			{Number: 9, Title: TitlePrefix + " nil deref in foo.Bar"},
		},
	}
	adapter := NewGitHubIssueClient(client)

	num, found, err := adapter.FindDuplicate(NormalizeTitle("nil deref in foo.Bar"))
	if err != nil {
		t.Fatalf("FindDuplicate: %v", err)
	}
	if !found || num != 9 {
		t.Errorf("FindDuplicate = (%d, %v), want (9, true)", num, found)
	}
}

func TestGitHubIssueClient_FindDuplicate_IgnoresNonSelfAuditIssues(t *testing.T) {
	client := &fakeGhClient{
		issues: []gh.Issue{
			{Number: 3, Title: "nil deref in foo.Bar"}, // no self-audit prefix
		},
	}
	adapter := NewGitHubIssueClient(client)

	_, found, err := adapter.FindDuplicate(NormalizeTitle("nil deref in foo.Bar"))
	if err != nil {
		t.Fatalf("FindDuplicate: %v", err)
	}
	if found {
		t.Error("expected no match for an issue without the self-audit title prefix")
	}
}

func TestGitHubIssueClient_CreateFindingIssue_AddsLabel(t *testing.T) {
	client := &fakeGhClient{}
	adapter := NewGitHubIssueClient(client)

	num, err := adapter.CreateFindingIssue(TitlePrefix+" x", "body")
	if err != nil {
		t.Fatalf("CreateFindingIssue: %v", err)
	}
	if num != 1 {
		t.Errorf("num = %d, want 1", num)
	}
	if len(client.addedLabels) != 1 || client.addedLabels[0] != Label {
		t.Errorf("addedLabels = %v, want [%q]", client.addedLabels, Label)
	}
}
