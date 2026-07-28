// Package selfaudit implements the idle-time self-audit sweep (Issue #164):
// a lightweight, LLM-driven review pass that looks for bugs, missing test
// coverage, and security holes when the Superintendent's Issue queue is
// empty, and files (deduped) GitHub Issues for whatever it finds instead of
// fixing anything itself.
//
// Design split (documented here because the originating Issue left the
// exact Go-vs-LLM boundary as an engineering judgment call):
//
//   - The actual review — reading code, deciding whether something is a
//     real bug/gap/vulnerability worth filing — cannot reasonably be done in
//     Go. Unlike internal/requirements (which runs a concrete test command
//     per requirement) or internal/healthcheck (which runs a concrete health
//     check command), "review the codebase for bugs" has no deterministic
//     command to shell out to. That judgment is delegated to the calling
//     agent (the Superintendent), guided by the Instructions text below.
//   - Everything mechanical around that judgment — cadence tracking (via
//     internal/state, mirroring RequirementsSweepSince), deduping a finding
//     against existing Issues, and actually filing the Issue — is
//     implemented here in Go, so the LLM side only ever produces structured
//     findings and never talks to the GitHub API directly. This keeps the
//     "Superintendent never edits/fixes, only files Issues" boundary
//     enforced in code rather than relying on the LLM to police itself.
//
// This mirrors internal/healthcheck's split (Go does the dedupe/filing
// plumbing; the check command's pass/fail judgment is external) more than
// internal/requirements' (which does the "is it satisfied" judgment in Go
// too, since a test command's exit code is a deterministic verdict) — a code
// review sweep has no equivalent deterministic verdict to compute.
package selfaudit

import (
	"fmt"
	"regexp"
	"strings"
)

// TitlePrefix is the fixed prefix applied to every Issue this package
// files, used both for human-skimmable identification (mirroring
// internal/healthcheck.TitlePrefix's convention) and as the anchor for
// duplicate detection against existing Issues.
const TitlePrefix = "[self-audit]"

// Label is the GitHub label applied to every Issue this package files.
const Label = "self-audit"

// Finding is a single concrete issue surfaced by the calling agent's review
// (bug, missing test coverage, or security hole), to be filed as a GitHub
// Issue if it is not a duplicate of an existing one.
type Finding struct {
	// Title is a short human-readable summary, e.g. "nil pointer dereference
	// in foo.Bar when cfg is empty". Must not already include TitlePrefix —
	// File adds it.
	Title string `json:"title"`
	// Body is the full finding detail: what/where the problem is, why it
	// matters, and (for missing-test-coverage findings) what behavior is
	// untested. Should read like a well-specified Issue body, since it
	// becomes one verbatim (aside from an added provenance footer).
	Body string `json:"body"`
}

// FindingResult records what happened when File processed a single Finding.
type FindingResult struct {
	Title        string `json:"title"`
	IssueCreated bool   `json:"issue_created"`
	IssueNumber  int    `json:"issue_number,omitempty"`
	DuplicateOf  int    `json:"duplicate_of,omitempty"`
	IsDuplicate  bool   `json:"is_duplicate"`
}

// Summary is the aggregated outcome of File, returned to both the
// run_self_audit MCP tool and (indirectly, via the same code path) any
// future caller.
type Summary struct {
	FindingsReceived int             `json:"findings_received"`
	IssuesOpened     int             `json:"issues_opened"`
	Duplicates       int             `json:"duplicates_skipped"`
	Results          []FindingResult `json:"results"`
}

// IssueClient is the subset of GitHub issue operations File needs. It is
// deliberately narrow so tests can supply an in-memory fake, mirroring
// internal/requirements.IssueClient and internal/healthcheck.IssueClient.
type IssueClient interface {
	// FindDuplicate reports whether an Issue (open or closed) already exists
	// for a finding with this normalized title, and its number if so.
	FindDuplicate(normalizedTitle string) (number int, found bool, err error)
	// CreateFindingIssue opens a new self-audit-labeled Issue for a finding.
	CreateFindingIssue(title, body string) (number int, err error)
}

