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
