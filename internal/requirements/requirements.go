// Package requirements implements the requirements-doc reconcile sweep
// described in HERMIT Issue #106: the requirements document is treated as
// the declared "desired state" and HERMIT acts as a reconciler against it.
//
// Satisfaction of a requirement is judged solely by whether a corresponding
// test exists and passes — never by a side-table recording that a PR was
// merged in the past (a merged-PR record can silently go stale if a later
// regression breaks the requirement again).
package requirements

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// VerifyMode controls whether a requirement participates in the reconcile
// sweep at all.
type VerifyMode string

const (
	// VerifyTest is the default: satisfaction is judged by running the
	// requirement's corresponding test.
	VerifyTest VerifyMode = "test"
	// VerifyManual marks a requirement that cannot be verified by an
	// automated test (e.g. documentation upkeep). Sweep skips it entirely.
	VerifyManual VerifyMode = "manual"
)

// Requirement is a single parsed "## REQ-xxx: ..." block from a requirements
// document.
type Requirement struct {
	// ID is the stable requirement identifier, e.g. "REQ-001".
	ID string
	// Title is the text following the ID on the header line.
	Title string
	// AcceptanceCriteria is the raw text of the "- 受け入れ条件:" field (may
	// span multiple lines/bullets).
	AcceptanceCriteria string
	// Verify is "test" (default) or "manual".
	Verify VerifyMode
	// Body is the full raw text of this requirement's block (header plus
	// fields). It is kept for diagnostics/issue bodies, but is deliberately
	// NOT used to compute Hash (see Hash's doc comment).
	Body string
	// Hash is a stable content hash of only the spec-bearing fields
	// (AcceptanceCriteria and Verify), used to detect requirement-*spec*
	// changes across sweeps (e.g. to trigger a "review the test" issue).
	//
	// It deliberately excludes the rest of Body — most notably a
	// "- 実装状況:" (implementation status) progress-note field, which the
	// review-test workflow itself writes into when a human/engineer resolves
	// a review-test issue. Hashing the whole block created a self-sustaining
	// loop (Issue #182): resolving a review-test issue edited the
	// 実装状況 line, which changed Body's hash, which made the next sweep
	// think the requirement text had changed again, which re-opened
	// review-test forever — even though the actual spec (acceptance
	// criteria / verify mode) never changed. Also excludes the header
	// title and any free-form description text, so wording-only edits to
	// those don't spuriously trigger review-test either.
	Hash string
}

// reqHeaderRe matches a requirement header line, e.g.:
//
//	## REQ-001: HIGH risk PR auto-merge is forbidden
//
// Note: only horizontal whitespace ([ \t]) is used around ":" below, never
// the "\s" class — "\s" also matches "\n", which would let the match cross
// a line boundary and swallow the start of the following line into the
// captured value.
var reqHeaderRe = regexp.MustCompile(`(?m)^##[ \t]*(REQ-[A-Za-z0-9_-]+)[ \t]*:[ \t]*(.*)$`)

// acceptanceCriteriaRe matches a "- 受け入れ条件:" (or "- Acceptance Criteria:")
// field. The value continues until the next top-level "- " field or the end
// of the block.
var acceptanceCriteriaRe = regexp.MustCompile(`(?m)^-[ \t]*(?:受け入れ条件|[Aa]cceptance [Cc]riteria)[ \t]*:[ \t]*(.*)$`)

// verifyRe matches a "- verify: test|manual" field.
var verifyRe = regexp.MustCompile(`(?m)^-[ \t]*verify[ \t]*:[ \t]*(\S+)`)

// Parse extracts all "## REQ-xxx: ..." blocks from a requirements document.
// Requirements are returned in document order. Duplicate REQ-IDs are all
// returned (callers may want to treat that as a doc-authoring error, but
// Parse itself does not enforce uniqueness).
func Parse(doc string) ([]Requirement, error) {
	// Normalize line endings so hashing is stable across platforms.
	doc = strings.ReplaceAll(doc, "\r\n", "\n")

	matches := reqHeaderRe.FindAllStringSubmatchIndex(doc, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	var reqs []Requirement
	for i, m := range matches {
		start := m[0]
		end := len(doc)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		block := strings.TrimRight(doc[start:end], "\n")

		id := doc[m[2]:m[3]]
		title := strings.TrimSpace(doc[m[4]:m[5]])

		verify := VerifyTest
		if vm := verifyRe.FindStringSubmatch(block); vm != nil {
			switch strings.ToLower(strings.TrimSpace(vm[1])) {
			case "manual":
				verify = VerifyManual
			case "test", "":
				verify = VerifyTest
			default:
				// Unknown value: fall back to the safe default (test).
				verify = VerifyTest
			}
		}

		criteria := ""
		if cm := acceptanceCriteriaRe.FindStringSubmatch(block); cm != nil {
			criteria = strings.TrimSpace(cm[1])
		}

		reqs = append(reqs, Requirement{
			ID:                 id,
			Title:              title,
			AcceptanceCriteria: criteria,
			Verify:             verify,
			Body:               block,
		})
		reqs[len(reqs)-1].Hash = specHash(reqs[len(reqs)-1])
	}
	return reqs, nil
}

// specHash returns a stable hex-encoded sha256 hash of just the spec-bearing
// fields of req (AcceptanceCriteria and Verify) — see Requirement.Hash for
// why the rest of the block (title, description, 実装状況 progress notes,
// etc.) must NOT be included.
func specHash(req Requirement) string {
	s := "verify:" + string(req.Verify) + "\n" + "criteria:" + req.AcceptanceCriteria
	sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])
}
