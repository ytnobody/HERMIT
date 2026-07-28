package requirements

import (
	"strings"
	"testing"
)

const sampleDoc = `# 要件定義書

## REQ-001: HIGH リスク PR の自動マージ禁止
- 受け入れ条件: HIGH リスクと判定された PR は merge_pr を呼ばずコメントのみ行う
- verify: test

## REQ-002: ドキュメント整備
- 受け入れ条件: README にセットアップ手順が書かれている
- verify: manual

## REQ-003: 冪等な sweep
- 受け入れ条件: 同じ入力に対して複数回 sweep しても Issue が重複作成されない

## REQ-004:
- 受け入れ条件: ID のみでタイトルが空でも良い
- verify: test
`

func TestParse_ExtractsAllRequirements(t *testing.T) {
	reqs, err := Parse(sampleDoc)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(reqs) != 4 {
		t.Fatalf("expected 4 requirements, got %d", len(reqs))
	}

	want := []struct {
		id     string
		title  string
		verify VerifyMode
	}{
		{"REQ-001", "HIGH リスク PR の自動マージ禁止", VerifyTest},
		{"REQ-002", "ドキュメント整備", VerifyManual},
		{"REQ-003", "冪等な sweep", VerifyTest},
		{"REQ-004", "", VerifyTest},
	}

	for i, w := range want {
		got := reqs[i]
		if got.ID != w.id {
			t.Errorf("reqs[%d].ID = %q, want %q", i, got.ID, w.id)
		}
		if got.Title != w.title {
			t.Errorf("reqs[%d].Title = %q, want %q", i, got.Title, w.title)
		}
		if got.Verify != w.verify {
			t.Errorf("reqs[%d].Verify = %q, want %q", i, got.Verify, w.verify)
		}
		if got.Hash == "" {
			t.Errorf("reqs[%d].Hash is empty", i)
		}
	}
}

func TestParse_VerifyDefaultsToTest(t *testing.T) {
	reqs, err := Parse(sampleDoc)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	// REQ-003 has no "- verify:" field at all.
	for _, r := range reqs {
		if r.ID == "REQ-003" && r.Verify != VerifyTest {
			t.Errorf("REQ-003 verify = %q, want %q (default)", r.Verify, VerifyTest)
		}
	}
}

func TestParse_AcceptanceCriteriaExtracted(t *testing.T) {
	reqs, err := Parse(sampleDoc)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !strings.Contains(reqs[0].AcceptanceCriteria, "merge_pr") {
		t.Errorf("REQ-001 acceptance criteria = %q, expected to contain %q", reqs[0].AcceptanceCriteria, "merge_pr")
	}
}

func TestParse_NoRequirements(t *testing.T) {
	reqs, err := Parse("# just a title\n\nsome prose, no REQ headers")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 requirements, got %d", len(reqs))
	}
}

func TestParse_HashStableAcrossIdenticalInput(t *testing.T) {
	reqs1, _ := Parse(sampleDoc)
	reqs2, _ := Parse(sampleDoc)
	if reqs1[0].Hash != reqs2[0].Hash {
		t.Errorf("hash should be stable for identical input: %q != %q", reqs1[0].Hash, reqs2[0].Hash)
	}
}

func TestParse_HashChangesWithText(t *testing.T) {
	reqs1, _ := Parse(sampleDoc)
	changedDoc := strings.Replace(sampleDoc, "merge_pr を呼ばずコメントのみ行う", "merge_pr を呼ばずコメントし、Issue も作成する", 1)
	reqs2, _ := Parse(changedDoc)
	if reqs1[0].Hash == reqs2[0].Hash {
		t.Errorf("hash should change when requirement text changes")
	}
	// Unrelated requirements must keep the same hash.
	if reqs1[1].Hash != reqs2[1].Hash {
		t.Errorf("unrelated requirement's hash should not change")
	}
}

// TestParse_HashUnaffectedByImplementationStatusField is the regression test
// for Issue #182: hashing the whole requirement block (including a
// "- 実装状況:" progress-note field) meant that *resolving* a review-test
// issue — which involves writing findings into 実装状況 — changed the
// block's hash, which made the next sweep think the requirement text had
// changed again, re-opening review-test forever. The hash must depend only
// on 受け入れ条件 and verify, not on 実装状況.
func TestParse_HashUnaffectedByImplementationStatusField(t *testing.T) {
	before := `# 要件定義書

## REQ-200: 自己増殖しない要件
- 受け入れ条件: review-test が自己増殖しないこと
- verify: test
`
	reqsBefore, err := Parse(before)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Simulate resolving a review-test issue: an engineer/human appends an
	// "- 実装状況:" progress note to the block, without touching the
	// acceptance criteria or verify mode at all.
	after := `# 要件定義書

## REQ-200: 自己増殖しない要件
- 受け入れ条件: review-test が自己増殖しないこと
- verify: test
- 実装状況: #182 で調査済み。テストは要件を正しく検証している。
`
	reqsAfter, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if reqsBefore[0].Hash != reqsAfter[0].Hash {
		t.Errorf("hash changed after only 実装状況 was added: before=%q after=%q — this is the #182 self-reinforcing loop",
			reqsBefore[0].Hash, reqsAfter[0].Hash)
	}
}

// TestParse_HashUnaffectedByTitleOrDescriptionOnly covers the acceptance
// criterion that editing only the requirement heading text or free-form
// description prose (neither 受け入れ条件 nor verify) must not change Hash.
func TestParse_HashUnaffectedByTitleOrDescriptionOnly(t *testing.T) {
	before := `## REQ-201: 元のタイトル
補足説明の文章です。

- 受け入れ条件: 変わらない条件
- verify: test
`
	after := `## REQ-201: 書き直したタイトル
補足説明の文章を書き直しました。もっと詳しく説明します。

- 受け入れ条件: 変わらない条件
- verify: test
`
	reqsBefore, err := Parse(before)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	reqsAfter, err := Parse(after)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if reqsBefore[0].Hash != reqsAfter[0].Hash {
		t.Errorf("hash changed after only title/description prose changed: before=%q after=%q",
			reqsBefore[0].Hash, reqsAfter[0].Hash)
	}
}

// TestParse_HashChangesWithVerifyMode covers the acceptance criterion that
// switching verify: test <-> manual must still change Hash, since that's a
// genuine change to how the requirement is judged.
func TestParse_HashChangesWithVerifyMode(t *testing.T) {
	testDoc := `## REQ-202: verify 切り替え
- 受け入れ条件: 同じ条件文
- verify: test
`
	manualDoc := `## REQ-202: verify 切り替え
- 受け入れ条件: 同じ条件文
- verify: manual
`
	reqsTest, err := Parse(testDoc)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	reqsManual, err := Parse(manualDoc)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if reqsTest[0].Hash == reqsManual[0].Hash {
		t.Errorf("hash should change when verify mode changes between test and manual")
	}
}

func TestParse_CRLFNormalized(t *testing.T) {
	crlfDoc := strings.ReplaceAll(sampleDoc, "\n", "\r\n")
	reqsLF, _ := Parse(sampleDoc)
	reqsCRLF, _ := Parse(crlfDoc)
	if len(reqsLF) != len(reqsCRLF) {
		t.Fatalf("CRLF doc parsed to different requirement count: %d vs %d", len(reqsCRLF), len(reqsLF))
	}
	if reqsLF[0].Hash != reqsCRLF[0].Hash {
		t.Errorf("hash should be identical regardless of line-ending style")
	}
}
