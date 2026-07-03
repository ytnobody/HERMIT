package requirements

import (
	"fmt"
	"os"
	"path/filepath"
)

// Summary is the structured, aggregated result of a single reconcile-sweep
// run, produced by RunReconcileSweep. It is the shared return type used by
// both `hermit serve`'s startup sweep (cmd/hermit/main.go) and the
// run_requirements_sweep MCP tool (internal/mcp/tools.go, Issue #128) so the
// two call sites never diverge in what "a sweep ran" means.
type Summary struct {
	// Skipped is true when the sweep did not run at all — no requirements
	// document was found, no test_command was configured, or the document
	// contained no "## REQ-xxx:" blocks. SkipReason explains why. This is
	// never an error: an unconfigured or not-yet-adopted requirements
	// workflow is a normal, expected state for a project.
	Skipped    bool
	SkipReason string
	// Quiet is set alongside Skipped for skip reasons that are expected to
	// be common and unremarkable (no doc present yet, or a doc with no
	// REQ-ID blocks yet) — callers that log skip reasons (e.g. the
	// `hermit serve` startup path) can use this to avoid logging noise for
	// every project that simply hasn't adopted the requirements-doc
	// workflow, while still surfacing more actionable skips (like a missing
	// test_command) and always surfacing the reason via the MCP tool.
	Quiet bool

	// Satisfied/Unimplemented/Regressed/SkippedManual are per-status counts
	// across all requirements reconciled by this sweep (see Status).
	Satisfied     int
	Unimplemented int
	Regressed     int
	SkippedManual int
	// IssuesOpened is the number of new GitHub issues this sweep created
	// (implement/regression/review-test), i.e. the count of Results whose
	// IssueCreated is true.
	IssuesOpened int

	// Results holds the full per-requirement detail underlying the counts
	// above, for callers that want more than the summary (e.g. logging
	// which specific requirement had an issue opened for it).
	Results []ReqResult
}

// RunReconcileSweep runs the requirements reconcile sweep (Issue #106)
// against the requirements document at docPath (relative to rootDir), using
// testCommand as the [requirements].test_command template, and issues as the
// GitHub issue client used to open (deduped) issues for unimplemented,
// regressed, or text-changed requirements.
//
// docPath and testCommand are taken as already-resolved by the caller (e.g.
// cmd/hermit/main.go applies the "REQUIREMENTS.md" default when
// [requirements].doc is unset in harness.toml) — RunReconcileSweep itself
// has no opinion on defaults, only on what to do once it has concrete
// values.
//
// This is the shared core used by both `hermit serve`'s startup sweep and
// the run_requirements_sweep MCP tool (Issue #128): it never treats "nothing
// to do" (missing doc, missing test_command, empty doc) as an error — those
// are reported via Summary.Skipped/SkipReason so callers can render a clear
// "skipped, reason: ..." result instead of surfacing a spurious failure.
func RunReconcileSweep(rootDir, docPath, testCommand string, issues IssueClient) (Summary, error) {
	fullPath := filepath.Join(rootDir, docPath)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Summary{
				Skipped:    true,
				Quiet:      true,
				SkipReason: fmt.Sprintf("no requirements document found at %s", fullPath),
			}, nil
		}
		return Summary{}, fmt.Errorf("reading %s: %w", fullPath, err)
	}

	if testCommand == "" {
		return Summary{
			Skipped:    true,
			SkipReason: fmt.Sprintf("%s found but [requirements].test_command is not set in harness.toml", fullPath),
		}, nil
	}

	reqs, err := Parse(string(data))
	if err != nil {
		return Summary{}, fmt.Errorf("parsing %s: %w", fullPath, err)
	}
	if len(reqs) == 0 {
		return Summary{
			Skipped:    true,
			Quiet:      true,
			SkipReason: fmt.Sprintf("%s contains no \"## REQ-xxx:\" blocks", fullPath),
		}, nil
	}

	results, err := Sweep(reqs, SweepOptions{
		Runner: CommandRunner{Template: testCommand},
		Issues: issues,
		Hashes: NewFileHashStore(rootDir),
	})
	if err != nil {
		return Summary{}, err
	}

	summary := Summary{Results: results}
	for _, r := range results {
		switch r.Status {
		case Satisfied:
			summary.Satisfied++
		case Unimplemented:
			summary.Unimplemented++
		case Regressed:
			summary.Regressed++
		case Skipped:
			summary.SkippedManual++
		}
		if r.IssueCreated {
			summary.IssuesOpened++
		}
	}
	return summary, nil
}
