package healthcheck

import "time"

// Summary is the aggregated outcome of Reconcile: the raw per-check Results
// (already produced by RunChecks) plus counts of the side effects Reconcile
// performed against GitHub.
type Summary struct {
	// Results is the same slice passed in, returned here so a single
	// Summary value carries everything an MCP tool response needs.
	Results []Result
	// IssuesOpened is the number of new production-incident issues opened
	// this call (one per newly-failing check that had no matching open
	// issue yet).
	IssuesOpened int
	// RecoveredComments is the number of one-time "recovered at ..."
	// comments posted this call.
	RecoveredComments int
}

// Reconcile runs the dedup/issue-filing/recovery-comment logic described in
// Issue #190 against results (already produced by RunChecks for the given
// checks). now is passed in (rather than computed internally) so
// callers/tests control the timestamp recorded in issue bodies/comments.
//
// checks is used only to look up each result's original command (for the
// incident issue body) by name; results not present in checks (which
// should not happen in normal use, since results comes from
// RunChecks(checks)) are treated as having an empty command rather than
// erroring.
//
// For each result:
//   - ok:false with no existing open issue (matched via TitlePrefix) ->
//     a new production-incident issue is opened.
//   - ok:false with an existing open issue -> no-op (already tracked,
//     Issue #190 explicitly excludes ongoing-failure progress comments).
//   - ok:true with an existing open issue that has no "recovered at ..."
//     comment yet -> that one-time comment is posted (the issue is never
//     closed).
//   - ok:true with an existing open issue that already has the recovered
//     comment, or ok:true with no open issue at all -> no-op.
func Reconcile(checks []Check, results []Result, issues IssueClient, now time.Time) (Summary, error) {
	commands := make(map[string]string, len(checks))
	for _, c := range checks {
		commands[c.Name] = c.Command
	}

	summary := Summary{Results: results}
	for _, r := range results {
		num, found, err := issues.FindOpenIssue(r.Name)
		if err != nil {
			return summary, err
		}

		if !r.Ok {
			if found {
				continue
			}
			if _, err := issues.CreateIncidentIssue(r.Name, commands[r.Name], r.Output, now); err != nil {
				return summary, err
			}
			summary.IssuesOpened++
			continue
		}

		// r.Ok == true
		if !found {
			continue
		}
		already, err := issues.HasRecoveredComment(num)
		if err != nil {
			return summary, err
		}
		if already {
			continue
		}
		if err := issues.PostRecoveredComment(num, now); err != nil {
			return summary, err
		}
		summary.RecoveredComments++
	}
	return summary, nil
}