// nonAlnum matches runs of characters that are not letters or digits, used
// by NormalizeTitle to collapse punctuation/whitespace differences before
// comparing titles for duplicate detection.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeTitle reduces a finding (or existing Issue) title to a
// lowercase, punctuation-collapsed form for duplicate comparison, and trims
// TitlePrefix if present. This is intentionally a simple, exact-match-after-
// normalization heuristic rather than fuzzy/semantic matching — proportionate
// to a "lightweight" audit sweep (see package doc), not a from-scratch
// duplicate-detection engine. Two findings whose titles normalize to the
// same string are treated as duplicates; anything less similar than that is
// filed as a new Issue (a human can always close a near-duplicate by hand).
func NormalizeTitle(title string) string {
	t := strings.ToLower(title)
	t = strings.TrimPrefix(t, strings.ToLower(TitlePrefix))
	t = nonAlnum.ReplaceAllString(t, " ")
	return strings.TrimSpace(t)
}

// File dedupes each finding against existing Issues (via
// IssueClient.FindDuplicate) and creates a new self-audit Issue for every
// finding that is not a duplicate. Findings are processed independently —
// an error filing one finding's Issue aborts the remaining findings and
// returns the partial Summary alongside the error, so callers can see what
// was already filed.
//
// This is the single implementation shared by the run_self_audit MCP
// tool's on-demand path and (per CLAUDE.md's cadence step) the Superintendent
// cycle's throttled idle-time path — both converge on this function so the
// two invocation paths can never diverge in what "file a self-audit finding"
// means.
func File(findings []Finding, issues IssueClient) (Summary, error) {
	summary := Summary{FindingsReceived: len(findings)}
	for _, f := range findings {
		normalized := NormalizeTitle(f.Title)
		result := FindingResult{Title: f.Title}

		if normalized != "" {
			num, found, err := issues.FindDuplicate(normalized)
			if err != nil {
				summary.Results = append(summary.Results, result)
				return summary, err
			}
			if found {
				result.IsDuplicate = true
				result.DuplicateOf = num
				summary.Duplicates++
				summary.Results = append(summary.Results, result)
				continue
			}
		}

		title := fmt.Sprintf("%s %s", TitlePrefix, f.Title)
		num, err := issues.CreateFindingIssue(title, f.Body)
		if err != nil {
			summary.Results = append(summary.Results, result)
			return summary, err
		}
		result.IssueCreated = true
		result.IssueNumber = num
		summary.IssuesOpened++
		summary.Results = append(summary.Results, result)
	}
	return summary, nil
}

// Instructions is the shared prose describing the audit sweep an agent
// (Superintendent, or a human running run_self_audit on demand) should
// perform. It is the single source of truth for "what does a self-audit
// sweep actually look at", surfaced via the run_self_audit MCP tool's
// description/response so CLAUDE.md's cadence step and any on-demand caller
// see identical guidance.
const Instructions = `Perform a lightweight, proportionate review sweep of the codebase looking for three kinds of concrete problems:

1. Bugs: logic errors, unhandled error paths, race conditions, off-by-one/nil-dereference-shaped mistakes — anything you can point at a specific file/line and explain why it is wrong.
2. Missing test coverage: exported functions or behavior-critical branches (especially recently changed ones) with no corresponding test.
3. Security holes: unsanitized input reaching a shell/SQL/file-path sink, secrets committed to the repo, missing auth checks, or similar.

Scope this like a focused code-review pass (similar in spirit to the security-review skill), not an exhaustive static-analysis engine — a handful of well-substantiated findings is the expected output, not hundreds of nitpicks. Only surface something you are reasonably confident is a real, actionable problem.

Do NOT fix anything you find. For each real finding, call run_self_audit again with a "findings" array of {"title", "body"} objects — title a short summary, body the full detail (what/where/why it matters, with file references). This tool dedupes each finding against existing open and closed Issues by a normalized-title match and only files a new Issue when no duplicate exists; filing is otherwise automatic. Leave the fix itself to the normal Engineer pipeline.`
