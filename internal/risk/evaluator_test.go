package risk

import (
	"testing"

	gh "github.com/ytnobody/hermit/internal/github"
)

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name      string
		files     []gh.PRFile
		additions int
		deletions int
		wantLevel Level
	}{
		{
			name:      "LOW: small change",
			files:     []gh.PRFile{{Filename: "README.md"}},
			additions: 10, deletions: 5,
			wantLevel: Low,
		},
		{
			name: "HIGH: too many files",
			files: func() []gh.PRFile {
				f := make([]gh.PRFile, 20)
				for i := range f {
					f[i] = gh.PRFile{Filename: "pkg/foo.go"}
				}
				return f
			}(),
			additions: 100, deletions: 50,
			wantLevel: High,
		},
		{
			name:      "HIGH: large line diff",
			files:     []gh.PRFile{{Filename: "pkg/foo.go"}},
			additions: 400, deletions: 200,
			wantLevel: High,
		},
		{
			name:      "HIGH: cmd/ path",
			files:     []gh.PRFile{{Filename: "cmd/main.go"}},
			additions: 1, deletions: 0,
			wantLevel: High,
		},
		{
			name:      "HIGH: go.mod",
			files:     []gh.PRFile{{Filename: "go.mod"}},
			additions: 1, deletions: 0,
			wantLevel: High,
		},
		{
			name:      "HIGH: .github/ path",
			files:     []gh.PRFile{{Filename: ".github/workflows/ci.yml"}},
			additions: 5, deletions: 0,
			wantLevel: High,
		},
		{
			name: "MEDIUM: 10+ files",
			files: func() []gh.PRFile {
				f := make([]gh.PRFile, 10)
				for i := range f {
					f[i] = gh.PRFile{Filename: "pkg/foo.go"}
				}
				return f
			}(),
			additions: 50, deletions: 20,
			wantLevel: Medium,
		},
		{
			name:      "MEDIUM: internal/ change",
			files:     []gh.PRFile{{Filename: "internal/service/svc.go"}},
			additions: 30, deletions: 10,
			wantLevel: Medium,
		},
		{
			name:      "MEDIUM: 200+ lines",
			files:     []gh.PRFile{{Filename: "pkg/foo.go"}},
			additions: 150, deletions: 60,
			wantLevel: Medium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := Evaluate(tt.files, tt.additions, tt.deletions)
			if got != tt.wantLevel {
				t.Errorf("Evaluate() = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}
