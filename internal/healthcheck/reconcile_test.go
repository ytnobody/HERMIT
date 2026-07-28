package healthcheck

import (
	"testing"
	"time"
)

// fakeIssueClient is an in-memory IssueClient for testing Reconcile,
// mirroring the style of internal/requirements' test fakes.
type fakeIssueClient struct {
	// openIssues maps check name -> issue number, for issues considered
	// "open" for this test.
	openIssues map[string]int
	// recoveredComments tracks which issue numbers already have the
	// one-time recovered comment.
	recoveredComments map[int]bool

	created          []createdIssue
	postedRecovered  []int
	nextIssueNumber  int
	findOpenIssueErr error
	createIssueErr   error
	hasRecoveredErr  error
	postRecoveredErr error
}

type createdIssue struct {
	name, command, output string
	detectedAt            time.Time
}

func newFakeIssueClient() *fakeIssueClient {
	return &fakeIssueClient{
		openIssues:        map[string]int{},
		recoveredComments: map[int]bool{},
		nextIssueNumber:   100,
	}
}

func (f *fakeIssueClient) FindOpenIssue(name string) (int, bool, error) {
	if f.findOpenIssueErr != nil {
		return 0, false, f.findOpenIssueErr
	}
	num, ok := f.openIssues[name]
	return num, ok, nil
}

func (f *fakeIssueClient) CreateIncidentIssue(name, command, output string, detectedAt time.Time) (int, error) {
	if f.createIssueErr != nil {
		return 0, f.createIssueErr
	}
	f.nextIssueNumber++
	num := f.nextIssueNumber
	f.openIssues[name] = num
	f.created = append(f.created, createdIssue{name: name, command: command, output: output, detectedAt: detectedAt})
	return num, nil
}

func (f *fakeIssueClient) HasRecoveredComment(number int) (bool, error) {
	if f.hasRecoveredErr != nil {
		return false, f.hasRecoveredErr
	}
	return f.recoveredComments[number], nil
}

func (f *fakeIssueClient) PostRecoveredComment(number int, recoveredAt time.Time) error {
	if f.postRecoveredErr != nil {
		return f.postRecoveredErr
	}
	f.recoveredComments[number] = true
	f.postedRecovered = append(f.postedRecovered, number)
	return nil
}

func TestReconcile_FailingCheck_NoExistingIssue_OpensIssue(t *testing.T) {
	client := newFakeIssueClient()
	checks := []Check{{Name: "api-health", Command: "curl -sf https://example.com/healthz"}}
	results := []Result{{Name: "api-health", Ok: false, Output: "connection refused"}}
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	summary, err := Reconcile(checks, results, client, now)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.IssuesOpened != 1 {
		t.Fatalf("IssuesOpened = %d, want 1", summary.IssuesOpened)
	}
	if len(client.created) != 1 {
		t.Fatalf("created issues = %d, want 1", len(client.created))
	}
	got := client.created[0]
	if got.name != "api-health" || got.command != checks[0].Command || got.output != "connection refused" || !got.detectedAt.Equal(now) {
		t.Errorf("created issue = %+v, want matching name/command/output/detectedAt", got)
	}
}

func TestReconcile_FailingCheck_ExistingOpenIssue_NoDuplicate(t *testing.T) {
	client := newFakeIssueClient()
	client.openIssues["api-health"] = 42
	checks := []Check{{Name: "api-health", Command: "curl -sf https://example.com/healthz"}}
	results := []Result{{Name: "api-health", Ok: false, Output: "still down"}}

	summary, err := Reconcile(checks, results, client, time.Now())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.IssuesOpened != 0 {
		t.Errorf("IssuesOpened = %d, want 0 (should dedupe against existing open issue)", summary.IssuesOpened)
	}
	if len(client.created) != 0 {
		t.Errorf("created issues = %d, want 0", len(client.created))
	}
	if len(client.postedRecovered) != 0 {
		t.Errorf("postedRecovered = %v, want none (check is still failing)", client.postedRecovered)
	}
}

func TestReconcile_RecoveredCheck_PostsOneTimeComment(t *testing.T) {
	client := newFakeIssueClient()
	client.openIssues["api-health"] = 42
	checks := []Check{{Name: "api-health", Command: "curl -sf https://example.com/healthz"}}
	results := []Result{{Name: "api-health", Ok: true, Output: "200 OK"}}
	now := time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)

	summary, err := Reconcile(checks, results, client, now)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.RecoveredComments != 1 {
		t.Fatalf("RecoveredComments = %d, want 1", summary.RecoveredComments)
	}
	if len(client.postedRecovered) != 1 || client.postedRecovered[0] != 42 {
		t.Errorf("postedRecovered = %v, want [42]", client.postedRecovered)
	}
	// The issue itself must never be closed by Reconcile — fakeIssueClient
	// has no Close method at all, so a compile-time guarantee already holds;
	// this assertion documents the intent for a human reader.
	if _, stillOpen := client.openIssues["api-health"]; !stillOpen {
		t.Errorf("issue should remain open (no auto-close), openIssues = %v", client.openIssues)
	}
}

func TestReconcile_RecoveredCheck_AlreadyCommented_DoesNotDuplicate(t *testing.T) {
	client := newFakeIssueClient()
	client.openIssues["api-health"] = 42
	client.recoveredComments[42] = true
	checks := []Check{{Name: "api-health", Command: "curl -sf https://example.com/healthz"}}
	results := []Result{{Name: "api-health", Ok: true, Output: "200 OK"}}

	summary, err := Reconcile(checks, results, client, time.Now())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.RecoveredComments != 0 {
		t.Errorf("RecoveredComments = %d, want 0 (already posted)", summary.RecoveredComments)
	}
	if len(client.postedRecovered) != 0 {
		t.Errorf("postedRecovered = %v, want none", client.postedRecovered)
	}
}

func TestReconcile_PassingCheck_NoExistingIssue_NoOp(t *testing.T) {
	client := newFakeIssueClient()
	checks := []Check{{Name: "api-health", Command: "curl -sf https://example.com/healthz"}}
	results := []Result{{Name: "api-health", Ok: true, Output: "200 OK"}}

	summary, err := Reconcile(checks, results, client, time.Now())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.IssuesOpened != 0 || summary.RecoveredComments != 0 {
		t.Errorf("summary = %+v, want no-op", summary)
	}
}

func TestReconcile_MultipleChecks_Independent(t *testing.T) {
	client := newFakeIssueClient()
	client.openIssues["recovering-check"] = 7
	checks := []Check{
		{Name: "failing-check", Command: "false"},
		{Name: "recovering-check", Command: "true"},
		{Name: "passing-check", Command: "true"},
	}
	results := []Result{
		{Name: "failing-check", Ok: false, Output: "boom"},
		{Name: "recovering-check", Ok: true, Output: "ok now"},
		{Name: "passing-check", Ok: true, Output: "ok"},
	}

	summary, err := Reconcile(checks, results, client, time.Now())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.IssuesOpened != 1 {
		t.Errorf("IssuesOpened = %d, want 1", summary.IssuesOpened)
	}
	if summary.RecoveredComments != 1 {
		t.Errorf("RecoveredComments = %d, want 1", summary.RecoveredComments)
	}
}
