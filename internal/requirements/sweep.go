package requirements

import "fmt"

// Status is the reconcile outcome for a single requirement after one sweep.
type Status string

const (
	// Satisfied: a test exists for the requirement and it passes. No action
	// is taken (this is what makes the sweep idempotent).
	Satisfied Status = "satisfied"
	// Unimplemented: no test exists yet for the requirement.
	Unimplemented Status = "unimplemented"
	// Regressed: a test exists but is currently failing.
	Regressed Status = "regressed"
	// Skipped: the requirement is marked verify:manual and is not judged by
	// the sweep at all.
	Skipped Status = "skipped"
)

// ReqResult is the outcome of reconciling a single requirement.
type ReqResult struct {
	ReqID  string
	Title  string
	Status Status
	// IssueCreated is true when this sweep opened a new GitHub issue for the
	// requirement (implement/regression/review-test). It is false both when
	// no issue was needed (Satisfied/Skipped) and when an issue was already
	// needed but one already existed (dedup).
	IssueCreated bool
	// IssueKind is set alongside IssueCreated (or when an existing issue was
	// found and dedup'd) to say what kind of issue was involved.
	IssueKind IssueKind
	// HashChanged is true when the requirement's text differs from the last
	// sweep that recorded a hash for it.
	HashChanged bool
	// TestOutput is the raw combined output of the test command, useful for
	// debugging/logging.
	TestOutput string
}

// SweepOptions configures a single reconcile sweep run.
type SweepOptions struct {
	Runner Runner
	Issues IssueClient
	Hashes HashStore
}

// Sweep reconciles every requirement in reqs against its test, opening
// GitHub issues (deduped against already-open ones) for requirements that
// are unimplemented, regressed, or whose text changed since the last sweep.
// verify:manual requirements are skipped entirely.
//
// Sweep is safe to call repeatedly against an unchanged requirements doc and
// unchanged test results: it will not create duplicate issues.
func Sweep(reqs []Requirement, opts SweepOptions) ([]ReqResult, error) {
	prevHashes, err := opts.Hashes.Load()
	if err != nil {
		return nil, fmt.Errorf("loading requirement hashes: %w", err)
	}
	if prevHashes == nil {
		prevHashes = map[string]string{}
	}

	newHashes := make(map[string]string, len(prevHashes))
	// Preserve hashes for requirements not present in this sweep (e.g. a doc
	// that only contains a subset), so a partial sweep doesn't erase memory.
	for k, v := range prevHashes {
		newHashes[k] = v
	}

	var results []ReqResult
	for _, req := range reqs {
		if req.Verify == VerifyManual {
			results = append(results, ReqResult{ReqID: req.ID, Title: req.Title, Status: Skipped})
			continue
		}

		oldHash, hadHash := prevHashes[req.ID]
		hashChanged := hadHash && oldHash != req.Hash
		newHashes[req.ID] = req.Hash

		result := ReqResult{ReqID: req.ID, Title: req.Title, HashChanged: hashChanged}

		if hashChanged {
			created, err := EnsureIssue(opts.Issues, req.ID, KindReviewTest,
				fmt.Sprintf("%s: requirement text changed — review its test", req.ID),
				reviewTestBody(req))
			if err != nil {
				return nil, fmt.Errorf("ensuring review-test issue for %s: %w", req.ID, err)
			}
			if created {
				result.IssueCreated = true
				result.IssueKind = KindReviewTest
			}
		}

		status, output, runErr := opts.Runner.Run(req.ID)
		result.TestOutput = output
		if runErr != nil {
			return nil, fmt.Errorf("running test for %s: %w", req.ID, runErr)
		}

		switch status {
		case TestPassed:
			result.Status = Satisfied
		case TestNotFound:
			result.Status = Unimplemented
			created, err := EnsureIssue(opts.Issues, req.ID, KindImplement,
				fmt.Sprintf("%s: implement %s", req.ID, req.Title),
				implementBody(req))
			if err != nil {
				return nil, fmt.Errorf("ensuring implement issue for %s: %w", req.ID, err)
			}
			if created {
				result.IssueCreated = true
				result.IssueKind = KindImplement
			}
		case TestFailed:
			result.Status = Regressed
			created, err := EnsureIssue(opts.Issues, req.ID, KindRegression,
				fmt.Sprintf("%s: regression — %s is broken", req.ID, req.Title),
				regressionBody(req))
			if err != nil {
				return nil, fmt.Errorf("ensuring regression issue for %s: %w", req.ID, err)
			}
			if created {
				result.IssueCreated = true
				result.IssueKind = KindRegression
			}
		}

		results = append(results, result)
	}

	if err := opts.Hashes.Save(newHashes); err != nil {
		return nil, fmt.Errorf("saving requirement hashes: %w", err)
	}

	return results, nil
}

func implementBody(req Requirement) string {
	return fmt.Sprintf(
		"%s に対応するテストが見つかりませんでした。要件を実装し、対応するテストを追加してください。\n\n"+
			"## 受け入れ条件\n%s\n\n%s",
		req.ID, req.AcceptanceCriteria, marker(req.ID, KindImplement),
	)
}

func regressionBody(req Requirement) string {
	return fmt.Sprintf(
		"%s に対応するテストが失敗しています。リグレッションが発生しています。\n\n"+
			"## 受け入れ条件\n%s\n\n%s",
		req.ID, req.AcceptanceCriteria, marker(req.ID, KindRegression),
	)
}

func reviewTestBody(req Requirement) string {
	return fmt.Sprintf(
		"%s の要件テキストが変更されました。対応するテストが依然として要件を正しく検証しているか見直してください。\n\n"+
			"## 現在の受け入れ条件\n%s\n\n%s",
		req.ID, req.AcceptanceCriteria, marker(req.ID, KindReviewTest),
	)
}
