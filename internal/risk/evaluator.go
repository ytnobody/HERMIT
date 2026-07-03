package risk

import (
	"fmt"
	"strings"

	gh "github.com/ytnobody/hermit/internal/github"
)

type Level string

const (
	Low    Level = "LOW"
	Medium Level = "MEDIUM"
	High   Level = "HIGH"
)

// Config holds the tunable thresholds and path lists used by Evaluate to
// classify a PR's risk level. It is intentionally decoupled from any
// particular configuration file format so it can be constructed directly in
// tests or built from harness.toml (see cmd/hermit for the TOML mapping).
type Config struct {
	HighPaths           []string `json:"high_paths"`
	MediumPaths         []string `json:"medium_paths"`
	HighFileThreshold   int      `json:"high_file_threshold"`
	HighLineThreshold   int      `json:"high_line_threshold"`
	MediumFileThreshold int      `json:"medium_file_threshold"`
	MediumLineThreshold int      `json:"medium_line_threshold"`
}

// DefaultConfig returns the built-in risk policy that HERMIT has always
// used. It is the fallback applied whenever a value is left unspecified in
// harness.toml, preserving backward compatibility for existing projects.
func DefaultConfig() Config {
	return Config{
		HighPaths:           []string{"cmd/", "go.mod", ".github/"},
		MediumPaths:         []string{"internal/"},
		HighFileThreshold:   20,
		HighLineThreshold:   500,
		MediumFileThreshold: 10,
		MediumLineThreshold: 200,
	}
}

// Merge returns a copy of base with any non-zero-value field from override
// applied on top. It is used to layer harness.toml's [risk] section over the
// built-in defaults, and again to layer a per-repo [repos.risk] override over
// the resulting effective config. Fields left unset (empty slice or zero
// int) in override are left untouched, i.e. they fall back to base.
func Merge(base, override Config) Config {
	result := base
	if len(override.HighPaths) > 0 {
		result.HighPaths = override.HighPaths
	}
	if len(override.MediumPaths) > 0 {
		result.MediumPaths = override.MediumPaths
	}
	if override.HighFileThreshold > 0 {
		result.HighFileThreshold = override.HighFileThreshold
	}
	if override.HighLineThreshold > 0 {
		result.HighLineThreshold = override.HighLineThreshold
	}
	if override.MediumFileThreshold > 0 {
		result.MediumFileThreshold = override.MediumFileThreshold
	}
	if override.MediumLineThreshold > 0 {
		result.MediumLineThreshold = override.MediumLineThreshold
	}
	return result
}

// Evaluate classifies a PR's risk level using the built-in default policy.
// It is kept as a stable, zero-config entry point for existing callers; use
// EvaluateWithConfig to apply a policy sourced from harness.toml.
func Evaluate(files []gh.PRFile, additions, deletions int) (Level, []string) {
	return EvaluateWithConfig(files, additions, deletions, DefaultConfig())
}

// EvaluateWithConfig classifies a PR's risk level using the supplied Config
// instead of the built-in defaults, allowing per-project or per-repo risk
// policies.
func EvaluateWithConfig(files []gh.PRFile, additions, deletions int, cfg Config) (Level, []string) {
	total := additions + deletions
	var reasons []string

	if cfg.HighFileThreshold > 0 && len(files) >= cfg.HighFileThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more files changed", cfg.HighFileThreshold))
	}
	if cfg.HighLineThreshold > 0 && total >= cfg.HighLineThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more lines changed", cfg.HighLineThreshold))
	}
	for _, f := range files {
		for _, p := range cfg.HighPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" is in a high-risk path")
			}
		}
	}
	if len(reasons) > 0 {
		return High, reasons
	}

	if cfg.MediumFileThreshold > 0 && len(files) >= cfg.MediumFileThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more files changed", cfg.MediumFileThreshold))
	}
	if cfg.MediumLineThreshold > 0 && total >= cfg.MediumLineThreshold {
		reasons = append(reasons, fmt.Sprintf("%d or more lines changed", cfg.MediumLineThreshold))
	}
	for _, f := range files {
		for _, p := range cfg.MediumPaths {
			if strings.HasPrefix(f.Filename, p) || f.Filename == p {
				reasons = append(reasons, f.Filename+" has changes in a medium-risk path")
				break
			}
		}
	}
	if len(reasons) > 0 {
		return Medium, reasons
	}

	return Low, nil
}
