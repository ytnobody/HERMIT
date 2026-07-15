package readiness

import (
	"strings"
	"testing"
)

func TestEvaluate_emptyBody(t *testing.T) {
	cfg := DefaultConfig()
	got := Evaluate("", cfg)
	if got.Ready {
		t.Fatalf("expected not ready for empty body, got ready")
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("expected at least one reason")
	}
}

func TestEvaluate_tooShort(t *testing.T) {
	cfg := Config{MinBodyLength: 40, RequireAcceptanceCriteria: false, Label: DefaultLabel}
	got := Evaluate("short body", cfg)
	if got.Ready {
		t.Fatalf("expected not ready for short body, got ready")
	}
}

func TestEvaluate_missingAcceptanceCriteria(t *testing.T) {
	cfg := Config{MinBodyLength: 10, RequireAcceptanceCriteria: true, Label: DefaultLabel}
	body := "This is a sufficiently long body describing some work to do, but with no AC section."
	got := Evaluate(body, cfg)
	if got.Ready {
		t.Fatalf("expected not ready when acceptance criteria section missing")
	}
}

func TestEvaluate_readyWithEnglishAcceptanceCriteria(t *testing.T) {
	cfg := DefaultConfig()
	body := `## Background
This issue describes a well-specified change with enough detail to start work.

## Acceptance Criteria
- Thing one works
- Thing two works
`
	got := Evaluate(body, cfg)
	if !got.Ready {
		t.Fatalf("expected ready, got not ready with reasons: %v", got.Reasons)
	}
	if len(got.Reasons) != 0 {
		t.Fatalf("expected no reasons, got %v", got.Reasons)
	}
}

func TestEvaluate_readyWithJapaneseAcceptanceCriteria(t *testing.T) {
	cfg := DefaultConfig()
	body := `## 背景
このIssueは十分に長い説明を持ち、実装に着手できる情報が揃っています。

## 受け入れ条件
- 条件1を満たすこと
- 条件2を満たすこと
`
	got := Evaluate(body, cfg)
	if !got.Ready {
		t.Fatalf("expected ready, got not ready with reasons: %v", got.Reasons)
	}
}

func TestEvaluate_acceptanceCriteriaCheckDisabled(t *testing.T) {
	cfg := Config{MinBodyLength: 10, RequireAcceptanceCriteria: false, Label: DefaultLabel}
	body := "This is a long enough body with no acceptance criteria section at all, just prose."
	got := Evaluate(body, cfg)
	if !got.Ready {
		t.Fatalf("expected ready when acceptance-criteria check disabled, got reasons: %v", got.Reasons)
	}
}

func TestEvaluate_configurableThreshold(t *testing.T) {
	body := "0123456789" // 10 chars

	lenient := Config{MinBodyLength: 5, RequireAcceptanceCriteria: false, Label: DefaultLabel}
	if got := Evaluate(body, lenient); !got.Ready {
		t.Fatalf("expected ready under lenient (min=5) threshold, got reasons: %v", got.Reasons)
	}

	strict := Config{MinBodyLength: 100, RequireAcceptanceCriteria: false, Label: DefaultLabel}
	if got := Evaluate(body, strict); got.Ready {
		t.Fatalf("expected not ready under strict (min=100) threshold")
	}
}

func TestEvaluateWithComments(t *testing.T) {
	cfg := DefaultConfig()
	thinBody := "fix it"
	hearing := HearingComment([]string{"Issue body is too thin"})
	answer := `1. 目的: バグを直す
2. スコープ: internal/readiness のみ
3. 受け入れ条件: ラベル手動除去後に needs-clarification が再付与されないこと
4. やらないこと: readiness基準そのものの変更`

	tests := []struct {
		name      string
		body      string
		comments  []string
		wantReady bool
	}{
		{
			name:      "answers after hearing comment make the issue ready (Issue #149)",
			body:      thinBody,
			comments:  []string{hearing, answer},
			wantReady: true,
		},
		{
			name:      "hearing with no follow-up comments stays not ready",
			body:      thinBody,
			comments:  []string{hearing},
			wantReady: false,
		},
		{
			name:      "insufficient follow-up comment stays not ready",
			body:      thinBody,
			comments:  []string{hearing, "後で書きます"},
			wantReady: false,
		},
		{
			name:      "comments before any hearing are ignored",
			body:      thinBody,
			comments:  []string{answer},
			wantReady: false,
		},
		{
			name:      "only comments after the last hearing count",
			body:      thinBody,
			comments:  []string{hearing, answer, hearing},
			wantReady: false,
		},
		{
			name:      "reposted hearing comment is never counted as an answer",
			body:      thinBody,
			comments:  []string{hearing, hearing},
			wantReady: false,
		},
		{
			name: "ready body stays ready regardless of comments",
			body: `## 背景
このIssueは十分に長い説明を持ち、実装に着手できる情報が揃っています。

## 受け入れ条件
- 条件1を満たすこと`,
			comments:  nil,
			wantReady: true,
		},
		{
			name:      "no comments behaves like Evaluate",
			body:      thinBody,
			comments:  nil,
			wantReady: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateWithComments(tt.body, tt.comments, cfg)
			if got.Ready != tt.wantReady {
				t.Fatalf("EvaluateWithComments() ready = %v, want %v (reasons: %v)", got.Ready, tt.wantReady, got.Reasons)
			}
		})
	}
}

func TestHasHearingComment(t *testing.T) {
	hearing := HearingComment(nil)
	if !HasHearingComment([]string{"unrelated", hearing}) {
		t.Fatalf("expected hearing comment to be detected")
	}
	if HasHearingComment([]string{"unrelated", "also unrelated"}) {
		t.Fatalf("expected no hearing comment detected")
	}
	if HasHearingComment(nil) {
		t.Fatalf("expected no hearing comment detected for empty comments")
	}
}

func TestHasLabel(t *testing.T) {
	labels := []string{"bug", "Needs-Clarification"}
	if !HasLabel(labels, "needs-clarification") {
		t.Fatalf("expected case-insensitive match")
	}
	if HasLabel(labels, "enhancement") {
		t.Fatalf("expected no match for absent label")
	}
}

func TestHearingComment_containsMarkerAndFourQuestions(t *testing.T) {
	c := HearingComment([]string{"Issue body is empty"})
	if !strings.Contains(c, HearingMarker) {
		t.Fatalf("expected hearing comment to contain marker")
	}
	for _, want := range []string{"目的", "スコープ", "受け入れ条件", "やらないこと"} {
		if !strings.Contains(c, want) {
			t.Fatalf("expected hearing comment to mention %q", want)
		}
	}
}
