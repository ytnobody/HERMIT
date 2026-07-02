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
