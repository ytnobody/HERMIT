package requirements

import "fmt"

// IssueKind identifies why the sweep is opening an issue for a requirement.
type IssueKind string

const (
	// KindImplement: no test exists yet for this requirement.
	KindImplement IssueKind = "implement"
	// KindRegression: a test exists for this requirement but is failing.
	KindRegression IssueKind = "regression"
	// KindReviewTest: the requirement's text changed since the last sweep;
	// its test may now be stale (passing for the wrong reason).
	KindReviewTest IssueKind = "review-test"
)

// marker returns the machine-readable HTML-comment marker embedded in every
// issue the sweep creates. FindOpenIssue implementations should match open
// issues whose body contains this exact marker to dedupe — this is the
// mechanism that makes the sweep idempotent even if HERMIT's own hash-store
// state is lost or reset.
func marker(reqID string, kind IssueKind) string {
	return fmt.Sprintf("<!-- hermit:requirements-sweep req=%s kind=%s -->", reqID, kind)
}

// IssueClient is the subset of GitHub issue operations the reconcile sweep
// needs. It is deliberately narrow so tests can supply an in-memory fake.
type IssueClient interface {
	// FindOpenIssue reports whether an open issue already exists for the
	// given requirement ID and kind (i.e. whether creating a new one would
	// be a duplicate).
	FindOpenIssue(reqID string, kind IssueKind) (bool, error)
	// CreateIssue opens a new issue for the given requirement ID and kind.
	// title and body are the human-facing issue title/body; the
	// implementation is responsible for embedding the dedup marker (see
	// marker) into the created issue's body.
	CreateIssue(reqID string, kind IssueKind, title, body string) error
}

// EnsureIssue creates an issue for (reqID, kind) unless an open one already
// exists, returning whether a new issue was created.
func EnsureIssue(client IssueClient, reqID string, kind IssueKind, title, body string) (bool, error) {
	exists, err := client.FindOpenIssue(reqID, kind)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if err := client.CreateIssue(reqID, kind, title, body); err != nil {
		return false, err
	}
	return true, nil
}
