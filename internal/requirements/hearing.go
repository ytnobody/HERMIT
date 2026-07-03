package requirements

import (
	"fmt"
	"os"
	"path/filepath"

	gh "github.com/ytnobody/hermit/internal/github"
)

// HearingLabel is the label HERMIT applies to the "requirements hearing"
// Issue it opens (see EnsureHearingIssue) when no requirements document can
// be found at any of the configured paths (see DocExists).
//
// This label serves two purposes:
//  1. Idempotency: EnsureHearingIssue checks for an already-open Issue
//     carrying this label before creating a new one, so restarting `hermit
//     serve` never creates a duplicate.
//  2. Queue exclusion: list_issues excludes Issues carrying this label from
//     the Engineer queue (see internal/mcp/tools.go), since this Issue is
//     addressed to a human, not an Engineer. Once the human answers (as a
//     comment) and removes the label, the Issue re-enters the normal queue
//     and is picked up by the ordinary Engineer flow, which synthesizes the
//     requirements document from the Issue body/comments.
const HearingLabel = "hermit-hearing"

// DefaultHearingPaths is the built-in list of candidate requirements-document
// paths checked by DocExists when harness.toml's [requirements].paths is
// empty (including when the [requirements] section is absent entirely). The
// hearing precondition check is enabled by default — it is not a no-op
// without configuration — so that a project that never touches
// [requirements] still gets the guard against missing requirements docs.
var DefaultHearingPaths = []string{"REQUIREMENTS.md", "docs/requirements.md"}

// hearingIssueTitle is the title of the Issue opened by EnsureHearingIssue.
const hearingIssueTitle = "HERMIT: プロジェクトの要件定義について教えてください"

// DocExists reports whether a requirements document exists at any of paths,
// each resolved relative to rootDir (absolute paths are used as-is). Returns
// false if paths is empty.
func DocExists(rootDir string, paths []string) bool {
	for _, p := range paths {
		full := p
		if !filepath.IsAbs(p) {
			full = filepath.Join(rootDir, p)
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// HearingIssueClient is the subset of GitHub issue operations
// EnsureHearingIssue needs. *github.Client satisfies this directly (all
// three methods already exist on it), so no adapter is required in
// production code — this interface exists purely so tests can supply an
// in-memory fake.
type HearingIssueClient interface {
	// ListOpenIssues returns open issues, optionally filtered server-side by
	// label (pass HearingLabel to check for an existing hearing Issue).
	ListOpenIssues(label string) ([]gh.Issue, error)
	// CreateIssue opens a new issue and returns its number.
	CreateIssue(title, body string) (int, error)
	// AddLabel adds a label to an issue. Adding a label an issue already has
	// is expected to be a no-op (idempotent).
	AddLabel(number int, label string) error
}

// EnsureHearingIssue opens a "requirements hearing" Issue asking about
// purpose, scope, acceptance criteria, and out-of-scope items, unless an
// open Issue labeled HearingLabel already exists. Returns whether a new
// Issue was created.
//
// This is the mechanism that makes the requirements-doc precondition check
// idempotent: it is safe to call on every `hermit serve` startup.
func EnsureHearingIssue(client HearingIssueClient) (bool, error) {
	existing, err := client.ListOpenIssues(HearingLabel)
	if err != nil {
		return false, fmt.Errorf("checking for an existing hearing issue: %w", err)
	}
	if len(existing) > 0 {
		return false, nil
	}

	num, err := client.CreateIssue(hearingIssueTitle, HearingIssueBody())
	if err != nil {
		return false, fmt.Errorf("creating hearing issue: %w", err)
	}
	if err := client.AddLabel(num, HearingLabel); err != nil {
		return false, fmt.Errorf("labeling hearing issue #%d: %w", num, err)
	}
	return true, nil
}

// HearingIssueBody renders the structured requirements-hearing Issue body.
// It asks about the four points called out in Issue #104's design: purpose,
// scope, acceptance criteria, and explicit non-goals — the same structure
// used by the readiness package's per-Issue hearing comment, applied here at
// the project level.
func HearingIssueBody() string {
	return "" +
		"## HERMIT: 要件定義書が見つかりません\n\n" +
		"このプロジェクトには要件定義書（例: `REQUIREMENTS.md`）が見つかりませんでした。" +
		"HERMIT はIssue駆動で自律的に動作しますが、要件定義書がないと個々のIssueが" +
		"プロジェクトの目的に沿っているかを判断する基準がありません。\n\n" +
		"以下の項目について、このIssueへのコメントで回答をお願いします。\n\n" +
		"1. **目的**: このプロジェクトで達成したいことは何ですか？\n" +
		"2. **スコープ**: 対象とする機能・範囲はどこまでですか？\n" +
		"3. **受け入れ条件**: 何をもって完成とみなしますか？\n" +
		"4. **やらないこと**: 明示的に対象外とする事項は何ですか？\n\n" +
		"回答後、このIssueから `" + HearingLabel + "` ラベルを外してください。" +
		"通常のIssueキューに復帰し、Engineerがコメント内容をもとに要件定義書を作成します。\n"
}
