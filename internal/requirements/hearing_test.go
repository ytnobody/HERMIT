package requirements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/ytnobody/hermit/internal/github"
)

func TestDocExists_Found(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "REQUIREMENTS.md"), []byte("# Requirements"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !DocExists(dir, DefaultHearingPaths) {
		t.Errorf("expected DocExists to find REQUIREMENTS.md")
	}
}

func TestDocExists_FoundAtSecondCandidate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "requirements.md"), []byte("# Requirements"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !DocExists(dir, DefaultHearingPaths) {
		t.Errorf("expected DocExists to find docs/requirements.md")
	}
}

func TestDocExists_NotFound(t *testing.T) {
	dir := t.TempDir()
	if DocExists(dir, DefaultHearingPaths) {
		t.Errorf("expected DocExists to report false in an empty project")
	}
}

func TestDocExists_EmptyPaths(t *testing.T) {
	dir := t.TempDir()
	if DocExists(dir, nil) {
		t.Errorf("expected DocExists to report false with no candidate paths")
	}
}

func TestDocExists_IgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	// A directory named REQUIREMENTS.md should not count as the document.
	if err := os.MkdirAll(filepath.Join(dir, "REQUIREMENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if DocExists(dir, DefaultHearingPaths) {
		t.Errorf("expected DocExists to ignore a directory named REQUIREMENTS.md")
	}
}

// fakeHearingClient is an in-memory HearingIssueClient for tests.
type fakeHearingClient struct {
	issues    []gh.Issue
	nextNum   int
	listErr   error
	createErr error
	labelErr  error
}

func (f *fakeHearingClient) ListOpenIssues(label string) ([]gh.Issue, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if label == "" {
		return f.issues, nil
	}
	var out []gh.Issue
	for _, i := range f.issues {
		for _, l := range i.Labels {
			if l == label {
				out = append(out, i)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeHearingClient) CreateIssue(title, body string) (int, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.nextNum++
	f.issues = append(f.issues, gh.Issue{Number: f.nextNum, Title: title, Body: body})
	return f.nextNum, nil
}

func (f *fakeHearingClient) AddLabel(number int, label string) error {
	if f.labelErr != nil {
		return f.labelErr
	}
	for i := range f.issues {
		if f.issues[i].Number == number {
			f.issues[i].Labels = append(f.issues[i].Labels, label)
		}
	}
	return nil
}

func TestEnsureHearingIssue_CreatesWhenNoneOpen(t *testing.T) {
	client := &fakeHearingClient{}
	created, err := EnsureHearingIssue(client)
	if err != nil {
		t.Fatalf("EnsureHearingIssue() error = %v", err)
	}
	if !created {
		t.Fatalf("expected a hearing issue to be created")
	}
	if len(client.issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %d", len(client.issues))
	}
	got := client.issues[0]
	if len(got.Labels) != 1 || got.Labels[0] != HearingLabel {
		t.Errorf("expected the created issue to carry the %q label, got %v", HearingLabel, got.Labels)
	}
	for _, want := range []string{"目的", "スコープ", "受け入れ条件", "やらないこと"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("expected hearing issue body to mention %q", want)
		}
	}
}

func TestEnsureHearingIssue_IdempotentWhenAlreadyOpen(t *testing.T) {
	client := &fakeHearingClient{
		issues: []gh.Issue{
			{Number: 1, Title: "existing hearing", Labels: []string{HearingLabel}},
		},
		nextNum: 1,
	}
	created, err := EnsureHearingIssue(client)
	if err != nil {
		t.Fatalf("EnsureHearingIssue() error = %v", err)
	}
	if created {
		t.Fatalf("expected no new issue to be created when one is already open")
	}
	if len(client.issues) != 1 {
		t.Fatalf("expected still exactly 1 issue, got %d", len(client.issues))
	}
}

func TestEnsureHearingIssue_RepeatedCallsDoNotDuplicate(t *testing.T) {
	client := &fakeHearingClient{}
	for i := 0; i < 3; i++ {
		if _, err := EnsureHearingIssue(client); err != nil {
			t.Fatalf("EnsureHearingIssue() call %d error = %v", i, err)
		}
	}
	if len(client.issues) != 1 {
		t.Fatalf("expected exactly 1 issue after repeated calls (idempotency), got %d", len(client.issues))
	}
}

func TestEnsureHearingIssue_PropagatesListError(t *testing.T) {
	client := &fakeHearingClient{listErr: errFake}
	if _, err := EnsureHearingIssue(client); err == nil {
		t.Fatalf("expected error to propagate from ListOpenIssues")
	}
}

func TestEnsureHearingIssue_PropagatesCreateError(t *testing.T) {
	client := &fakeHearingClient{createErr: errFake}
	if _, err := EnsureHearingIssue(client); err == nil {
		t.Fatalf("expected error to propagate from CreateIssue")
	}
}

func TestEnsureHearingIssue_PropagatesLabelError(t *testing.T) {
	client := &fakeHearingClient{labelErr: errFake}
	if _, err := EnsureHearingIssue(client); err == nil {
		t.Fatalf("expected error to propagate from AddLabel")
	}
}
