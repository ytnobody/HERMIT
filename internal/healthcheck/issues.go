package healthcheck

import (
	"fmt"
	"time"
)

// IncidentLabel is the GitHub label applied to every issue this package
// opens for a failing health check.
const IncidentLabel = "production-incident"

// TitlePrefix returns the "[health-check: <name>]" title-prefix convention
// used to identify/dedupe the GitHub issue for a given check name. Per
// Issue #190, dedup is judged by scanning open issue titles for this exact
// prefix (not a hidden marker comment, unlike internal/requirements' sweep)
// so a human skimming the issue list can immediately tell which check an
// incident issue is about.
func TitlePrefix(name string) string {
	return fmt.Sprintf("[health-check: %s]", name)
}

// RecoveredTrigger is the substring HasRecoveredComment implementations
// look for in an issue's comments to decide whether the one-time recovery
// comment has already been posted (see PostRecoveredComment).
const RecoveredTrigger = "recovered at"

// IssueClient is the subset of GitHub issue operations the health-check
// reconcile needs. It is deliberately narrow so tests can supply an
// in-memory fake, mirroring internal/requirements.IssueClient.
type IssueClient interface {
	// FindOpenIssue reports whether an open issue already exists for the
	// given check name (matched via TitlePrefix), and its issue number if
	// so.
	FindOpenIssue(name string) (number int, found bool, err error)
	// CreateIncidentIssue opens a new production-incident-labeled issue for
	// a failing check, embedding the check name, command, failure output,
	// and detection time into the issue body.
	CreateIncidentIssue(name, command, output string, detectedAt time.Time) (number int, err error)
	// HasRecoveredComment reports whether the given issue already has a
	// one-time "recovered at ..." comment posted on it.
	HasRecoveredComment(number int) (bool, error)
	// PostRecoveredComment posts the one-time "recovered at <time>" comment
	// on the given issue. The issue is never closed by this package (Issue
	// #190 explicitly excludes auto-close).
	PostRecoveredComment(number int, recoveredAt time.Time) error
}
