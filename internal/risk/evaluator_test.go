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

// TestEvaluateWithConfig_DefaultConfigMatchesEvaluate verifies that calling
// EvaluateWithConfig with DefaultConfig() behaves identically to the
// zero-config Evaluate entry point, i.e. the hardcoded defaults are
// preserved for backward compatibility.
func TestEvaluateWithConfig_DefaultConfigMatchesEvaluate(t *testing.T) {
	files := []gh.PRFile{{Filename: "internal/service/svc.go"}}
	wantLevel, wantReasons := Evaluate(files, 30, 10)
	gotLevel, gotReasons := EvaluateWithConfig(files, 30, 10, DefaultConfig())
	if gotLevel != wantLevel {
		t.Errorf("EvaluateWithConfig() level = %v, want %v", gotLevel, wantLevel)
	}
	if len(gotReasons) != len(wantReasons) {
		t.Errorf("EvaluateWithConfig() reasons = %v, want %v", gotReasons, wantReasons)
	}
}

// TestEvaluateWithConfig_CustomThresholds verifies that a custom Config
// (e.g. sourced from harness.toml's [risk] section) is actually applied
// instead of the hardcoded defaults.
func TestEvaluateWithConfig_CustomThresholds(t *testing.T) {
	cfg := Config{
		HighPaths:           []string{"server/"},
		MediumPaths:         []string{"lib/"},
		HighFileThreshold:   3,
		HighLineThreshold:   50,
		MediumFileThreshold: 2,
		MediumLineThreshold: 20,
	}

	t.Run("LOW under custom thresholds", func(t *testing.T) {
		files := []gh.PRFile{{Filename: "app/main.py"}}
		level, _ := EvaluateWithConfig(files, 5, 5, cfg)
		if level != Low {
			t.Errorf("got %v, want LOW", level)
		}
	})

	t.Run("HIGH: custom high path, would not match default highPaths", func(t *testing.T) {
		files := []gh.PRFile{{Filename: "server/main.py"}}
		level, reasons := EvaluateWithConfig(files, 1, 0, cfg)
		if level != High {
			t.Errorf("got %v, want HIGH", level)
		}
		if len(reasons) == 0 {
			t.Errorf("expected reasons to be populated")
		}
	})

	t.Run("HIGH: custom low file threshold", func(t *testing.T) {
		files := []gh.PRFile{{Filename: "a.py"}, {Filename: "b.py"}, {Filename: "c.py"}}
		level, _ := EvaluateWithConfig(files, 1, 1, cfg)
		if level != High {
			t.Errorf("got %v, want HIGH", level)
		}
	})

	t.Run("MEDIUM: custom medium path", func(t *testing.T) {
		files := []gh.PRFile{{Filename: "lib/util.py"}}
		level, _ := EvaluateWithConfig(files, 1, 0, cfg)
		if level != Medium {
			t.Errorf("got %v, want MEDIUM", level)
		}
	})

	t.Run("MEDIUM: custom line threshold", func(t *testing.T) {
		files := []gh.PRFile{{Filename: "app/main.py"}}
		level, _ := EvaluateWithConfig(files, 15, 10, cfg)
		if level != Medium {
			t.Errorf("got %v, want MEDIUM", level)
		}
	})

	t.Run("a path that is only high-risk under the default policy is not HIGH under a custom policy", func(t *testing.T) {
		// "cmd/" is a default highPaths entry but is absent from the custom
		// cfg.HighPaths, and the file/line counts stay under every custom
		// threshold, so this must resolve to LOW rather than HIGH.
		files := []gh.PRFile{{Filename: "cmd/main.go"}}
		level, _ := EvaluateWithConfig(files, 1, 0, cfg)
		if level != Low {
			t.Errorf("got %v, want LOW", level)
		}
	})
}

