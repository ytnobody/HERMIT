package requirements

import (
	"errors"
	"testing"
)

// --- fakes ---------------------------------------------------------------

type fakeRunner struct {
	statuses map[string]TestStatus
	calls    []string
}

func (f *fakeRunner) Run(reqID string) (TestStatus, string, error) {
	f.calls = append(f.calls, reqID)
	st, ok := f.statuses[reqID]
	if !ok {
		st = TestNotFound
	}
	return st, "output for " + reqID, nil
}

type issueRecord struct {
	ReqID string
	Kind  IssueKind
	Title string
	Body  string
}

type fakeIssueClient struct {
	open      map[string]bool
	created   []issueRecord
	findErr   error
	createErr error
}

func issueKey(reqID string, kind IssueKind) string { return reqID + "|" + string(kind) }

func (f *fakeIssueClient) FindOpenIssue(reqID string, kind IssueKind) (bool, error) {
	if f.findErr != nil {
		return false, f.findErr
	}
	return f.open[issueKey(reqID, kind)], nil
}

func (f *fakeIssueClient) CreateIssue(reqID string, kind IssueKind, title, body string) error {
	if f.createErr != nil {
		return f.createErr
	}
	if f.open == nil {
		f.open = map[string]bool{}
	}
	f.open[issueKey(reqID, kind)] = true
	f.created = append(f.created, issueRecord{ReqID: reqID, Kind: kind, Title: title, Body: body})
	return nil
}

func reqTest(id, title string) Requirement {
	return Requirement{ID: id, Title: title, AcceptanceCriteria: "criteria for " + id, Verify: VerifyTest, Body: id + " body v1", Hash: hashText(id + " body v1")}
}

func reqManual(id, title string) Requirement {
	return Requirement{ID: id, Title: title, AcceptanceCriteria: "criteria for " + id, Verify: VerifyManual, Body: id + " manual body", Hash: hashText(id + " manual body")}
}

// --- tests -----------------------------------------------------------------

func TestSweep_AllSatisfied_NoIssuesCreated_Idempotent(t *testing.T) {
	reqs := []Requirement{reqTest("REQ-001", "foo"), reqTest("REQ-002", "bar")}
	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-001": TestPassed, "REQ-002": TestPassed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	for i := 0; i < 2; i++ {
		results, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
		if err != nil {
			t.Fatalf("run %d: Sweep() error = %v", i, err)
		}
		for _, r := range results {
			if r.Status != Satisfied {
				t.Errorf("run %d: %s status = %q, want %q", i, r.ReqID, r.Status, Satisfied)
			}
			if r.IssueCreated {
				t.Errorf("run %d: %s unexpectedly created an issue", i, r.ReqID)
			}
		}
		if len(issues.created) != 0 {
			t.Errorf("run %d: expected no issues created, got %d: %+v", i, len(issues.created), issues.created)
		}
	}
}

func TestSweep_Unimplemented_CreatesExactlyOneIssue_NoDuplicateOnRerun(t *testing.T) {
	reqs := []Requirement{reqTest("REQ-010", "not built yet")}
	runner := &fakeRunner{statuses: map[string]TestStatus{}} // no entry => NotFound
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	results, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if len(results) != 1 || results[0].Status != Unimplemented {
		t.Fatalf("results = %+v, want single Unimplemented result", results)
	}
	if !results[0].IssueCreated || results[0].IssueKind != KindImplement {
		t.Errorf("expected an implement issue to be created, got %+v", results[0])
	}
	if len(issues.created) != 1 {
		t.Fatalf("expected exactly 1 issue created, got %d", len(issues.created))
	}

	// Re-run: same unimplemented state, but an open issue already exists.
	results2, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() (2nd run) error = %v", err)
	}
	if results2[0].Status != Unimplemented {
		t.Errorf("2nd run status = %q, want %q", results2[0].Status, Unimplemented)
	}
	if results2[0].IssueCreated {
		t.Errorf("2nd run should not create a duplicate issue")
	}
	if len(issues.created) != 1 {
		t.Errorf("expected still exactly 1 issue after re-run, got %d", len(issues.created))
	}
}

func TestSweep_Regressed_CreatesRegressionIssue_Deduped(t *testing.T) {
	reqs := []Requirement{reqTest("REQ-020", "broken")}
	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-020": TestFailed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	results, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if results[0].Status != Regressed {
		t.Errorf("status = %q, want %q", results[0].Status, Regressed)
	}
	if len(issues.created) != 1 || issues.created[0].Kind != KindRegression {
		t.Fatalf("expected 1 regression issue, got %+v", issues.created)
	}

	// Re-run: still failing, must not duplicate.
	if _, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes}); err != nil {
		t.Fatalf("Sweep() (2nd run) error = %v", err)
	}
	if len(issues.created) != 1 {
		t.Errorf("expected still exactly 1 regression issue after re-run, got %d", len(issues.created))
	}
}

