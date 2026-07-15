package risk

// REQ-named test for the requirements reconcile sweep (Issue #152). See
// REQUIREMENTS.md, REQ-006.

import (
	"testing"

	gh "github.com/ytnobody/hermit/internal/github"
)

// TestREQ006_RiskLevelThresholds verifies REQ-006: evaluate_risk classifies
// PRs as LOW / MEDIUM / HIGH according to the default thresholds, including
// the boundary values (20 files / 500 lines / cmd|go.mod|.github for HIGH,
// 10 files / 200 lines / internal for MEDIUM).
func TestREQ006_RiskLevelThresholds(t *testing.T) {
	manyFiles := func(n int, name string) []gh.PRFile {
		files := make([]gh.PRFile, n)
		for i := range files {
			files[i] = gh.PRFile{Filename: name}
		}
		return files
	}

	tests := []struct {
		name      string
		files     []gh.PRFile
		additions int
		deletions int
		want      Level
	}{
		{"LOW: small change outside risky paths", []gh.PRFile{{Filename: "README.md"}}, 10, 5, Low},
		{"HIGH: 20 files (boundary)", manyFiles(20, "pkg/foo.go"), 10, 0, High},
		{"HIGH: 500 lines (boundary)", []gh.PRFile{{Filename: "pkg/foo.go"}}, 300, 200, High},
		{"HIGH: cmd/ path", []gh.PRFile{{Filename: "cmd/main.go"}}, 1, 0, High},
		{"HIGH: go.mod", []gh.PRFile{{Filename: "go.mod"}}, 1, 0, High},
		{"HIGH: .github/ path", []gh.PRFile{{Filename: ".github/workflows/ci.yml"}}, 1, 0, High},
		{"MEDIUM: 10 files (boundary)", manyFiles(10, "pkg/foo.go"), 10, 0, Medium},
		{"MEDIUM: 200 lines (boundary)", []gh.PRFile{{Filename: "pkg/foo.go"}}, 150, 50, Medium},
		{"MEDIUM: internal/ core path", []gh.PRFile{{Filename: "internal/mcp/tools.go"}}, 5, 0, Medium},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			level, reasons := Evaluate(tc.files, tc.additions, tc.deletions)
			if level != tc.want {
				t.Errorf("Evaluate() level = %v, want %v (reasons: %v)", level, tc.want, reasons)
			}
			if tc.want != Low && len(reasons) == 0 {
				t.Errorf("expected non-empty reasons for %v level", tc.want)
			}
		})
	}
}
