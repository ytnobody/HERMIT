// Package readiness implements a deterministic (non-LLM) heuristic for
// deciding whether a GitHub Issue contains enough information for an
// Engineer to safely start implementation, instead of relying on the LLM
// to notice — and consistently act on — thin or ambiguous Issue bodies.
//
// The scoring concept already existed as lessons.HasClarification (a -20
// point deduction applied after the fact, once a PR has already been
// created). This package pushes the same judgment earlier in the
// pipeline: before an Issue is ever handed to an Engineer.
package readiness

import (
	"fmt"
	"regexp"
	"strings"
)

// HearingMarker is embedded (as an HTML comment, invisible when rendered)
// in every hearing comment HERMIT posts. It is used to detect whether a
// hearing comment has already been posted on an Issue, so that repeated
// list_issues calls do not repost it (idempotency).
const HearingMarker = "<!-- hermit:readiness-hearing -->"

// DefaultLabel is the label applied to Issues judged not ready for
// implementation. list_issues excludes Issues carrying this label from the
// queue until a human removes it (or posts the configured trigger comment).
const DefaultLabel = "needs-clarification"

// DefaultMinBodyLength is the minimum number of non-whitespace characters
// an Issue body must contain to be considered ready, when not overridden by
// harness.toml.
const DefaultMinBodyLength = 40

// acceptanceCriteriaRe matches common headings/labels used to mark an
// acceptance-criteria section, in both English and Japanese.
var acceptanceCriteriaRe = regexp.MustCompile(`(?i)(acceptance criteria|受け?入れ条件)`)

// Config holds the configurable thresholds used by Evaluate. Values are
// sourced from the [readiness] section of harness.toml; see
// cmd/hermit/main.go's Config.Readiness for the TOML mapping and defaults.
type Config struct {
	// MinBodyLength is the minimum number of non-whitespace characters
	// required in the Issue body.
	MinBodyLength int
	// RequireAcceptanceCriteria, when true, requires the Issue body to
	// contain an acceptance-criteria-like section (English or Japanese).
	RequireAcceptanceCriteria bool
	// Label is the GitHub label applied to Issues judged not ready, and
	// used to exclude them from list_issues.
	Label string
}

// DefaultConfig returns the built-in default thresholds, used when
// harness.toml has no [readiness] section (or leaves fields at zero value).
func DefaultConfig() Config {
	return Config{
		MinBodyLength:             DefaultMinBodyLength,
		RequireAcceptanceCriteria: true,
		Label:                     DefaultLabel,
	}
}

// Result is the outcome of evaluating a single Issue.
type Result struct {
	Ready   bool
	Reasons []string
}

// Evaluate deterministically judges whether an Issue body contains enough
// information to start implementation. It never calls out to an LLM: the
// same input always produces the same output.
func Evaluate(body string, cfg Config) Result {
	trimmed := strings.TrimSpace(body)

	var reasons []string
	switch {
	case trimmed == "":
		reasons = append(reasons, "Issue body is empty")
	case len(trimmed) < cfg.MinBodyLength:
		reasons = append(reasons, fmt.Sprintf("Issue body is shorter than %d characters (%d)", cfg.MinBodyLength, len(trimmed)))
	}

	if cfg.RequireAcceptanceCriteria && !acceptanceCriteriaRe.MatchString(body) {
		reasons = append(reasons, "No acceptance-criteria section found (e.g. \"Acceptance Criteria\" / \"受け入れ条件\")")
	}

	return Result{
		Ready:   len(reasons) == 0,
		Reasons: reasons,
	}
}

// HasLabel reports whether labels contains the given label name
// (case-insensitive).
func HasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, label) {
			return true
		}
	}
	return false
}

// HearingComment renders the structured clarification-request comment
// posted on Issues judged not ready. It asks for the four points called
// out in the readiness heuristic's design: purpose, scope, acceptance
// criteria, and explicit non-goals — plus the specific reasons the
// heuristic flagged this Issue.
func HearingComment(reasons []string) string {
	var sb strings.Builder
	sb.WriteString(HearingMarker)
	sb.WriteString("\n## HERMIT: 実装に着手する前の確認事項\n\n")
	sb.WriteString("このIssueは現状の記述だけでは実装に着手するには情報が不足していると判断しました。推測での実装を避けるため、以下の4点についてコメントで回答をお願いします。\n\n")
	if len(reasons) > 0 {
		sb.WriteString("検出した不足点:\n")
		for _, r := range reasons {
			fmt.Fprintf(&sb, "- %s\n", r)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("1. **目的**: このIssueで何を達成したいですか？\n")
	sb.WriteString("2. **スコープ**: 変更してよい範囲（対象ファイル/機能）はどこまでですか？\n")
	sb.WriteString("3. **受け入れ条件**: 完了したとみなせる具体的な条件は何ですか？\n")
	sb.WriteString("4. **やらないこと**: 今回のIssueで対応しない（対応してはいけない）ことは何ですか？\n\n")
	sb.WriteString("回答後、このIssueから `needs-clarification` ラベルを外していただくか、トリガーコメントを投稿していただくことで、通常のキューに復帰します。\n")
	return sb.String()
}
