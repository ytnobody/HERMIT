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
	r := Requirement{ID: id, Title: title, AcceptanceCriteria: "criteria for " + id, Verify: VerifyTest, Body: id + " body v1"}
	r.Hash = specHash(r)
	return r
}

func reqManual(id, title string) Requirement {
	r := Requirement{ID: id, Title: title, AcceptanceCriteria: "criteria for " + id, Verify: VerifyManual, Body: id + " manual body"}
	r.Hash = specHash(r)
	return r
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
	// Seed the hash store with a stale hash (under the *current* scheme
	// version, so this simulates a genuine spec change since the last
	// sweep rather than a scheme migration).
	if err := hashes.Save(HashSchemeVersion, map[string]string{"REQ-040": "some-stale-hash-value"}); err != nil {
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

// TestSweep_ImplementationStatusOnlyChange_DoesNotFireReviewTest is the
// end-to-end regression test for Issue #182's self-reinforcing loop:
// resolving a review-test issue by recording findings in the requirement's
// "- 実装状況:" field must not, by itself, cause the next sweep to fire
// review-test again.
func TestSweep_ImplementationStatusOnlyChange_DoesNotFireReviewTest(t *testing.T) {
	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-180": TestPassed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	before := reqTest("REQ-180", "自己増殖しない")

	// First sweep: establish a baseline hash (no prior hash yet, so no
	// review-test fires — this mirrors TestSweep_FirstRunEver...).
	if _, err := Sweep([]Requirement{before}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes}); err != nil {
		t.Fatalf("Sweep() (baseline run) error = %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("baseline run should not create any issue, got %+v", issues.created)
	}

	// Simulate an engineer resolving a (hypothetical) review-test issue by
	// writing a finding into 実装状況 — same AcceptanceCriteria and Verify,
	// only the progress-note field differs, so Body differs but Hash must
	// not.
	after := before
	after.Body = before.Body + "\n- 実装状況: #182 の調査により、テストは要件を満たしていることを確認済み。"
	after.Hash = specHash(after)
	if after.Hash != before.Hash {
		t.Fatalf("precondition failed: specHash must be unaffected by Body/実装状況 change")
	}

	results, err := Sweep([]Requirement{after}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() (post-resolution run) error = %v", err)
	}
	if results[0].HashChanged {
		t.Errorf("HashChanged = true after only 実装状況 changed — this is the #182 self-reinforcing loop")
	}
	if results[0].IssueCreated {
		t.Errorf("a review-test issue was re-created after only 実装状況 changed: %+v", results[0])
	}
	if len(issues.created) != 0 {
		t.Errorf("expected no issues created across both sweeps, got %+v", issues.created)
	}
}

// TestSweep_HashSchemeMigration_DoesNotFireReviewTest_JustRecomputesAndSaves
// covers the migration requirement: when the stored hashes were computed
// under an older/unknown HashSchemeVersion (e.g. a store from before Issue
// #182, or bumped again in the future), the first sweep afterward must not
// treat every requirement as "changed" (which would fire review-test for
// the entire document at once) — it should just silently recompute and
// persist hashes under the current scheme.
func TestSweep_HashSchemeMigration_DoesNotFireReviewTest_JustRecomputesAndSaves(t *testing.T) {
	req := reqTest("REQ-190", "migration safe")
	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-190": TestPassed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	// Seed the store as if it were written by an older hash scheme: some
	// unrelated hash value, saved under version 1 (not HashSchemeVersion).
	if err := hashes.Save(1, map[string]string{"REQ-190": "old-scheme-hash-unrelated-to-spec"}); err != nil {
		t.Fatalf("seeding hash store: %v", err)
	}

	results, err := Sweep([]Requirement{req}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() error = %v", err)
	}
	if results[0].HashChanged {
		t.Errorf("HashChanged = true on the first sweep after a hash-scheme version change; want false (migration, not a real change)")
	}
	if results[0].IssueCreated {
		t.Errorf("review-test issue created on scheme-migration sweep: %+v", results[0])
	}
	if len(issues.created) != 0 {
		t.Errorf("expected no issues created on scheme-migration sweep, got %+v", issues.created)
	}

	version, saved, err := hashes.Load()
	if err != nil {
		t.Fatalf("Load() after migration sweep: %v", err)
	}
	if version != HashSchemeVersion {
		t.Errorf("stored version = %d after migration sweep, want %d", version, HashSchemeVersion)
	}
	if saved["REQ-190"] != req.Hash {
		t.Errorf("stored hash = %q, want recomputed current-scheme hash %q", saved["REQ-190"], req.Hash)
	}

	// A subsequent sweep with the identical requirement must now be a true
	// no-op (no HashChanged), since the store has caught up to the current
	// scheme.
	results2, err := Sweep([]Requirement{req}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep() (2nd run) error = %v", err)
	}
	if results2[0].HashChanged {
		t.Errorf("2nd run after migration: expected HashChanged = false")
	}
}

// TestREQ016_ReviewTestHashIgnoresImplementationStatus is the REQ-016
// acceptance test (see REQUIREMENTS.md), matching the test_command naming
// convention (test_command runs "^TestREQ016" to judge REQ-016). It
// exercises every clause of REQ-016's 受け入れ条件 end-to-end via Sweep:
// resolving a review-test issue (実装状況 edit only) must not re-fire
// review-test, while a genuine 受け入れ条件 or verify-mode change must.
func TestREQ016_ReviewTestHashIgnoresImplementationStatus(t *testing.T) {
	docV1 := `## REQ-300: 自己増殖しない
- 受け入れ条件: 元の条件文
- verify: test
`
	// "実装状況" is added, spec fields (受け入れ条件/verify) untouched: this
	// simulates resolving a review-test issue.
	docV1PlusStatus := `## REQ-300: 自己増殖しない
- 受け入れ条件: 元の条件文
- verify: test
- 実装状況: 調査済み。テストは要件を正しく検証している。
`
	// A genuine spec change: 受け入れ条件 text itself changes.
	docV2 := `## REQ-300: 自己増殖しない
- 受け入れ条件: 変更された条件文
- verify: test
- 実装状況: 調査済み。テストは要件を正しく検証している。
`
	// A genuine spec change: verify mode flips to manual.
	docV3 := `## REQ-300: 自己増殖しない
- 受け入れ条件: 変更された条件文
- verify: manual
- 実装状況: 調査済み。テストは要件を正しく検証している。
`

	runner := &fakeRunner{statuses: map[string]TestStatus{"REQ-300": TestPassed}}
	issues := &fakeIssueClient{}
	hashes := NewMemHashStore()

	mustParse := func(doc string) Requirement {
		reqs, err := Parse(doc)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if len(reqs) != 1 {
			t.Fatalf("expected 1 requirement, got %d", len(reqs))
		}
		return reqs[0]
	}

	// Sweep 1: baseline, no prior hash.
	if _, err := Sweep([]Requirement{mustParse(docV1)}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes}); err != nil {
		t.Fatalf("Sweep 1 error = %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("Sweep 1: expected no issues, got %+v", issues.created)
	}

	// Sweep 2: only 実装状況 added -> must NOT fire review-test.
	res2, err := Sweep([]Requirement{mustParse(docV1PlusStatus)}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep 2 error = %v", err)
	}
	if res2[0].HashChanged || res2[0].IssueCreated {
		t.Errorf("Sweep 2 (実装状況 only): HashChanged=%v IssueCreated=%v, want both false", res2[0].HashChanged, res2[0].IssueCreated)
	}

	// Sweep 3: 受け入れ条件 changes -> must fire review-test.
	res3, err := Sweep([]Requirement{mustParse(docV2)}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep 3 error = %v", err)
	}
	if !res3[0].HashChanged || !res3[0].IssueCreated || res3[0].IssueKind != KindReviewTest {
		t.Errorf("Sweep 3 (受け入れ条件 changed): got %+v, want HashChanged/IssueCreated=true, IssueKind=%q", res3[0], KindReviewTest)
	}

	// Sweep 4: verify flips test -> manual -> must fire review-test again.
	res4, err := Sweep([]Requirement{mustParse(docV3)}, SweepOptions{Runner: runner, Issues: issues, Hashes: hashes})
	if err != nil {
		t.Fatalf("Sweep 4 error = %v", err)
	}
	if !res4[0].HashChanged {
		t.Errorf("Sweep 4 (verify test->manual): HashChanged = false, want true")
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