func TestSweep_ManualVerify_SkippedEntirely(t *testing.T) {
	reqs := []Requirement{reqManual("REQ-030", "docs only")}
	runner := &fakeRunner{statuses: map[string]TestStatus{}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	results, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if len(results) != 1 || results[0].Status != Skipped {
		t.Fatalf("results = %+v, want single Skipped result", results)
	}
	if len(runner.calls) != 0 {
		t.Errorf("runner should not be invoked for verify:manual requirements, got calls %v", runner.calls)
	}
	if len(issues.created) != 0 {
		t.Errorf("no issues should be created for verify:manual requirements, got %+v", issues.created)
	}
}

func TestSweep_HashChange_CreatesReviewIssue_ThenIdempotent(t *testing.T) {
	req := reqTest("REQ-040", "wording changed")
	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-040": TestPassed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()
	// Seed the hash store with a stale hash to simulate a text change since
	// the last sweep.
	if err := hashes.Save(map[string]string{"REQ-040": "some-stale-hash-value"}); err != nil {
		t.Fatalf("seeding hash store: %v", err)
	}

	results, err := Sweep([]Requirement{req}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if !results[0].HashChanged {
		t.Errorf("expected HashChanged = true")
	}
	if !results[0].IssueCreated || results[0].IssueKind != KindReviewTest {
		t.Errorf("expected a review-test issue to be created, got %+v", results[0])
	}
	if results[0].Status != Satisfied {
		t.Errorf("status = %q, want %q (test still passes)", results[0].Status, Satisfied)
	}

	// Re-run with the same (now-current) requirement text: no further change.
	results2, err := Sweep([]Requirement{req}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() (2nd run) error = %v", err)
	}
	if results2[0].HashChanged {
		t.Errorf("2nd run: expected HashChanged = false")
	}
	if results2[0].IssueCreated {
		t.Errorf("2nd run: should not create another review-test issue")
	}
}

func TestSweep_FirstRunEver_NoStaleHash_DoesNotFireReviewIssue(t *testing.T) {
	// On the very first sweep ever (empty hash store), there's no prior hash
	// to compare against, so this must NOT be treated as a "text changed"
	// event.
	req := reqTest("REQ-050", "brand new")
	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-050": TestPassed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	results, err := Sweep([]Requirement{req}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if results[0].HashChanged {
		t.Errorf("first-ever sweep should not report HashChanged")
	}
	if results[0].IssueCreated {
		t.Errorf("first-ever sweep should not create a review-test issue")
	}
}

func TestSweep_PreExistingOpenIssue_PreventsCreation(t *testing.T) {
	// Even without any local sweep state, if an open issue already exists on
	// GitHub for this REQ-ID/kind (e.g. discovered independently), Sweep
	// must not create a duplicate.
	reqs := []Requirement{reqTest("REQ-060", "already tracked")}
	runner := &fakeRunner{statuses: map[string]TestStatus{}} // NotFound
	issues := &fakeIssueClient{open: map[string]bool{issueKey("REQ-060", KindImplement): true}}
	hashes := NewMemHashStore()

	results, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if results[0].IssueCreated {
		t.Errorf("should not create an issue when one is already open")
	}
	if len(issues.created) != 0 {
		t.Errorf("expected no new issues, got %+v", issues.created)
	}
}

func TestSweep_PropagatesIssueClientError(t *testing.T) {
	reqs := []Requirement{reqTest("REQ-070", "boom")}
	runner := &fakeRunner{statuses: map[string]TestStatus{}}
	issues := &fakeIssueClient{findErr: errors.New("github unavailable")}
	hashes := NewMemHashStore()

	_, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err == nil {
		t.Fatalf("expected error to propagate from IssueClient.FindOpenIssue")
	}
}

func TestSweep_MixedRequirements(t *testing.T) {
	reqs := []Requirement{
		reqTest("REQ-100", "ok"),
		reqTest("REQ-101", "missing"),
		reqTest("REQ-102", "broken"),
		reqManual("REQ-103", "manual"),
	}
	runner := &fakeRunner{statuses: map[string]TestStatus{
		"REQ-100": TestPassed,
		"REQ-102": TestFailed,
		// REQ-101 intentionally absent -> NotFound
	}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	results, err := Sweep(reqs, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}

	byID := map[string]ReqResult{}
	for _, r := range results {
		byID[r.ReqID] = r
	}
	if byID["REQ-100"].Status != Satisfied {
		t.Errorf("REQ-100 = %q, want Satisfied", byID["REQ-100"].Status)
	}
	if byID["REQ-101"].Status != Unimplemented {
		t.Errorf("REQ-101 = %q, want Unimplemented", byID["REQ-101"].Status)
	}
	if byID["REQ-102"].Status != Regressed {
		t.Errorf("REQ-102 = %q, want Regressed", byID["REQ-102"].Status)
	}
	if byID["REQ-103"].Status != Skipped {
		t.Errorf("REQ-103 = %q, want Skipped", byID["REQ-103"].Status)
	}
	if len(issues.created) != 2 {
		t.Errorf("expected 2 issues created (implement + regression), got %d: %+v", len(issues.created), issues.created)
	}
}