func TestDefaultConfig_MatchesLegacyHardcodedValues(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HighFileThreshold != 20 || cfg.HighLineThreshold != 500 {
		t.Errorf("unexpected high thresholds: %+v", cfg)
	}
	if cfg.MediumFileThreshold != 10 || cfg.MediumLineThreshold != 200 {
		t.Errorf("unexpected medium thresholds: %+v", cfg)
	}
	wantHighPaths := []string{"cmd/", "go.mod", ".github/"}
	if len(cfg.HighPaths) != len(wantHighPaths) {
		t.Fatalf("unexpected high paths: %+v", cfg.HighPaths)
	}
	for i, p := range wantHighPaths {
		if cfg.HighPaths[i] != p {
			t.Errorf("HighPaths[%d] = %q, want %q", i, cfg.HighPaths[i], p)
		}
	}
	wantMediumPaths := []string{"internal/"}
	if len(cfg.MediumPaths) != len(wantMediumPaths) || cfg.MediumPaths[0] != wantMediumPaths[0] {
		t.Errorf("unexpected medium paths: %+v", cfg.MediumPaths)
	}
}

func TestMerge(t *testing.T) {
	base := DefaultConfig()

	t.Run("empty override leaves base untouched", func(t *testing.T) {
		got := Merge(base, Config{})
		if got.HighFileThreshold != base.HighFileThreshold || got.MediumFileThreshold != base.MediumFileThreshold {
			t.Errorf("Merge() = %+v, want unchanged base %+v", got, base)
		}
		if len(got.HighPaths) != len(base.HighPaths) || len(got.MediumPaths) != len(base.MediumPaths) {
			t.Errorf("Merge() paths = %+v, want unchanged base paths %+v", got, base)
		}
	})

	t.Run("partial override only replaces set fields", func(t *testing.T) {
		override := Config{HighFileThreshold: 5}
		got := Merge(base, override)
		if got.HighFileThreshold != 5 {
			t.Errorf("HighFileThreshold = %d, want 5", got.HighFileThreshold)
		}
		if got.HighLineThreshold != base.HighLineThreshold {
			t.Errorf("HighLineThreshold = %d, want unchanged %d", got.HighLineThreshold, base.HighLineThreshold)
		}
		if got.MediumFileThreshold != base.MediumFileThreshold {
			t.Errorf("MediumFileThreshold = %d, want unchanged %d", got.MediumFileThreshold, base.MediumFileThreshold)
		}
		if len(got.HighPaths) != len(base.HighPaths) {
			t.Errorf("HighPaths = %+v, want unchanged %+v", got.HighPaths, base.HighPaths)
		}
	})

	t.Run("full override replaces every field", func(t *testing.T) {
		override := Config{
			HighPaths:           []string{"a/"},
			MediumPaths:         []string{"b/"},
			HighFileThreshold:   1,
			HighLineThreshold:   2,
			MediumFileThreshold: 3,
			MediumLineThreshold: 4,
		}
		got := Merge(base, override)
		if got.HighFileThreshold != 1 || got.HighLineThreshold != 2 || got.MediumFileThreshold != 3 || got.MediumLineThreshold != 4 {
			t.Errorf("Merge() = %+v, want %+v", got, override)
		}
		if len(got.HighPaths) != 1 || got.HighPaths[0] != "a/" {
			t.Errorf("HighPaths = %+v, want [a/]", got.HighPaths)
		}
		if len(got.MediumPaths) != 1 || got.MediumPaths[0] != "b/" {
			t.Errorf("MediumPaths = %+v, want [b/]", got.MediumPaths)
		}
	})

	t.Run("layering two merges (repo override over already-merged global config)", func(t *testing.T) {
		globalOverride := Config{HighFileThreshold: 8}
		globalResolved := Merge(base, globalOverride)
		repoOverride := Config{MediumFileThreshold: 1}
		repoResolved := Merge(globalResolved, repoOverride)

		if repoResolved.HighFileThreshold != 8 {
			t.Errorf("expected global override to persist through repo-level merge, got %d", repoResolved.HighFileThreshold)
		}
		if repoResolved.MediumFileThreshold != 1 {
			t.Errorf("expected repo override to apply, got %d", repoResolved.MediumFileThreshold)
		}
		if repoResolved.HighLineThreshold != base.HighLineThreshold {
			t.Errorf("expected untouched fields to fall back to base default, got %d", repoResolved.HighLineThreshold)
		}
	})
}
